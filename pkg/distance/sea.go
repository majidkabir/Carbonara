package distance

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/majidkabir/sootcase/pkg/calc"
)

// SeaNetwork routes sea legs over a maritime line network, such as
// Eurostat's SeaRoute "marnet" (EUPL-1.2, not bundled; see README).
//
// Origin and destination are snapped to the nearest network node; the
// distance is snap-in + shortest path + snap-out.
type SeaNetwork struct {
	nodes []calc.Location
	adj   [][]seaEdge
	grid  struct {
		once  sync.Once
		cells map[[2]int][]int // connected nodes by 1° cell
	}
	// Avoid lists passages (the "pass" property) that routes must not use.
	Avoid map[string]bool
	// MaxSnapKm rejects points farther than this from the network
	// (e.g. an inland coordinate). 0 means no limit.
	MaxSnapKm float64
}

type seaEdge struct {
	to   int
	km   float64
	pass string
}

// DefaultAvoid are passages closed unless asked for: the Arctic routes,
// which are seasonal (SeaRoute closes them by default too).
var DefaultAvoid = []string{"northwest", "northeast"}

// LoadSeaNetwork reads a GeoJSON FeatureCollection of LineString or
// MultiLineString features. A "pass" property names straits and canals
// (suez, panama, kiel, ...) so they can be avoided.
func LoadSeaNetwork(r io.Reader) (*SeaNetwork, error) {
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.NewDecoder(r).Decode(&fc); err != nil {
		return nil, fmt.Errorf("sea network: %w", err)
	}
	if fc.Type != "FeatureCollection" {
		return nil, fmt.Errorf("sea network: want a GeoJSON FeatureCollection, got %q", fc.Type)
	}
	n := &SeaNetwork{Avoid: map[string]bool{}}
	for _, p := range DefaultAvoid {
		n.Avoid[p] = true
	}
	ids := map[[2]int64]int{}
	node := func(lon, lat float64) int {
		lon = normLon(lon)
		k := [2]int64{int64(math.Round(lon * 1e6)), int64(math.Round(lat * 1e6))}
		if id, ok := ids[k]; ok {
			return id
		}
		ids[k] = len(n.nodes)
		n.nodes = append(n.nodes, calc.Location{Lat: lat, Lon: lon})
		n.adj = append(n.adj, nil)
		return ids[k]
	}
	for i, f := range fc.Features {
		pass, _ := f.Properties["pass"].(string)
		pass = strings.ToLower(strings.TrimSpace(pass))
		var lines [][][]float64
		switch f.Geometry.Type {
		case "LineString":
			var l [][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &l); err != nil {
				return nil, fmt.Errorf("sea network: feature %d: %w", i, err)
			}
			lines = append(lines, l)
		case "MultiLineString":
			if err := json.Unmarshal(f.Geometry.Coordinates, &lines); err != nil {
				return nil, fmt.Errorf("sea network: feature %d: %w", i, err)
			}
		default:
			continue
		}
		for _, l := range lines {
			for j := 1; j < len(l); j++ {
				if len(l[j-1]) < 2 || len(l[j]) < 2 {
					return nil, fmt.Errorf("sea network: feature %d: bad coordinate", i)
				}
				a, b := node(l[j-1][0], l[j-1][1]), node(l[j][0], l[j][1])
				if a == b {
					continue
				}
				km := Haversine(n.nodes[a], n.nodes[b])
				n.adj[a] = append(n.adj[a], seaEdge{b, km, pass})
				n.adj[b] = append(n.adj[b], seaEdge{a, km, pass})
			}
		}
	}
	edges := 0
	for _, es := range n.adj {
		edges += len(es)
	}
	if edges == 0 {
		return nil, errors.New("sea network: no line segments")
	}
	return n, nil
}

// normLon maps longitude to (-180, 180], so lines that cross the date line
// as 180.5 or -180 join the same nodes.
func normLon(lon float64) float64 {
	lon = math.Mod(lon+180, 360)
	if lon <= 0 {
		lon += 360
	}
	return lon - 180
}

