package distance

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func FuzzSeaNetwork(f *testing.F) {
	f.Add(testNet, 0.0, 0.0, 30.0, 0.0)
	f.Add(`{"type":"FeatureCollection","features":[{"geometry":{"type":"MultiLineString","coordinates":[[[1,2],[3,4]],[]]}}]}`, 1.0, 2.0, 3.0, 4.0)
	f.Fuzz(func(t *testing.T, net string, lon1, lat1, lon2, lat2 float64) {
		n, err := LoadSeaNetwork(strings.NewReader(net))
		if err != nil {
			return
		}
		d, err := n.Distance(context.Background(), calc.Sea, calc.Location{Lon: lon1, Lat: lat1}, calc.Location{Lon: lon2, Lat: lat2})
		if err == nil && (d < 0 || math.IsInf(d, 1)) {
			t.Fatalf("distance %v", d)
		}
	})
}
