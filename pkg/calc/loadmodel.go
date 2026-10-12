package calc

import (
	"errors"
	"fmt"
	"sort"
)

// LoadModel gives a vehicle's emissions per vehicle-km as a function of
// load, so shipments can use their own load factor and empty running
// instead of a fleet-average factor:
//
//	E(lf)          = emissions per vehicle-km at load factor lf, interpolated
//	                 linearly between Points
//	g/tkm (TTW/WTT) = (E(lf) + etf * E(0)) / (PayloadT * lf)
//
// lf is the share of payload capacity used on the loaded trip (0 < lf <= 1),
// etf the empty trip factor: empty km per loaded km.
type LoadModel struct {
	Mode        Mode        `json:"mode"`
	VehicleType string      `json:"vehicle_type"` // vehicle class, without a load suffix
	PayloadT    float64     `json:"payload_t"`
	Points      []LoadPoint `json:"points"`
	Source      string      `json:"source"`
	Year        int         `json:"year"`
}

// LoadPoint is emissions per vehicle-km at one load factor.
type LoadPoint struct {
	Load float64 `json:"load"`          // 0..1
	TTW  float64 `json:"ttw_g_per_vkm"` // tank-to-wheel
	WTT  float64 `json:"wtt_g_per_vkm"` // well-to-tank
}

// LoadModelProvider is optionally implemented by a FactorProvider.
type LoadModelProvider interface {
	LoadModel(mode Mode, vehicleType string) (LoadModel, error)
}

// ErrInvalidLoad reports bad load_factor or empty_trip_factor input.
var ErrInvalidLoad = errors.New("invalid load data")

// Validate checks the model is usable: points sorted, covering 0 and 1.
func (m LoadModel) Validate() error {
	if m.PayloadT <= 0 {
		return fmt.Errorf("load model %s/%s: payload must be > 0", m.Mode, m.VehicleType)
	}
	if len(m.Points) < 2 {
		return fmt.Errorf("load model %s/%s: need at least 2 points", m.Mode, m.VehicleType)
	}
	if !sort.SliceIsSorted(m.Points, func(i, j int) bool { return m.Points[i].Load < m.Points[j].Load }) {
		return fmt.Errorf("load model %s/%s: points must be sorted by load", m.Mode, m.VehicleType)
	}
	if m.Points[0].Load != 0 || m.Points[len(m.Points)-1].Load != 1 {
		return fmt.Errorf("load model %s/%s: points must span load 0 to 1", m.Mode, m.VehicleType)
	}
	for i, p := range m.Points {
		if p.TTW < 0 || p.WTT < 0 {
			return fmt.Errorf("load model %s/%s: negative value", m.Mode, m.VehicleType)
		}
		if i > 0 && p.Load == m.Points[i-1].Load {
			return fmt.Errorf("load model %s/%s: duplicate load %v", m.Mode, m.VehicleType, p.Load)
		}
	}
	return nil
}

// at interpolates emissions per vehicle-km at load lf.
func (m LoadModel) at(lf float64) (ttw, wtt float64) {
	ps := m.Points
	for i := 1; i < len(ps); i++ {
		if lf <= ps[i].Load {
			a, b := ps[i-1], ps[i]
			t := (lf - a.Load) / (b.Load - a.Load)
			return a.TTW + t*(b.TTW-a.TTW), a.WTT + t*(b.WTT-a.WTT)
		}
	}
	last := ps[len(ps)-1]
	return last.TTW, last.WTT
}

// PerTonneKm returns TTW and WTT in g CO2e per tonne-km for a load factor
// and empty trip factor.
func (m LoadModel) PerTonneKm(lf, etf float64) (ttw, wtt float64, err error) {
	if lf <= 0 || lf > 1 {
		return 0, 0, fmt.Errorf("%w: load_factor must be > 0 and <= 1, got %v", ErrInvalidLoad, lf)
	}
	if etf < 0 {
		return 0, 0, fmt.Errorf("%w: empty_trip_factor must be >= 0, got %v", ErrInvalidLoad, etf)
	}
	lt, lw := m.at(lf)
	et, ew := m.at(0)
	t := m.PayloadT * lf
	return (lt + etf*et) / t, (lw + etf*ew) / t, nil
}
