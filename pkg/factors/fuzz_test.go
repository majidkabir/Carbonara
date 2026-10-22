package factors

import (
	"strings"
	"testing"

	"github.com/majidkabir/sootcase/pkg/calc"
)

// Any dataset JSON either fails to parse or gives a dataset whose own
// entries can all be looked up.
func FuzzParse(f *testing.F) {
	f.Add(`{"name":"x","legs":[{"mode":"road","vehicle_type":"a","ttw_g_per_tkm":1}],"vehicle_aliases":[{"mode":"road","alias":"b","vehicle_type":"a"}],"fuel_aliases":{"d":"f"},"fuels":[{"fuel":"f","unit":"l"}],"grids":[{"country":"DNK","g_per_kwh":1}],"container_loads":{"avg":10},"load_models":[{"mode":"road","vehicle_type":"a","payload_t":1,"points":[{"load":0},{"load":1}]}],"distance_adjustments":[{"mode":"air","add_km":95}]}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, in string) {
		ds, err := Parse(strings.NewReader(in))
		if err != nil {
			return
		}
		for _, l := range ds.Legs {
			if _, err := ds.LegFactor(l.Mode, l.VehicleType); err != nil {
				t.Fatalf("leg %s/%s not found after parse: %v", l.Mode, l.VehicleType, err)
			}
		}
		for _, a := range ds.VehicleAliases {
			if _, err := ds.LegFactor(a.Mode, a.Alias); err != nil {
				t.Fatalf("alias %s not found: %v", a.Alias, err)
			}
		}
		for _, m := range ds.LoadModels {
			if _, _, err := m.PerTonneKm(0.5, 0.1); err != nil {
				t.Fatalf("load model %s: %v", m.VehicleType, err)
			}
		}
		_ = ds.Distances()
		_, _ = ds.LoadModel(calc.Road, "a")
	})
}
