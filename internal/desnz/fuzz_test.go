package desnz

import (
	"strings"
	"testing"
)

// Rows come from a spreadsheet: tab-separated cells, one row per line.
func FuzzParse(f *testing.F) {
	var b strings.Builder
	for _, r := range fixture {
		b.WriteString(strings.Join(r, "\t") + "\n")
	}
	f.Add(b.String())
	f.Fuzz(func(t *testing.T, in string) {
		var rows [][]string
		for _, line := range strings.Split(in, "\n") {
			rows = append(rows, strings.Split(line, "\t"))
		}
		res, err := Parse(rows)
		if err != nil {
			return
		}
		for _, f := range res.Dataset.Legs {
			if f.TTW < 0 && f.TTW > -1e300 && f.VehicleType == "" {
				t.Fatalf("bad factor %+v", f)
			}
		}
	})
}
