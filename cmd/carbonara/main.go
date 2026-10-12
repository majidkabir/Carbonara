// Command carbonara calculates freight emissions.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/majidkabir/carbonara/pkg/batch"
	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/csvio"
	"github.com/majidkabir/carbonara/pkg/distance"
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
  carbonara import-grid  [-o out.json] owid-energy.csv  convert grid intensities
  carbonara toc     [-o layer.json] operations.json      intensities from your own fuel data
  carbonara version

Use "-" as the file name to read stdin. The default dataset is UK DESNZ 2026.
calc, batch and serve also take -osrm URL (road routing) and -sea-network
file.geojson (sea routing); see "carbonara calc -h".`

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
		"import-grid":  runImportGrid,
		"toc":          runTOC,
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
type common struct {
	factorFile, osrmURL           string
	seaNetwork, seaAvoid, seaOpen string
	seaMaxSnapKm                  float64
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.factorFile, "factors", os.Getenv("CARBONARA_FACTORS"),
		"embedded dataset name ("+strings.Join(factors.EmbeddedNames(), ", ")+") or a dataset JSON file, "+
			"or several comma-separated to layer them (e.g. "+factors.Default+",my-hubs.json); "+
			"also $CARBONARA_FACTORS (default "+factors.Default+")")
	fs.StringVar(&c.osrmURL, "osrm", os.Getenv("CARBONARA_OSRM_URL"),
		"OSRM server URL for road distances, also $CARBONARA_OSRM_URL (default: adjusted great-circle)")
	fs.StringVar(&c.seaNetwork, "sea-network", os.Getenv("CARBONARA_SEA_NETWORK"),
		"maritime network GeoJSON for sea distances, also $CARBONARA_SEA_NETWORK (default: adjusted great-circle)")
	fs.StringVar(&c.seaAvoid, "sea-avoid", "", "comma-separated passages to avoid, e.g. suez,panama")
	fs.StringVar(&c.seaOpen, "sea-open", "", "comma-separated passages to open that are closed by default ("+
		strings.Join(distance.DefaultAvoid, ",")+")")
	fs.Float64Var(&c.seaMaxSnapKm, "sea-max-snap-km", 0, "reject sea points farther than this from the network (0 = no limit)")
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
	routed := map[calc.Mode]calc.DistanceProvider{}
	if c.osrmURL != "" {
		o, err := distance.NewOSRM(c.osrmURL)
		if err != nil {
			return calc.Calculator{}, nil, err
		}
		routed[calc.Road] = o
	}
	if c.seaNetwork != "" {
		n, err := c.loadSea()
		if err != nil {
			return calc.Calculator{}, nil, err
		}
		routed[calc.Sea] = n
	}
	var dist calc.DistanceProvider = ds.Distances()
	if len(routed) > 0 {
		dist = distance.ByMode{Providers: routed, Fallback: dist}
	}
	return calc.Calculator{Factors: ds, Distances: dist}, ds, nil
}

func (c *common) loadSea() (*distance.SeaNetwork, error) {
	f, err := os.Open(c.seaNetwork)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := distance.LoadSeaNetwork(f)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, p := range n.Passes() {
		known[p] = true
	}
	set := func(list string, avoid bool) error {
		for _, p := range strings.Split(list, ",") {
			if p = strings.ToLower(strings.TrimSpace(p)); p == "" {
				continue
			}
			if !known[p] {
				return fmt.Errorf("sea network has no passage %q (has: %s)", p, strings.Join(n.Passes(), ", "))
			}
			n.Avoid[p] = avoid
		}
		return nil
	}
	if err := set(c.seaOpen, false); err != nil {
		return nil, err
	}
	if err := set(c.seaAvoid, true); err != nil {
		return nil, err
	}
	n.MaxSnapKm = c.seaMaxSnapKm
	return n, nil
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
	workers := fs.Int("workers", 0, "parallel calculations (default: one per CPU)")
	ungrouped := fs.Bool("ungrouped", false, "allow a shipment's rows anywhere in the file (loads the whole file into memory)")
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

	var src batch.Source
	if *ungrouped {
		items, err := csvio.Read(f)
		if err != nil {
			return err
		}
		src = &sliceSource{items: items}
	} else {
		r, err := csvio.NewReader(bufio.NewReaderSize(f, 1<<20))
		if err != nil {
			return err
		}
		src = r
	}

	bw := bufio.NewWriterSize(os.Stdout, 1<<20)
	w, err := csvio.NewWriter(bw)
	if err != nil {
		return err
	}
	ungroupedSeen := false
	stats, err := batch.Process(context.Background(), calculator, src, *workers,
		func(it csvio.Item, res calc.Result, err error) error {
			if err != nil && strings.Contains(err.Error(), "must be together") {
				ungroupedSeen = true
			}
			return w.Write(it.Shipment.ID, res, err)
		})
	if ferr := w.Flush(); err == nil {
		err = ferr
	}
	if ferr := bw.Flush(); err == nil {
		err = ferr
	}
	fmt.Fprintf(os.Stderr, "%d shipments, %d failed\n", stats.Shipments, stats.Failed)
	if ungroupedSeen {
		fmt.Fprintln(os.Stderr, "hint: some shipments' rows are split up; sort the file by shipment_id, or use -ungrouped")
	}
	return err
}

type sliceSource struct {
	items []csvio.Item
	i     int
}

func (s *sliceSource) Next() (csvio.Item, error) {
	if s.i >= len(s.items) {
		return csvio.Item{}, io.EOF
	}
	s.i++
	return s.items[s.i-1], nil
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
