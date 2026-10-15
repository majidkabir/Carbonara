// Command gazetteer builds pkg/places/data/places.tsv.gz, the place list
// the calculator searches: cities from GeoNames, UN/LOCODE transport
// locations (ports, rail and road terminals, inland depots, border
// crossings) and airports with IATA codes from OurAirports.
//
//	go run ./tools/gazetteer -fetch -out pkg/places/data/places.tsv.gz
//
// Without -fetch it reads the files from -dir (same names as the URLs'
// last path parts). GeoNames is optional: without cities5000.zip the list
// has no cities and no English names for UN/LOCODE places.
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/majidkabir/carbonara/pkg/places"
)

var sources = []string{
	"https://raw.githubusercontent.com/datasets/un-locode/main/data/code-list.csv",
	"https://raw.githubusercontent.com/datasets/un-locode/main/data/subdivision-codes.csv",
	"https://raw.githubusercontent.com/datasets/un-locode/main/data/country-codes.csv",
	"https://raw.githubusercontent.com/davidmegginson/ourairports-data/main/airports.csv",
	"https://download.geonames.org/export/dump/cities5000.zip",
	"https://download.geonames.org/export/dump/admin1CodesASCII.txt",
}

type entry struct {
	id, kinds, name, alt, region, country string
	lat, lon                              float64
	rank                                  int
}

type city struct {
	entry
	admin1 string          // GeoNames admin1 code, e.g. "GA" in the US
	names  map[string]bool // folded name, ascii name and alternate names
}

// portCoords are the coordinates of large sea ports that UN/LOCODE lists
// without coordinates and whose name isn't a GeoNames city: the
// calculator's sea port list (pkg/places/seaports.go) needs them.
var portCoords = map[string][2]float64{
	"TWKHH": {22.61, 120.29}, "TWKEL": {25.15, 121.74}, "JPNGO": {35.06, 136.87}, "IDBLW": {3.79, 98.69},
	"PHMNL": {14.59, 120.96}, "MMRGN": {16.77, 96.17}, "RUVVO": {43.11, 131.89}, "INNSA": {18.95, 72.95},
	"INMUN": {22.74, 69.70}, "PKKHI": {24.84, 66.98}, "AEJEA": {25.01, 55.06}, "OMSLL": {16.94, 54.00},
	"OMSOH": {24.50, 56.62}, "ILASH": {31.82, 34.64}, "GBFXT": {51.95, 1.32}, "GBSOU": {50.90, -1.42},
	"GBTIL": {51.46, 0.36}, "ESVLC": {39.44, -0.32}, "ESALG": {36.13, -5.43}, "ESBCN": {41.35, 2.16},
	"ITTRS": {45.63, 13.77}, "ITSAL": {40.67, 14.74}, "SIKOP": {45.56, 13.74}, "BGVAR": {43.19, 27.90},
	"PLGDN": {54.40, 18.67}, "LTKLJ": {55.70, 21.13}, "LVRIX": {56.99, 24.07}, "IEDUB": {53.35, -6.21},
	"EGPSD": {31.25, 32.31}, "MAPTM": {35.89, -5.50}, "KEMBA": {-4.06, 39.65}, "DJJIB": {11.60, 43.13},
	"SDPZU": {19.61, 37.23}, "NGAPP": {6.44, 3.37}, "NGTIN": {6.44, 3.35}, "GHTEM": {5.63, 0.01},
	"SNDKR": {14.68, -17.43}, "TGLFW": {6.14, 1.28}, "BJCOO": {6.35, 2.43}, "CMDLA": {4.05, 9.69},
	"AOLAD": {-8.80, 13.24}, "MZMPM": {-25.97, 32.57}, "NAWVB": {-22.95, 14.50}, "MUPLU": {-20.15, 57.49},
	"USOAK": {37.80, -122.30}, "USSEA": {47.58, -122.35}, "USTIW": {47.27, -122.41}, "USSAV": {32.12, -81.14},
	"USCHS": {32.79, -79.92}, "USORF": {36.90, -76.33}, "USMIA": {25.77, -80.17}, "USJAX": {30.40, -81.55},
	"USPHL": {39.90, -75.14}, "USMOB": {30.69, -88.04}, "USMSY": {29.93, -90.06}, "USBOS": {42.35, -71.03},
	"CAVAN": {49.29, -123.10}, "CAPRR": {54.30, -130.35}, "CAMTR": {45.55, -73.53}, "CAHAL": {44.64, -63.57},
	"PABLB": {8.95, -79.57}, "PAMIT": {9.36, -79.88}, "HNPCR": {15.84, -87.95}, "COCTG": {10.39, -75.52},
	"COBUN": {3.89, -77.07}, "UYMVD": {-34.90, -56.21}, "JMKIN": {17.98, -76.83}, "BSFPO": {26.52, -78.77},
	"AUFRE": {-32.05, 115.74},
}

