package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majidkabir/sootcase/pkg/calc"
	"github.com/majidkabir/sootcase/pkg/factors"
)

// downFirst fails the first n road routes for a temporary reason; lanes
// with lon 99 are never routable (a permanent failure).
type downFirst struct {
	n     int32
	calls atomic.Int32
}

func (d *downFirst) Distance(_ context.Context, _ calc.Mode, from, _ calc.Location) (float64, error) {
	if from.Lon == 99 {
		return 0, fmt.Errorf("osrm: NoRoute: Impossible route")
	}
	if d.calls.Add(1) <= d.n {
		return 0, fmt.Errorf("osrm: %w (connection refused)", calc.ErrTemporary)
	}
	return 100, nil
}

func retryServer(t *testing.T, d *downFirst) *httptest.Server {
	t.Helper()
	ds, err := factors.Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	c := calc.Calculator{Factors: ds, Distances: d, RetryWaits: []time.Duration{time.Millisecond, time.Millisecond}}
	srv := httptest.NewServer((&Server{Calc: c, Factors: ds}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

const leg = `{"mode":"road","vehicle_type":"truck_40t","from":{"lat":55.6,"lon":%v},"to":{"lat":53.5,"lon":10}}`

func TestBatch_RetriesTemporaryFailures(t *testing.T) {
	// Two temporary failures: the retries absorb them, nothing fails.
	srv := retryServer(t, &downFirst{n: 2})
	body := fmt.Sprintf(`{"shipments":[{"id":"a","weight_kg":1000,"legs":[`+leg+`]},{"id":"b","weight_kg":1000,"legs":[`+leg+`]},{"id":"c","weight_kg":1000,"legs":[`+leg+`]}]}`, 12, 12, 99)
	var out BatchResponse
	resp, err := http.Post(srv.URL+"/v1/calculate/batch?add_road_legs=false", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Failed != 1 || out.Retryable != 0 || out.Results[2].Error == "" || out.Results[2].Retryable {
		t.Fatalf("want only the unroutable shipment failed, not retryable: %+v", out)
	}

	// Down for good: the shipment fails, marked retryable.
	srv = retryServer(t, &downFirst{n: 1 << 30})
	resp, err = http.Post(srv.URL+"/v1/calculate/batch?add_road_legs=false", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	out = BatchResponse{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Failed != 3 || out.Retryable != 2 || !out.Results[0].Retryable || out.Results[2].Retryable {
		t.Fatalf("want two retryable failures and one not: %+v", out)
	}

	// A single shipment: 503 with Retry-After.
	resp, err = http.Post(srv.URL+"/v1/calculate", "application/json", strings.NewReader(fmt.Sprintf(`{"weight_kg":1000,"legs":[`+leg+`]}`, 12)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Errorf("single: status %d, Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}
