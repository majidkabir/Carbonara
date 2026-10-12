package factors

import (
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

func TestEmbeddedSample(t *testing.T) {
	d, err := Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	f, err := d.LegFactor(calc.Road, "truck_40t")
	if err != nil {
		t.Fatal(err)
	}
	if f.WTW() != 75 {
		t.Errorf("WTW = %v, want 75", f.WTW())
	}
	if _, err := d.LegFactor(calc.Road, "unicycle"); err == nil {
		t.Error("want error for unknown vehicle type")
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]string{
		"duplicate": `{"legs":[{"mode":"road","vehicle_type":"a"},{"mode":"road","vehicle_type":"a"}]}`,
		"negative":  `{"legs":[{"mode":"road","vehicle_type":"a","ttw_g_per_tkm":-1}]}`,
		"bad json":  `{`,
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestEmbeddedSample_Fuel(t *testing.T) {
	d, err := Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.FuelFactor("diesel"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FuelFactor("unobtainium"); err == nil {
		t.Error("want error for unknown fuel")
	}
}
