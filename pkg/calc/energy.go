package calc

import (
	"errors"
	"fmt"
	"strings"
)

// GridFactor is the emissions intensity of a country's electricity.
type GridFactor struct {
	Country string  `json:"country"` // ISO 3166-1 alpha-3, e.g. "DNK"
	Year    int     `json:"year"`
	G       float64 `json:"g_per_kwh"` // g CO2e per kWh generated
	Source  string  `json:"source"`
}

// GridProvider is optionally implemented by a FactorProvider.
type GridProvider interface {
	Grid(country string) (GridFactor, error)
}

// MethodEnergy is the method for legs given as energy per tonne-km.
const MethodEnergy = "energy"

// ErrInvalidEnergy reports bad energy_kwh_per_tkm or country input.
var ErrInvalidEnergy = errors.New("invalid energy data")

// energyLeg computes an electric leg: the vehicle emits nothing (TTW = 0);
// generating the electricity is upstream (WTT = kWh/tkm x grid g/kWh).
func (c Calculator) energyLeg(lr LegResult, leg Leg) (LegResult, error) {
	if leg.EnergyKWhPerTkm <= 0 {
		return LegResult{}, fmt.Errorf("%w: energy_kwh_per_tkm must be > 0", ErrInvalidEnergy)
	}
	if strings.TrimSpace(leg.Country) == "" {
		return LegResult{}, fmt.Errorf("%w: energy_kwh_per_tkm needs country (ISO 3166 alpha-3, e.g. DNK)", ErrInvalidEnergy)
	}
	gp, ok := c.Factors.(GridProvider)
	if !ok {
		return LegResult{}, fmt.Errorf("%w: dataset has no electricity grid factors", ErrInvalidEnergy)
	}
	g, err := gp.Grid(leg.Country)
	if err != nil {
		return LegResult{}, err
	}
	f := Factor{
		Mode: leg.Mode, VehicleType: leg.VehicleType, TTW: 0, WTT: leg.EnergyKWhPerTkm * g.G, Year: g.Year,
		Source: fmt.Sprintf("%g kWh/tkm x %s grid %g g/kWh (%d, %s)", leg.EnergyKWhPerTkm, g.Country, g.G, g.Year, g.Source),
	}
	lr.Method = MethodEnergy
	lr.Factor = &f
	lr.EnergyKWhPerTkm, lr.Country = leg.EnergyKWhPerTkm, g.Country
	lr.TTWKg = 0
	lr.WTWKg = lr.TonneKm * f.WTT / 1000
	return lr, nil
}
