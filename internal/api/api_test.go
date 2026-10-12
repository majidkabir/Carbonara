package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/distance"
	"github.com/majidkabir/carbonara/pkg/factors"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ds, err := factors.Embedded("sample")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Calc: calc.Calculator{Factors: ds, Distances: distance.GreatCircle{}}, Factors: ds}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp, out
}

func TestCalculate(t *testing.T) {
	srv := newTestServer(t)
	// sample truck_40t: 60 + 15 g/tkm. 10 t x 500 km -> 375 kg WTW
	resp, out := post(t, srv.URL+"/v1/calculate",
		`{"id":"s1","weight_kg":10000,"legs":[{"id":"l1","mode":"road","vehicle_type":"truck_40t","distance_km":500}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %v", resp.StatusCode, out)
	}
	if out["wtw_kg"].(float64) != 375 {
		t.Errorf("wtw_kg = %v, want 375", out["wtw_kg"])
	}
}

func TestCalculate_Errors(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		name, body string
		status     int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"unknown field", `{"weight_kg":1,"legz":[]}`, http.StatusBadRequest},
		{"zero weight", `{"weight_kg":0,"legs":[{"mode":"road"}]}`, http.StatusUnprocessableEntity},
		{"unknown vehicle", `{"weight_kg":1,"legs":[{"mode":"road","vehicle_type":"x","distance_km":1}]}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, out := post(t, srv.URL+"/v1/calculate", tc.body)
			if resp.StatusCode != tc.status || out["error"] == "" {
				t.Errorf("status %d body %v, want %d with error", resp.StatusCode, out, tc.status)
			}
		})
	}
}

func TestCalculateBatch(t *testing.T) {
	srv := newTestServer(t)
	resp, out := post(t, srv.URL+"/v1/calculate/batch", `{"shipments":[
		{"id":"ok","weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"truck_40t","distance_km":100}]},
		{"id":"bad","weight_kg":0,"legs":[{"mode":"road"}]}
	]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	results := out["results"].([]any)
	if len(results) != 2 || out["failed"].(float64) != 1 {
		t.Fatalf("got %v", out)
	}
	if results[0].(map[string]any)["result"] == nil || results[1].(map[string]any)["error"] == nil {
		t.Errorf("got %v", results)
	}
}

func TestGetEndpoints(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/", "/healthz", "/v1/factors", "/openapi.yaml"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
	resp, err := http.Get(srv.URL + "/v1/calculate")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/calculate: status %d, want 405", resp.StatusCode)
	}
}
