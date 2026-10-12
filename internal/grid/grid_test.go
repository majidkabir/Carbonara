package grid

import (
	"strings"
	"testing"
)

const sample = `country,year,iso_code,population,carbon_intensity_elec
Denmark,2024,DNK,5900000,130.5
Denmark,2025,DNK,5950000,114.41
Denmark,2026,DNK,6000000,
World,2025,,8000000000,470
Europe (EU),2025,OWID_EU27,,250
Sweden,2023,SWE,,40
`

func TestParseOWID(t *testing.T) {
	f, err := ParseOWID(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Grids) != 2 {
		t.Fatalf("grids = %+v", f.Grids)
	}
	if d := f.Grids[0]; d.Country != "DNK" || d.Year != 2025 || d.G != 114.41 || !strings.Contains(d.Source, "Denmark") {
		t.Errorf("DNK = %+v (want latest non-empty year)", d)
	}
	if !strings.Contains(f.Note, "CC BY 4.0") {
		t.Error("attribution missing")
	}
	for name, in := range map[string]string{
		"empty":     "",
		"no column": "country,year,iso_code\nDenmark,2025,DNK\n",
		"no values": "country,year,iso_code,carbon_intensity_elec\nWorld,2025,,1\n",
		"bad value": "country,year,iso_code,carbon_intensity_elec\nDenmark,2025,DNK,abc\n",
	} {
		if _, err := ParseOWID(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
