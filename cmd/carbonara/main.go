// Command carbonara calculates freight emissions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/csvio"
	"github.com/majidkabir/carbonara/pkg/distance"
	"github.com/majidkabir/carbonara/pkg/factors"
)

const usageText = `Carbonara — freight CO2e calculator

Usage:
  carbonara calc    [-factors file.json] shipment.json   one shipment, JSON result
  carbonara batch   [-factors file.json] shipments.csv   many shipments, CSV result
  carbonara serve   [-factors file.json] [-addr :8080]   HTTP API
  carbonara factors [-factors file.json]                 print the factor dataset

Use "-" as the file name to read stdin.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usageText)
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"calc":    runCalc,
		"batch":   runBatch,
		"serve":   runServe,
		"factors": runFactors,
	}
	cmd, ok := cmds[os.Args[1]]
	if !ok {
		if os.Args[1] != "help" && os.Args[1] != "-h" && os.Args[1] != "--help" {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		}
		fmt.Fprintln(os.Stderr, usageText)
		os.Exit(2)
	}
	if err := cmd(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// common holds flags shared by all subcommands.
type common struct{ factorFile string }

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.factorFile, "factors", os.Getenv("CARBONARA_FACTORS"), "factor dataset JSON, or $CARBONARA_FACTORS (default: embedded sample)")
}

func (c *common) dataset() (*factors.Dataset, error) {
	if c.factorFile != "" {
		return factors.Load(c.factorFile)
	}
	fmt.Fprintln(os.Stderr, "warning: using embedded sample factors (placeholder values)")
	return factors.Embedded("sample")
}

func (c *common) calculator() (calc.Calculator, *factors.Dataset, error) {
	ds, err := c.dataset()
	if err != nil {
		return calc.Calculator{}, nil, err
	}
	return calc.Calculator{Factors: ds, Distances: distance.GreatCircle{}}, ds, nil
}

func open(name string) (io.ReadCloser, error) {
	if name == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	return os.Open(name)
}

func runCalc(args []string) error {
	fs := flag.NewFlagSet("calc", flag.ExitOnError)
	var c common
	c.register(fs)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("calc: expected one shipment file")
	}
	calculator, _, err := c.calculator()
	if err != nil {
		return err
	}
	f, err := open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	var s calc.Shipment
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return fmt.Errorf("parse shipment: %w", err)
	}
	res, err := calculator.Calculate(context.Background(), s)
	if err != nil {
		return err
	}
	return printJSON(res)
}

func runBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	var c common
	c.register(fs)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("batch: expected one CSV file")
	}
	calculator, _, err := c.calculator()
	if err != nil {
		return err
	}
	f, err := open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	items, err := csvio.Read(f)
	if err != nil {
		return err
	}
	w, err := csvio.NewWriter(os.Stdout)
	if err != nil {
		return err
	}
	failed := 0
	for _, it := range items {
		err := it.Err
		var res calc.Result
		if err == nil {
			res, err = calculator.Calculate(context.Background(), it.Shipment)
		}
		if err != nil {
			failed++
		}
		if werr := w.Write(it.Shipment.ID, res, err); werr != nil {
			return werr
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d shipments, %d failed\n", len(items), failed)
	return nil
}

func runFactors(args []string) error {
	fs := flag.NewFlagSet("factors", flag.ExitOnError)
	var c common
	c.register(fs)
	_ = fs.Parse(args)
	ds, err := c.dataset()
	if err != nil {
		return err
	}
	return printJSON(ds)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
