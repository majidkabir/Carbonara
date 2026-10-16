package distance

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
)

// Region is an OSRM server covering a bounding box.
type Region struct {
	Name                           string
	MinLon, MinLat, MaxLon, MaxLat float64
	OSRM                           *OSRM
}

func (r Region) contains(l calc.Location) bool {
	return l.Lon >= r.MinLon && l.Lon <= r.MaxLon && l.Lat >= r.MinLat && l.Lat <= r.MaxLat
}

func (r Region) area() float64 { return (r.MaxLon - r.MinLon) * (r.MaxLat - r.MinLat) }

// Regions routes with one of several OSRM servers, each built for part of
// the world. A road graph of the whole planet needs more memory than most
// servers have; overlapping regions (say Europe with the Middle East, Asia
// with the Middle East) each fit, and a route is taken from the smallest
// region that holds both of its ends. When none does, the route is joined
// where two regions overlap (see regions_via.go).
type Regions []Region

// ParseRegions reads "name=url@minlon,minlat,maxlon,maxlat;name=url@...".
// configure, if set, is applied to each region's OSRM client.
func ParseRegions(spec string, configure func(*OSRM)) (Regions, error) {
	var rs Regions
	for _, part := range strings.Split(spec, ";") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		name, rest, ok1 := strings.Cut(part, "=")
		u, box, ok2 := strings.Cut(rest, "@")
		f := strings.Split(box, ",")
		if !ok1 || !ok2 || name == "" || len(f) != 4 {
			return nil, fmt.Errorf("osrm regions: want name=url@minlon,minlat,maxlon,maxlat, got %q", part)
		}
		var v [4]float64
		for i, s := range f {
			x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, fmt.Errorf("osrm regions: %s: bad number %q", name, s)
			}
			v[i] = x
		}
		if v[0] >= v[2] || v[1] >= v[3] || v[0] < -180 || v[2] > 180 || v[1] < -90 || v[3] > 90 {
			return nil, fmt.Errorf("osrm regions: %s: bounding box %v is not min < max within the world", name, v)
		}
		o, err := NewOSRM(u)
		if err != nil {
			return nil, fmt.Errorf("osrm regions: %s: %w", name, err)
		}
		if configure != nil {
			configure(o)
		}
		rs = append(rs, Region{Name: name, MinLon: v[0], MinLat: v[1], MaxLon: v[2], MaxLat: v[3], OSRM: o})
	}
	if len(rs) == 0 {
		return nil, fmt.Errorf("osrm regions: none given")
	}
	return rs, nil
}

// pick returns the smallest region holding both ends.
func (rs Regions) pick(from, to calc.Location) (*Region, error) {
	var best *Region
	for i := range rs {
		r := &rs[i]
		if r.contains(from) && r.contains(to) && (best == nil || r.area() < best.area()) {
			best = r
		}
	}
	if best == nil {
		names := make([]string, len(rs))
		for i, r := range rs {
			names[i] = r.Name
		}
		return nil, fmt.Errorf("osrm: no routing region holds both %.4f,%.4f and %.4f,%.4f (regions: %s)",
			from.Lat, from.Lon, to.Lat, to.Lon, strings.Join(names, ", "))
	}
	return best, nil
}

// Distance implements calc.DistanceProvider.
func (rs Regions) Distance(ctx context.Context, m calc.Mode, from, to calc.Location) (float64, error) {
	r, err := rs.Route(ctx, m, from, to)
	return r.Km, err
}

// Route implements calc.Router.
func (rs Regions) Route(ctx context.Context, m calc.Mode, from, to calc.Location) (calc.Route, error) {
	r, err := rs.pick(from, to)
	if err != nil {
		return rs.routeVia(ctx, m, from, to, err)
	}
	return r.OSRM.Route(ctx, m, from, to)
}

// DistanceMethod implements calc.DistanceMethoder.
func (rs Regions) DistanceMethod(m calc.Mode) string { return rs[0].OSRM.DistanceMethod(m) }

// DistanceType implements calc.DistanceTyper.
func (rs Regions) DistanceType(calc.Mode) string { return calc.DistSFD }

// Prefetch implements calc.Prefetcher, region by region.
func (rs Regions) Prefetch(ctx context.Context, lanes []calc.Lane) {
	by := map[*Region][]calc.Lane{}
	for _, l := range lanes {
		if r, err := rs.pick(l.From, l.To); err == nil {
			by[r] = append(by[r], l)
		}
	}
	for r, ls := range by {
		r.OSRM.Prefetch(ctx, ls)
	}
}
