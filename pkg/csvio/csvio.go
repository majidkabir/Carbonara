// Package csvio reads shipments from CSV and writes results as CSV.
//
// Input has a header row; column order is free and unknown columns are
// ignored. One row per leg, or per hub stop when hub_type is set. Rows are
// grouped into shipments by shipment_id, in order of first appearance.
//
//	shipment_id, weight_kg                          required
//	leg_id, mode, vehicle_type, distance_km         leg
//	from_lat, from_lon, to_lat, to_lon              leg, used when distance_km is empty
//	fuel, fuel_amount, vehicle_tonne_km             leg, fuel-based method
//	hub_id, hub_type                                hub stop
package csvio

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
)

// Item is a parsed shipment, or the error that stopped it from parsing.
type Item struct {
	Shipment calc.Shipment
	Err      error
}

// Read parses all shipments from r. Row-level problems are reported on the
// affected shipment's Item, so one bad row doesn't fail the whole file.
// Structural problems (no header, missing required columns) return an error.
func Read(r io.Reader) ([]Item, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))] = i
	}
	for _, req := range []string{"shipment_id", "weight_kg"} {
		if _, ok := col[req]; !ok {
			return nil, fmt.Errorf("missing required column %q", req)
		}
	}

	var items []*Item
	byID := map[string]*Item{}
	line := 1
	for {
		rec, err := cr.Read()
		line++
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		row := rowReader{rec: rec, col: col}
		id := row.str("shipment_id")
		if id == "" {
			return nil, fmt.Errorf("line %d: empty shipment_id", line)
		}
		it, ok := byID[id]
		if !ok {
			it = &Item{Shipment: calc.Shipment{ID: id}}
			byID[id] = it
			items = append(items, it)
		}
		if it.Err != nil {
			continue
		}
		if err := addRow(&it.Shipment, row); err != nil {
			it.Err = fmt.Errorf("line %d: %w", line, err)
		}
	}

	out := make([]Item, len(items))
	for i, it := range items {
		out[i] = *it
	}
	return out, nil
}

func addRow(s *calc.Shipment, row rowReader) error {
	w, err := row.float("weight_kg")
	if err != nil {
		return err
	}
	switch {
	case w == 0:
		// weight may be given on the first row only
	case s.WeightKg == 0:
		s.WeightKg = w
	case s.WeightKg != w:
		return fmt.Errorf("weight_kg %v conflicts with earlier %v", w, s.WeightKg)
	}

	if ht := row.str("hub_type"); ht != "" {
		s.Hubs = append(s.Hubs, calc.HubStop{ID: row.str("hub_id"), HubType: ht})
		return nil
	}

	leg := calc.Leg{
		ID:          row.str("leg_id"),
		Mode:        calc.Mode(strings.ToLower(row.str("mode"))),
		VehicleType: row.str("vehicle_type"),
	}
	if leg.Mode == "" {
		return errors.New("leg row without mode")
	}
	var errs []error
	get := func(name string) float64 {
		v, err := row.float(name)
		errs = append(errs, err)
		return v
	}
	leg.DistanceKm = get("distance_km")
	leg.From = calc.Location{Lat: get("from_lat"), Lon: get("from_lon")}
	leg.To = calc.Location{Lat: get("to_lat"), Lon: get("to_lon")}
	if fuel := row.str("fuel"); fuel != "" {
		leg.Fuel = &calc.FuelUse{Fuel: fuel, Amount: get("fuel_amount"), VehicleTonneKm: get("vehicle_tonne_km")}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	s.Legs = append(s.Legs, leg)
	return nil
}

type rowReader struct {
	rec []string
	col map[string]int
}

func (r rowReader) str(name string) string {
	i, ok := r.col[name]
	if !ok || i >= len(r.rec) {
		return ""
	}
	return strings.TrimSpace(r.rec[i])
}

// float returns 0 for an empty or missing cell.
func (r rowReader) float(name string) (float64, error) {
	s := r.str(name)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", name, s)
	}
	return v, nil
}

// ResultHeader is the header row written by Writer.
var ResultHeader = []string{"shipment_id", "tonne_km", "ttw_kg", "wtw_kg", "intensity_g_per_tkm", "error"}

// Writer writes one summary row per shipment.
type Writer struct{ cw *csv.Writer }

// NewWriter writes the header and returns a Writer.
func NewWriter(w io.Writer) (*Writer, error) {
	cw := csv.NewWriter(w)
	if err := cw.Write(ResultHeader); err != nil {
		return nil, err
	}
	return &Writer{cw: cw}, nil
}

// Write writes a result, or the error for that shipment.
func (w *Writer) Write(id string, r calc.Result, err error) error {
	if err != nil {
		return w.cw.Write([]string{id, "", "", "", "", err.Error()})
	}
	return w.cw.Write([]string{id, num(r.TonneKm), num(r.TTWKg), num(r.WTWKg), num(r.IntensityG), ""})
}

// Flush flushes buffered rows and reports any write error.
func (w *Writer) Flush() error {
	w.cw.Flush()
	return w.cw.Error()
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
