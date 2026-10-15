package calc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flaky fails the first n calls with err, then answers 100 km.
type flaky struct {
	n     int32
	err   error
	calls atomic.Int32
}

func (f *flaky) Distance(context.Context, Mode, Location, Location) (float64, error) {
	if f.calls.Add(1) <= f.n {
		return 0, f.err
	}
	return 100, nil
}

func TestCalculateRetrying(t *testing.T) {
	temp := fmt.Errorf("osrm: %w (connection refused)", ErrTemporary)
	s := Shipment{ID: "s", WeightKg: 1000, Legs: []Leg{{ID: "a", Mode: Road, VehicleType: "truck_40t", From: cph, To: aar}}}
	waits := []time.Duration{time.Millisecond, time.Millisecond}

	f := &flaky{n: 2, err: temp}
	c := Calculator{Factors: testFactors, Distances: f, RetryWaits: waits}
	if r, err := c.CalculateRetrying(context.Background(), s); err != nil || r.Legs[0].DistanceKm != 100 || f.calls.Load() != 3 {
		t.Errorf("two temporary failures, then success: %v, %d calls", err, f.calls.Load())
	}

	f = &flaky{n: 9, err: temp}
	c.Distances = f
	if _, err := c.CalculateRetrying(context.Background(), s); !Temporary(err) || f.calls.Load() != 3 {
		t.Errorf("still failing: %v (temporary %v), %d calls", err, Temporary(err), f.calls.Load())
	}

	f = &flaky{n: 9, err: errors.New("osrm: NoRoute: Impossible route")}
	c.Distances = f
	if _, err := c.CalculateRetrying(context.Background(), s); err == nil || Temporary(err) || f.calls.Load() != 1 {
		t.Errorf("a permanent failure isn't retried: %v, %d calls", err, f.calls.Load())
	}
}

// A road leg to add that fails for a temporary reason fails the shipment
// (to be retried), instead of being left out.
func TestRoadLegs_TemporaryFailureIsNotDropped(t *testing.T) {
	temp := fmt.Errorf("osrm: %w", ErrTemporary)
	c := Calculator{Factors: connectFactors, Distances: roadDown{toy, temp}, Places: toyPlaces}
	s := Shipment{ID: "s", WeightKg: 1000, Legs: []Leg{
		{ID: "a", Mode: Sea, VehicleType: "container", From: westEnd, To: hamburg},
		{ID: "b", Mode: Rail, VehicleType: "train", From: berlin, To: terminal},
	}}
	if _, err := c.Calculate(context.Background(), s); !Temporary(err) || !strings.Contains(err.Error(), "added-1") {
		t.Errorf("want a temporary failure of the added leg, got %v", err)
	}
}

// roadDown is a world whose road routing is down.
type roadDown struct {
	world
	err error
}

func (r roadDown) Route(ctx context.Context, m Mode, a, b Location) (Route, error) {
	if m == Road {
		return Route{}, r.err
	}
	return r.world.Route(ctx, m, a, b)
}

func (r roadDown) Distance(ctx context.Context, m Mode, a, b Location) (float64, error) {
	rt, err := r.Route(ctx, m, a, b)
	return rt.Km, err
}
