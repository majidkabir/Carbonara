package calc

import (
	"context"
	"errors"
	"fmt"
)

// FactorProvider supplies emission factors.
type FactorProvider interface {
	LegFactor(mode Mode, vehicleType string) (Factor, error)
	HubFactor(hubType string) (HubFactor, error)
}

// DistanceProvider supplies the distance in km for a leg when it isn't given.
type DistanceProvider interface {
	Distance(ctx context.Context, mode Mode, from, to Location) (float64, error)
}

// Calculator computes shipment emissions.
type Calculator struct {
	Factors   FactorProvider
	Distances DistanceProvider
}

var (
	ErrInvalidWeight = errors.New("shipment weight must be > 0")
	ErrNoLegs        = errors.New("shipment has no legs")
)

// Calculate returns the emissions of a shipment.
//
//	tonne-km = (weight_kg / 1000) * distance_km
//	TTW kg   = tonne-km * TTW g/tkm / 1000
//	WTW kg   = tonne-km * WTW g/tkm / 1000
//	hub kg   = (weight_kg / 1000) * hub g/t / 1000
func (c Calculator) Calculate(ctx context.Context, s Shipment) (Result, error) {
	if s.WeightKg <= 0 {
		return Result{}, ErrInvalidWeight
	}
	if len(s.Legs) == 0 {
		return Result{}, ErrNoLegs
	}
	tonnes := s.WeightKg / 1000
	res := Result{ShipmentID: s.ID}

	for _, leg := range s.Legs {
		f, err := c.Factors.LegFactor(leg.Mode, leg.VehicleType)
		if err != nil {
			return Result{}, fmt.Errorf("leg %q: %w", leg.ID, err)
		}
		dist := leg.DistanceKm
		if dist <= 0 {
			if c.Distances == nil {
				return Result{}, fmt.Errorf("leg %q: no distance given and no distance provider", leg.ID)
			}
			if dist, err = c.Distances.Distance(ctx, leg.Mode, leg.From, leg.To); err != nil {
				return Result{}, fmt.Errorf("leg %q: distance: %w", leg.ID, err)
			}
		}
		tkm := tonnes * dist
		lr := LegResult{
			LegID:      leg.ID,
			DistanceKm: dist,
			TonneKm:    tkm,
			TTWKg:      tkm * f.TTW / 1000,
			WTWKg:      tkm * f.WTW() / 1000,
			Factor:     f,
		}
		res.Legs = append(res.Legs, lr)
		res.TonneKm += lr.TonneKm
		res.TTWKg += lr.TTWKg
		res.WTWKg += lr.WTWKg
	}

	transportWTW := res.WTWKg
	if res.TonneKm > 0 {
		res.IntensityG = transportWTW * 1000 / res.TonneKm
	}

	for _, h := range s.Hubs {
		hf, err := c.Factors.HubFactor(h.HubType)
		if err != nil {
			return Result{}, fmt.Errorf("hub %q: %w", h.ID, err)
		}
		hr := HubResult{HubID: h.ID, WTWKg: tonnes * hf.WTWPerTonneG / 1000}
		res.Hubs = append(res.Hubs, hr)
		res.WTWKg += hr.WTWKg
	}
	return res, nil
}
