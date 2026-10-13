package distance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

// fakeOSRM answers /route/v1/... like an OSRM server and counts calls.
func fakeOSRM(t *testing.T, body string, status int, calls *int32, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		*gotPath = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOSRM_Distance(t *testing.T) {
	var calls int32
	var path string
	srv := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":312456.7,"duration":12000}]}`, 200, &calls, &path)
	o, err := NewOSRM(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	d, err := o.Distance(context.Background(), calc.Road, copenhagen, hamburg)
	if err != nil {
		t.Fatal(err)
	}
	if d != 312.4567 {
		t.Errorf("distance = %v km, want 312.4567", d)
	}
	// OSRM takes lon,lat order.
	if want := "/route/v1/driving/12.56830,55.67610;9.99370,53.55110"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	// Second identical call is served from cache.
	if _, err := o.Distance(context.Background(), calc.Road, copenhagen, hamburg); err != nil || calls != 1 {
		t.Errorf("calls = %d, err = %v; want cached", calls, err)
	}
	if o.DistanceMethod(calc.Road) != "osrm" {
		t.Error("method name")
	}
}

func TestOSRM_Errors(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
		want   string
	}{
		"no route": {`{"code":"NoRoute","message":"Impossible route between points"}`, 400, "NoRoute"},
		"empty":    {`{"code":"Ok","routes":[]}`, 200, "no route"},
		"not json": {`<html>bad gateway</html>`, 502, "HTTP 502"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var calls int32
			var path string
			srv := fakeOSRM(t, tc.body, tc.status, &calls, &path)
			o, _ := NewOSRM(srv.URL)
			_, err := o.Distance(context.Background(), calc.Road, copenhagen, hamburg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
	for _, bad := range []string{"", "localhost:5000", "ftp://x", "http://"} {
		if _, err := NewOSRM(bad); err == nil {
			t.Errorf("NewOSRM(%q): want error", bad)
		}
	}
}

func TestByMode(t *testing.T) {
	var calls int32
	var path string
	srv := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":100000}]}`, 200, &calls, &path)
	o, _ := NewOSRM(srv.URL)
	gc, _ := NewGreatCircle([]Adjustment{{Mode: calc.Air, AddKm: 50}})
	b := ByMode{Providers: map[calc.Mode]calc.DistanceProvider{calc.Road: o}, Fallback: gc}

	c := calc.Calculator{Factors: fakeFactors{}, Distances: b}
	res, err := c.Calculate(context.Background(), calc.Shipment{WeightKg: 1000, Legs: []calc.Leg{
		{ID: "road", Mode: calc.Road, VehicleType: "x", From: copenhagen, To: hamburg},
		{ID: "rail", Mode: calc.Rail, VehicleType: "x", From: copenhagen, To: hamburg},
		{ID: "air", Mode: calc.Air, VehicleType: "x", From: copenhagen, To: hamburg},
		{ID: "given", Mode: calc.Road, VehicleType: "x", DistanceKm: 7},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if b.DistanceType(calc.Road) != calc.DistSFD || b.DistanceType(calc.Rail) != calc.DistGCD || b.DistanceType(calc.Air) != "" {
		t.Errorf("distance types: road %q rail %q air %q", b.DistanceType(calc.Road), b.DistanceType(calc.Rail), b.DistanceType(calc.Air))
	}
	want := map[string]struct {
		km     float64
		method string
	}{
		"road":  {100, "osrm"},
		"rail":  {Haversine(copenhagen, hamburg), "great_circle"},
		"air":   {Haversine(copenhagen, hamburg) + 50, "great_circle_adjusted"},
		"given": {7, calc.DistanceGiven},
	}
	for _, l := range res.Legs {
		w := want[l.LegID]
		if l.DistanceKm != w.km || l.DistanceMethod != w.method {
			t.Errorf("%s: %v km via %q, want %v via %q", l.LegID, l.DistanceKm, l.DistanceMethod, w.km, w.method)
		}
	}
}

// fakeFactors returns a 1 g/tkm factor for anything.
type fakeFactors struct{}

func (fakeFactors) LegFactor(m calc.Mode, vt string) (calc.Factor, error) {
	return calc.Factor{Mode: m, VehicleType: vt, TTW: 1}, nil
}
func (fakeFactors) HubFactor(string) (calc.HubFactor, error)   { return calc.HubFactor{}, nil }
func (fakeFactors) FuelFactor(string) (calc.FuelFactor, error) { return calc.FuelFactor{}, nil }
