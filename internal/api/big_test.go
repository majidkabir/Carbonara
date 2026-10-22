package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majidkabir/sootcase/pkg/calc"
	"github.com/majidkabir/sootcase/pkg/factors"
)

// slowRoads takes 20 ms a route and records the most calculations at once.
type slowRoads struct{ now, peak atomic.Int32 }

func (s *slowRoads) Distance(context.Context, calc.Mode, calc.Location, calc.Location) (float64, error) {
	n := s.now.Add(1)
	for p := s.peak.Load(); n > p && !s.peak.CompareAndSwap(p, n); p = s.peak.Load() {
	}
	time.Sleep(20 * time.Millisecond)
	s.now.Add(-1)
	return 100, nil
}

func TestBigBatchesTakeTurns(t *testing.T) {
	ds, err := factors.Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	roads := &slowRoads{}
	s := &Server{Calc: calc.Calculator{Factors: ds, Distances: roads, NoRoadLegs: true}, Factors: ds,
		Limits: Limits{BigBatch: 3, BigConcurrent: 1, BatchWorkers: 3, Timeout: 5 * time.Second}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ship := `{"weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"truck_40t","from":{"lat":55,"lon":12},"to":{"lat":54,"lon":10}}]}`
	big := `{"shipments":[` + strings.TrimSuffix(strings.Repeat(ship+",", 3), ",") + `]}`

	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := http.Post(srv.URL+"/v1/calculate/batch", "application/json", strings.NewReader(big))
			if err != nil {
				t.Error(err)
				return
			}
			r.Body.Close()
			codes[i] = r.StatusCode
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Errorf("batch %d: %d", i, c)
		}
	}
	// One big batch at a time: never more than its 3 workers routing.
	if p := roads.peak.Load(); p > 3 {
		t.Errorf("%d routes at once, want at most 3 (one big batch)", p)
	}
}
