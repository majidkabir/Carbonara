// Package calc is Carbonara's calculation core: the shipment model and the
// emission calculation, with no dependencies outside the standard library.
//
// A Calculator combines a FactorProvider (emission factors) with an
// optional DistanceProvider (for legs without a given distance). Package
// factors provides datasets, including the built-in UK DESNZ factors, and
// package distance provides great-circle, road (OSRM) and sea routing.
//
// Each leg is calculated by one of four methods, chosen by its inputs:
//
//   - intensity (default): tonne-km x a published g CO2e/tkm factor
//   - fuel (Leg.Fuel set): measured fuel x fuel factor, allocated by tonne-km
//   - load model (Leg.LoadFactor set): a vehicle's per-km emissions at that
//     load, plus empty running, over its payload
//   - energy (Leg.EnergyKWhPerTkm set): electricity x the country's grid
//
// Results split emissions into tank-to-wheel (TTW), well-to-tank (WTT) and
// well-to-wheel (WTW), per leg and hub, and record the method, data type
// (primary, modelled, default), distance method and factor source of each.
package calc
