// Package calc holds the Carbonara domain model and the ISO 14083-style
// emission calculation. It has no dependencies outside the standard library;
// factors and distances are supplied through small interfaces.
package calc

// Mode is a transport mode.
type Mode string

const (
	Road           Mode = "road"
	Rail           Mode = "rail"
	Sea            Mode = "sea"
	Air            Mode = "air"
	InlandWaterway Mode = "inland_waterway"
)

// Location is a point on the map.
type Location struct {
	Name string  `json:"name,omitempty"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Leg is one transport movement within a shipment.
type Leg struct {
	ID          string   `json:"id"`
	Mode        Mode     `json:"mode"`
	VehicleType string   `json:"vehicle_type"`
	From        Location `json:"from"`
	To          Location `json:"to"`
	// DistanceKm overrides the DistanceProvider when > 0 (e.g. actual odometer distance).
	DistanceKm float64 `json:"distance_km,omitempty"`
	// Fuel, when set, switches the leg to the fuel-based (primary data) method.
	Fuel *FuelUse `json:"fuel,omitempty"`
}

// FuelUse is measured fuel for the vehicle trip a leg is part of.
type FuelUse struct {
	Fuel   string  `json:"fuel"`   // e.g. "diesel", must match a FuelFactor
	Amount float64 `json:"amount"` // in the FuelFactor's unit
	// VehicleTonneKm is the total tonne-km carried on that trip, used to
	// allocate fuel to this shipment by tonne-km share. 0 means the whole
	// trip belongs to this shipment.
	VehicleTonneKm float64 `json:"vehicle_tonne_km,omitempty"`
}

// FuelFactor converts fuel to emissions, in kg CO2e per unit of fuel.
type FuelFactor struct {
	Fuel   string  `json:"fuel"`
	Unit   string  `json:"unit"` // e.g. "l", "kg", "kWh"
	TTW    float64 `json:"ttw_kg_per_unit"`
	WTT    float64 `json:"wtt_kg_per_unit"`
	Source string  `json:"source"`
	Year   int     `json:"year"`
}

// WTW is the well-to-wheel factor (WTT + TTW).
func (f FuelFactor) WTW() float64 { return f.TTW + f.WTT }

// Calculation methods recorded on each leg result.
const (
	MethodIntensity = "intensity"
	MethodFuel      = "fuel"
)

// HubStop is a transshipment or warehouse activity between legs.
type HubStop struct {
	ID      string `json:"id"`
	HubType string `json:"hub_type"`
}

// Shipment is the unit of calculation.
type Shipment struct {
	ID string `json:"id"`
	// WeightKg is the cargo weight. For containers, give TEU instead, with
	// either TEUNetTonnes or a ContainerLoad profile from the dataset.
	WeightKg      float64   `json:"weight_kg,omitempty"`
	TEU           float64   `json:"teu,omitempty"`
	TEUNetTonnes  float64   `json:"teu_net_tonnes,omitempty"`
	ContainerLoad string    `json:"container_load,omitempty"`
	Legs          []Leg     `json:"legs"`
	Hubs          []HubStop `json:"hubs,omitempty"`
}

// Factor is an intensity-based emission factor for a mode/vehicle type,
// expressed in grams CO2e per tonne-km.
type Factor struct {
	Mode        Mode    `json:"mode"`
	VehicleType string  `json:"vehicle_type"`
	TTW         float64 `json:"ttw_g_per_tkm"` // tank-to-wheel
	WTT         float64 `json:"wtt_g_per_tkm"` // well-to-tank
	Source      string  `json:"source"`
	Year        int     `json:"year"`
}

// WTW is the well-to-wheel factor (WTT + TTW).
func (f Factor) WTW() float64 { return f.TTW + f.WTT }

// HubFactor is a hub emission factor in grams CO2e (WTW) per tonne handled.
type HubFactor struct {
	HubType      string  `json:"hub_type"`
	WTWPerTonneG float64 `json:"wtw_g_per_tonne"`
	Source       string  `json:"source"`
	Year         int     `json:"year"`
}

// LegResult is the outcome for one leg.
type LegResult struct {
	LegID      string  `json:"leg_id"`
	DistanceKm float64 `json:"distance_km"`
	// DistanceMethod says where the distance came from: "given" (input),
	// or the provider's name, e.g. "great_circle", "osrm".
	DistanceMethod string  `json:"distance_method,omitempty"`
	TonneKm        float64 `json:"tonne_km"`
	TTWKg          float64 `json:"ttw_kg"`
	WTWKg          float64 `json:"wtw_kg"`
	Method         string  `json:"method"`
	// Factor is set for the intensity method, FuelFactor and
	// AllocationShare for the fuel method.
	Factor          *Factor     `json:"factor,omitempty"`
	FuelFactor      *FuelFactor `json:"fuel_factor,omitempty"`
	AllocationShare float64     `json:"allocation_share,omitempty"`
}

// HubResult is the outcome for one hub stop.
type HubResult struct {
	HubID string  `json:"hub_id"`
	WTWKg float64 `json:"wtw_kg"`
}

// Result is the outcome for a whole shipment.
type Result struct {
	ShipmentID string `json:"shipment_id"`
	// WeightKg is the cargo weight used; WeightBasis says how it was set:
	// "weight" (given) or "teu" (TEU x net tonnes per TEU).
	WeightKg    float64     `json:"weight_kg"`
	WeightBasis string      `json:"weight_basis"`
	Legs        []LegResult `json:"legs"`
	Hubs        []HubResult `json:"hubs,omitempty"`
	TonneKm     float64     `json:"tonne_km"`
	TTWKg       float64     `json:"ttw_kg"`
	WTWKg       float64     `json:"wtw_kg"`
	// IntensityG is transport WTW intensity in g CO2e per tonne-km (hubs excluded).
	IntensityG float64 `json:"intensity_g_per_tkm"`
}
