// Package distance provides DistanceProvider implementations.
package distance

import (
	"context"
	"math"

	"github.com/majidkabir/carbonara/pkg/calc"
)

const earthRadiusKm = 6371.0088 // mean Earth radius (IUGG)

// GreatCircle returns the great-circle (haversine) distance, scaled by a
// per-mode distance adjustment factor (DAF) to approximate real routes.
//
// Adjust defaults to 1.0 (pure great-circle). Configure it with the DAFs from
// the GLEC Framework before real use; routed providers (OSRM, SeaRoute) will
// replace this for road and sea.
type GreatCircle struct {
	Adjust map[calc.Mode]float64
}

// Distance implements calc.DistanceProvider.
func (g GreatCircle) Distance(_ context.Context, m calc.Mode, from, to calc.Location) (float64, error) {
	d := Haversine(from, to)
	if f, ok := g.Adjust[m]; ok && f > 0 {
		d *= f
	}
	return d, nil
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
