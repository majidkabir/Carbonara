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