func main() {
	fetch := flag.Bool("fetch", false, "download the sources")
	dir := flag.String("dir", ".", "directory with the source files (without -fetch)")
	out := flag.String("out", "pkg/places/data/places.tsv.gz", "output file")
	flag.Parse()
	files := map[string][]byte{}
	for _, u := range sources {
		name := path.Base(u)
		var b []byte
		var err error
		if *fetch {
			b, err = get(u)
		} else {
			b, err = os.ReadFile(filepath.Join(*dir, name))
		}
		if err != nil {
			if strings.Contains(u, "geonames") {
				log.Printf("no %s (%v): no cities", name, err)
				continue
			}
			log.Fatal(err)
		}
		files[name] = b
	}

	countries := map[string]string{}
	for _, r := range readCSV(files["country-codes.csv"])[1:] {
		countries[r[0]] = strings.TrimSuffix(strings.TrimSuffix(r[1], " (the)"), " (The)")
	}
	cities := readCities(files["cities5000.zip"], files["admin1CodesASCII.txt"])
	grid := map[[3]int][]*city{} // country index, 0.2° cells
	cell := func(cc string, lat, lon float64) [3]int {
		return [3]int{int(cc[0])<<8 | int(cc[1]), int(math.Floor(lat * 5)), int(math.Floor(lon * 5))}
	}
	var all []entry
	byName := map[string][]*city{} // by country and folded name
	for _, c := range cities {
		all = append(all, c.entry)
		k := cell(c.country, c.lat, c.lon)
		grid[k] = append(grid[k], c)
		for n := range c.names {
			byName[c.country+" "+n] = append(byName[c.country+" "+n], c)
		}
	}
	// cityCoords finds a UN/LOCODE place without coordinates among the
	// cities of its country by name: in its subdivision if a city there has
	// the name (GeoNames and UN/LOCODE share them in some countries, the US
	// for one), else the largest.
	cityCoords := func(cc, sub string, names ...string) (float64, float64, bool) {
		var best *city
		inSub := func(c *city) bool { return sub != "" && c.admin1 == sub }
		for _, n := range names {
			for _, c := range byName[cc+" "+places.Fold(n)] {
				if best == nil || inSub(c) && !inSub(best) || inSub(c) == inSub(best) && c.rank > best.rank {
					best = c
				}
			}
		}
		if best == nil {
			return 0, 0, false
		}
		return best.lat, best.lon, true
	}
	nByName, nByTable := 0, 0
	// englishName finds a nearby city that has the place's name among its
	// names, and returns that city's own (usually English) name.
	englishName := func(cc string, lat, lon float64, folded string) (string, int) {
		k := cell(cc, lat, lon)
		best, bestD := (*city)(nil), 25.0
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				for _, c := range grid[[3]int{k[0], k[1] + dy, k[2] + dx}] {
					if !c.names[folded] {
						continue
					}
					if d := km(lat, lon, c.lat, c.lon); d < bestD {
						best, bestD = c, d
					}
				}
			}
		}
		if best == nil || places.Fold(best.name) == folded {
			if best != nil {
				return "", best.rank
			}
			return "", 0
		}
		return best.name, best.rank
	}

	subdiv := map[string]string{}
	for _, r := range readCSV(files["subdivision-codes.csv"])[1:] {
		subdiv[r[0]+"-"+r[1]] = r[2]
	}
	nLocode := 0
	for _, r := range readCSV(files["code-list.csv"])[1:] {
		// Change,Country,Location,Name,NameWoDiacritics,Subdivision,Status,Function,Date,IATA,Coordinates,Remarks
		if r[0] == "X" || r[6] == "XX" || len(r[2]) != 3 {
			continue
		}
		lat, lon, ok := locodeCoords(r[10])
		if c, fixed := portCoords[r[1]+r[2]]; fixed && !ok {
			lat, lon, ok = c[0], c[1], true
			nByTable++
		} else if !ok && strings.TrimSpace(r[10]) == "" {
			if lat, lon, ok = cityCoords(r[1], r[5], r[3], r[4]); ok {
				nByName++
			}
		}
		if !ok {
			continue
		}
		kinds, rank := "", 25
		fn := r[7] + "--------"
		for i, k := range map[int]struct {
			letter string
			bonus  int
		}{0: {"p", 25}, 1: {"r", 10}, 2: {"t", 0}, 5: {"i", 10}, 7: {"b", 0}} { // airports come from OurAirports
			if fn[i] != '-' && fn[i] != '0' {
				kinds += k.letter
				rank += k.bonus
			}
		}
		if kinds == "" {
			continue
		}
		if kinds == "t" { // a road terminal only: often just a town name
			rank -= 10
		}
		kinds = sortLetters(kinds)
		alt, cityRank := englishName(r[1], lat, lon, places.Fold(r[3]))
		if cityRank > 40 {
			rank += (cityRank - 40) / 2
		}
		all = append(all, entry{id: "locode:" + r[1] + r[2], kinds: kinds, name: r[3], alt: alt,
			region: subdiv[r[1]+"-"+r[5]], country: r[1], lat: lat, lon: lon, rank: min(rank, 95)})
		nLocode++
	}

	nAir := 0
	for i, r := range readCSV(files["airports.csv"]) {
		// id,ident,type,name,latitude_deg,longitude_deg,elevation_ft,continent,iso_country,iso_region,municipality,scheduled_service,icao_code,iata_code,...
		if i == 0 || len(r[13]) != 3 || r[2] == "closed" || r[2] == "heliport" {
			continue
		}
		lat, err1 := strconv.ParseFloat(r[4], 64)
		lon, err2 := strconv.ParseFloat(r[5], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rank := map[string]int{"large_airport": 60, "medium_airport": 45}[r[2]]
		if rank == 0 {
			rank = 25
		}
		if r[11] == "yes" {
			rank += 3
		}
		all = append(all, entry{id: "iata:" + r[13], kinds: "a", name: r[3], alt: r[10], country: r[8], lat: lat, lon: lon, rank: rank})
		nAir++
	}

	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.ModTime = time.Time{}
	cc := make([]string, 0, len(countries))
	for c := range countries {
		cc = append(cc, c)
	}
	sort.Strings(cc)
	for _, c := range cc {
		fmt.Fprintf(zw, "C\t%s\t%s\n", c, clean(countries[c]))
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.id] {
			continue
		}
		seen[e.id] = true
		fmt.Fprintf(zw, "P\t%s\t%s\t%s\t%s\t%s\t%s\t%.5f\t%.5f\t%d\n", e.id, e.kinds, clean(e.name), clean(e.alt),
			clean(e.region), e.country, e.lat, e.lon, e.rank)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: %d cities, %d UN/LOCODE places (%d placed by city name, %d by the port table), %d airports, %d bytes",
		*out, len(cities), nLocode, nByName, nByTable, nAir, buf.Len())
}

