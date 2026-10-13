package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/factors"
)

// blockingDistance waits until released or the request context ends.
type blockingDistance struct{ release chan struct{} }

func (b blockingDistance) Distance(ctx context.Context, _ calc.Mode, _, _ calc.Location) (float64, error) {
	select {
	case <-b.release:
		return 100, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

type panicky struct{ calc.FactorProvider }

func (panicky) LegFactor(calc.Mode, string) (calc.Factor, error) { panic("boom") }

func server(t *testing.T, fp calc.FactorProvider, dp calc.DistanceProvider, l Limits) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer((&Server{Calc: calc.Calculator{Factors: fp, Distances: dp}, Factors: fp, Limits: l}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

const routed = `{"weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"hgv","from":{"lat":55,"lon":12},"to":{"lat":53,"lon":10}}]}`

func TestLimits_Busy(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	bd := blockingDistance{release: make(chan struct{})}
	srv := server(t, ds, bd, Limits{MaxConcurrent: 1, Timeout: 5 * time.Second})

	var wg sync.WaitGroup
	wg.Add(1)
	first := make(chan int, 1)
	go func() {
		defer wg.Done()
		resp, err := http.Post(srv.URL+"/v1/calculate", "application/json", strings.NewReader(routed))
		if err == nil {
			resp.Body.Close()
			first <- resp.StatusCode
		}
	}()
	time.Sleep(100 * time.Millisecond) // let the first request take the slot
	resp, out := post(t, srv.URL+"/v1/calculate", routed)
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" || out["error"] == nil {
		t.Errorf("second request: %d %v, want 503 with Retry-After", resp.StatusCode, out)
	}
	close(bd.release)
	wg.Wait()
	if code := <-first; code != http.StatusOK {
		t.Errorf("first request: %d", code)
	}
}

func TestLimits_Timeout(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	srv := server(t, ds, blockingDistance{release: make(chan struct{})}, Limits{Timeout: 100 * time.Millisecond})
	resp, out := post(t, srv.URL+"/v1/calculate", routed)
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("calculate: %d %v, want 504", resp.StatusCode, out)
	}
	resp, out = post(t, srv.URL+"/v1/calculate/batch", `{"shipments":[`+routed+`,`+routed+`]}`)
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(out["error"].(string), "batch stopped after 0 of 2") {
		t.Errorf("batch: %d %v, want 504 with progress", resp.StatusCode, out)
	}
}

func TestLimits_MaxLegs(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	srv := server(t, ds, nil, Limits{MaxLegs: 2})
	leg := `{"mode":"road","vehicle_type":"hgv","distance_km":10}`
	three := `{"weight_kg":1,"legs":[` + leg + `,` + leg + `,` + leg + `]}`
	if resp, _ := post(t, srv.URL+"/v1/calculate", three); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("calculate: %d, want 413", resp.StatusCode)
	}
	resp, out := post(t, srv.URL+"/v1/calculate/batch", `{"shipments":[`+three+`,{"weight_kg":1,"legs":[`+leg+`]}]}`)
	if resp.StatusCode != http.StatusOK || out["failed"].(float64) != 1 {
		t.Errorf("batch: %d %v, want one per-item failure", resp.StatusCode, out)
	}
}

func TestPanicRecovery(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	srv := server(t, panicky{ds}, nil, Limits{})
	resp, out := post(t, srv.URL+"/v1/calculate", `{"weight_kg":1,"legs":[{"mode":"road","vehicle_type":"hgv","distance_km":1}]}`)
	if resp.StatusCode != http.StatusInternalServerError || out["error"] != "internal error" {
		t.Errorf("%d %v, want 500 internal error", resp.StatusCode, out)
	}
	// The server keeps serving.
	if r, err := http.Get(srv.URL + "/healthz"); err != nil || r.StatusCode != http.StatusOK {
		t.Errorf("healthz after panic: %v", err)
	}
}

func TestSecureHeaders(t *testing.T) {
	ds, _ := factors.Open(factors.Default)
	srv := server(t, ds, nil, Limits{})
	resp, _ := post(t, srv.URL+"/v1/calculate", `{"weight_kg":1,"legs":[{"mode":"road","vehicle_type":"hgv","distance_km":1}]}`)
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers %v", resp.Header)
	}
}
