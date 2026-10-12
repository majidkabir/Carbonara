package distance

import (
	"context"
	"math"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

var (
	copenhagen = calc.Location{Name: "Copenhagen", Lat: 55.6761, Lon: 12.5683}
	hamburg    = calc.Location{Name: "Hamburg", Lat: 53.5511, Lon: 9.9937}
)

func TestHaversine(t *testing.T) {
	if d := Haversine(copenhagen, copenhagen); d != 0 {
		t.Errorf("same point = %v, want 0", d)
	}
	// Copenhagen-Hamburg is roughly 290 km as the crow flies.
	if d := Haversine(copenhagen, hamburg); math.Abs(d-290) > 5 {
		t.Errorf("CPH-HAM = %.1f km, want ~290", d)
	}
	if a, b := Haversine(copenhagen, hamburg), Haversine(hamburg, copenhagen); math.Abs(a-b) > 1e-9 {
		t.Errorf("not symmetric: %v vs %v", a, b)
	}
}

func TestGreatCircle_Adjust(t *testing.T) {
	g, err := NewGreatCircle([]Adjustment{
		{Mode: calc.Road, Multiplier: 1.2},
		{Mode: calc.Air, AddKm: 100},
		{Mode: calc.Rail, Multiplier: 1.1, AddKm: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := Haversine(copenhagen, hamburg)
	want := map[calc.Mode]float64{
		calc.Road: base * 1.2,
		calc.Air:  base + 100,
		calc.Rail: base*1.1 + 10,
		calc.Sea:  base, // no adjustment configured
	}
	for m, w := range want {
		got, _ := g.Distance(context.Background(), m, copenhagen, hamburg)
		if math.Abs(got-w) > 1e-9 {
			t.Errorf("%s = %v, want %v", m, got, w)
		}
	}
}

func TestNewGreatCircle_Invalid(t *testing.T) {
	cases := map[string][]Adjustment{
		"negative":  {{Mode: calc.Road, Multiplier: -1}},
		"duplicate": {{Mode: calc.Road, Multiplier: 1.1}, {Mode: calc.Road, Multiplier: 1.2}},
	}
	for name, adjs := range cases {
		if _, err := NewGreatCircle(adjs); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
