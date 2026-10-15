package distance

import (
	"context"
	"errors"
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
	// Not a chain of legs, so no road legs between them.
	res, err := c.Calculate(calc.WithoutRoadLegs(context.Background()), calc.Shipment{WeightKg: 1000, Legs: []calc.Leg{
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

func TestOSRM_Waterway(t *testing.T) {
	var calls int32
	var path string
	srv := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":220000}],"waypoints":[{"distance":800},{"distance":15500}]}`, 200, &calls, &path)
	o, _ := NewOSRM(srv.URL)
	o.Method, o.MaxSnapKm = "osrm_waterway", 10
	_, err := o.Distance(context.Background(), calc.InlandWaterway, copenhagen, hamburg)
	var off *calc.OffNetworkError
	if !errors.As(err, &off) || off.FromOff() || !off.ToOff() || !strings.Contains(err.Error(), "destination is 15.5 km from the waterway network") {
		t.Errorf("want snap error, got %v", err)
	}
	o.MaxSnapKm = 20
	if d, err := o.Distance(context.Background(), calc.InlandWaterway, copenhagen, hamburg); err != nil || d != 220 {
		t.Errorf("d = %v, %v", d, err)
	}
	if o.DistanceMethod(calc.InlandWaterway) != "osrm_waterway" || o.DistanceType(calc.InlandWaterway) != calc.DistSFD {
		t.Error("method or type")
	}
}

func TestOSRM_RouteParts(t *testing.T) {
	var calls int32
	var path string
	body := `{"code":"Ok","routes":[{"distance":120000,"legs":[
		{"steps":[{"distance":50000,"mode":"driving"},{"distance":19000,"mode":"ferry"},{"distance":0,"mode":"ferry"}]},
		{"steps":[{"distance":1500,"mode":"ferry"},{"distance":49500,"mode":"driving"}]}]}]}`
	srv := fakeOSRM(t, body, 200, &calls, &path)
	o, _ := NewOSRM(srv.URL)
	o.PartModes = RoadParts
	r, err := o.Route(context.Background(), calc.Road, copenhagen, hamburg)
	if err != nil || r.Km != 120 || len(r.Parts) != 1 || r.Parts[calc.PartFerry] != 20.5 {
		t.Errorf("route = %+v, %v", r, err)
	}
	// Distance and Route share the cache.
	if d, _ := o.Distance(context.Background(), calc.Road, copenhagen, hamburg); d != 120 || calls != 1 {
		t.Errorf("d = %v, calls = %d", d, calls)
	}
	// Through ByMode, and for a provider without parts.
	b := ByMode{Providers: map[calc.Mode]calc.DistanceProvider{calc.Road: o}, Fallback: GreatCircle{}}
	if r, _ := b.Route(context.Background(), calc.Road, copenhagen, hamburg); r.Parts[calc.PartFerry] != 20.5 {
		t.Errorf("by mode %+v", r)
	}
	if r, err := b.Route(context.Background(), calc.Air, copenhagen, hamburg); err != nil || r.Parts != nil || r.Km < 280 {
		t.Errorf("fallback %+v %v", r, err)
	}
}

func TestOSRM_TemporaryOrNot(t *testing.T) {
	var calls int32
	var path string
	for _, c := range []struct {
		name, body string
		status     int
		temporary  bool
	}{
		{"no route", `{"code":"NoRoute","message":"Impossible route"}`, 400, false},
		{"no segment", `{"code":"NoSegment","message":"Could not find a matching segment"}`, 400, false},
		{"bad gateway", `<html>bad gateway</html>`, 502, true},
		{"busy", `{"code":"TooBusy"}`, 503, true},
		{"rate limited", ``, 429, true},
		{"cut off", `{"code":"Ok","routes":[{"dist`, 200, true},
		{"bad request page", `<html>no</html>`, 400, false},
	} {
		srv := fakeOSRM(t, c.body, c.status, &calls, &path)
		o, _ := NewOSRM(srv.URL)
		_, err := o.Distance(context.Background(), calc.Road, copenhagen, hamburg)
		if err == nil || calc.Temporary(err) != c.temporary {
			t.Errorf("%s: error %v, temporary %v, want %v", c.name, err, calc.Temporary(err), c.temporary)
		}
		srv.Close()
	}
	// Nothing listening.
	o, _ := NewOSRM("http://127.0.0.1:1")
	if _, err := o.Distance(context.Background(), calc.Road, copenhagen, hamburg); !calc.Temporary(err) {
		t.Errorf("unreachable: %v, want temporary", err)
	}
	// A cancelled request isn't the server's fault, nor worth retrying.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := o.Distance(ctx, calc.Road, copenhagen, hamburg); calc.Temporary(err) || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
