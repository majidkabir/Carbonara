package desnz

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/factors"
)

// Synthetic rows in the flat-file layout. Values are made up.
var fixture = [][]string{
	{"UK Government GHG Conversion Factors for Company Reporting"},
	{},
	{"ID", "Scope", "Level 1", "Level 2", "Level 3", "Level 4", "Column Text", "UOM", "GHG/Unit", "GHG Conversion Factor 2026"},
	// freight TTW with matching WTT
	{"1", "Scope 3", "Freighting goods", "HGV (all diesel)", "All HGVs", "", "Average laden", "tonne.km", "kg CO2e", "0.1053"},
	{"2", "Scope 3", "Freighting goods", "HGV (all diesel)", "All HGVs", "", "Average laden", "tonne.km", "kg CO2", "0.104"}, // not CO2e: ignored
	{"3", "Scope 3", "WTT- delivery vehs & freight", "WTT- HGV (all diesel)", "All HGVs", "", "Average laden", "tonne.km", "kg CO2e", "0.0251"},
	{"4", "Scope 3", "Freighting goods", "Cargo ship", "Container ship", "Average", "", "tonne.km", "kg CO2e", "0.0161"},
	{"5", "Scope 3", "WTT- delivery vehs & freight", "Cargo ship", "Container ship", "Average", "", "tonne.km", "kg CO2e", "0.0039"},
	// freight without WTT
	{"6", "Scope 3", "Freighting goods", "Rail", "Freight train", "", "", "tonne.km", "kg CO2e", "0.0277"},
	// unknown mode
	{"7", "Scope 3", "Freighting goods", "Pipeline", "Crude", "", "", "tonne.km", "kg CO2e", "0.001"},
	// per km row: not tonne.km, ignored
	{"8", "Scope 3", "Freighting goods", "HGV (all diesel)", "All HGVs", "", "Average laden", "km", "kg CO2e", "0.9"},
	// blank factor: skipped
	{"9", "Scope 3", "Freighting goods", "Freight flights", "Domestic, to/from UK", "With RF", "", "tonne.km", "kg CO2e", ""},
	{"10", "Scope 3", "Freighting goods", "Freight flights", "International, to/from non-UK", "With RF", "", "tonne.km", "kg CO2e", "1.1"},
	{"11", "Scope 3", "WTT- delivery vehs & freight", "WTT- Freight flights", "International, to/from non-UK", "With RF", "", "tonne.km", "kg CO2e", "0.25"},
	// fuels
	{"12", "Scope 1", "Fuels", "Liquid fuels", "Diesel (average biofuel blend)", "", "", "litres", "kg CO2e", "2.5"},
	{"13", "Scope 3", "WTT- fuels", "Liquid fuels", "Diesel (average biofuel blend)", "", "", "litres", "kg CO2e", "0.6"},
	{"14", "Scope 1", "Fuels", "Liquid fuels", "Diesel (average biofuel blend)", "", "", "tonnes", "kg CO2e", "3000"},
	{"16", "Scope 1", "Bioenergy", "Biofuel", "HVO", "", "", "litres", "kg CO2e", "0.03"},
	{"17", "Scope 3", "WTT- bioenergy", "Biofuel", "HVO", "", "", "litres", "kg CO2e", "0.5"},
	{"18", "Scope 3", "WTT- business travel- air", "Flights", "Domestic", "", "", "passenger.km", "kg CO2e", ""},
	// unrelated category
	{"15", "Scope 2", "UK electricity", "Electricity generated", "Electricity: UK", "", "", "kWh", "kg CO2e", "0.2"},
}

func TestParse(t *testing.T) {
	res, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	ds := res.Dataset
	if ds.Name != "desnz-2026" || !strings.Contains(ds.Note, "Open Government Licence v3.0") {
		t.Errorf("name=%q note=%q", ds.Name, ds.Note)
	}

	want := map[string]calc.Factor{
		"road/hgv_all_diesel/all_hgvs/average_laden":               {TTW: 105.3, WTT: 25.1},
		"sea/cargo_ship/container_ship/average":                    {TTW: 16.1, WTT: 3.9},
		"rail/rail/freight_train":                                  {TTW: 27.7, WTT: 0},
		"air/freight_flights/international_to_from_non_uk/with_rf": {TTW: 1100, WTT: 250},
	}
	if len(ds.Legs) != len(want) {
		t.Fatalf("got %d legs, want %d: %+v", len(ds.Legs), len(want), ds.Legs)
	}
	for _, f := range ds.Legs {
		w, ok := want[string(f.Mode)+"/"+f.VehicleType]
		if !ok {
			t.Errorf("unexpected leg %s/%s", f.Mode, f.VehicleType)
			continue
		}
		if f.TTW != w.TTW || f.WTT != w.WTT || f.Year != 2026 || !strings.HasPrefix(f.Source, "DESNZ 2026: Freighting goods > ") {
			t.Errorf("%s: got %+v, want TTW %v WTT %v", f.VehicleType, f, w.TTW, w.WTT)
		}
	}

	if res.Blank != 1 {
		t.Errorf("blank = %d, want 1 (only the freight row; unrelated blanks not counted)", res.Blank)
	}
	if len(ds.Fuels) != 3 {
		t.Fatalf("got %d fuels: %+v", len(ds.Fuels), ds.Fuels)
	}
	d := ds.Fuels[0]
	if d.Fuel != "liquid_fuels/diesel_average_biofuel_blend/litres" || d.Unit != "litres" || d.TTW != 2.5 || d.WTT != 0.6 {
		t.Errorf("diesel = %+v", d)
	}
	if h := ds.Fuels[2]; h.Fuel != "biofuel/hvo/litres" || h.TTW != 0.03 || h.WTT != 0.5 {
		t.Errorf("hvo = %+v", h)
	}

	joined := strings.Join(res.Warnings, "\n")
	for _, w := range []string{"no WTT factor for rail/freight_train", `no transport mode for "Pipeline"`, "no WTT factor for fuel liquid_fuels/diesel_average_biofuel_blend/tonnes"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing warning %q in:\n%s", w, joined)
		}
	}
}

// The output must load as a regular dataset and drive a calculation.
func TestParse_RoundTrip(t *testing.T) {
	res, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(res.Dataset); err != nil {
		t.Fatal(err)
	}
	ds, err := factors.Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ds.LegFactor(calc.Road, "hgv_all_diesel/all_hgvs/average_laden")
	if err != nil || f.WTW() != 130.4 {
		t.Errorf("WTW = %v, %v; want 130.4", f.WTW(), err)
	}
}

func TestParse_BadInput(t *testing.T) {
	cases := map[string][][]string{
		"no header": {{"a", "b"}, {"1", "2"}},
		"no year": {
			{"Level 1", "Level 2", "Level 3", "Level 4", "Column Text", "UOM", "GHG/Unit", "GHG Conversion Factor"},
		},
		"missing column": {{"Level 1", "UOM", "GHG Conversion Factor 2026"}},
		"no freight rows": {
			{"Level 1", "Level 2", "Level 3", "Level 4", "Column Text", "UOM", "GHG/Unit", "GHG Conversion Factor 2026"},
			{"Fuels", "Liquid fuels", "Diesel", "", "", "litres", "kg CO2e", "2.5"},
		},
	}
	for name, rows := range cases {
		if _, err := Parse(rows); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestClean(t *testing.T) {
	if got := grams(0.1053); got != 105.3 {
		t.Errorf("grams(0.1053) = %v", got)
	}
	if got := clean(0.123456789012345); got != 0.123456789012 {
		t.Errorf("clean = %v", got)
	}
}
