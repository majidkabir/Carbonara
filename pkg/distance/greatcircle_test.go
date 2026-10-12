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
	g := GreatCircle{Adjust: map[calc.Mode]float64{calc.Road: 1.2}}
	base := Haversine(copenhagen, hamburg)
	road, _ := g.Distance(context.Background(), calc.Road, copenhagen, hamburg)
	sea, _ := g.Distance(context.Background(), calc.Sea, copenhagen, hamburg)
	if math.Abs(road-base*1.2) > 1e-9 || sea != base {
		t.Errorf("road=%v sea=%v base=%v", road, sea, base)
	}
}
