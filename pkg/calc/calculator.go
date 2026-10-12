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

// DistanceMethoder is optionally implemented by a DistanceProvider to name
// how it obtains distances for a mode (e.g. "osrm", "great_circle"). The
// name is recorded on each leg result.
type DistanceMethoder interface {
	DistanceMethod(mode Mode) string
}

// ContainerLoadProvider is optionally implemented by a FactorProvider to
// resolve container load profiles (e.g. "average") to net tonnes per TEU.
type ContainerLoadProvider interface {
	ContainerLoad(name string) (float64, error)
}

// DistanceGiven is the method recorded when the input supplied distance_km.
const DistanceGiven = "given"

// Calculator computes shipment emissions.
type Calculator struct {
	Factors   FactorProvider
	Distances DistanceProvider
}

var (
	ErrInvalidWeight = errors.New("shipment weight must be > 0")
	ErrNoLegs        = errors.New("shipment has no legs")
	ErrInvalidFuel   = errors.New("invalid fuel data")
	ErrInvalidTEU    = errors.New("invalid container data")
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
	weightKg, basis, err := c.weight(s)
	if err != nil {
		return Result{}, err
	}
	if len(s.Legs) == 0 {
		return Result{}, ErrNoLegs
	}
	tonnes := weightKg / 1000
	res := Result{ShipmentID: s.ID, WeightKg: weightKg, WeightBasis: basis}

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

// weight returns the cargo weight in kg and how it was determined.
func (c Calculator) weight(s Shipment) (float64, string, error) {
	if s.TEU == 0 && s.TEUNetTonnes == 0 && s.ContainerLoad == "" {
		if s.WeightKg <= 0 {
			return 0, "", ErrInvalidWeight
		}
		return s.WeightKg, "weight", nil
	}
	switch {
	case s.WeightKg > 0:
		return 0, "", fmt.Errorf("%w: give weight_kg or teu, not both", ErrInvalidTEU)
	case s.TEU <= 0:
		return 0, "", fmt.Errorf("%w: teu must be > 0", ErrInvalidTEU)
	case s.TEUNetTonnes > 0 && s.ContainerLoad != "":
		return 0, "", fmt.Errorf("%w: give teu_net_tonnes or container_load, not both", ErrInvalidTEU)
	}
	perTEU := s.TEUNetTonnes
	if s.ContainerLoad != "" {
		clp, ok := c.Factors.(ContainerLoadProvider)
		if !ok {
			return 0, "", fmt.Errorf("%w: dataset has no container load profiles", ErrInvalidTEU)
		}
		var err error
		if perTEU, err = clp.ContainerLoad(s.ContainerLoad); err != nil {
			return 0, "", fmt.Errorf("%w: %v", ErrInvalidTEU, err)
		}
	}
	if perTEU <= 0 {
		return 0, "", fmt.Errorf("%w: need teu_net_tonnes or container_load", ErrInvalidTEU)
	}
	return s.TEU * perTEU * 1000, "teu", nil
}

func (c Calculator) leg(ctx context.Context, tonnes float64, leg Leg) (LegResult, error) {
	dist, method, err := c.distance(ctx, leg)
	if err != nil {
		return LegResult{}, err
	}
	tkm := tonnes * dist
	lr := LegResult{LegID: leg.ID, DistanceKm: dist, DistanceMethod: method, TonneKm: tkm}

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

func (c Calculator) distance(ctx context.Context, leg Leg) (float64, string, error) {
	if leg.DistanceKm > 0 {
		return leg.DistanceKm, DistanceGiven, nil
	}
	if c.Distances == nil {
		return 0, "", errors.New("no distance given and no distance provider")
	}
	d, err := c.Distances.Distance(ctx, leg.Mode, leg.From, leg.To)
	if err != nil {
		return 0, "", fmt.Errorf("distance: %w", err)
	}
	method := ""
	if dm, ok := c.Distances.(DistanceMethoder); ok {
		method = dm.DistanceMethod(leg.Mode)
	}
	return d, method, nil
}
