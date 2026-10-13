package report

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/factors"
)

const testData = `{"name":"t",
 "legs":[{"mode":"road","vehicle_type":"truck","ttw_g_per_tkm":60,"wtt_g_per_tkm":15,"source":"road src","year":2026},
         {"mode":"sea","vehicle_type":"ship","ttw_g_per_tkm":8,"wtt_g_per_tkm":2,"source":"sea src","year":2026}],
 "hubs":[{"hub_type":"terminal","wtw_g_per_tonne":1000,"source":"hub src","year":2026}],
 "fuels":[{"fuel":"diesel","unit":"l","ttw_kg_per_unit":2.5,"wtt_kg_per_unit":0.6,"source":"fuel src","year":2026}]}`

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Worked by hand:
//
//	A: 10 t; road 100 km (no distance type): 1,000 tkm x 60/15 g = 60/15 kg
//	   sea 1,000 km SFD: 10,000 tkm x 8/2 g = 80/20 kg; terminal 10 t x 1 kg = 10 kg
//	B: 5 t; road 50 km on 10 l diesel (primary): 250 tkm, 25/6 kg
//	C: fails.
func TestReport_HandWorked(t *testing.T) {
	ds, err := factors.Parse(strings.NewReader(testData))
	if err != nil {
		t.Fatal(err)
	}
	c := calc.Calculator{Factors: ds}
	ctx := context.Background()
	b := New(Meta{Organisation: "Acme", PeriodFrom: "2026-01-01", PeriodTo: "2026-03-31"})
	b.now = func() time.Time { return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) }

	a, err := c.Calculate(ctx, calc.Shipment{ID: "A", WeightKg: 10_000,
		Legs: []calc.Leg{{Mode: calc.Road, VehicleType: "truck", DistanceKm: 100},
			{Mode: calc.Sea, VehicleType: "ship", DistanceKm: 1000, DistanceType: calc.DistSFD}},
		Hubs: []calc.HubStop{{HubType: "terminal"}}})
	if err != nil {
		t.Fatal(err)
	}
	b.Add("A", a, nil)
	bres, err := c.Calculate(ctx, calc.Shipment{ID: "B", WeightKg: 5_000,
		Legs: []calc.Leg{{Mode: calc.Road, VehicleType: "truck", DistanceKm: 50,
			Fuel: &calc.FuelUse{Fuel: "diesel", Amount: 10}}}})
	if err != nil {
		t.Fatal(err)
	}
	b.Add("B", bres, nil)
	b.Add("C", calc.Result{}, errors.New("bad"))

	r := b.Report()
	if r.Coverage.Shipments != 3 || r.Coverage.Calculated != 2 || r.Coverage.Failed != 1 ||
		r.Coverage.Legs != 3 || r.Coverage.HubStops != 1 || r.Coverage.FailedIDs[0] != "C" {
		t.Errorf("coverage %+v", r.Coverage)
	}
	near(t, "weight", r.Coverage.WeightT, 15)
	near(t, "ttw", r.Total.TTWKg, 165)
	near(t, "wtt", r.Total.WTTKg, 41)
	near(t, "transport", r.Total.TransportWTWKg, 206)
	near(t, "hub", r.Total.HubWTWKg, 10)
	near(t, "total", r.Total.WTWKg, 216)
	near(t, "tkm", r.Total.TonneKm, 11_250)
	near(t, "intensity", r.Total.IntensityG, 206_000.0/11_250)
	near(t, "sfd share", r.Total.DistanceTypes["sfd"], 10_000.0/11_250)
	near(t, "unspecified share", r.Total.DistanceTypes[Unspecified], 1_250.0/11_250)

	if len(r.Modes) != 2 || r.Modes[0].Mode != calc.Road {
		t.Fatalf("modes %+v", r.Modes)
	}
	road := r.Modes[0]
	if road.Legs != 2 {
		t.Errorf("road legs %d", road.Legs)
	}
	near(t, "road wtw", road.WTWKg, 106)
	near(t, "road intensity", road.IntensityG, 106_000.0/1_250)
	near(t, "sea intensity", r.Modes[1].IntensityG, 10)

	if len(r.Hubs) != 1 || r.Hubs[0].Stops != 1 {
		t.Fatalf("hubs %+v", r.Hubs)
	}
	near(t, "hub tonnes", r.Hubs[0].Tonnes, 10)
	near(t, "hub intensity", r.Hubs[0].IntensityGT, 1000)

	if len(r.Energy) != 2 || r.Energy[0].Carrier != Unspecified || r.Energy[1].Carrier != "diesel" {
		t.Fatalf("energy %+v", r.Energy)
	}
	near(t, "diesel share", r.Energy[1].Share, 31.0/206)

	near(t, "primary", r.Data.Primary, 31.0/216)
	near(t, "default", r.Data.Default, 185.0/216)
	near(t, "modelled", r.Data.Modelled, 0)
	if len(r.Data.DefaultSources) != 3 || r.Data.DefaultSources[0].Source != "sea src (2026)" {
		t.Errorf("sources %+v", r.Data.DefaultSources)
	}
	if r.Statement != MethodStatement {
		t.Errorf("conformity must not be declared by default")
	}
	joined := strings.Join(r.Notes, "\n")
	for _, want := range []string{"1 of 3 shipments", "(C)", "well-to-wheel only", "11.1% of tonne-km", "sources are listed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q:\n%s", want, joined)
		}
	}

	var md bytes.Buffer
	if err := WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**Organisation:** Acme", "2026-01-01 to 2026-03-31", "| **Total GHG emissions (WTW)** | **216.0** |",
		"11,250 tonne-km", "| road | 2 | 1,250 | unspecified |", "SFD 88.9%, unspecified 11.1%", "| terminal | 1 | 10.0 | 10.0 | 1,000.0 |"} {
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown lacks %q:\n%s", want, md.String())
		}
	}
}

