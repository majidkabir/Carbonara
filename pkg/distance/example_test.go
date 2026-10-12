package distance_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/distance"
)

// Route over a maritime network, with and without a canal.
func ExampleSeaNetwork() {
	net := `{"type":"FeatureCollection","features":[
	  {"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[0,0],[0,10],[10,10],[10,0]]}},
	  {"type":"Feature","properties":{"pass":"suez"},"geometry":{"type":"LineString","coordinates":[[0,0],[10,0]]}}]}`
	n, err := distance.LoadSeaNetwork(strings.NewReader(net))
	if err != nil {
		panic(err)
	}
	a, b := calc.Location{Lat: 0, Lon: 0}, calc.Location{Lat: 0, Lon: 10}
	d, _ := n.Distance(context.Background(), calc.Sea, a, b)
	n.Avoid["suez"] = true
	around, _ := n.Distance(context.Background(), calc.Sea, a, b)
	fmt.Printf("via suez %.0f km, around %.0f km\n", d, around)
	// Output:
	// via suez 1112 km, around 3319 km
}
