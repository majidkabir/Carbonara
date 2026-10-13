package csvio

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// Read and the streaming Reader must never panic, and on input whose
// shipments are grouped they must agree.
func FuzzReaders(f *testing.F) {
	f.Add(input)
	f.Add("shipment_id,teu,container_load,mode,vehicle_type,distance_km\nC1,2,average,sea,x,100\nC1,,,road,hgv,10\n")
	f.Add("\uFEFFshipment_id,weight_kg\nA,1\n,2\n")
	f.Fuzz(func(t *testing.T, in string) {
		all, errAll := Read(strings.NewReader(in))
		r, err := NewReader(strings.NewReader(in))
		if err != nil {
			if errAll == nil {
				t.Fatalf("NewReader failed (%v) where Read succeeded", err)
			}
			return
		}
		var streamed []Item
		for {
			it, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return
			}
			if it.Line < 2 {
				t.Fatalf("item line %d", it.Line)
			}
			streamed = append(streamed, it)
		}
		if errAll != nil {
			return
		}
		grouped := true
		for _, it := range streamed {
			if it.Err != nil && strings.Contains(it.Err.Error(), "must be together") {
				grouped = false
			}
		}
		if grouped && len(streamed) != len(all) {
			t.Fatalf("grouped input: stream %d items, Read %d", len(streamed), len(all))
		}
	})
}
