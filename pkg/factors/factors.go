// Package factors provides emission factor datasets. Datasets are embedded
// in the binary (see Embedded and EmbeddedNames) or loaded from a JSON file
// with the same shape, e.g. one converted from GLEC or a licensed source.
package factors

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/distance"
)

// Default is the embedded dataset used when none is chosen.
const Default = "desnz-2026"

//go:embed data/*.json
var embedded embed.FS

// Dataset is a named set of factors that implements calc.FactorProvider.
type Dataset struct {
	Name  string            `json:"name"`
	Note  string            `json:"note,omitempty"`
	Legs  []calc.Factor     `json:"legs"`
	Hubs  []calc.HubFactor  `json:"hubs"`
	Fuels []calc.FuelFactor `json:"fuels"`
	// DistanceAdjustments turn great-circle distances into estimated
	// actual distances per mode, matching the dataset's methodology.
	DistanceAdjustments []distance.Adjustment `json:"distance_adjustments,omitempty"`
	// VehicleAliases and FuelAliases map short names to full ids. Lookups
	// accept either; results always carry the full id.
	VehicleAliases []VehicleAlias    `json:"vehicle_aliases,omitempty"`
	FuelAliases    map[string]string `json:"fuel_aliases,omitempty"`
	// ContainerLoads maps profile names (e.g. "average") to net cargo
	// tonnes per TEU, for shipments given in TEU.
	ContainerLoads map[string]float64 `json:"container_loads,omitempty"`

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

// VehicleAlias is a short name for a vehicle type within one mode.
type VehicleAlias struct {
	Mode        calc.Mode `json:"mode"`
	Alias       string    `json:"alias"`
	VehicleType string    `json:"vehicle_type"`
}

// EmbeddedNames lists the datasets bundled in the binary.
func EmbeddedNames() []string {
	entries, _ := fs.ReadDir(embedded, "data")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), path.Ext(e.Name())))
	}
	sort.Strings(names)
	return names
}

// Open loads an embedded dataset by name, or a JSON file by path.
// An empty name opens Default.
func Open(nameOrPath string) (*Dataset, error) {
	if nameOrPath == "" {
		nameOrPath = Default
	}
	for _, n := range EmbeddedNames() {
		if n == nameOrPath {
			return Embedded(n)
		}
	}
	return Load(nameOrPath)
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
	if _, err := distance.NewGreatCircle(d.DistanceAdjustments); err != nil {
		return nil, err
	}
	for _, a := range d.VehicleAliases {
		k := legKey(a.Mode, a.Alias)
		target, ok := d.legIdx[legKey(a.Mode, a.VehicleType)]
		if !ok {
			return nil, fmt.Errorf("vehicle alias %s points to unknown %s", k, a.VehicleType)
		}
		if _, clash := d.legIdx[k]; clash {
			return nil, fmt.Errorf("vehicle alias %s clashes with an existing vehicle type", k)
		}
		d.legIdx[k] = target
	}
	for name, t := range d.ContainerLoads {
		if t <= 0 {
			return nil, fmt.Errorf("container load %q must be > 0 tonnes per TEU", name)
		}
	}
	for alias, id := range d.FuelAliases {
		target, ok := d.fuelIdx[id]
		if !ok {
			return nil, fmt.Errorf("fuel alias %s points to unknown %s", alias, id)
		}
		if _, clash := d.fuelIdx[alias]; clash {
			return nil, fmt.Errorf("fuel alias %s clashes with an existing fuel", alias)
		}
		d.fuelIdx[alias] = target
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

// ContainerLoad implements calc.ContainerLoadProvider.
func (d *Dataset) ContainerLoad(name string) (float64, error) {
	if t, ok := d.ContainerLoads[name]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("no container load profile %q in dataset %q", name, d.Name)
}

// Distances returns a great-circle provider using the dataset's adjustments.
func (d *Dataset) Distances() distance.GreatCircle {
	g, _ := distance.NewGreatCircle(d.DistanceAdjustments) // validated in Parse
	return g
}

var (
	_ calc.FactorProvider        = (*Dataset)(nil)
	_ calc.ContainerLoadProvider = (*Dataset)(nil)
)
