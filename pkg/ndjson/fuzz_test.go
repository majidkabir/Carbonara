package ndjson

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func FuzzReader(f *testing.F) {
	f.Add("{\"id\":\"a\",\"weight_kg\":1}\n\n{\"id\":\"c\",\nnot json\n{\"id\":\"d\"} {}")
	f.Add(strings.Repeat("x", 300) + "\n{}\n")
	f.Fuzz(func(t *testing.T, in string) {
		r := NewReader(strings.NewReader(in), 128)
		last := 0
		for i := 0; ; i++ {
			it, err := r.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatalf("unexpected structural error: %v", err)
			}
			if it.Line <= last {
				t.Fatalf("line numbers not increasing: %d after %d", it.Line, last)
			}
			last = it.Line
			if i > len(in)+1 {
				t.Fatal("more items than input bytes")
			}
		}
	})
}
