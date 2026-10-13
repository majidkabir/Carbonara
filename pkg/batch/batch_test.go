package batch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/csvio"
)

type factors struct{}

func (factors) LegFactor(m calc.Mode, vt string) (calc.Factor, error) {
	return calc.Factor{Mode: m, VehicleType: vt, TTW: 1}, nil
}
func (factors) HubFactor(string) (calc.HubFactor, error) { return calc.HubFactor{}, errors.New("none") }
func (factors) FuelFactor(string) (calc.FuelFactor, error) {
	return calc.FuelFactor{}, errors.New("none")
}

// slowDistance sleeps a random time so workers finish out of order.
type slowDistance struct{}

func (slowDistance) Distance(context.Context, calc.Mode, calc.Location, calc.Location) (float64, error) {
	time.Sleep(time.Duration(rand.Intn(300)) * time.Microsecond)
	return 100, nil
}

// source yields n shipments; every 7th is a parse error; inflight tracks
// items read but not yet written.
type source struct {
	n, i     int
	inflight *int64
	max      *int64
	failAt   int
}

func (s *source) Next() (csvio.Item, error) {
	if s.failAt > 0 && s.i == s.failAt {
		return csvio.Item{}, errors.New("disk on fire")
	}
	if s.i >= s.n {
		return csvio.Item{}, io.EOF
	}
	id := fmt.Sprintf("S%d", s.i)
	s.i++
	if v := atomic.AddInt64(s.inflight, 1); v > atomic.LoadInt64(s.max) {
		atomic.StoreInt64(s.max, v)
	}
	it := csvio.Item{Shipment: calc.Shipment{ID: id, WeightKg: 1000, Legs: []calc.Leg{{Mode: calc.Road, VehicleType: "x",
		From: calc.Location{Lat: 55, Lon: 12}, To: calc.Location{Lat: 56, Lon: 10}}}}}
	if s.i%7 == 0 {
		it.Err = errors.New("bad row")
	}
	return it, nil
}

func TestProcess_OrderAndBound(t *testing.T) {
	var inflight, maxInflight int64
	src := &source{n: 2000, inflight: &inflight, max: &maxInflight}
	c := calc.Calculator{Factors: factors{}, Distances: slowDistance{}}
	var got []string
	var mu sync.Mutex
	workers := 8
	stats, err := Process(context.Background(), c, src, workers, func(it csvio.Item, r calc.Result, err error) error {
		mu.Lock()
		defer mu.Unlock()
		atomic.AddInt64(&inflight, -1)
		got = append(got, it.Shipment.ID)
		if (err != nil) != (it.Err != nil) {
			t.Errorf("%s: err %v, item err %v", it.Shipment.ID, err, it.Err)
		}
		if err == nil && r.WTWKg != 0.1 { // 1 t x 100 km x 1 g/tkm = 0.1 kg
			t.Errorf("%s: wtw %v", it.Shipment.ID, r.WTWKg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Shipments != 2000 || stats.Failed != 2000/7 {
		t.Errorf("stats = %+v", stats)
	}
	for i, id := range got {
		if id != fmt.Sprintf("S%d", i) {
			t.Fatalf("out of order at %d: %s", i, id)
		}
	}
	// Window is workers*4; the reader may hold one more while waiting.
	if maxInflight > int64(workers*4+1) {
		t.Errorf("max in flight %d, want <= %d", maxInflight, workers*4+1)
	}
}

func TestProcess_SinkError(t *testing.T) {
	var a, b int64
	src := &source{n: 1000, inflight: &a, max: &b}
	c := calc.Calculator{Factors: factors{}, Distances: slowDistance{}}
	n := 0
	_, err := Process(context.Background(), c, src, 4, func(csvio.Item, calc.Result, error) error {
		n++
		if n == 10 {
			return errors.New("disk full")
		}
		return nil
	})
	if err == nil || err.Error() != "disk full" || n != 10 {
		t.Errorf("err = %v after %d writes", err, n)
	}
}

func TestProcess_ReadError(t *testing.T) {
	var a, b int64
	src := &source{n: 1000, inflight: &a, max: &b, failAt: 50}
	c := calc.Calculator{Factors: factors{}, Distances: slowDistance{}}
	n := 0
	stats, err := Process(context.Background(), c, src, 0, func(csvio.Item, calc.Result, error) error { n++; return nil })
	if err == nil || err.Error() != "disk on fire" {
		t.Errorf("err = %v", err)
	}
	if n != 50 || stats.Shipments != 50 {
		t.Errorf("wrote %d (stats %+v), want the 50 read before the error", n, stats)
	}
}
