package main

import (
	"math"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func TestFleet(t *testing.T) {
	hdr := []string{"IMO Number", "Name", "Ship type", "Total fuel consumption [m tonnes]", "Total CO₂ emissions [m tonnes]",
		"CO₂ emissions from all voyages between ports under a MS jurisdiction [m tonnes]",
		"CO₂ emissions from all voyages which departed from ports under a MS jurisdiction [m tonnes]",
		"CO₂ emissions from all voyages to ports under a MS jurisdiction [m tonnes]",
		"Total CO₂eq emissions [m tonnes]", "CO₂ emissions per distance [kg CO₂ / n mile]", "CO₂ emissions per transport work (mass) [g CO₂ / m tonnes · n miles]"}
	rows := [][]string{
		{"Ship"}, {""}, hdr,
		// Deep-sea: 1,000 t fuel, 3,150 t CO2, 3,200 t CO2e; 10 g/t·nm -> 315e6 t·nm; 200 kg/nm -> 20,000 t cargo.
		{"1", "A", "Container ship", "1000", "3150", "100", "1500", "1400", "3200", "200", "10"},
		// Deep-sea: 2,000 t fuel, 6,300 t CO2, 6,400 t CO2e; 5 g/t·nm -> 1,260e6 t·nm.
		{"2", "B", "Container ship", "2000", "6300", "0", "3000", "3000", "6400", "400", "5"},
		// Short-sea: mostly between EU ports; 30 g/t·nm -> 10.5e6 t·nm.
		{"3", "C", "Container ship", "100", "315", "250", "30", "20", "320", "300", "30"},
		{"4", "D", "Bulk carrier", "5000", "15000", "0", "7000", "7000", "15500", "100", "3"},
		{"5", "E", "Container ship", "0", "0", "0", "0", "0", "0", "N/A", "Division by zero!"},
		{"6", "F", "Container ship", "10", "31.5", "0", "15", "15", "32", "1000", "0.001"}, // a million tonnes of cargo: bad data
	}
	short, deep, err := fleet(rows, "Container ship")
	if err != nil {
		t.Fatal(err)
	}
	if deep.ships != 2 || deep.skipped != 2 || !deep.hasCO2e || deep.fuelT != 3000 || deep.co2eT != 9600 || math.Abs(deep.workTNm-1575e6) > 1 {
		t.Fatalf("deep %+v", deep)
	}
	if short.ships != 1 || short.fuelT != 100 || math.Abs(short.workTNm-10.5e6) > 1 {
		t.Fatalf("short %+v", short)
	}
	ds := dataset(2024, "version 1 of 01-01-2025", short, deep, 700)
	s, d := ds.Legs[0], ds.Legs[1]
	// Deep: 9,600 t CO2e / (1,575e6 t·nm x 1.852) = 3.29 g/tkm; 3,000 t x 700 kg / same = 0.72.
	if d.TTW != 3.29 || d.WTT != 0.72 || d.DistanceBasis != calc.DistActual || d.VehicleType != "container_ship/eu_mrv_2024/deep_sea" {
		t.Errorf("deep %+v", d)
	}
	// Short: 320 t / (10.5e6 x 1.852) = 16.46; 100 t x 700 kg / same = 3.60.
	if s.TTW != 16.46 || s.WTT != 3.6 || s.VehicleType != "container_ship/eu_mrv_2024/short_sea" {
		t.Errorf("short %+v", s)
	}
	if !strings.Contains(d.Source, "2 deep-sea container ships") || !strings.Contains(ds.Note, "reproduction authorised") {
		t.Errorf("source %q, note %q", d.Source, ds.Note)
	}
	if a := ds.VehicleAliases[0]; a.Alias != "container_ship" || a.VehicleType != d.VehicleType || a.ShortVehicleType != s.VehicleType || a.ShortBelowKm != ShortBelowKm {
		t.Errorf("alias %+v", a)
	}
	if _, _, err := fleet([][]string{{"x"}}, "Container ship"); err == nil {
		t.Error("no header accepted")
	}
}
