package main

import (
	"math"
	"strings"
	"testing"
)

func TestFleet(t *testing.T) {
	hdr := []string{"IMO Number", "Name", "Ship type", "Total fuel consumption [m tonnes]", "Total CO₂ emissions [m tonnes]",
		"Total CO₂eq emissions [m tonnes]", "CO₂ emissions per distance [kg CO₂ / n mile]", "CO₂ emissions per transport work (mass) [g CO₂ / m tonnes · n miles]"}
	rows := [][]string{
		{"Ship"}, {""}, hdr,
		// 1,000 t fuel, 3,150 t CO2, 3,200 t CO2e; 10 g/t·nm -> 315e6 t·nm; 200 kg/nm -> 20,000 t cargo.
		{"1", "A", "Container ship", "1000", "3150", "3200", "200", "10"},
		// 2,000 t fuel, 6,300 t CO2, 6,400 t CO2e; 5 g/t·nm -> 1,260e6 t·nm.
		{"2", "B", "Container ship", "2000", "6300", "6400", "400", "5"},
		{"3", "C", "Bulk carrier", "5000", "15000", "15500", "100", "3"},
		{"4", "D", "Container ship", "0", "0", "0", "N/A", "Division by zero!"},
		{"5", "E", "Container ship", "10", "31.5", "32", "1000", "0.001"}, // a million tonnes of cargo: bad data
	}
	fl, err := fleet(rows, "Container ship")
	if err != nil {
		t.Fatal(err)
	}
	if fl.ships != 2 || fl.skipped != 2 || !fl.hasCO2e || fl.fuelT != 3000 || fl.co2eT != 9600 || math.Abs(fl.workTNm-1575e6) > 1 {
		t.Fatalf("%+v", fl)
	}
	ds := dataset(2024, "version 1 of 01-01-2025", fl, 700)
	f := ds.Legs[0]
	// 9,600 t CO2e / (1,575e6 t·nm x 1.852) = 3.29 g/tkm; 3,000 t x 700 kg / same = 0.72.
	if f.TTW != 3.29 || f.WTT != 0.72 || f.DistanceBasis != "actual" || f.VehicleType != "container_ship/eu_mrv_2024" {
		t.Errorf("%+v", f)
	}
	if !strings.Contains(f.Source, "2 container ships") || !strings.Contains(ds.Note, "reproduction authorised") {
		t.Errorf("source %q, note %q", f.Source, ds.Note)
	}
	if len(ds.VehicleAliases) != 1 || ds.VehicleAliases[0].Alias != "container_ship" {
		t.Errorf("aliases %+v", ds.VehicleAliases)
	}
	if _, err := fleet([][]string{{"x"}}, "Container ship"); err == nil {
		t.Error("no header accepted")
	}
}
