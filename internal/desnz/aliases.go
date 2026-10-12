package desnz

import (
	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/distance"
	"github.com/majidkabir/carbonara/pkg/factors"
)

// Short names for the most-used factors. Targets are DESNZ label slugs; if
// DESNZ renames a category, the importer warns and drops that alias.
//
// Choices: road aliases are "average laden" (DESNZ's fleet-average load),
// air excludes radiative forcing (as ISO 14083 reports), fuel aliases are
// per litre.
var vehicleAliases = []factors.VehicleAlias{
	{Mode: calc.Road, Alias: "hgv", VehicleType: "hgv_non_refrigerated_all_diesel/average_non_refrigerated_hgvs/average_laden"},
	{Mode: calc.Road, Alias: "hgv_rigid", VehicleType: "hgv_non_refrigerated_all_diesel/average_non_refrigerated_rigids/average_laden"},
	{Mode: calc.Road, Alias: "hgv_artic", VehicleType: "hgv_non_refrigerated_all_diesel/average_non_refrigerated_artics/average_laden"},
	{Mode: calc.Road, Alias: "hgv_artic_33t", VehicleType: "hgv_non_refrigerated_all_diesel/articulated_33t/average_laden"},
	{Mode: calc.Road, Alias: "hgv_reefer", VehicleType: "hgv_refrigerated_all_diesel/average_refrigerated_hgvs/average_laden"},
	{Mode: calc.Road, Alias: "van", VehicleType: "vans/average_up_to_3_5_tonnes/diesel"},
	{Mode: calc.Road, Alias: "van_electric", VehicleType: "vans/average_up_to_3_5_tonnes/battery_electric_vehicle"},
	{Mode: calc.Rail, Alias: "freight_train", VehicleType: "rail/freight_train"},
	{Mode: calc.Sea, Alias: "container_ship", VehicleType: "cargo_ship/container_ship/average"},
	{Mode: calc.Sea, Alias: "bulk_carrier", VehicleType: "cargo_ship/bulk_carrier/average"},
	{Mode: calc.Sea, Alias: "general_cargo", VehicleType: "cargo_ship/general_cargo/average"},
	{Mode: calc.Sea, Alias: "roro", VehicleType: "cargo_ship/roro_ferry/average"},
	{Mode: calc.Sea, Alias: "ropax", VehicleType: "cargo_ship/large_ropax_ferry/average"},
	{Mode: calc.Air, Alias: "air_freight", VehicleType: "freight_flights/international_to_from_non_uk/without_rf"},
}

var fuelAliases = map[string]string{
	"diesel":          "liquid_fuels/diesel_average_biofuel_blend/litres",
	"diesel_mineral":  "liquid_fuels/diesel_100pct_mineral_diesel/litres",
	"petrol":          "liquid_fuels/petrol_average_biofuel_blend/litres",
	"hvo":             "biofuel/biodiesel_hvo/litres",
	"marine_gas_oil":  "liquid_fuels/marine_gas_oil/litres",
	"marine_fuel_oil": "liquid_fuels/marine_fuel_oil/litres",
	"jet_fuel":        "liquid_fuels/aviation_turbine_fuel/litres",
}

// distanceAdjustments are methodology conventions, not DESNZ data: DESNZ
// publishes no distance rules, so Carbonara adds the standard air rule.
var distanceAdjustments = []distance.Adjustment{
	{Mode: calc.Air, AddKm: 95, Source: "EN 16258 convention: great-circle distance + 95 km for take-off and landing (also used by EcoTransIT World)"},
}

// addAliases attaches the aliases whose targets exist in ds.
func addAliases(ds *factors.Dataset, warn func(string, ...any)) {
	ds.DistanceAdjustments = append(ds.DistanceAdjustments, distanceAdjustments...)
	legs := map[string]bool{}
	for _, f := range ds.Legs {
		legs[string(f.Mode)+"/"+f.VehicleType] = true
	}
	for _, a := range vehicleAliases {
		if !legs[string(a.Mode)+"/"+a.VehicleType] {
			warn("alias %s/%s: target %s not in this file, dropped", a.Mode, a.Alias, a.VehicleType)
			continue
		}
		ds.VehicleAliases = append(ds.VehicleAliases, a)
	}
	fuels := map[string]bool{}
	for _, f := range ds.Fuels {
		fuels[f.Fuel] = true
	}
	for alias, id := range fuelAliases {
		if !fuels[id] {
			warn("fuel alias %s: target %s not in this file, dropped", alias, id)
			continue
		}
		if ds.FuelAliases == nil {
			ds.FuelAliases = map[string]string{}
		}
		ds.FuelAliases[alias] = id
	}
}
