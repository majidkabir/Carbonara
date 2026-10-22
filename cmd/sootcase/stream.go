package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/majidkabir/sootcase/pkg/batch"
	"github.com/majidkabir/sootcase/pkg/calc"
	"github.com/majidkabir/sootcase/pkg/csvio"
	"github.com/majidkabir/sootcase/pkg/ndjson"
)

// runStream reads one shipment JSON per line and writes one result per line,
// in input order, for use in pipes (e.g. between Kafka consumer and producer).
func runStream(args []string) error {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)
	var c common
	c.register(fs)
	workers := fs.Int("workers", 0, "parallel calculations (default: one per CPU)")
	maxLine := fs.Int("max-line-bytes", ndjson.DefaultMaxLine, "longest input line accepted")
	flushEvery := fs.Duration("flush", 100*time.Millisecond, "how often output is flushed; 0 flushes after every line")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		return errors.New("stream: expected at most one input file (default stdin)")
	}
	calculator, _, err := c.calculator()
	if err != nil {
		return err
	}
	var in io.Reader = os.Stdin
	if fs.NArg() == 1 {
		f, err := open(fs.Arg(0))
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}

	bw := bufio.NewWriterSize(os.Stdout, 256<<10)
	w := ndjson.NewWriter(bw)
	var mu sync.Mutex
	flush := func() error { mu.Lock(); defer mu.Unlock(); return bw.Flush() }

	// Flush on a timer so results reach the next process promptly even
	// while input is idle, and on SIGINT/SIGTERM before exiting.
	stop := make(chan struct{})
	defer close(stop)
	if *flushEvery > 0 {
		go func() {
			t := time.NewTicker(*flushEvery)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					_ = flush()
				case <-stop:
					return
				}
			}
		}()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sig
		_ = flush()
		fmt.Fprintf(os.Stderr, "stream: %v, stopping (results in flight are dropped)\n", s)
		os.Exit(130)
	}()

	stats, err := batch.Process(context.Background(), calculator, ndjson.NewReader(in, *maxLine), *workers,
		func(it csvio.Item, res calc.Result, cerr error) error {
			mu.Lock()
			defer mu.Unlock()
			if err := w.Write(it, res, cerr); err != nil {
				return err
			}
			if *flushEvery == 0 {
				return bw.Flush()
			}
			return nil
		})
	if ferr := flush(); err == nil {
		err = ferr
	}
	fmt.Fprintf(os.Stderr, "%d shipments, %d failed\n", stats.Shipments, stats.Failed)
	return err
}
