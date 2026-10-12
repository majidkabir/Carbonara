package calc

import (
	"context"
	"errors"
	"math"
	"testing"
)

type fakeFactors struct {
	legs map[string]Factor
	hubs map[string]HubFactor
}

func (f fakeFactors) LegFactor(m Mode, vt string) (Factor, error) {
	if x, ok := f.legs[string(m)+"/"+vt]; ok {
		return x, nil
	}
	return Factor{}, errors.New("no factor")
}

func (f fakeFactors) HubFactor(t string) (HubFactor, error) {
	if x, ok := f.hubs[t]; ok {
		return x, nil
	}
	return HubFactor{}, errors.New("no hub factor")
}

type fixedDistance float64

func (d fixedDistance) Distance(context.Context, Mode, Location, Location) (float64, error) {
	return float64(d), nil
}

var testFactors = fakeFactors{
	legs: map[string]Factor{
		"road/truck_40t": {Mode: Road, VehicleType: "truck_40t", TTW: 60, WTT: 15},
		"sea/container":  {Mode: Sea, VehicleType: "container", TTW: 8, WTT: 2},
	},
	hubs: map[string]HubFactor{"container_terminal": {HubType: "container_terminal", WTWPerTonneG: 1000}},
}

const eps = 1e-9

func approx(a, b float64) bool { return math.Abs(a-b) < eps }

// Golden tests: hand-computed expected values. Add GLEC Framework worked
// examples here as they are transcribed.
func TestCalculate_Golden(t *testing.T) {
	tests := []struct {
		name                      string
		shipment                  Shipment
		wantTkm, wantTTW, wantWTW float64
		wantIntensity             float64
	}{
		{
			name: "single road leg: 10 t x 500 km",
			shipment: Shipment{ID: "s1", WeightKg: 10_000, Legs: []Leg{
				{ID: "l1", Mode: Road, VehicleType: "truck_40t", DistanceKm: 500},
			}},
			wantTkm: 5000, wantTTW: 300, wantWTW: 375, wantIntensity: 75,
		},
		{
			name: "road + sea + terminal: 20 t",
			shipment: Shipment{ID: "s2", WeightKg: 20_000,
				Legs: []Leg{
					{ID: "l1", Mode: Road, VehicleType: "truck_40t", DistanceKm: 100},
					{ID: "l2", Mode: Sea, VehicleType: "container", DistanceKm: 1000},
				},
				Hubs: []HubStop{{ID: "h1", HubType: "container_terminal"}},
			},
			// road: 2000 tkm -> TTW 120, WTW 150; sea: 20000 tkm -> TTW 160, WTW 200; hub: 20 kg
			wantTkm: 22000, wantTTW: 280, wantWTW: 370, wantIntensity: 350.0 * 1000 / 22000,
		},
	}
	c := Calculator{Factors: testFactors}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := c.Calculate(context.Background(), tt.shipment)
			if err != nil {
				t.Fatal(err)
			}
			if !approx(r.TonneKm, tt.wantTkm) || !approx(r.TTWKg, tt.wantTTW) ||
				!approx(r.WTWKg, tt.wantWTW) || !approx(r.IntensityG, tt.wantIntensity) {
				t.Errorf("got tkm=%v ttw=%v wtw=%v int=%v, want %v %v %v %v",
					r.TonneKm, r.TTWKg, r.WTWKg, r.IntensityG,
					tt.wantTkm, tt.wantTTW, tt.wantWTW, tt.wantIntensity)
			}
		})
	}
}

func TestCalculate_UsesDistanceProvider(t *testing.T) {
	c := Calculator{Factors: testFactors, Distances: fixedDistance(250)}
	r, err := c.Calculate(context.Background(), Shipment{WeightKg: 1000, Legs: []Leg{
		{ID: "l1", Mode: Road, VehicleType: "truck_40t"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !approx(r.Legs[0].DistanceKm, 250) {
		t.Errorf("distance = %v, want 250", r.Legs[0].DistanceKm)
	}
}

func TestCalculate_Errors(t *testing.T) {
	c := Calculator{Factors: testFactors}
	ctx := context.Background()
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 0, Legs: []Leg{{}}}); !errors.Is(err, ErrInvalidWeight) {
		t.Errorf("want ErrInvalidWeight, got %v", err)
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1}); !errors.Is(err, ErrNoLegs) {
		t.Errorf("want ErrNoLegs, got %v", err)
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{ID: "x", Mode: Air, DistanceKm: 1}}}); err == nil {
		t.Error("want error for missing factor")
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{ID: "x", Mode: Road, VehicleType: "truck_40t"}}}); err == nil {
		t.Error("want error for missing distance")
	}
}

// Property: more weight or more distance never means fewer emissions.
func FuzzCalculate_Monotonic(f *testing.F) {
	f.Add(1000.0, 100.0, 500.0, 50.0)
	c := Calculator{Factors: testFactors}
	f.Fuzz(func(t *testing.T, w, d, dw, dd float64) {
		if !(w > 0 && w < 1e7 && d > 0 && d < 1e5 && dw >= 0 && dw < 1e7 && dd >= 0 && dd < 1e5) {
			t.Skip()
		}
		run := func(w, d float64) float64 {
			r, err := c.Calculate(context.Background(), Shipment{WeightKg: w, Legs: []Leg{
				{ID: "l", Mode: Road, VehicleType: "truck_40t", DistanceKm: d},
			}})
			if err != nil {
				t.Fatal(err)
			}
			return r.WTWKg
		}
		if run(w+dw, d+dd) < run(w, d) {
			t.Errorf("emissions decreased: w=%v d=%v dw=%v dd=%v", w, d, dw, dd)
		}
	})
}
