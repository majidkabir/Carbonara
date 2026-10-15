package distance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
)

// TableChunk is how many lanes go into one OSRM table request. With
// distinct origins and destinations that is up to 100 coordinates,
// OSRM's default --max-table-size.
const TableChunk = 50

// MaxTableWaste is how many table cells per wanted lane a chunk may cost
// before Prefetch leaves its lanes to single routes.
const MaxTableWaste = 4

// tableParallel is how many table requests run at once.
const tableParallel = 4

// Prefetch implements calc.Prefetcher: it fills the route cache from OSRM
// distance tables, which cost far less per lane than single routes.
//
// Tables give distances only. When the provider reports ferry crossings
// (PartModes has only "ferry"), each chunk is also fetched with ferries
// excluded (OSRM's car profile marks them excludable); lanes whose distance
// is the same both ways have no ferry and are cached, and the rest are
// left to Route, which fetches their steps. Providers with other parts
// (rail electrification) need steps for every lane and don't prefetch.
func (o *OSRM) Prefetch(ctx context.Context, lanes []calc.Lane) {
	ferryCheck := false
	for mode, kind := range o.PartModes {
		if mode != "ferry" || kind != calc.PartFerry {
			return
		}
		ferryCheck = true
	}
	var todo []calc.Lane
	o.mu.Lock()
	for _, l := range lanes {
		if _, ok := o.cache[cacheKey(l.From, l.To)]; !ok {
			todo = append(todo, l)
		}
	}
	o.mu.Unlock()
	// Lanes from the same origin share a table row: group them.
	sort.SliceStable(todo, func(i, j int) bool {
		a, b := todo[i].From, todo[j].From
		return a.Lat < b.Lat || (a.Lat == b.Lat && a.Lon < b.Lon)
	})

	sem := make(chan struct{}, tableParallel)
	var wg sync.WaitGroup
	for i := 0; i < len(todo); i += TableChunk {
		chunk := todo[i:min(i+TableChunk, len(todo))]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			o.prefetchChunk(ctx, chunk, ferryCheck)
		}()
	}
	wg.Wait()
}

func cacheKey(from, to calc.Location) string {
	return fmt.Sprintf("%.5f,%.5f;%.5f,%.5f", from.Lon, from.Lat, to.Lon, to.Lat)
}

func (o *OSRM) prefetchChunk(ctx context.Context, lanes []calc.Lane, ferryCheck bool) {
	// Distinct coordinates: origins first, then destinations.
	var coords []string
	idx := map[string]int{}
	add := func(l calc.Location) int {
		k := fmt.Sprintf("%.5f,%.5f", l.Lon, l.Lat)
		if i, ok := idx[k]; ok {
			return i
		}
		idx[k] = len(coords)
		coords = append(coords, k)
		return len(coords) - 1
	}
	src := make([]int, len(lanes))
	for i, l := range lanes {
		src[i] = add(l.From)
	}
	nSrc := len(coords)
	idx = map[string]int{} // destinations get their own indices
	dst := make([]int, len(lanes))
	for i, l := range lanes {
		dst[i] = add(l.To) - nSrc
	}
	// A table computes every origin-destination cell. When the lanes use
	// only a few of them (scattered, unrelated lanes), single routes are
	// cheaper, especially on MLD, whose many-to-many tables over long
	// distances are slow: leave such chunks to Route.
	if nSrc*(len(coords)-nSrc) > MaxTableWaste*len(lanes) {
		return
	}

	all, snapsS, snapsD, err := o.table(ctx, coords, nSrc, "")
	if err != nil {
		return
	}
	var noFerry [][]*float64
	if ferryCheck {
		if noFerry, _, _, err = o.table(ctx, coords, nSrc, "ferry"); err != nil {
			return // e.g. the profile can't exclude ferries: leave all to Route
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cache == nil || len(o.cache)+len(lanes) > maxCacheEntries {
		o.cache = make(map[string]calc.Route)
	}
	for i, l := range lanes {
		d := all[src[i]][dst[i]]
		if d == nil {
			continue // no route: let Route report it
		}
		if o.MaxSnapKm > 0 && (snapsS[src[i]]/1000 > o.MaxSnapKm || snapsD[dst[i]]/1000 > o.MaxSnapKm) {
			continue // let Route report the snap error
		}
		if ferryCheck {
			nf := noFerry[src[i]][dst[i]]
			if nf == nil || math.Abs(*nf-*d) > 1 {
				continue // the route uses a ferry: Route fetches its steps
			}
		}
		o.cache[cacheKey(l.From, l.To)] = calc.Route{Km: *d / 1000}
	}
}

// table fetches an OSRM distance table from the first nSrc coordinates to
// the rest, with snap distances, optionally excluding a road class.
func (o *OSRM) table(ctx context.Context, coords []string, nSrc int, exclude string) ([][]*float64, []float64, []float64, error) {
	profile := o.Profile
	if profile == "" {
		profile = "driving"
	}
	var s, d []string
	for i := range coords {
		if i < nSrc {
			s = append(s, strconv.Itoa(i))
		} else {
			d = append(d, strconv.Itoa(i))
		}
	}
	u := fmt.Sprintf("%s/table/v1/%s/%s?annotations=distance&sources=%s&destinations=%s",
		o.BaseURL, url.PathEscape(profile), strings.Join(coords, ";"), strings.Join(s, ";"), strings.Join(d, ";"))
	if exclude != "" {
		u += "&exclude=" + url.QueryEscape(exclude)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Code      string       `json:"code"`
		Distances [][]*float64 `json:"distances"`
		Sources   []struct {
			Distance float64 `json:"distance"`
		} `json:"sources"`
		Destinations []struct {
			Distance float64 `json:"distance"`
		} `json:"destinations"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse*4)).Decode(&body); err != nil {
		return nil, nil, nil, err
	}
	if body.Code != "Ok" || len(body.Distances) != nSrc || len(body.Sources) != nSrc || len(body.Destinations) != len(coords)-nSrc {
		return nil, nil, nil, fmt.Errorf("osrm table: %s", body.Code)
	}
	for _, row := range body.Distances {
		if len(row) != len(coords)-nSrc {
			return nil, nil, nil, fmt.Errorf("osrm table: bad row")
		}
	}
	ss := make([]float64, nSrc)
	for i, x := range body.Sources {
		ss[i] = x.Distance
	}
	ds := make([]float64, len(body.Destinations))
	for i, x := range body.Destinations {
		ds[i] = x.Distance
	}
	return body.Distances, ss, ds, nil
}

// Prefetch implements calc.Prefetcher, passing each mode's lanes to its
// provider when that provider prefetches.
func (b ByMode) Prefetch(ctx context.Context, lanes []calc.Lane) {
	byMode := map[calc.Mode][]calc.Lane{}
	for _, l := range lanes {
		byMode[l.Mode] = append(byMode[l.Mode], l)
	}
	for m, ls := range byMode {
		if p, ok := b.pick(m).(calc.Prefetcher); ok {
			p.Prefetch(ctx, ls)
		}
	}
}
