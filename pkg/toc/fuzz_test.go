package toc

import (
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/factors"
)

func FuzzParseBuild(f *testing.F) {
	f.Add(ops)
	f.Add(`{"hocs":[{"id":"h","tonnes":1,"energy":[{"fuel":"electricity/DNK","amount":1}]}]}`)
	ds, err := factors.Open(factors.Default)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, in string) {
		o, err := Parse(strings.NewReader(in))
		if err != nil {
			return
		}
		_, _ = Build(o, ds)
	})
}
