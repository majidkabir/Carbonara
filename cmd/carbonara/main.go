// Command carbonara calculates freight emissions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/csvio"
	"github.com/majidkabir/carbonara/pkg/factors"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usageText = `Carbonara — freight CO2e calculator

Usage:
  carbonara calc    [-factors name|file] shipment.json   one shipment, JSON result
  carbonara batch   [-factors name|file] shipments.csv   many shipments, CSV result
  carbonara serve   [-factors name|file] [-addr :8080]   HTTP API
  carbonara factors [-factors name|file]                 print the factor dataset
  carbonara import-desnz [-o out.json] flat-file.xlsx   convert UK DESNZ factors
  carbonara version

Use "-" as the file name to read stdin. The default dataset is UK DESNZ 2026.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usageText)
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"calc":         runCalc,
		"batch":        runBatch,
		"serve":        runServe,
		"factors":      runFactors,
		"import-desnz": runImportDESNZ,
		"version":      func([]string) error { fmt.Println(version); return nil },
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
	fs.StringVar(&c.factorFile, "factors", os.Getenv("CARBONARA_FACTORS"),
		"embedded dataset name ("+strings.Join(factors.EmbeddedNames(), ", ")+") or a dataset JSON file; "+
			"also $CARBONARA_FACTORS (default "+factors.Default+")")
}

func (c *common) dataset() (*factors.Dataset, error) {
	ds, err := factors.Open(c.factorFile)
	if err != nil {
		return nil, err
	}
	if ds.Name == "sample" {
		fmt.Fprintln(os.Stderr, "warning: using sample factors (placeholder values)")
	}
	return ds, nil
}

func (c *common) calculator() (calc.Calculator, *factors.Dataset, error) {
	ds, err := c.dataset()
	if err != nil {
		return calc.Calculator{}, nil, err
	}
	return calc.Calculator{Factors: ds, Distances: ds.Distances()}, ds, nil
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
