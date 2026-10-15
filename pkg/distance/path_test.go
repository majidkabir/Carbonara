package distance

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func TestSeaNetwork_Path(t *testing.T) {
	n := loadTestNet(t)
	n.Avoid["suez"] = true
	at := func(lon, lat float64) calc.Location { return calc.Location{Lon: lon, Lat: lat} }
	r, err := n.Route(calc.WithPath(context.Background()), calc.Sea, at(0.1, 0), at(10, 0.1))
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]float64{{0.1, 0}, {0, 0}, {0, 10}, {10, 10}, {10, 0}, {10, 0.1}}
	if len(r.Path) != len(want) {
		t.Fatalf("path %v", r.Path)
	}
	for i := range want {
		if r.Path[i] != want[i] {
			t.Fatalf("path %v, want %v", r.Path, want)
		}
	}
	d, _ := n.Distance(context.Background(), calc.Sea, at(0.1, 0), at(10, 0.1))
	if math.Abs(d-r.Km) > 1e-9 {
		t.Errorf("route %v km, distance %v km", r.Km, d)
	}
	if r, _ := n.Route(context.Background(), calc.Sea, at(0.1, 0), at(10, 0.1)); r.Path != nil {
		t.Error("path without asking")
	}
}

func TestOSRM_Path(t *testing.T) {
	var calls int32
	var path string
	srv := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":1000,"geometry":{"type":"LineString","coordinates":[[12.5,55.6],[11,55],[10,53.5]]}}]}`, 200, &calls, &path)
	o, _ := NewOSRM(srv.URL)
	ctx := calc.WithPath(context.Background())
	r, err := o.Route(ctx, calc.Road, copenhagen, hamburg)
	if err != nil || len(r.Path) != 3 || r.Path[1] != [2]float64{11, 55} {
		t.Fatalf("%+v %v", r, err)
	}
	// Paths aren't cached, and a cached route doesn't stand in for one.
	if _, err := o.Route(context.Background(), calc.Road, copenhagen, hamburg); err != nil || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls %d", calls)
	}
	if r, _ := o.Route(ctx, calc.Road, copenhagen, hamburg); len(r.Path) != 3 || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("path route served from cache: calls %d", calls)
	}
}
