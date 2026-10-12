// Package factors provides emission factor datasets. A sample dataset is
// embedded in the binary; real datasets (GLEC, DEFRA, licensed sets) can be
// loaded from a file with the same JSON shape.
package factors

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/majidkabir/carbonara/pkg/calc"
)

//go:embed data/*.json
var embedded embed.FS

// Dataset is a named set of factors that implements calc.FactorProvider.
type Dataset struct {
	Name  string            `json:"name"`
	Note  string            `json:"note,omitempty"`
	Legs  []calc.Factor     `json:"legs"`
	Hubs  []calc.HubFactor  `json:"hubs"`
	Fuels []calc.FuelFactor `json:"fuels"`

	legIdx  map[string]calc.Factor
	hubIdx  map[string]calc.HubFactor
	fuelIdx map[string]calc.FuelFactor
}

// Embedded loads a dataset bundled in the binary, e.g. "sample".
func Embedded(name string) (*Dataset, error) {
	f, err := embedded.Open("data/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("embedded dataset %q: %w", name, err)
	}
	defer f.Close()
	return Parse(f)
}

// Load reads a dataset from a JSON file.
func Load(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse decodes and indexes a dataset, rejecting duplicates and negative values.
func Parse(r io.Reader) (*Dataset, error) {
	var d Dataset
	if err := json.NewDecoder(r).Decode(&d); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}
	d.legIdx = make(map[string]calc.Factor, len(d.Legs))
	for _, f := range d.Legs {
		if f.TTW < 0 || f.WTT < 0 {
			return nil, fmt.Errorf("negative factor for %s/%s", f.Mode, f.VehicleType)
		}
		k := legKey(f.Mode, f.VehicleType)
		if _, dup := d.legIdx[k]; dup {
			return nil, fmt.Errorf("duplicate factor for %s", k)
		}
		d.legIdx[k] = f
	}
	d.hubIdx = make(map[string]calc.HubFactor, len(d.Hubs))
	for _, h := range d.Hubs {
		if h.WTWPerTonneG < 0 {
			return nil, fmt.Errorf("negative hub factor for %s", h.HubType)
		}
		if _, dup := d.hubIdx[h.HubType]; dup {
			return nil, fmt.Errorf("duplicate hub factor for %s", h.HubType)
		}
		d.hubIdx[h.HubType] = h
	}
	d.fuelIdx = make(map[string]calc.FuelFactor, len(d.Fuels))
	for _, f := range d.Fuels {
		if f.TTW < 0 || f.WTT < 0 {
			return nil, fmt.Errorf("negative fuel factor for %s", f.Fuel)
		}
		if _, dup := d.fuelIdx[f.Fuel]; dup {
			return nil, fmt.Errorf("duplicate fuel factor for %s", f.Fuel)
		}
		d.fuelIdx[f.Fuel] = f
	}
	return &d, nil
}

func legKey(m calc.Mode, vt string) string { return string(m) + "/" + vt }

// LegFactor implements calc.FactorProvider.
func (d *Dataset) LegFactor(m calc.Mode, vt string) (calc.Factor, error) {
	if f, ok := d.legIdx[legKey(m, vt)]; ok {
		return f, nil
	}
	return calc.Factor{}, fmt.Errorf("no factor for %s/%s in dataset %q", m, vt, d.Name)
}

// HubFactor implements calc.FactorProvider.
func (d *Dataset) HubFactor(t string) (calc.HubFactor, error) {
	if h, ok := d.hubIdx[t]; ok {
		return h, nil
	}
	return calc.HubFactor{}, fmt.Errorf("no hub factor for %s in dataset %q", t, d.Name)
}

// FuelFactor implements calc.FactorProvider.
func (d *Dataset) FuelFactor(fuel string) (calc.FuelFactor, error) {
	if f, ok := d.fuelIdx[fuel]; ok {
		return f, nil
	}
	return calc.FuelFactor{}, fmt.Errorf("no fuel factor for %s in dataset %q", fuel, d.Name)
}

var _ calc.FactorProvider = (*Dataset)(nil)
