package factors

import (
	"context"
	"errors"
	"math"
	"os"
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
	if strings.Join(names, ",") != "desnz-2026,eu-mrv-2024,nl-iww-2026,sample" {
		t.Errorf("EmbeddedNames = %v", names)
	}
	ds, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if ds.Name != "desnz-2026+nl-iww-2026+eu-mrv-2024" || len(ds.Legs) < 100 || len(ds.Fuels) < 100 ||
		!strings.Contains(ds.Note, "Open Government Licence v3.0") {
		t.Errorf("default dataset: name=%q legs=%d fuels=%d", ds.Name, len(ds.Legs), len(ds.Fuels))
	}
	// Spot-check one published value end to end (DESNZ 2026 flat file).
	f, err := ds.LegFactor(calc.Rail, "rail/freight_train")
	if err != nil || f.TTW != 25.83 || f.WTT != 6.91 || f.Year != 2026 {
		t.Errorf("rail freight = %+v, %v", f, err)
	}
	// GLEC v3.2: air is measured on great-circle distance, with no +95 km on
	// top; 95 km is only the default DAF from actual flown distance.
	if _, adjusted := ds.Distances().Adjust[calc.Air]; adjusted {
		t.Error("air distances must not be adjusted on top of great-circle")
	}
	if f, _ := ds.LegFactor(calc.Air, "air_freight"); f.DistanceBasis != calc.DistGCD {
		t.Errorf("air factor basis = %q, want gcd", f.DistanceBasis)
	}
	if d, ok := ds.DAF(calc.Air, calc.DistActual, calc.DistGCD); !ok || d.Apply(1095) != 1000 {
		t.Errorf("air actual->gcd DAF = %+v, %v", d, ok)
	}
	if d, ok := ds.DAF(calc.Sea, calc.DistSFD, calc.DistActual); !ok || math.Abs(d.Apply(1000)-1150) > 1e-9 {
		t.Errorf("sea sfd->actual DAF = %+v, %v", d, ok)
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

func TestOpen_Layered(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	hubs := write("hubs.json", `{"name":"my-hubs","note":"own measurements",
		"hubs":[{"hub_type":"transhipment","wtw_g_per_tonne":1300,"source":"own"}],
		"container_loads":{"average":10}}`)

	ds, err := Open(Default + "," + hubs)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Name != "desnz-2026+nl-iww-2026+eu-mrv-2024+my-hubs" || !strings.Contains(ds.Note, "own measurements") || !strings.Contains(ds.Note, "Open Government Licence") {
		t.Errorf("name=%q note=%q", ds.Name, ds.Note)
	}
	if h, err := ds.HubFactor("transhipment"); err != nil || h.WTWPerTonneG != 1300 {
		t.Errorf("hub = %+v, %v", h, err)
	}
	if _, err := ds.LegFactor(calc.Road, "hgv"); err != nil {
		t.Errorf("DESNZ alias lost in layering: %v", err)
	}
	if _, err := ds.Grid("DNK"); err != nil {
		t.Errorf("default grid lost in layering: %v", err)
	}
	if _, err := ds.LoadModel(calc.Road, "hgv"); err != nil {
		t.Errorf("load model lost in layering: %v", err)
	}

	// End to end: a shipment with a hub stop now calculates.
	c := calc.Calculator{Factors: ds, Distances: ds.Distances()}
	r, err := c.Calculate(context.Background(), calc.Shipment{WeightKg: 10_000,
		Legs: []calc.Leg{{Mode: calc.Road, VehicleType: "hgv", DistanceKm: 100}},
		Hubs: []calc.HubStop{{ID: "h", HubType: "transhipment"}}})
	if err != nil || len(r.Hubs) != 1 || r.Hubs[0].WTWKg != 13 {
		t.Errorf("hub calc = %+v, %v", r.Hubs, err)
	}

	clash := map[string]string{
		"duplicate leg":       `{"legs":[{"mode":"rail","vehicle_type":"rail/freight_train","ttw_g_per_tkm":1}]}`,
		"duplicate alias":     `{"fuel_aliases":{"diesel":"biofuel/biodiesel_hvo/litres"}}`,
		"duplicate DAF":       `{"dafs":[{"mode":"air","from":"actual","to":"gcd","add_km":-100,"source":"x"}]}`,
		"duplicate container": `{"container_loads":{"average":12}}`,
	}
	for name, body := range clash {
		layers := Default + "," + hubs + "," + write(strings.ReplaceAll(name, " ", "_")+".json", body)
		if _, err := Open(layers); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := Open(Default + "," + dir + "/missing.json"); err == nil {
		t.Error("missing layer: want error")
	}
}

func TestLegFactor_MissingModeHint(t *testing.T) {
	ds, _ := Open("desnz-2026") // DESNZ alone has no inland waterway factors
	_, err := ds.LegFactor(calc.InlandWaterway, "barge")
	if err == nil || !strings.Contains(err.Error(), "has no inland_waterway factors") || !strings.Contains(err.Error(), "carbonara toc") {
		t.Errorf("err = %v", err)
	}
	if _, err := ds.LegFactor(calc.Road, "unicycle"); err == nil || strings.Contains(err.Error(), "has no road factors") {
		t.Errorf("road has factors, want plain not-found error, got %v", err)
	}
}

func TestParse_DAFs(t *testing.T) {
	ok := `{"legs":[{"mode":"sea","vehicle_type":"s","ttw_g_per_tkm":1,"distance_basis":"actual"}],
		"dafs":[{"mode":"sea","from":"sfd","to":"actual","multiplier":1.15,"source":"x"}]}`
	ds, err := Parse(strings.NewReader(ok))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := ds.DAF(calc.Sea, calc.DistSFD, calc.DistActual); !found {
		t.Error("DAF not indexed")
	}
	for name, in := range map[string]string{
		"bad basis":     `{"legs":[{"mode":"sea","vehicle_type":"s","distance_basis":"miles"}]}`,
		"bad type":      `{"dafs":[{"mode":"sea","from":"sfd","to":"odometer"}]}`,
		"same types":    `{"dafs":[{"mode":"sea","from":"sfd","to":"sfd"}]}`,
		"no mode":       `{"dafs":[{"from":"sfd","to":"gcd"}]}`,
		"negative mult": `{"dafs":[{"mode":"sea","from":"sfd","to":"gcd","multiplier":-1}]}`,
		"duplicate":     `{"dafs":[{"mode":"sea","from":"sfd","to":"gcd"},{"mode":"sea","from":"sfd","to":"gcd"}]}`,
	} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// Inland waterway factors from www.co2emissiefactoren.nl 2026 (CE Delft
// STREAM 2020 v2), checked row by row against the published list.
func TestInlandWaterway_NL2026(t *testing.T) {
	ds, err := Open(Default)
	if err != nil {
		t.Fatal(err)
	}
	for vt, want := range map[string][2]float64{
		"barge_bulk_small":       {31, 10},
		"barge_bulk_medium":      {23, 8},
		"barge_bulk_large":       {16, 5},
		"barge_container_40teu":  {41, 13},
		"barge_container_96teu":  {39, 13},
		"barge_container_208teu": {24, 8},
		"barge_container_348teu": {20, 7},
		"barge":                  {23, 8},
		"container_barge":        {24, 8},
	} {
		f, err := ds.LegFactor(calc.InlandWaterway, vt)
		if err != nil || f.TTW != want[0] || f.WTT != want[1] || f.DistanceBasis != calc.DistActual {
			t.Errorf("%s = %+v, %v", vt, f, err)
		}
	}

	c := calc.Calculator{Factors: ds, Distances: ds.Distances()}
	ctx := context.Background()
	// Rotterdam-Duisburg, 220 km by river: 1000 t x 220 km x 31 g = 6.82 t.
	r, err := c.Calculate(ctx, calc.Shipment{WeightKg: 1_000_000,
		Legs: []calc.Leg{{Mode: calc.InlandWaterway, VehicleType: "barge", DistanceKm: 220}}})
	if err != nil || math.Abs(r.WTWKg-6820) > 1e-6 {
		t.Errorf("given distance: %v kg, %v", r.WTWKg, err)
	}
	// A shortest feasible distance needs no DAF on inland waterways (GLEC).
	r, err = c.Calculate(ctx, calc.Shipment{WeightKg: 1_000_000,
		Legs: []calc.Leg{{Mode: calc.InlandWaterway, VehicleType: "barge", DistanceKm: 220, DistanceType: calc.DistSFD}}})
	if err != nil || math.Abs(r.WTWKg-6820) > 1e-6 {
		t.Errorf("sfd distance: %v kg, %v", r.WTWKg, err)
	}
	// Straight-line distance from coordinates would undercount a river
	// route, and there is no default DAF for it, so it is refused.
	_, err = c.Calculate(ctx, calc.Shipment{WeightKg: 1000,
		Legs: []calc.Leg{{Mode: calc.InlandWaterway, VehicleType: "barge",
			From: calc.Location{Lat: 51.95, Lon: 4.05}, To: calc.Location{Lat: 51.45, Lon: 6.73}}}})
	if !errors.Is(err, calc.ErrNoDAF) {
		t.Errorf("coordinates only: want ErrNoDAF, got %v", err)
	}
}

func TestShortAlias(t *testing.T) {
	dir := t.TempDir()
	base := dir + "/base.json"
	over := dir + "/over.json"
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(base, `{"name":"base","legs":[{"mode":"sea","vehicle_type":"avg","ttw_g_per_tkm":16}],
		"vehicle_aliases":[{"mode":"sea","alias":"ship","vehicle_type":"avg"}]}`)
	write(over, `{"name":"over","legs":[{"mode":"sea","vehicle_type":"feeder","ttw_g_per_tkm":14},{"mode":"sea","vehicle_type":"ocean","ttw_g_per_tkm":6}],
		"vehicle_aliases":[{"mode":"sea","alias":"ship","vehicle_type":"ocean","short_vehicle_type":"feeder","short_below_km":2000}]}`)
	ds, err := Open(base + "," + over)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		km   float64
		want string
	}{{560, "feeder"}, {1999, "feeder"}, {2000, "ocean"}, {20000, "ocean"}} {
		f, note, err := ds.LegFactorFor(calc.Sea, "ship", tc.km)
		if err != nil || f.VehicleType != tc.want || !strings.Contains(note, tc.want) {
			t.Errorf("%v km: %s %q %v", tc.km, f.VehicleType, note, err)
		}
	}
	// Without a distance, the long meaning; plain types are untouched.
	if f, _ := ds.LegFactor(calc.Sea, "ship"); f.VehicleType != "ocean" {
		t.Errorf("LegFactor: %s", f.VehicleType)
	}
	if f, note, _ := ds.LegFactorFor(calc.Sea, "avg", 100); f.VehicleType != "avg" || note != "" {
		t.Errorf("plain: %s %q", f.VehicleType, note)
	}
	write(over, `{"name":"over","legs":[{"mode":"sea","vehicle_type":"ocean","ttw_g_per_tkm":6}],
		"vehicle_aliases":[{"mode":"sea","alias":"ship2","vehicle_type":"ocean","short_vehicle_type":"nope","short_below_km":2000}]}`)
	if _, err := Open(base + "," + over); err == nil {
		t.Error("unknown short vehicle type accepted")
	}
}