// Nodes returns the node count, for diagnostics.
func (n *SeaNetwork) Nodes() int { return len(n.nodes) }

// Passes lists the passage names present in the network.
func (n *SeaNetwork) Passes() []string {
	seen := map[string]bool{}
	for _, es := range n.adj {
		for _, e := range es {
			if e.pass != "" {
				seen[e.pass] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func seaCell(lat, lon float64) [2]int {
	return [2]int{int(math.Floor(lat)), wrapCell(int(math.Floor(lon)))}
}

// wrapCell puts a longitude cell in [-180, 180).
func wrapCell(x int) int { return ((x+180)%360+360)%360 - 180 }

// nearest finds the nearest connected node.
func (n *SeaNetwork) nearest(p calc.Location) (int, float64) { return n.nearestWithin(p, math.Inf(1)) }

// nearestWithin finds the nearest connected node, searching 1° cells in
// rings around p until no farther ring can hold a nearer node. Past limit
// km it may give up early, returning -1 and +Inf.
func (n *SeaNetwork) nearestWithin(p calc.Location, limit float64) (int, float64) {
	n.grid.once.Do(func() {
		n.grid.cells = map[[2]int][]int{}
		for i, q := range n.nodes {
			if len(n.adj[i]) > 0 {
				c := seaCell(q.Lat, q.Lon)
				n.grid.cells[c] = append(n.grid.cells[c], i)
			}
		}
	})
	best, bestKm := -1, math.Inf(1)
	brute := func() (int, float64) {
		for i, q := range n.nodes {
			if len(n.adj[i]) == 0 {
				continue
			}
			if d := Haversine(p, q); d < bestKm {
				best, bestKm = i, d
			}
		}
		return best, bestKm
	}
	if math.Abs(p.Lat) > 70 {
		return brute() // near the poles, routes over the pole spoil the rings
	}
	c := seaCell(p.Lat, p.Lon)
	for r := 0; r <= 15; r++ {
		// A node in ring r is at least r-1 whole cells away in latitude or
		// longitude. Two points Δλ apart in longitude, both within latitude
		// φ of the equator, are at least 2/π·cos φ·Δλ apart.
		if r > 1 {
			φ := math.Min(90, math.Abs(p.Lat)+float64(r)) * math.Pi / 180
			if bound := 2 / math.Pi * math.Cos(φ) * float64(r-1) * 111.19; bound > bestKm {
				return best, bestKm
			} else if bound > limit {
				return -1, math.Inf(1)
			}
		}
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dy), abs(dx)) != r {
					continue
				}
				for _, i := range n.grid.cells[[2]int{c[0] + dy, wrapCell(c[1] + dx)}] {
					if d := Haversine(p, n.nodes[i]); d < bestKm {
						best, bestKm = i, d
					}
				}
			}
		}
	}
	if bestKm > limit || 2/math.Pi*math.Cos(math.Min(90, math.Abs(p.Lat)+16)*math.Pi/180)*15*111.19 > limit {
		return -1, math.Inf(1)
	}
	best, bestKm = -1, math.Inf(1)
	return brute() // far from any node
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Distance implements calc.DistanceProvider.
func (n *SeaNetwork) Distance(ctx context.Context, _ calc.Mode, from, to calc.Location) (float64, error) {
	a, snapA := n.nearest(from)
	b, snapB := n.nearest(to)
	if a < 0 || b < 0 {
		return 0, errors.New("sea network: no connected nodes")
	}
	if e := (&calc.OffNetworkError{Mode: calc.Sea, FromKm: snapA, ToKm: snapB, MaxKm: n.MaxSnapKm}); n.MaxSnapKm > 0 && (e.FromOff() || e.ToOff()) {
		return 0, e
	}
	path, _, err := n.shortest(ctx, a, b, false)
	if err != nil {
		return 0, err
	}
	return snapA + path + snapB, nil
}

// Route implements calc.Router: the distance, and when asked for
// (calc.WithPath) the path: origin, network nodes, destination.
func (n *SeaNetwork) Route(ctx context.Context, m calc.Mode, from, to calc.Location) (calc.Route, error) {
	if !calc.WantPath(ctx) {
		d, err := n.Distance(ctx, m, from, to)
		return calc.Route{Km: d}, err
	}
	a, snapA := n.nearest(from)
	b, snapB := n.nearest(to)
	if a < 0 || b < 0 {
		return calc.Route{}, errors.New("sea network: no connected nodes")
	}
	if n.MaxSnapKm > 0 && (snapA > n.MaxSnapKm || snapB > n.MaxSnapKm) {
		_, err := n.Distance(ctx, m, from, to) // for its error message
		return calc.Route{}, err
	}
	d, nodes, err := n.shortest(ctx, a, b, true)
	if err != nil {
		return calc.Route{}, err
	}
	path := make([][2]float64, 0, len(nodes)+2)
	path = append(path, [2]float64{from.Lon, from.Lat})
	for _, i := range nodes {
		path = append(path, [2]float64{n.nodes[i].Lon, n.nodes[i].Lat})
	}
	path = append(path, [2]float64{to.Lon, to.Lat})
	return calc.Route{Km: snapA + d + snapB, Path: path}, nil
}

// DistanceMethod implements calc.DistanceMethoder.
func (n *SeaNetwork) DistanceMethod(calc.Mode) string { return "sea_network" }

// DistanceType implements calc.DistanceTyper: the shortest path on the
// maritime network is a shortest feasible distance.
func (n *SeaNetwork) DistanceType(calc.Mode) string { return calc.DistSFD }

// shortest returns the network distance from src to dst and, with
// withNodes, the nodes on the way (src and dst included).
func (n *SeaNetwork) shortest(ctx context.Context, src, dst int, withNodes bool) (float64, []int, error) {
	if src == dst {
		return 0, []int{src}, nil
	}
	dist := make([]float64, len(n.nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	var prev []int
	if withNodes {
		prev = make([]int, len(n.nodes))
		for i := range prev {
			prev[i] = -1
		}
	}
	dist[src] = 0
	pq := &nodeHeap{{src, 0}}
	for steps := 0; pq.Len() > 0; steps++ {
		if steps%4096 == 0 && ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		cur := heap.Pop(pq).(nodeDist)
		if cur.d > dist[cur.node] {
			continue
		}
		if cur.node == dst {
			if !withNodes {
				return cur.d, nil, nil
			}
			var nodes []int
			for v := dst; v != -1; v = prev[v] {
				nodes = append(nodes, v)
			}
			for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
			return cur.d, nodes, nil
		}
		for _, e := range n.adj[cur.node] {
			if e.pass != "" && n.Avoid[e.pass] {
				continue
			}
			if nd := cur.d + e.km; nd < dist[e.to] {
				dist[e.to] = nd
				if withNodes {
					prev[e.to] = cur.node
				}
				heap.Push(pq, nodeDist{e.to, nd})
			}
		}
	}
	return 0, nil, errors.New("sea network: no route (check avoided passages)")
}

type nodeDist struct {
	node int
	d    float64
}

type nodeHeap []nodeDist

func (h nodeHeap) Len() int           { return len(h) }
func (h nodeHeap) Less(i, j int) bool { return h[i].d < h[j].d }
func (h nodeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)        { *h = append(*h, x.(nodeDist)) }
func (h *nodeHeap) Pop() any          { old := *h; x := old[len(old)-1]; *h = old[:len(old)-1]; return x }

// SnapKm implements calc.Snapper: how far a point is from the network.
func (n *SeaNetwork) SnapKm(_ calc.Mode, p calc.Location, maxKm float64) (float64, bool) {
	_, km := n.nearestWithin(p, maxKm)
	return km, len(n.nodes) > 0
}
