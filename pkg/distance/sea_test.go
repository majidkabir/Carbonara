package distance

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/majidkabir/sootcase/pkg/calc"
)

// A small network on the equator: a long way round A-B-C-D, and a
// shortcut A-D through "suez". Plus a line crossing the date line written
// as 179 -> 180.5, which must join a line starting at -179.5.
const testNet = `{"type":"FeatureCollection","features":[
 {"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[0,0],[0,10],[10,10],[10,0]]}},
 {"type":"Feature","properties":{"pass":"Suez"},"geometry":{"type":"LineString","coordinates":[[0,0],[10,0]]}},
 {"type":"Feature","properties":{"pass":null},"geometry":{"type":"MultiLineString","coordinates":[[[10,0],[20,0]],[[20,0],[30,0]]]}},
 {"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[170,0],[179,0],[180.5,0]]}},
 {"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[-179.5,0],[-170,0]]}},
 {"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[5,5]}}
]}`

func loadTestNet(t *testing.T) *SeaNetwork {
	t.Helper()
	n, err := LoadSeaNetwork(strings.NewReader(testNet))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func km(lon1, lat1, lon2, lat2 float64) float64 {
	return Haversine(calc.Location{Lon: lon1, Lat: lat1}, calc.Location{Lon: lon2, Lat: lat2})
}

func TestSeaNetwork_Routes(t *testing.T) {
	n := loadTestNet(t)
	ctx := context.Background()
	at := func(lon, lat float64) calc.Location { return calc.Location{Lon: lon, Lat: lat} }

	short := km(0, 0, 10, 0)
	long := km(0, 0, 0, 10) + km(0, 10, 10, 10) + km(10, 10, 10, 0)

	d, err := n.Distance(ctx, calc.Sea, at(0, 0), at(10, 0))
	if err != nil || math.Abs(d-short) > 1e-6 {
		t.Errorf("via suez = %v, %v; want %v", d, err, short)
	}

	n.Avoid["suez"] = true
	d, err = n.Distance(ctx, calc.Sea, at(0, 0), at(10, 0))
	if err != nil || math.Abs(d-long) > 1e-6 {
		t.Errorf("avoiding suez = %v, %v; want %v", d, err, long)
	}
	delete(n.Avoid, "suez")

	// MultiLineString is followed.
	if d, err := n.Distance(ctx, calc.Sea, at(0, 0), at(30, 0)); err != nil || math.Abs(d-short-km(10, 0, 30, 0)) > 1e-6 {
		t.Errorf("multiline = %v, %v", d, err)
	}

	// Across the date line: 170 -> -170 is 20 degrees, not 340.
	if d, err := n.Distance(ctx, calc.Sea, at(170, 0), at(-170, 0)); err != nil || math.Abs(d-km(170, 0, -170, 0)) > 1e-6 {
		t.Errorf("date line = %v, %v; want %v", d, err, km(170, 0, -170, 0))
	}

	// Off-network points add their snap distance.
	if d, _ := n.Distance(ctx, calc.Sea, at(0, -1), at(10, 0)); math.Abs(d-short-km(0, -1, 0, 0)) > 1e-6 {
		t.Errorf("snap = %v", d)
	}

	if n.DistanceType(calc.Sea) != calc.DistSFD {
		t.Error("sea network distances are shortest feasible")
	}
	if got := n.DistanceMethod(calc.Sea); got != "sea_network" {
		t.Errorf("method = %q", got)
	}
	if got := strings.Join(n.Passes(), ","); got != "suez" {
		t.Errorf("passes = %q (names are lowercased)", got)
	}
}

func TestSeaNetwork_Errors(t *testing.T) {
	n := loadTestNet(t)
	ctx := context.Background()
	at := func(lon, lat float64) calc.Location { return calc.Location{Lon: lon, Lat: lat} }

	// Disconnected parts.
	if _, err := n.Distance(ctx, calc.Sea, at(0, 0), at(170, 0)); err == nil {
		t.Error("want no-route error between disconnected parts")
	}
	n.MaxSnapKm = 50
	if _, err := n.Distance(ctx, calc.Sea, at(5, 5), at(10, 0)); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Errorf("want snap error, got %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	n.MaxSnapKm = 0
	if _, err := n.Distance(cancelled, calc.Sea, at(0, 0), at(30, 0)); err == nil {
		t.Error("want context error")
	}

	for name, in := range map[string]string{
		"not json":       `{`,
		"not collection": `{"type":"Feature"}`,
		"no lines":       `{"type":"FeatureCollection","features":[]}`,
		"bad coord":      `{"type":"FeatureCollection","features":[{"geometry":{"type":"LineString","coordinates":[[1],[2,3]]}}]}`,
	} {
		if _, err := LoadSeaNetwork(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNormLon(t *testing.T) {
	for in, want := range map[float64]float64{0: 0, 180: 180, -180: 180, 180.5: -179.5, -179.5: -179.5, 359: -1, 540: 180} {
		if got := normLon(in); math.Abs(got-want) > 1e-9 {
			t.Errorf("normLon(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestDefaultAvoid(t *testing.T) {
	n := loadTestNet(t)
	if !n.Avoid["northwest"] || !n.Avoid["northeast"] {
		t.Error("Arctic passages should be closed by default")
	}
}
