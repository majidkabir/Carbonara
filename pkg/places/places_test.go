package places

import (
	"testing"
	"time"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"København": "kobenhavn", "Düsseldorf": "dusseldorf", "Straße": "strasse", "Łódź": "lodz",
		"  Frankfurt am Main ": "frankfurt am main", "St. Petersburg": "st petersburg", "Ærøskøbing": "aeroskobing",
	} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func index(t *testing.T) *Index {
	t.Helper()
	start := time.Now()
	ix, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if ix.Len() < 50000 {
		t.Fatalf("only %d places", ix.Len())
	}
	t.Logf("%d places loaded in %v", ix.Len(), time.Since(start))
	return ix
}

func TestLookup(t *testing.T) {
	ix := index(t)
	for code, want := range map[string]string{"NLRTM": "locode:NLRTM", "nl rtm": "locode:NLRTM", "CPH": "iata:CPH", "iata:CPH": "iata:CPH"} {
		p, ok := ix.Lookup(code)
		if !ok || p.ID != want {
			t.Errorf("Lookup(%q) = %q %v, want %q", code, p.ID, ok, want)
		}
	}
	p, _ := ix.Lookup("DEHAM")
	if p.Lat < 53 || p.Lat > 54 || p.Lon < 9.5 || p.Lon > 10.5 || p.CountryName != "Germany" {
		t.Errorf("Hamburg: %+v", p)
	}
	if _, ok := ix.Lookup("XXZZZ"); ok {
		t.Error("unknown code found")
	}
}

func TestSearch(t *testing.T) {
	ix := index(t)
	first := func(q string, kinds ...string) Place {
		t.Helper()
		r := ix.Search(q, kinds, 5)
		if len(r) == 0 {
			t.Fatalf("%q: nothing found", q)
		}
		return r[0]
	}
	if p := first("rotterdam", Port); p.ID != "locode:NLRTM" {
		t.Errorf("rotterdam port: %s %s", p.ID, p.Name)
	}
	if p := first("CPH"); p.ID != "iata:CPH" {
		t.Errorf("CPH: %s", p.ID)
	}
	if p := first("nlrtm"); p.ID != "locode:NLRTM" {
		t.Errorf("nlrtm: %s", p.ID)
	}
	if p := first("kobenhavn", Port); p.ID != "locode:DKCPH" {
		t.Errorf("kobenhavn: %s", p.ID)
	}
	if p := first("Duisb", Port); p.ID != "locode:DEDUI" {
		t.Errorf("Duisb: %s", p.ID)
	}
	if p := first("copenhagen", Airport); p.ID != "iata:CPH" {
		t.Errorf("copenhagen airport: %s", p.ID)
	}
	for _, p := range ix.Search("hamburg", []string{Airport}, 10) {
		if !anyKind(p.Kinds, map[string]bool{Airport: true}) {
			t.Errorf("kind filter let %s through", p.ID)
		}
	}
	if r := ix.Search("", nil, 5); len(r) != 0 {
		t.Error("empty query matched")
	}
}

func BenchmarkSearch(b *testing.B) {
	ix, _ := Default()
	for b.Loop() {
		ix.Search("hamb", nil, 10)
	}
}
