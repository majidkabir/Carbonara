package grid

import (
	"strings"
	"testing"
)

func FuzzParseOWID(f *testing.F) {
	f.Add(sample)
	f.Fuzz(func(t *testing.T, in string) {
		g, err := ParseOWID(strings.NewReader(in))
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, x := range g.Grids {
			if len(x.Country) != 3 || x.G < 0 || seen[x.Country] {
				t.Fatalf("bad grid %+v", x)
			}
			seen[x.Country] = true
		}
	})
}
