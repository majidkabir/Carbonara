package calc

import (
	"context"
	"errors"
	"testing"
)

type withGrid struct{ fakeFactors }

func (withGrid) Grid(c string) (GridFactor, error) {
	if c == "DNK" {
		return GridFactor{Country: "DNK", Year: 2025, G: 100}, nil
	}
	return GridFactor{}, errors.New("unknown country")
}

func TestCalculate_Energy(t *testing.T) {
	c := Calculator{Factors: withGrid{testFactors}}
	ctx := context.Background()
	// 20 t x 300 km = 6000 tkm; 0.03 kWh/tkm x 100 g/kWh = 3 g/tkm -> 18 kg, all WTT
	r, err := c.Calculate(ctx, Shipment{WeightKg: 20_000, Legs: []Leg{
		{ID: "l", Mode: Rail, VehicleType: "electric", DistanceKm: 300, EnergyKWhPerTkm: 0.03, Country: "DNK"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	l := r.Legs[0]
	if l.Method != MethodEnergy || l.TTWKg != 0 || !approx(l.WTWKg, 18) || l.Country != "DNK" || l.Factor.WTT != 3 {
		t.Errorf("got %+v", l)
	}

	fu := FuelUse{Fuel: "diesel", Amount: 1}
	bad := map[string]Leg{
		"no country":      {Mode: Rail, DistanceKm: 1, EnergyKWhPerTkm: 0.03},
		"country only":    {Mode: Rail, DistanceKm: 1, Country: "DNK"},
		"negative energy": {Mode: Rail, DistanceKm: 1, EnergyKWhPerTkm: -1, Country: "DNK"},
	}
	for name, leg := range bad {
		if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{leg}}); !errors.Is(err, ErrInvalidEnergy) {
			t.Errorf("%s: want ErrInvalidEnergy, got %v", name, err)
		}
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{Mode: Rail, DistanceKm: 1, EnergyKWhPerTkm: 0.03, Country: "XXX"}}}); err == nil {
		t.Error("unknown country: want error")
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{Mode: Rail, DistanceKm: 1, EnergyKWhPerTkm: 0.03, Country: "DNK", Fuel: &fu}}}); err == nil {
		t.Error("energy and fuel: want error")
	}
	plain := Calculator{Factors: testFactors}
	if _, err := plain.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{Mode: Rail, DistanceKm: 1, EnergyKWhPerTkm: 0.03, Country: "DNK"}}}); !errors.Is(err, ErrInvalidEnergy) {
		t.Errorf("no grids: want ErrInvalidEnergy, got %v", err)
	}
}
