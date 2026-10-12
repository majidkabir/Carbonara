package toc

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/factors"
)

const ops = `{
  "name": "acme", "period": "2025", "year": 2025,
  "tocs": [{"id": "road_dk_ambient", "mode": "road", "tonne_km": 1000000,
            "fuels": [{"fuel": "diesel", "amount": 20000}, {"fuel": "hvo", "amount": 5000}]}],
  "hocs": [{"id": "cph_crossdock", "tonnes": 50000,
            "energy": [{"fuel": "electricity/DNK", "amount": 400000}]}]
}`

func TestBuild_AgainstDESNZ(t *testing.T) {
	ds, err := factors.Open(factors.Default)
	if err != nil {
		t.Fatal(err)
	}
	o, err := Parse(strings.NewReader(ops))
	if err != nil {
		t.Fatal(err)
	}
	layer, err := Build(o, ds)
	if err != nil {
		t.Fatal(err)
	}

	diesel, _ := ds.FuelFactor("diesel")
	hvo, _ := ds.FuelFactor("hvo")
	grid, _ := ds.Grid("DNK")
	wantTTW := (20000*diesel.TTW + 5000*hvo.TTW) * 1000 / 1e6
	wantWTT := (20000*diesel.WTT + 5000*hvo.WTT) * 1000 / 1e6
	leg := layer.Legs[0]
	if math.Abs(leg.TTW-wantTTW) > 1e-9 || math.Abs(leg.WTT-wantWTT) > 1e-9 || leg.DataType != calc.DataPrimary ||
		leg.VehicleType != "road_dk_ambient" || leg.Year != 2025 {
		t.Errorf("toc factor = %+v, want TTW %v WTT %v", leg, wantTTW, wantWTT)
	}
	hub := layer.Hubs[0]
	if want := 400000 * grid.G / 50000; math.Abs(hub.WTWPerTonneG-want) > 1e-9 || hub.DataType != calc.DataPrimary {
		t.Errorf("hoc factor = %+v, want %v g/t", hub, want)
	}

	// Layer on DESNZ and calculate a shipment through both.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(layer); err != nil {
		t.Fatal(err)
	}
	merged, err := layered(t, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	c := calc.Calculator{Factors: merged}
	r, err := c.Calculate(context.Background(), calc.Shipment{WeightKg: 10_000,
		Legs: []calc.Leg{{ID: "toc", Mode: calc.Road, VehicleType: "road_dk_ambient", DistanceKm: 200},
			{ID: "avg", Mode: calc.Road, VehicleType: "hgv", DistanceKm: 50}},
		Hubs: []calc.HubStop{{ID: "h", HubType: "cph_crossdock"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Legs[0].DataType != calc.DataPrimary || r.Legs[1].DataType != calc.DataDefault || r.Hubs[0].DataType != calc.DataPrimary {
		t.Errorf("data types %q %q %q", r.Legs[0].DataType, r.Legs[1].DataType, r.Hubs[0].DataType)
	}
	if want := 2000 * (wantTTW + wantWTT) / 1000; math.Abs(r.Legs[0].WTWKg-want) > 1e-9 {
		t.Errorf("toc leg = %v kg, want %v", r.Legs[0].WTWKg, want)
	}
	wantShare := (r.Legs[0].WTWKg + r.Hubs[0].WTWKg) / r.WTWKg
	if math.Abs(r.PrimaryDataShare-wantShare) > 1e-12 {
		t.Errorf("primary share %v, want %v", r.PrimaryDataShare, wantShare)
	}
}

func layered(t *testing.T, layer []byte) (*factors.Dataset, error) {
	t.Helper()
	p := t.TempDir() + "/layer.json"
	if err := writeFile(p, layer); err != nil {
		return nil, err
	}
	return factors.Open(factors.Default + "," + p)
}

func TestParse_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":       `{"name":"x"}`,
		"unknown key": `{"tocs":[{"id":"a","mode":"road","tonne_km":1,"fuels":[{"fuel":"diesel","amount":1}]}],"extra":1}`,
		"bad id":      `{"tocs":[{"id":"Bad ID","mode":"road","tonne_km":1,"fuels":[{"fuel":"diesel","amount":1}]}]}`,
		"dup id":      `{"tocs":[{"id":"a","mode":"road","tonne_km":1,"fuels":[{"fuel":"d","amount":1}]}],"hocs":[{"id":"a","tonnes":1,"energy":[{"fuel":"d","amount":1}]}]}`,
		"no mode":     `{"tocs":[{"id":"a","tonne_km":1,"fuels":[{"fuel":"diesel","amount":1}]}]}`,
		"zero tkm":    `{"tocs":[{"id":"a","mode":"road","tonne_km":0,"fuels":[{"fuel":"diesel","amount":1}]}]}`,
		"no fuels":    `{"tocs":[{"id":"a","mode":"road","tonne_km":1,"fuels":[]}]}`,
		"neg amount":  `{"tocs":[{"id":"a","mode":"road","tonne_km":1,"fuels":[{"fuel":"diesel","amount":-1}]}]}`,
		"zero tonnes": `{"hocs":[{"id":"h","tonnes":0,"energy":[{"fuel":"diesel","amount":1}]}]}`,
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestBuild_UnknownFuel(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	o, err := Parse(strings.NewReader(`{"tocs":[{"id":"a","mode":"road","tonne_km":1,"fuels":[{"fuel":"unobtainium","amount":1}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(o, ds); err == nil || !strings.Contains(err.Error(), `toc "a"`) {
		t.Errorf("want error naming the toc, got %v", err)
	}
}

func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o600) }