func get(u string) ([]byte, error) {
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func readCSV(b []byte) [][]string {
	r := csv.NewReader(bytes.NewReader(b))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	return rows
}

func readCities(zipped, admin1 []byte) []*city {
	if zipped == nil {
		return nil
	}
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil || len(zr.File) == 0 {
		log.Fatalf("cities zip: %v", err)
	}
	f, err := zr.File[0].Open()
	if err != nil {
		log.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	regions := map[string]string{}
	for _, l := range strings.Split(string(admin1), "\n") {
		if p := strings.Split(l, "\t"); len(p) >= 2 {
			regions[p[0]] = p[1]
		}
	}
	var out []*city
	for _, l := range strings.Split(string(b), "\n") {
		// geonameid,name,asciiname,alternatenames,lat,lon,class,code,country,cc2,admin1,...,population(14)
		p := strings.Split(l, "\t")
		if len(p) < 15 || len(p[8]) != 2 {
			continue
		}
		lat, err1 := strconv.ParseFloat(p[4], 64)
		lon, err2 := strconv.ParseFloat(p[5], 64)
		pop, _ := strconv.ParseFloat(p[14], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rank := 40
		if pop > 5000 {
			rank = min(80, 40+int(10*math.Log10(pop/5000)))
		}
		c := &city{entry: entry{id: "geonames:" + p[0], kinds: "c", name: p[1], region: regions[p[8]+"."+p[10]],
			country: p[8], lat: lat, lon: lon, rank: rank}, admin1: p[10], names: map[string]bool{}}
		for _, n := range append([]string{p[1], p[2]}, strings.Split(p[3], ",")...) {
			if n = places.Fold(n); n != "" {
				c.names[n] = true
			}
		}
		out = append(out, c)
	}
	return out
}

// locodeCoords parses "5155N 00430E".
func locodeCoords(s string) (lat, lon float64, ok bool) {
	p := strings.Fields(s)
	if len(p) != 2 || len(p[0]) != 5 || len(p[1]) != 6 {
		return 0, 0, false
	}
	dm := func(v string, deg int, neg byte) (float64, bool) {
		d, err1 := strconv.Atoi(v[:deg])
		m, err2 := strconv.Atoi(v[deg : len(v)-1])
		if err1 != nil || err2 != nil || m >= 60 {
			return 0, false
		}
		x := float64(d) + float64(m)/60
		if v[len(v)-1] == neg {
			x = -x
		}
		return x, true
	}
	lat, ok1 := dm(p[0], 2, 'S')
	lon, ok2 := dm(p[1], 3, 'W')
	return lat, lon, ok1 && ok2 && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 && (lat != 0 || lon != 0)
}

func km(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	dφ, dλ := φ2-φ1, (lon2-lon1)*math.Pi/180
	a := math.Sin(dφ/2)*math.Sin(dφ/2) + math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

func sortLetters(s string) string {
	b := []byte(s)
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	return string(b)
}

func clean(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s))
}
