// Command mrv builds pkg/factors/data/eu-mrv-<year>.json, a container ship
// emission factor from the EU's public MRV data: the fuel, CO2e and cargo
// that container ships calling at EU ports report each year under
// Regulation (EU) 2015/757, published by EMSA (THETIS-MRV). EMSA allows
// reproduction with acknowledgement of the source.
//
//	go run ./tools/mrv -fetch -year 2024 -out pkg/factors/data/eu-mrv-2024.json
//
// Without -fetch it reads -in, the "EU MRV Publication of information"
// workbook downloaded from https://mrv.emsa.europa.eu/#public/emission-report.
//
// Ships are split by trade: short-sea ships emit at least half their voyage
// CO2 on voyages between EU ports, deep-sea ships less. Each factor is its
// fleet's transport-work-weighted average: all CO2e (TTW) the ships report,
// divided by all their transport work (cargo mass times distance actually
// sailed). Well-to-tank comes from the fuel they report, at DESNZ's
// well-to-tank factor for marine fuel oil. "container_ship" means the
// short-sea factor on legs under ShortBelowKm and deep-sea above. Distances are
// actual, so a routed (shortest feasible) sea distance gets the GLEC sea
// distance adjustment (x 1.15).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/majidkabir/sootcase/internal/xlsx"
	"github.com/majidkabir/sootcase/pkg/calc"
	"github.com/majidkabir/sootcase/pkg/factors"
)

const api = "https://mrv.emsa.europa.eu/api/public-emission-report"

// nmKm is a nautical mile in km.
const nmKm = 1.852

// Ships reporting more cargo than this on average (the largest container
// ships carry well under 250,000 t) or less than a tonne have bad data.
const maxCargoT, minCargoT = 250_000.0, 1.0

// wttFuel is the DESNZ fuel whose well-to-tank factor per tonne is used.
const wttFuel = "liquid_fuels/marine_fuel_oil/tonnes"

// ShortBelowKm is the leg distance below which "container_ship" means a
// short-sea ship. It is Sootcase's rule, not a standard's: feeder and
// intra-European services sail legs of hundreds to a couple of thousand km,
// deep-sea services thousands.
const ShortBelowKm = 2000

// shortSeaShare is the share of a ship's voyage CO2 on voyages between EU
// ports from which it counts as short-sea.
const shortSeaShare = 0.5

func main() {
	fetch := flag.Bool("fetch", false, "download the workbook from EMSA")
	in := flag.String("in", "", "workbook to read without -fetch")
	year := flag.Int("year", 2024, "MRV reporting period")
	out := flag.String("out", "", "dataset JSON to write (default stdout)")
	flag.Parse()

	var data []byte
	var version string
	var err error
	if *fetch {
		data, version, err = download(*year)
	} else {
		data, err = os.ReadFile(*in)
		version = *in
	}
	if err != nil {
		log.Fatal(err)
	}
	f, err := xlsx.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		log.Fatal(err)
	}
	var sheet string
	for _, n := range f.SheetNames() {
		if strings.Contains(n, "Full") {
			sheet = n
		}
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		log.Fatal(err)
	}
	desnz, err := factors.Embedded("desnz-2026")
	if err != nil {
		log.Fatal(err)
	}
	fuel, err := desnz.FuelFactor(wttFuel)
	if err != nil {
		log.Fatal(err)
	}
	short, deep, err := fleet(rows, "Container ship")
	if err != nil {
		log.Fatal(err)
	}
	ds := dataset(*year, version, short, deep, fuel.WTT)
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	b = append(b, '\n')
	if *out == "" {
		os.Stdout.Write(b)
	} else if err := os.WriteFile(*out, b, 0o644); err != nil {
		log.Fatal(err)
	}
	for i, t := range []totals{short, deep} {
		log.Printf("%s: %d ships (%d left out for bad data), TTW %.2f, WTT %.2f g CO2e per actual tonne-km",
			ds.Legs[i].VehicleType, t.ships, t.skipped, ds.Legs[i].TTW, ds.Legs[i].WTT)
	}
}

