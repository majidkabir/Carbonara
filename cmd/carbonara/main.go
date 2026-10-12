// Command carbonara calculates freight emissions.
//
//	carbonara calc [-factors file.json] shipment.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/distance"
	"github.com/majidkabir/carbonara/pkg/factors"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "calc":
		if err := runCalc(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Carbonara — freight CO2e calculator

Usage:
  carbonara calc [-factors file.json] shipment.json`)
}

func runCalc(args []string) error {
	fs := flag.NewFlagSet("calc", flag.ExitOnError)
	factorFile := fs.String("factors", "", "factor dataset JSON (default: embedded sample)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("expected one shipment file")
	}

	var ds *factors.Dataset
	var err error
	if *factorFile != "" {
		ds, err = factors.Load(*factorFile)
	} else {
		ds, err = factors.Embedded("sample")
		fmt.Fprintln(os.Stderr, "warning: using embedded sample factors (placeholder values)")
	}
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var s calc.Shipment
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse shipment: %w", err)
	}

	c := calc.Calculator{Factors: ds, Distances: distance.GreatCircle{}}
	res, err := c.Calculate(context.Background(), s)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}
