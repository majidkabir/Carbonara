package calc

import (
	"context"
	"errors"
	"testing"
)

var truck = LoadModel{
	Mode: Road, VehicleType: "truck", PayloadT: 20,
	Points: []LoadPoint{{0, 600, 100}, {1, 1000, 200}},
}

func TestLoadModel_PerTonneKm(t *testing.T) {
	cases := []struct {
		lf, etf, ttw, wtt float64
	}{
		{1, 0, 50, 10},      // 1000/20, 200/20
		{0.5, 0, 80, 15},    // (800)/10, (150)/10
		{0.5, 0.5, 110, 20}, // (800+0.5*600)/10, (150+0.5*100)/10
		{0.25, 1, 260, 45},  // (700+600)/5, (125+100)/5
	}
	for _, c := range cases {
		ttw, wtt, err := truck.PerTonneKm(c.lf, c.etf)
		if err != nil || !approx(ttw, c.ttw) || !approx(wtt, c.wtt) {
			t.Errorf("lf=%v etf=%v: got %v %v %v, want %v %v", c.lf, c.etf, ttw, wtt, err, c.ttw, c.wtt)
		}
	}
	for _, bad := range [][2]float64{{0, 0}, {1.1, 0}, {-0.5, 0}, {0.5, -1}} {
		if _, _, err := truck.PerTonneKm(bad[0], bad[1]); !errors.Is(err, ErrInvalidLoad) {
			t.Errorf("lf=%v etf=%v: want ErrInvalidLoad, got %v", bad[0], bad[1], err)
		}
	}
}

func TestLoadModel_PiecewiseThroughMidpoint(t *testing.T) {
	m := LoadModel{PayloadT: 10, Points: []LoadPoint{{0, 100, 0}, {0.5, 300, 0}, {1, 400, 0}}}
	if ttw, _, _ := m.PerTonneKm(0.5, 0); !approx(ttw, 60) { // 300 / 5
		t.Errorf("at 0.5 = %v, want 60", ttw)
	}
	if ttw, _, _ := m.PerTonneKm(0.75, 0); !approx(ttw, 350/7.5) {
		t.Errorf("at 0.75 = %v, want %v", ttw, 350/7.5)
	}
}

func TestLoadModel_Validate(t *testing.T) {
	bad := map[string]LoadModel{
		"no payload": {Points: truck.Points},
		"one point":  {PayloadT: 1, Points: truck.Points[:1]},
		"unsorted":   {PayloadT: 1, Points: []LoadPoint{{1, 1, 1}, {0, 1, 1}}},
		"no zero":    {PayloadT: 1, Points: []LoadPoint{{0.1, 1, 1}, {1, 1, 1}}},
		"duplicate":  {PayloadT: 1, Points: []LoadPoint{{0, 1, 1}, {0, 1, 1}, {1, 1, 1}}},
		"negative":   {PayloadT: 1, Points: []LoadPoint{{0, -1, 1}, {1, 1, 1}}},
	}
	for name, m := range bad {
		if m.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := truck.Validate(); err != nil {
		t.Error(err)
	}
}

type withModels struct {
	fakeFactors
}

func (withModels) LoadModel(m Mode, vt string) (LoadModel, error) {
	if m == Road && vt == "truck" {
		return truck, nil
	}
	return LoadModel{}, errors.New("no model")
}

func TestCalculate_LoadModel(t *testing.T) {
	c := Calculator{Factors: withModels{testFactors}}
	ctx := context.Background()
	// 10 t x 100 km = 1000 tkm; lf 0.5 etf 0.5: 110 TTW + 20 WTT g/tkm
	r, err := c.Calculate(ctx, Shipment{WeightKg: 10_000, Legs: []Leg{
		{ID: "l", Mode: Road, VehicleType: "truck", DistanceKm: 100, LoadFactor: 0.5, EmptyTripFactor: 0.5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	l := r.Legs[0]
	if l.Method != MethodLoadModel || !approx(l.TTWKg, 110) || !approx(l.WTWKg, 130) ||
		l.LoadFactor != 0.5 || l.EmptyTripFactor != 0.5 || l.PayloadT != 20 || l.Factor == nil {
		t.Errorf("got %+v", l)
	}

	fu := FuelUse{Fuel: "diesel", Amount: 1}
	bad := map[string]Leg{
		"etf without lf":  {Mode: Road, VehicleType: "truck", DistanceKm: 1, EmptyTripFactor: 0.3},
		"fuel and lf":     {Mode: Road, VehicleType: "truck", DistanceKm: 1, LoadFactor: 0.5, Fuel: &fu},
		"lf out of range": {Mode: Road, VehicleType: "truck", DistanceKm: 1, LoadFactor: 2},
	}
	for name, leg := range bad {
		if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{leg}}); !errors.Is(err, ErrInvalidLoad) {
			t.Errorf("%s: want ErrInvalidLoad, got %v", name, err)
		}
	}
	if _, err := c.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{Mode: Road, VehicleType: "bike", DistanceKm: 1, LoadFactor: 0.5}}}); err == nil {
		t.Error("want error for unknown model")
	}
	plain := Calculator{Factors: testFactors}
	if _, err := plain.Calculate(ctx, Shipment{WeightKg: 1, Legs: []Leg{{Mode: Road, VehicleType: "truck", DistanceKm: 1, LoadFactor: 0.5}}}); !errors.Is(err, ErrInvalidLoad) {
		t.Errorf("want ErrInvalidLoad without models, got %v", err)
	}
}