// download fetches the latest version of a reporting period's workbook.
func download(year int) ([]byte, string, error) {
	c := &http.Client{Timeout: 5 * time.Minute}
	get := func(u string) ([]byte, error) {
		resp, err := c.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	}
	b, err := get(api + "/downloadable-files")
	if err != nil {
		return nil, "", err
	}
	var list struct {
		Results []struct {
			ReportingPeriod int    `json:"reportingPeriod"`
			Version         int    `json:"version"`
			GenerationDate  string `json:"generationDate"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, "", fmt.Errorf("downloadable files: %w", err)
	}
	for _, r := range list.Results {
		if r.ReportingPeriod == year {
			data, err := get(fmt.Sprintf("%s/reporting-period-document/binary/%d/%d", api, year, r.Version))
			return data, fmt.Sprintf("version %d of %s", r.Version, strings.Fields(r.GenerationDate)[0]), err
		}
	}
	return nil, "", fmt.Errorf("no MRV publication for %d", year)
}

// totals is what a fleet reported in a year.
type totals struct {
	ships, skipped int
	co2eT, fuelT   float64 // tonnes
	workTNm        float64 // tonne-nautical miles
	hasCO2e        bool    // CO2e (with CH4 and N2O) rather than CO2 only
}

// fleet adds up the full-year reports of one ship type, short-sea and
// deep-sea apart. rows is the workbook sheet: a few title rows, then a
// header row starting with "IMO Number".
func fleet(rows [][]string, shipType string) (short, deep totals, err error) {
	h := -1
	for i, r := range rows {
		if len(r) > 0 && strings.TrimSpace(r[0]) == "IMO Number" {
			h = i
			break
		}
	}
	if h < 0 {
		return short, deep, fmt.Errorf("no header row")
	}
	col := func(prefix string) int {
		for i, name := range rows[h] {
			if strings.HasPrefix(name, prefix) {
				return i
			}
		}
		return -1
	}
	cType, cFuel, cCO2, cCO2e := col("Ship type"), col("Total fuel consumption ["), col("Total CO₂ emissions ["), col("Total CO₂eq emissions [")
	cPerWork := col("CO₂ emissions per transport work (mass) [")
	cIntra, cFrom, cTo := col("CO₂ emissions from all voyages between ports"), col("CO₂ emissions from all voyages which departed"), col("CO₂ emissions from all voyages to ports")
	if cType < 0 || cFuel < 0 || cCO2 < 0 || cPerWork < 0 || cIntra < 0 || cFrom < 0 || cTo < 0 {
		return short, deep, fmt.Errorf("missing columns (type %d, fuel %d, CO2 %d, CO2 per transport work %d, voyages %d %d %d)",
			cType, cFuel, cCO2, cPerWork, cIntra, cFrom, cTo)
	}
	short.hasCO2e, deep.hasCO2e = cCO2e >= 0, cCO2e >= 0
	cell := func(r []string, i int) float64 {
		if i < 0 || i >= len(r) {
			return math.NaN()
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(r[i]), 64)
		if err != nil {
			return math.NaN()
		}
		return v
	}
	cPerDist := col("CO₂ emissions per distance [")
	for _, r := range rows[h+1:] {
		if cType >= len(r) || r[cType] != shipType {
			continue
		}
		fuel, co2, perWork := cell(r, cFuel), cell(r, cCO2), cell(r, cPerWork) // g CO2 per t·nm
		co2e := co2
		if cCO2e >= 0 {
			co2e = cell(r, cCO2e)
		}
		intra, from, to := cell(r, cIntra), cell(r, cFrom), cell(r, cTo)
		voyages := intra + from + to
		ok := fuel > 0 && co2 > 0 && co2e > 0 && perWork > 0 && voyages > 0
		if ok && cPerDist >= 0 {
			// Average cargo on board: (kg CO2 per nm) / (g CO2 per t·nm).
			cargo := cell(r, cPerDist) * 1000 / perWork
			ok = cargo >= minCargoT && cargo <= maxCargoT
		}
		t := &deep
		if ok && intra/voyages >= shortSeaShare {
			t = &short
		}
		if !ok {
			deep.skipped++ // counted once, with the whole fleet
			continue
		}
		t.ships++
		t.fuelT += fuel
		t.co2eT += co2e
		t.workTNm += co2 * 1e6 / perWork
	}
	if short.ships == 0 || deep.ships == 0 {
		return short, deep, fmt.Errorf("%s reports: %d short-sea, %d deep-sea", shipType, short.ships, deep.ships)
	}
	short.skipped = deep.skipped
	return short, deep, nil
}

// dataset is the factor file for a year's fleet totals; wttKgPerT is the
// well-to-tank kg CO2e per tonne of fuel.
func dataset(year int, version string, short, deep totals, wttKgPerT float64) factors.Dataset {
	leg := func(kind, who string, t totals) calc.Factor {
		workTKm := t.workTNm * nmKm
		gases := "CO2e (CO2, CH4, N2O)"
		if !t.hasCO2e {
			gases = "CO2 only"
		}
		return calc.Factor{
			Mode: calc.Sea, VehicleType: fmt.Sprintf("container_ship/eu_mrv_%d/%s", year, kind), Year: year, DistanceBasis: calc.DistActual,
			TTW: round2(t.co2eT * 1e6 / workTKm), WTT: round2(t.fuelT * wttKgPerT * 1000 / workTKm),
			Source: fmt.Sprintf("EU MRV %d (EMSA THETIS-MRV, %s): %d %s container ships (%s) with full-year reports, %.0f billion tonne-km. "+
				"TTW: all %s they reported divided by their transport work (cargo mass x distance actually sailed). "+
				"WTT: their fuel x DESNZ 2026 marine fuel oil well-to-tank (%.1f kg CO2e/t).",
				year, version, t.ships, strings.ReplaceAll(kind, "_", "-"), who, workTKm/1e9, gases, wttKgPerT),
		}
	}
	pct := fmt.Sprintf("%.0f%%", shortSeaShare*100)
	s := leg("short_sea", "at least "+pct+" of their voyage CO2 between EU ports", short)
	d := leg("deep_sea", "less than "+pct+" of their voyage CO2 between EU ports", deep)
	return factors.Dataset{
		Name: fmt.Sprintf("eu-mrv-%d", year),
		Note: fmt.Sprintf("Container ship factors from EU MRV data (Regulation (EU) 2015/757), reporting period %d, as published by EMSA (THETIS-MRV, %s). "+
			"Source: European Maritime Safety Agency; reproduction authorised with acknowledgement of the source. "+
			"\"container_ship\" means the short-sea factor on legs under %d km and the deep-sea factor from %d km.", year, version, ShortBelowKm, ShortBelowKm),
		Legs: []calc.Factor{s, d},
		VehicleAliases: []factors.VehicleAlias{
			{Mode: calc.Sea, Alias: "container_ship", VehicleType: d.VehicleType, ShortVehicleType: s.VehicleType, ShortBelowKm: ShortBelowKm},
			{Mode: calc.Sea, Alias: "container_ship_short_sea", VehicleType: s.VehicleType},
			{Mode: calc.Sea, Alias: "container_ship_deep_sea", VehicleType: d.VehicleType},
		},
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
