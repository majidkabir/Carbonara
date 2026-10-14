package calc

import (
	"context"
	"errors"
	"testing"
)

type fakePlaces map[string]Location

func (f fakePlaces) Resolve(code string) (Location, bool) { l, ok := f[code]; return l, ok }

type recordDistance struct{ from, to *Location }

func (d recordDistance) Distance(_ context.Context, _ Mode, a, b Location) (float64, error) {
	*d.from, *d.to = a, b
	return 100, nil
}

func TestCalculate_LocationCodes(t *testing.T) {
	var from, to Location
	c := Calculator{Factors: testFactors, Distances: recordDistance{&from, &to},
		Places: fakePlaces{"NLRTM": {Name: "Rotterdam (NLRTM)", Code: "NLRTM", Lat: 51.9, Lon: 4.5}}}
	legs := []Leg{{ID: "l1", Mode: Road, VehicleType: "truck_40t", From: Location{Code: "NLRTM"}, To: Location{Lat: 53, Lon: 10}}}
	s := Shipment{WeightKg: 1000, Legs: legs}
	if _, err := c.Calculate(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if from.Lat != 51.9 || from.Lon != 4.5 || to.Lat != 53 {
		t.Errorf("routed %+v -> %+v", from, to)
	}
	if legs[0].From.Lat != 0 {
		t.Error("the caller's shipment was changed")
	}
	// Coordinates win over a code.
	s.Legs = []Leg{{ID: "l1", Mode: Road, VehicleType: "truck_40t", From: Location{Code: "NLRTM", Lat: 1, Lon: 2}, To: Location{Lat: 53, Lon: 10}}}
	if _, err := c.Calculate(context.Background(), s); err != nil || from.Lat != 1 {
		t.Errorf("coordinates with a code: %v %+v", err, from)
	}
	s.Legs[0].From = Location{Code: "XXXXX"}
	if _, err := c.Calculate(context.Background(), s); !errors.Is(err, ErrUnknownLocation) {
		t.Errorf("unknown code: %v", err)
	}
	c.Places = nil
	s.Legs[0].From = Location{Code: "NLRTM"}
	if _, err := c.Calculate(context.Background(), s); !errors.Is(err, ErrUnknownLocation) {
		t.Errorf("no place list: %v", err)
	}
}
