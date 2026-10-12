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

	"github.com/majidkabir/carbonara/pkg/calc"
)

// SeaNetwork routes sea legs over a maritime line network, such as
// Eurostat's SeaRoute "marnet" (EUPL-1.2, not bundled; see README).
//
// Origin and destination are snapped to the nearest network node; the
// distance is snap-in + shortest path + snap-out.
type SeaNetwork struct {
	nodes []calc.Location
	adj   [][]seaEdge
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
	if len(n.nodes) == 0 {
		return nil, errors.New("sea network: no line features")
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

func (n *SeaNetwork) nearest(p calc.Location) (int, float64) {
	best, bestKm := -1, math.Inf(1)
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

// Distance implements calc.DistanceProvider.
func (n *SeaNetwork) Distance(ctx context.Context, _ calc.Mode, from, to calc.Location) (float64, error) {
	a, snapA := n.nearest(from)
	b, snapB := n.nearest(to)
	if n.MaxSnapKm > 0 {
		if snapA > n.MaxSnapKm {
			return 0, fmt.Errorf("sea network: origin is %.0f km from the network (max %.0f)", snapA, n.MaxSnapKm)
		}
		if snapB > n.MaxSnapKm {
			return 0, fmt.Errorf("sea network: destination is %.0f km from the network (max %.0f)", snapB, n.MaxSnapKm)
		}
	}
	path, err := n.shortest(ctx, a, b)
	if err != nil {
		return 0, err
	}
	return snapA + path + snapB, nil
}

// DistanceMethod implements calc.DistanceMethoder.
func (n *SeaNetwork) DistanceMethod(calc.Mode) string { return "sea_network" }

func (n *SeaNetwork) shortest(ctx context.Context, src, dst int) (float64, error) {
	if src == dst {
		return 0, nil
	}
	dist := make([]float64, len(n.nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	dist[src] = 0
	pq := &nodeHeap{{src, 0}}
	for steps := 0; pq.Len() > 0; steps++ {
		if steps%4096 == 0 && ctx.Err() != nil {
			return 0, ctx.Err()
		}
		cur := heap.Pop(pq).(nodeDist)
		if cur.d > dist[cur.node] {
			continue
		}
		if cur.node == dst {
			return cur.d, nil
		}
		for _, e := range n.adj[cur.node] {
			if e.pass != "" && n.Avoid[e.pass] {
				continue
			}
			if nd := cur.d + e.km; nd < dist[e.to] {
				dist[e.to] = nd
				heap.Push(pq, nodeDist{e.to, nd})
			}
		}
	}
	return 0, errors.New("sea network: no route (check avoided passages)")
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
