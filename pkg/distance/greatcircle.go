// Package distance provides DistanceProvider implementations.
package distance

import (
	"context"
	"fmt"
	"math"

	"github.com/majidkabir/carbonara/pkg/calc"
)

const earthRadiusKm = 6371.0088 // mean Earth radius (IUGG)

// Adjustment converts a great-circle distance into an estimated actual
// distance for one mode: adjusted = gcd * Multiplier + AddKm.
// A zero Multiplier means 1. Frameworks express these as distance
// adjustment factors (multiplicative) or fixed additions (e.g. for air).
type Adjustment struct {
	Mode       calc.Mode `json:"mode"`
	Multiplier float64   `json:"multiplier,omitempty"`
	AddKm      float64   `json:"add_km,omitempty"`
	Source     string    `json:"source,omitempty"`
}

// Apply returns the adjusted distance.
func (a Adjustment) Apply(gcd float64) float64 {
	m := a.Multiplier
	if m == 0 {
		m = 1
	}
	return gcd*m + a.AddKm
}

// GreatCircle returns the great-circle (haversine) distance, adjusted per
// mode when an Adjustment is configured. Without one the raw great-circle
// distance is used.
type GreatCircle struct {
	Adjust map[calc.Mode]Adjustment
}

// NewGreatCircle validates adjustments and returns a provider.
func NewGreatCircle(adjs []Adjustment) (GreatCircle, error) {
	g := GreatCircle{Adjust: make(map[calc.Mode]Adjustment, len(adjs))}
	for _, a := range adjs {
		if a.Multiplier < 0 || a.AddKm < 0 {
			return GreatCircle{}, fmt.Errorf("distance adjustment for %s: values must be >= 0", a.Mode)
		}
		if _, dup := g.Adjust[a.Mode]; dup {
			return GreatCircle{}, fmt.Errorf("duplicate distance adjustment for %s", a.Mode)
		}
		g.Adjust[a.Mode] = a
	}
	return g, nil
}

// Distance implements calc.DistanceProvider.
func (g GreatCircle) Distance(_ context.Context, m calc.Mode, from, to calc.Location) (float64, error) {
	d := Haversine(from, to)
	if a, ok := g.Adjust[m]; ok {
		d = a.Apply(d)
	}
	return d, nil
}

// DistanceMethod implements calc.DistanceMethoder.
func (g GreatCircle) DistanceMethod(m calc.Mode) string {
	if _, ok := g.Adjust[m]; ok {
		return "great_circle_adjusted"
	}
	return "great_circle"
}

// DistanceType implements calc.DistanceTyper: plain great-circle distance
// is "gcd"; once adjusted it is an estimate of unknown type.
func (g GreatCircle) DistanceType(m calc.Mode) string {
	if _, ok := g.Adjust[m]; ok {
		return ""
	}
	return calc.DistGCD
}

// Haversine returns the great-circle distance in km.
func Haversine(a, b calc.Location) float64 {
	lat1, lat2 := rad(a.Lat), rad(b.Lat)
	dLat, dLon := lat2-lat1, rad(b.Lon-a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

var _ calc.DistanceProvider = GreatCircle{}
