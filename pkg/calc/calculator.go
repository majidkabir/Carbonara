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
	FuelFactor(fuel string) (FuelFactor, error)
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
	ErrInvalidFuel   = errors.New("invalid fuel data")
)

// Calculate returns the emissions of a shipment.
//
// Intensity method (default, modelled data):
//
//	tonne-km = (weight_kg / 1000) * distance_km
//	TTW kg   = tonne-km * TTW g/tkm / 1000
//	WTW kg   = tonne-km * WTW g/tkm / 1000
//
// Fuel method (leg.Fuel set, primary data):
//
//	share    = tonne-km / vehicle_tonne_km   (1 when vehicle_tonne_km is 0)
//	TTW kg   = fuel_amount * TTW kg/unit * share
//	WTW kg   = fuel_amount * WTW kg/unit * share
//
// Hubs (WTW only):
//
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
		lr, err := c.leg(ctx, tonnes, leg)
		if err != nil {
			return Result{}, fmt.Errorf("leg %q: %w", leg.ID, err)
		}
		res.Legs = append(res.Legs, lr)
		res.TonneKm += lr.TonneKm
		res.TTWKg += lr.TTWKg
		res.WTWKg += lr.WTWKg
	}

	if res.TonneKm > 0 {
		res.IntensityG = res.WTWKg * 1000 / res.TonneKm
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

func (c Calculator) leg(ctx context.Context, tonnes float64, leg Leg) (LegResult, error) {
	dist, err := c.distance(ctx, leg)
	if err != nil {
		return LegResult{}, err
	}
	tkm := tonnes * dist
	lr := LegResult{LegID: leg.ID, DistanceKm: dist, TonneKm: tkm}

	if leg.Fuel != nil {
		return c.fuelLeg(lr, *leg.Fuel)
	}

	f, err := c.Factors.LegFactor(leg.Mode, leg.VehicleType)
	if err != nil {
		return LegResult{}, err
	}
	lr.Method = MethodIntensity
	lr.Factor = &f
	lr.TTWKg = tkm * f.TTW / 1000
	lr.WTWKg = tkm * f.WTW() / 1000
	return lr, nil
}

func (c Calculator) fuelLeg(lr LegResult, fu FuelUse) (LegResult, error) {
	if fu.Amount < 0 || fu.VehicleTonneKm < 0 {
		return LegResult{}, fmt.Errorf("%w: amount and vehicle_tonne_km must be >= 0", ErrInvalidFuel)
	}
	share := 1.0
	if fu.VehicleTonneKm > 0 {
		share = lr.TonneKm / fu.VehicleTonneKm
		if share > 1+1e-9 {
			return LegResult{}, fmt.Errorf("%w: shipment tonne-km %.2f exceeds vehicle tonne-km %.2f",
				ErrInvalidFuel, lr.TonneKm, fu.VehicleTonneKm)
		}
	}
	ff, err := c.Factors.FuelFactor(fu.Fuel)
	if err != nil {
		return LegResult{}, err
	}
	lr.Method = MethodFuel
	lr.FuelFactor = &ff
	lr.AllocationShare = share
	lr.TTWKg = fu.Amount * ff.TTW * share
	lr.WTWKg = fu.Amount * ff.WTW() * share
	return lr, nil
}

func (c Calculator) distance(ctx context.Context, leg Leg) (float64, error) {
	if leg.DistanceKm > 0 {
		return leg.DistanceKm, nil
	}
	if c.Distances == nil {
		return 0, errors.New("no distance given and no distance provider")
	}
	d, err := c.Distances.Distance(ctx, leg.Mode, leg.From, leg.To)
	if err != nil {
		return 0, fmt.Errorf("distance: %w", err)
	}
	return d, nil
}
