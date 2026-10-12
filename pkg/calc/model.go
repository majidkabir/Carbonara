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
}

// HubStop is a transshipment or warehouse activity between legs.
type HubStop struct {
	ID      string `json:"id"`
	HubType string `json:"hub_type"`
}

// Shipment is the unit of calculation.
type Shipment struct {
	ID       string    `json:"id"`
	WeightKg float64   `json:"weight_kg"`
	Legs     []Leg     `json:"legs"`
	Hubs     []HubStop `json:"hubs,omitempty"`
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
	TonneKm    float64 `json:"tonne_km"`
	TTWKg      float64 `json:"ttw_kg"`
	WTWKg      float64 `json:"wtw_kg"`
	Factor     Factor  `json:"factor"`
}

// HubResult is the outcome for one hub stop.
type HubResult struct {
	HubID string  `json:"hub_id"`
	WTWKg float64 `json:"wtw_kg"`
}

// Result is the outcome for a whole shipment.
type Result struct {
	ShipmentID string      `json:"shipment_id"`
	Legs       []LegResult `json:"legs"`
	Hubs       []HubResult `json:"hubs,omitempty"`
	TonneKm    float64     `json:"tonne_km"`
	TTWKg      float64     `json:"ttw_kg"`
	WTWKg      float64     `json:"wtw_kg"`
	// IntensityG is transport WTW intensity in g CO2e per tonne-km (hubs excluded).
	IntensityG float64 `json:"intensity_g_per_tkm"`
}
