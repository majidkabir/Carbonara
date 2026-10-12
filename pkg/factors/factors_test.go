package factors

import (
	"math"
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

func TestContainerLoads(t *testing.T) {
	ds, err := Parse(strings.NewReader(`{"container_loads":{"average":10}}`))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := ds.ContainerLoad("average"); err != nil || v != 10 {
		t.Errorf("average = %v, %v", v, err)
	}
	if _, err := ds.ContainerLoad("x"); err == nil {
		t.Error("want error for unknown profile")
	}
	if _, err := Parse(strings.NewReader(`{"container_loads":{"x":0}}`)); err == nil {
		t.Error("want error for zero tonnes")
	}
}

// The load models are derived from DESNZ per-km rows; at the published
// load points they must reproduce DESNZ's own per-tonne-km factors.
func TestLoadModelsReproduceDESNZ(t *testing.T) {
	ds, err := Open(Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.LoadModels) < 16 {
		t.Fatalf("got %d load models", len(ds.LoadModels))
	}
	worst := 0.0
	for _, m := range ds.LoadModels {
		for _, p := range []struct {
			lf     float64
			suffix string
		}{{1, "100pct_laden"}, {0.5, "50pct_laden"}} {
			want, err := ds.LegFactor(m.Mode, m.VehicleType+"/"+p.suffix)
			if err != nil {
				t.Fatal(err)
			}
			ttw, wtt, err := m.PerTonneKm(p.lf, 0)
			if err != nil {
				t.Fatal(err)
			}
			// TTW and WTW must match DESNZ within rounding. WTT alone may
			// differ by up to ~3.6%: DESNZ's own WTT per-km and per-tonne-km
			// rows imply payloads that differ by that much, and the model
			// follows the per-km rows (WTT proportional to TTW, ratio ~0.234).
			checks := []struct {
				name      string
				got, want float64
				tol       float64
			}{
				{"TTW", ttw, want.TTW, 0.01},
				{"WTW", ttw + wtt, want.WTW(), 0.01},
				{"WTT", wtt, want.WTT, 0.04},
			}
			for _, c := range checks {
				rel := math.Abs(c.got/c.want - 1)
				if c.name == "WTW" {
					worst = math.Max(worst, rel)
				}
				if rel > c.tol {
					t.Errorf("%s %s at %v: model %.3f vs DESNZ %.3f (%.2f%%)", m.VehicleType, c.name, p.lf, c.got, c.want, rel*100)
				}
			}
		}
	}
	t.Logf("worst WTW deviation from DESNZ: %.3f%%", worst*100)

	// Alias and full ids find the class model.
	for _, vt := range []string{"hgv", "hgv_non_refrigerated_all_diesel/average_non_refrigerated_hgvs/average_laden",
		"hgv_non_refrigerated_all_diesel/average_non_refrigerated_hgvs"} {
		if m, err := ds.LoadModel(calc.Road, vt); err != nil || m.VehicleType != "hgv_non_refrigerated_all_diesel/average_non_refrigerated_hgvs" {
			t.Errorf("LoadModel(%q) = %q, %v", vt, m.VehicleType, err)
		}
	}
	if _, err := ds.LoadModel(calc.Road, "van"); err == nil {
		t.Error("vans have no laden rows in DESNZ, want no load model")
	}
}

func TestGrids(t *testing.T) {
	ds, err := Open(Default)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ds.Grid("dnk")
	if err != nil || g.Country != "DNK" || g.G <= 0 || g.Year < 2024 {
		t.Errorf("DNK grid = %+v, %v", g, err)
	}
	if !strings.Contains(ds.GridNote, "CC BY 4.0") {
		t.Error("default grid attribution missing")
	}
	f, err := ds.FuelFactor("electricity/DNK")
	if err != nil || f.Unit != "kWh" || f.TTW != 0 || f.WTT != g.G/1000 {
		t.Errorf("electricity fuel = %+v, %v", f, err)
	}
	if _, err := ds.Grid("DK"); err == nil {
		t.Error("want error for 2-letter code")
	}
	if _, err := ds.FuelFactor("electricity/ZZZ"); err == nil {
		t.Error("want error for unknown country")
	}

	// A dataset's own grids replace the default.
	own, err := Parse(strings.NewReader(`{"grids":[{"country":"dnk","g_per_kwh":50}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := own.Grid("DNK"); g.G != 50 {
		t.Errorf("own grid = %+v", g)
	}
	if _, err := own.Grid("SWE"); err == nil {
		t.Error("own grids should not merge with the default")
	}
	for name, in := range map[string]string{
		"dup":      `{"grids":[{"country":"DNK","g_per_kwh":1},{"country":"dnk","g_per_kwh":2}]}`,
		"bad iso":  `{"grids":[{"country":"DK","g_per_kwh":1}]}`,
		"negative": `{"grids":[{"country":"DNK","g_per_kwh":-1}]}`,
	} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