func TestReport_DeclareISOAndModels(t *testing.T) {
	ds, err := factors.Open(factors.Default)
	if err != nil {
		t.Fatal(err)
	}
	c := calc.Calculator{Factors: ds}
	b := New(Meta{DeclareISO: true})
	r1, err := c.Calculate(context.Background(), calc.Shipment{WeightKg: 10_000, Legs: []calc.Leg{
		{Mode: calc.Road, VehicleType: "hgv", DistanceKm: 300, LoadFactor: 0.6, EmptyTripFactor: 0.2},
		{Mode: calc.Rail, VehicleType: "freight_train", DistanceKm: 500, EnergyKWhPerTkm: 0.03, Country: "DNK"},
		{Mode: calc.Air, VehicleType: "air_freight", DistanceKm: 1000, DistanceType: calc.DistActual},
	}})
	if err != nil {
		t.Fatal(err)
	}
	b.Add("x", r1, nil)
	r := b.Report()
	if r.Statement != ISOStatement || len(r.Datasets) != 1 {
		t.Errorf("statement %q datasets %v", r.Statement, r.Datasets)
	}
	if len(r.Data.Models) != 2 || r.Data.Models[0].Parameters["countries"] != "DNK" ||
		r.Data.Models[1].Parameters["load_factor"] != "0.6" || r.Data.Models[1].Parameters["empty_trip_factor"] != "0.2" {
		t.Errorf("models %+v", r.Data.Models)
	}
	near(t, "modelled+default", r.Data.Modelled+r.Data.Default, 1)
	if len(r.DAFs) != 1 || r.DAFs[0].Legs != 1 || !strings.Contains(r.DAFs[0].Source, "95 km") {
		t.Errorf("dafs %+v", r.DAFs)
	}
	if r.Energy[0].Carrier == "" {
		t.Error("empty carrier")
	}
}

func TestReport_Empty(t *testing.T) {
	r := New(Meta{}).Report()
	if r.Total.IntensityG != 0 || r.Title == "" || len(r.Notes) != 0 {
		t.Errorf("%+v", r)
	}
	var md bytes.Buffer
	if err := WriteMarkdown(&md, r); err != nil || !strings.Contains(md.String(), "Period:** not stated") {
		t.Errorf("%v\n%s", err, md.String())
	}
}

func TestEsc(t *testing.T) {
	if got := esc("a|b\n<script>"); got != `a\|b &lt;script&gt;` {
		t.Errorf("esc = %q", got)
	}
	if num(1234567.891, 2) != "1,234,567.89" || num(-1000, 0) != "-1,000" || num(12, 1) != "12.0" {
		t.Error("num")
	}
}
