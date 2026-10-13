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
	// FerryVehicle is the sea vehicle type for ferry crossings found on
	// road routes (default DefaultFerryVehicle).
	FerryVehicle string
}

// Input errors returned by Calculate. Wrapped errors carry details; match
// them with errors.Is.
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
	if err := validate(s); err != nil {
		return Result{}, err
	}
	weightKg, basis, err := c.weight(s)
	if err != nil {
		return Result{}, err
	}
	if len(s.Legs) == 0 {
		return Result{}, ErrNoLegs
	}
	tonnes := weightKg / 1000
	res := Result{ShipmentID: s.ID, WeightKg: weightKg, WeightBasis: basis}
	if n, ok := c.Factors.(DatasetNamer); ok {
		res.Dataset = n.DatasetName()
	}

	for _, leg := range s.Legs {
		lr, err := c.leg(ctx, tonnes, leg)
		if err != nil {
			return Result{}, fmt.Errorf("leg %q: %w", leg.ID, err)
		}
		lr.WTTKg = lr.WTWKg - lr.TTWKg
		if lr.TonneKm > 0 {
			lr.IntensityG = lr.WTWKg * 1000 / lr.TonneKm
		}
		lr.DataType = dataType(lr.Method)
		if lr.Method == MethodIntensity && lr.Factor != nil && lr.Factor.DataType != "" {
			lr.DataType = lr.Factor.DataType
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
		hr := HubResult{HubID: h.ID, HubType: h.HubType, WTWKg: tonnes * hf.WTWPerTonneG / 1000,
			DataType: DataDefault, Source: hf.Source}
		if hf.DataType != "" {
			hr.DataType = hf.DataType
		}
		res.Hubs = append(res.Hubs, hr)
		res.WTWKg += hr.WTWKg
	}
	// Hub factors are WTW only, so hubs add to WTW and, for lack of a
	// split, are not attributed to TTW or WTT.
	res.WTTKg = res.WTWKg - res.TTWKg
	for _, h := range res.Hubs {
		res.WTTKg -= h.WTWKg
	}
	var primary float64
	for _, l := range res.Legs {
		main := l.WTWKg
		for _, p := range l.Parts {
			main -= p.WTWKg
			if p.DataType == DataPrimary {
				primary += p.WTWKg
			}
		}
		if l.DataType == DataPrimary {
			primary += main
		}
	}
	for _, h := range res.Hubs {
		if h.DataType == DataPrimary {
			primary += h.WTWKg
		}
	}
	if res.WTWKg > 0 {
		res.PrimaryDataShare = primary / res.WTWKg
	}
	if err := checkResult(res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// dataType labels a leg by how its emissions were obtained: measured fuel is
// primary data; the load and energy methods model the operation from
// shipment-specific inputs; the intensity method uses published defaults.
func dataType(method string) string {
	switch method {
	case MethodFuel:
		return DataPrimary
	case MethodLoadModel, MethodEnergy:
		return DataModelled
	default:
		return DataDefault
	}
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
	dist, method, dtype, routeParts, err := c.distance(ctx, leg)
	if err != nil {
		return LegResult{}, err
	}

	// Split off parts calculated differently, and calculate the rest of the
	// leg (mainKm) as usual.
	mainKm := dist
	var parts []LegPart
	if ferryKm := routeParts[PartFerry]; leg.Mode == Road && ferryKm > 0 {
		if ferryKm >= dist {
			return LegResult{}, fmt.Errorf("%w: the road route is all ferry; give it as a sea leg", ErrInvalidInput)
		}
		p, err := c.ferry(tonnes, ferryKm, dtype)
		if err != nil {
			return LegResult{}, err
		}
		parts = append(parts, p)
		mainKm -= ferryKm
	}
	// Electric rail with known non-electrified track: that track runs on
	// the vehicle's own factor (diesel), the rest on electricity. Track with
	// unknown electrification follows the input (electric).
	if dieselKm := routeParts[PartNotElectrified]; leg.Mode == Rail && leg.EnergyKWhPerTkm != 0 && dieselKm > 0 && dieselKm < dist {
		p, err := c.electricPart(tonnes, dist-dieselKm, leg)
		if err != nil {
			return LegResult{}, err
		}
		parts = append(parts, p)
		mainKm = dieselKm
		leg.EnergyKWhPerTkm, leg.Country = 0, ""
	} else if leg.Mode == Rail && leg.EnergyKWhPerTkm != 0 && dieselKm >= dist {
		// No electrified track at all: the vehicle's own factor for all of it.
		leg.EnergyKWhPerTkm, leg.Country = 0, ""
	}

	lr, err := c.main(tonnes, mainKm, dtype, leg)
	if err != nil {
		return LegResult{}, err
	}
	lr.LegID, lr.Mode, lr.VehicleType = leg.ID, leg.Mode, leg.VehicleType
	lr.DistanceKm, lr.DistanceMethod, lr.DistanceType = dist, method, dtype
	lr.TonneKm = tonnes * dist
	lr.RouteParts = routeParts
	for _, p := range parts {
		lr.TTWKg += p.TTWKg
		lr.WTWKg += p.WTWKg
	}
	lr.Parts = parts
	return lr, nil
}

// main calculates a leg, or the part of it left after LegParts, over km.
func (c Calculator) main(tonnes, dist float64, dtype string, leg Leg) (LegResult, error) {
	lr := LegResult{TonneKm: tonnes * dist}

	methods := 0
	for _, set := range []bool{leg.Fuel != nil, leg.LoadFactor != 0, leg.EnergyKWhPerTkm != 0} {
		if set {
			methods++
		}
	}
	if methods > 1 {
		return LegResult{}, fmt.Errorf("leg uses more than one of fuel, load_factor and energy_kwh_per_tkm")
	}
	if leg.Country != "" && leg.EnergyKWhPerTkm == 0 {
		return LegResult{}, fmt.Errorf("%w: country is only used with energy_kwh_per_tkm", ErrInvalidEnergy)
	}
	if leg.EnergyKWhPerTkm != 0 {
		return c.energyLeg(lr, leg)
	}
	if leg.EmptyTripFactor != 0 && leg.LoadFactor == 0 {
		return LegResult{}, fmt.Errorf("%w: empty_trip_factor needs load_factor", ErrInvalidLoad)
	}
	if leg.Fuel != nil {
		return c.fuelLeg(lr, *leg.Fuel)
	}
	if leg.LoadFactor != 0 {
		return c.loadLeg(lr, leg)
	}

	f, err := c.Factors.LegFactor(leg.Mode, leg.VehicleType)
	if err != nil {
		return LegResult{}, err
	}
	// Emissions = tonne-km x intensity x DAF (GLEC Framework, ISO 14083).
	adjKm, daf, err := c.adjust(leg.Mode, dtype, f.DistanceBasis, dist)
	if err != nil {
		return LegResult{}, err
	}
	if daf != nil && dist > 0 {
		lr.DAF, lr.DAFSource = adjKm/dist, daf.Source
	}
	adjTkm := tonnes * adjKm
	lr.Method = MethodIntensity
	lr.Factor = &f
	lr.TTWKg = adjTkm * f.TTW / 1000
	lr.WTWKg = adjTkm * f.WTW() / 1000
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

func (c Calculator) loadLeg(lr LegResult, leg Leg) (LegResult, error) {
	lmp, ok := c.Factors.(LoadModelProvider)
	if !ok {
		return LegResult{}, fmt.Errorf("%w: dataset has no load models", ErrInvalidLoad)
	}
	m, err := lmp.LoadModel(leg.Mode, leg.VehicleType)
	if err != nil {
		return LegResult{}, err
	}
	ttw, wtt, err := m.PerTonneKm(leg.LoadFactor, leg.EmptyTripFactor)
	if err != nil {
		return LegResult{}, err
	}
	f := Factor{
		Mode: m.Mode, VehicleType: m.VehicleType, TTW: ttw, WTT: wtt, Year: m.Year,
		Source: fmt.Sprintf("load model (load factor %g, empty trip factor %g, payload %.1f t) from %s",
			leg.LoadFactor, leg.EmptyTripFactor, m.PayloadT, m.Source),
	}
	lr.Method = MethodLoadModel
	lr.Factor = &f
	lr.LoadFactor, lr.EmptyTripFactor, lr.PayloadT = leg.LoadFactor, leg.EmptyTripFactor, m.PayloadT
	lr.TTWKg = lr.TonneKm * ttw / 1000
	lr.WTWKg = lr.TonneKm * f.WTW() / 1000
	return lr, nil
}

func (c Calculator) distance(ctx context.Context, leg Leg) (km float64, method, dtype string, parts map[string]float64, err error) {
	if leg.DistanceKm > 0 {
		return leg.DistanceKm, DistanceGiven, leg.DistanceType, nil, nil
	}
	if leg.DistanceType != "" {
		return 0, "", "", nil, fmt.Errorf("%w: distance_type describes distance_km, which is not given", ErrInvalidInput)
	}
	if c.Distances == nil {
		return 0, "", "", nil, errors.New("no distance given and no distance provider")
	}
	var d float64
	if r, ok := c.Distances.(Router); ok {
		var rt Route
		rt, err = r.Route(ctx, leg.Mode, leg.From, leg.To)
		d, parts = rt.Km, rt.Parts
	} else {
		d, err = c.Distances.Distance(ctx, leg.Mode, leg.From, leg.To)
	}
	if err != nil {
		return 0, "", "", nil, fmt.Errorf("distance: %w", err)
	}
	for k, v := range parts {
		if !(v >= 0) || v > d*(1+1e-9) {
			return 0, "", "", nil, fmt.Errorf("distance: route part %s of %v km in a %v km route", k, v, d)
		}
	}
	if dm, ok := c.Distances.(DistanceMethoder); ok {
		method = dm.DistanceMethod(leg.Mode)
	}
	if dt, ok := c.Distances.(DistanceTyper); ok {
		dtype = dt.DistanceType(leg.Mode)
	}
	return d, method, dtype, parts, nil
}
