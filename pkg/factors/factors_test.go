package factors

import (
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func TestEmbeddedSample(t *testing.T) {
	d, err := Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	f, err := d.LegFactor(calc.Road, "truck_40t")
	if err != nil {
		t.Fatal(err)
	}
	if f.WTW() != 75 {
		t.Errorf("WTW = %v, want 75", f.WTW())
	}
	if _, err := d.LegFactor(calc.Road, "unicycle"); err == nil {
		t.Error("want error for unknown vehicle type")
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]string{
		"duplicate": `{"legs":[{"mode":"road","vehicle_type":"a"},{"mode":"road","vehicle_type":"a"}]}`,
		"negative":  `{"legs":[{"mode":"road","vehicle_type":"a","ttw_g_per_tkm":-1}]}`,
		"bad json":  `{`,
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestEmbeddedSample_Fuel(t *testing.T) {
	d, err := Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.FuelFactor("diesel"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FuelFactor("unobtainium"); err == nil {
		t.Error("want error for unknown fuel")
	}
}

func TestOpen(t *testing.T) {
	names := EmbeddedNames()
	if strings.Join(names, ",") != "desnz-2026,sample" {
		t.Errorf("EmbeddedNames = %v", names)
	}
	ds, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if ds.Name != Default || len(ds.Legs) < 100 || len(ds.Fuels) < 100 ||
		!strings.Contains(ds.Note, "Open Government Licence v3.0") {
		t.Errorf("default dataset: name=%q legs=%d fuels=%d", ds.Name, len(ds.Legs), len(ds.Fuels))
	}
	// Spot-check one published value end to end (DESNZ 2026 flat file).
	f, err := ds.LegFactor(calc.Rail, "rail/freight_train")
	if err != nil || f.TTW != 25.83 || f.WTT != 6.91 || f.Year != 2026 {
		t.Errorf("rail freight = %+v, %v", f, err)
	}
	if a := ds.Distances().Adjust[calc.Air]; a.AddKm != 95 || a.Multiplier != 0 {
		t.Errorf("air distance rule = %+v, want great-circle + 95 km", a)
	}
	if _, err := Open("testdata-does-not-exist.json"); err == nil {
		t.Error("want error for missing file")
	}
}

func TestParse_DistanceAdjustments(t *testing.T) {
	ds, err := Parse(strings.NewReader(`{"name":"x","distance_adjustments":[{"mode":"air","add_km":95}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if a := ds.Distances().Adjust[calc.Air]; a.AddKm != 95 {
		t.Errorf("air adjustment = %+v", a)
	}
	if _, err := Parse(strings.NewReader(`{"distance_adjustments":[{"mode":"air","add_km":-1}]}`)); err == nil {
		t.Error("want error for negative adjustment")
	}
}

func TestAliases(t *testing.T) {
	base := `"legs":[{"mode":"road","vehicle_type":"long/name","ttw_g_per_tkm":1}],
		"fuels":[{"fuel":"long/diesel","unit":"l","ttw_kg_per_unit":2}]`
	ds, err := Parse(strings.NewReader(`{` + base + `,
		"vehicle_aliases":[{"mode":"road","alias":"hgv","vehicle_type":"long/name"}],
		"fuel_aliases":{"diesel":"long/diesel"}}`))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ds.LegFactor(calc.Road, "hgv")
	if err != nil || f.VehicleType != "long/name" {
		t.Errorf("alias lookup = %+v, %v; want resolved full id", f, err)
	}
	if _, err := ds.LegFactor(calc.Rail, "hgv"); err == nil {
		t.Error("alias must be scoped to its mode")
	}
	if ff, err := ds.FuelFactor("diesel"); err != nil || ff.Fuel != "long/diesel" {
		t.Errorf("fuel alias = %+v, %v", ff, err)
	}
	bad := map[string]string{
		"unknown target": `"vehicle_aliases":[{"mode":"road","alias":"x","vehicle_type":"nope"}]`,
		"clash":          `"vehicle_aliases":[{"mode":"road","alias":"long/name","vehicle_type":"long/name"}]`,
		"fuel unknown":   `"fuel_aliases":{"x":"nope"}`,
	}
	for name, extra := range bad {
		if _, err := Parse(strings.NewReader(`{` + base + `,` + extra + `}`)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
