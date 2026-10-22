// Package grid converts Our World in Data's energy dataset (electricity
// carbon intensity from Ember) into Sootcase grid factors: the latest year
// with data for each country.
//
// OWID data is CC BY 4.0; Ember data is CC BY 4.0. Both need attribution,
// carried in the output note.
package grid

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/majidkabir/sootcase/pkg/calc"
)

// Attribution is the credit CC BY 4.0 requires.
const Attribution = "Electricity carbon intensity: Ember, Yearly Electricity Data; processed by Our World in Data " +
	"(github.com/owid/energy-data). Licensed under CC BY 4.0."

// File is the JSON shape of an embedded grid table.
type File struct {
	Name  string            `json:"name"`
	Note  string            `json:"note"`
	Grids []calc.GridFactor `json:"grids"`
}

// ParseOWID reads owid-energy-data.csv and keeps, per country, the latest
// year with a carbon_intensity_elec value. Regional aggregates (no 3-letter
// ISO code) are skipped.
func ParseOWID(r io.Reader) (File, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return File{}, fmt.Errorf("owid: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, need := range []string{"country", "year", "iso_code", "carbon_intensity_elec"} {
		if _, ok := col[need]; !ok {
			return File{}, fmt.Errorf("owid: missing column %q", need)
		}
	}
	latest := map[string]calc.GridFactor{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return File{}, fmt.Errorf("owid: %w", err)
		}
		iso := strings.TrimSpace(rec[col["iso_code"]])
		v := strings.TrimSpace(rec[col["carbon_intensity_elec"]])
		if len(iso) != 3 || v == "" {
			continue
		}
		year, err := strconv.Atoi(rec[col["year"]])
		if err != nil {
			continue
		}
		g, err := strconv.ParseFloat(v, 64)
		if err != nil || g < 0 {
			return File{}, fmt.Errorf("owid: %s %d: bad carbon_intensity_elec %q", iso, year, v)
		}
		if cur, ok := latest[iso]; !ok || year > cur.Year {
			latest[iso] = calc.GridFactor{Country: iso, Year: year, G: g,
				Source: "Ember via Our World in Data: " + rec[col["country"]]}
		}
	}
	if len(latest) == 0 {
		return File{}, errors.New("owid: no carbon_intensity_elec values found")
	}
	f := File{Name: "grid-ember-owid", Note: Attribution}
	for _, g := range latest {
		f.Grids = append(f.Grids, g)
	}
	sort.Slice(f.Grids, func(i, j int) bool { return f.Grids[i].Country < f.Grids[j].Country })
	return f, nil
}
