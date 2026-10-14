package distance

import (
	"context"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func TestRegions(t *testing.T) {
	var eCalls, wCalls int32
	var p1, p2 string
	europe := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":1000,"duration":1}]}`, 200, &eCalls, &p1)
	world := fakeOSRM(t, `{"code":"Ok","routes":[{"distance":2000,"duration":1}]}`, 200, &wCalls, &p2)
	rs, err := ParseRegions("europe="+europe.URL+"@-25,34,45,72; big="+world.URL+"@-30,-40,80,75", func(o *OSRM) { o.MaxSnapKm = 1 })
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].OSRM.MaxSnapKm != 1 {
		t.Fatalf("parsed %+v", rs)
	}
	ctx := context.Background()
	// Both ends in both regions: the smaller one routes.
	if d, err := rs.Distance(ctx, calc.Road, copenhagen, hamburg); err != nil || d != 1 || eCalls != 1 || wCalls != 0 {
		t.Fatalf("europe: %v %v calls %d/%d", d, err, eCalls, wCalls)
	}
	// One end outside the small region: the big one routes.
	cairo := calc.Location{Lat: 30.04, Lon: 31.24}
	if d, err := rs.Distance(ctx, calc.Road, copenhagen, cairo); err != nil || d != 2 || wCalls != 1 {
		t.Fatalf("big: %v %v", d, err)
	}
	// No region holds both.
	beijing := calc.Location{Lat: 39.9, Lon: 116.4}
	if _, err := rs.Distance(ctx, calc.Road, copenhagen, beijing); err == nil || !strings.Contains(err.Error(), "no routing region") {
		t.Fatalf("outside: %v", err)
	}
	if rs.DistanceMethod(calc.Road) != "osrm" || rs.DistanceType(calc.Road) != calc.DistSFD {
		t.Error("method or type")
	}
	for _, bad := range []string{"", "x", "eu=http://a@1,2,3", "eu=http://a@3,0,1,5", "eu=ftp://a@0,0,1,1", "eu=http://a@0,0,1,x"} {
		if _, err := ParseRegions(bad, nil); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
