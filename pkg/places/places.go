// Package places finds transport locations by name or code: cities
// (GeoNames), ports, rail and road terminals and other UN/LOCODE
// locations, and airports by IATA code (OurAirports). The list is built
// by tools/gazetteer and embedded, so searches need no outside service.
package places

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/majidkabir/carbonara/pkg/calc"
)

//go:embed data/places.tsv.gz
var embedded []byte

// Kinds of place. A UN/LOCODE location can be several at once.
const (
	City    = "city"    // a city or town (GeoNames, population 5,000 or more)
	Port    = "port"    // sea or inland port (UN/LOCODE function 1)
	Rail    = "rail"    // rail terminal (function 2)
	Road    = "road"    // road terminal (function 3)
	Airport = "airport" // airport (IATA code, or UN/LOCODE function 4)
	Inland  = "inland"  // multimodal / inland clearance depot (function 6)
	Border  = "border"  // border crossing (function B)
)

var kindLetter = map[byte]string{'c': City, 'p': Port, 'r': Rail, 't': Road, 'a': Airport, 'i': Inland, 'b': Border}

// Place is a searchable location.
type Place struct {
	ID          string   `json:"id"`             // "locode:NLRTM", "iata:CPH", "geonames:2618425"
	Code        string   `json:"code,omitempty"` // UN/LOCODE or IATA code
	Kinds       []string `json:"kinds"`
	Name        string   `json:"name"`
	AltName     string   `json:"alt_name,omitempty"` // e.g. the English name of "København"
	Region      string   `json:"region,omitempty"`
	Country     string   `json:"country"` // ISO 3166-1 alpha-2
	CountryName string   `json:"country_name,omitempty"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`

	rank  int
	name  string // folded, with a leading space: " kobenhavn"
	alt   string // folded, with a leading space
	words string // folded " name alt region country "
}

// Index is a loaded place list.
type Index struct {
	places []Place
	byCode map[string]int
}

var (
	once    sync.Once
	def     *Index
	loadErr error
)

// Default returns the embedded place list, loading it on first use.
func Default() (*Index, error) {
	once.Do(func() { def, loadErr = Load(embedded) })
	return def, loadErr
}

// Load reads a gzipped place list made by tools/gazetteer.
func Load(gz []byte) (*Index, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("places: %w", err)
	}
	defer zr.Close()
	countries := map[string]string{}
	ix := &Index{byCode: map[string]int{}}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		f := strings.Split(sc.Text(), "\t")
		switch {
		case len(f) == 3 && f[0] == "C":
			countries[f[1]] = f[2]
		case len(f) == 10 && f[0] == "P":
			lat, err1 := strconv.ParseFloat(f[7], 64)
			lon, err2 := strconv.ParseFloat(f[8], 64)
			rank, err3 := strconv.Atoi(f[9])
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, fmt.Errorf("places: line %d: bad number", n)
			}
			p := Place{ID: f[1], Name: f[3], AltName: f[4], Region: f[5], Country: f[6], Lat: lat, Lon: lon, rank: rank}
			for i := 0; i < len(f[2]); i++ {
				if k, ok := kindLetter[f[2][i]]; ok {
					p.Kinds = append(p.Kinds, k)
				}
			}
			if _, code, ok := strings.Cut(p.ID, ":"); ok && !strings.HasPrefix(p.ID, "geonames:") {
				p.Code = code
			}
			ix.places = append(ix.places, p)
		case len(f) == 1 && f[0] == "":
		default:
			return nil, fmt.Errorf("places: line %d: unexpected format", n)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("places: %w", err)
	}
	for i := range ix.places {
		p := &ix.places[i]
		p.CountryName = countries[p.Country]
		p.name, p.alt = " "+fold(p.Name), " "+fold(p.AltName)
		p.words = p.name + p.alt + " " + fold(p.Region) + " " + fold(p.CountryName) + " " + strings.ToLower(p.Country) + " "
		ix.byCode[p.ID] = i
		if p.Code != "" {
			if _, dup := ix.byCode[p.Code]; !dup {
				ix.byCode[p.Code] = i
			}
		}
	}
	return ix, nil
}

// Len is the number of places.
func (ix *Index) Len() int { return len(ix.places) }

// Lookup finds a place by UN/LOCODE ("NLRTM", "NL RTM"), IATA code
// ("CPH") or id ("geonames:2618425").
func (ix *Index) Lookup(code string) (Place, bool) {
	c := strings.TrimSpace(code)
	if !strings.Contains(c, ":") {
		c = strings.ToUpper(strings.ReplaceAll(c, " ", ""))
	}
	i, ok := ix.byCode[c]
	if !ok {
		return Place{}, false
	}
	return ix.places[i], true
}

// Search returns up to limit places matching q by code or name, best
// first. With kinds, only places of one of those kinds.
func (ix *Index) Search(q string, kinds []string, limit int) []Place {
	if limit <= 0 {
		limit = 10
	}
	fq := fold(q)
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(q), " ", ""))
	if fq == "" {
		return nil
	}
	tokens := strings.Fields(fq)
	fq = " " + fq
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	type hit struct{ i, score int }
	var hits []hit
	for i := range ix.places {
		p := &ix.places[i]
		if len(want) > 0 && !anyKind(p.Kinds, want) {
			continue
		}
		s := 0
		switch {
		case p.Code != "" && p.Code == code:
			s = 1000
		case p.name == fq:
			s = 500
		case p.alt == fq:
			s = 450
		case len(fq) < 3:
		case strings.HasPrefix(p.name, fq):
			s = 400
		case strings.HasPrefix(p.alt, fq):
			s = 350
		case strings.Contains(p.name, fq):
			s = 300
		case strings.Contains(p.alt, fq):
			s = 250
		case len(tokens) > 1 && allWordPrefixes(p.words, tokens):
			s = 200
		}
		if s > 0 {
			hits = append(hits, hit{i, s + p.rank})
		}
	}
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return ix.places[hits[a].i].Name < ix.places[hits[b].i].Name
	})
	out := make([]Place, 0, min(limit, len(hits)))
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		out = append(out, ix.places[h.i])
	}
	return out
}

func anyKind(kinds []string, want map[string]bool) bool {
	for _, k := range kinds {
		if want[k] {
			return true
		}
	}
	return false
}

func allWordPrefixes(words string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(words, " "+t) {
			return false
		}
	}
	return true
}

// Resolve finds a location by code for the calculator.
func (ix *Index) Resolve(code string) (calc.Location, bool) {
	p, ok := ix.Lookup(code)
	if !ok {
		return calc.Location{}, false
	}
	name := p.Name
	if p.Code != "" {
		name += " (" + p.Code + ")"
	}
	return calc.Location{Name: name, Code: p.Code, Lat: p.Lat, Lon: p.Lon}, true
}

// Codes resolves location codes with the embedded place list, loading it
// on first use.
type Codes struct{}

func (Codes) Resolve(code string) (calc.Location, bool) {
	ix, err := Default()
	if err != nil {
		return calc.Location{}, false
	}
	return ix.Resolve(code)
}
