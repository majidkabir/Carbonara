// Package batch calculates a stream of shipments in parallel while keeping
// results in input order and memory bounded.
package batch

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/csvio"
)

// Source yields shipments; Next returns io.EOF after the last one.
// csvio.Reader is a Source.
type Source interface {
	Next() (csvio.Item, error)
}

// Sink receives results in input order. err is the shipment's own parse
// or calculation error. Returning an error stops the batch.
type Sink func(item csvio.Item, res calc.Result, err error) error

// Stats summarises a run.
type Stats struct {
	Shipments, Failed int
}

// Process calculates every shipment from src with the given number of
// workers (0 means one per CPU) and passes results to sink in input order.
// At most a few items per worker are in flight, so memory doesn't grow
// with the input. It returns the first read or sink error, after sending
// every result that came before it.
func Process(ctx context.Context, c calc.Calculator, src Source, workers int, sink Sink) (Stats, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		seq  int
		item csvio.Item
	}
	type result struct {
		job
		res calc.Result
		err error
	}
	window := make(chan struct{}, workers*4) // items read but not yet written
	jobs := make(chan job, workers)
	results := make(chan result, workers)

	var readErr error
	go func() {
		defer close(jobs)
		for seq := 0; ; seq++ {
			select {
			case window <- struct{}{}:
			case <-ctx.Done():
				return
			}
			it, err := src.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				readErr = err
				return
			}
			select {
			case jobs <- job{seq, it}:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := result{job: j, err: j.item.Err}
				if r.err == nil {
					r.res, r.err = c.Calculate(ctx, j.item.Shipment)
				}
				results <- r
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	var stats Stats
	var sinkErr error
	pending := map[int]result{}
	next := 0
	for r := range results {
		if sinkErr != nil {
			continue // drain so workers can exit
		}
		pending[r.seq] = r
		for {
			p, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			next++
			stats.Shipments++
			if p.err != nil {
				stats.Failed++
			}
			if err := sink(p.item, p.res, p.err); err != nil {
				sinkErr = err
				cancel()
				break
			}
			<-window
		}
	}
	if sinkErr != nil {
		return stats, sinkErr
	}
	// readErr is written before jobs closes, and results closes after
	// every worker has drained jobs, so reading it here is race-free.
	return stats, readErr
}
