package distance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
)

// OSRM gets road distances from an OSRM server's route service
// (http://project-osrm.org/docs/v5.24.0/api/#route-service).
//
// Point it at your own server: the public demo server is not meant for
// production traffic.
type OSRM struct {
	BaseURL string       // e.g. "http://localhost:5000"
	Profile string       // default "driving"
	Client  *http.Client // default: 10s timeout

	mu    sync.Mutex
	cache map[string]float64
}

// maxCacheEntries bounds the per-process route cache.
const maxCacheEntries = 50_000

// NewOSRM validates baseURL and returns a provider.
func NewOSRM(baseURL string) (*OSRM, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("osrm: invalid base URL %q", baseURL)
	}
	return &OSRM{BaseURL: strings.TrimRight(baseURL, "/")}, nil
}

// Distance implements calc.DistanceProvider. The mode is ignored: OSRM routes
// whatever its profile is built for.
func (o *OSRM) Distance(ctx context.Context, _ calc.Mode, from, to calc.Location) (float64, error) {
	key := fmt.Sprintf("%.5f,%.5f;%.5f,%.5f", from.Lon, from.Lat, to.Lon, to.Lat)
	o.mu.Lock()
	if d, ok := o.cache[key]; ok {
		o.mu.Unlock()
		return d, nil
	}
	o.mu.Unlock()

	profile := o.Profile
	if profile == "" {
		profile = "driving"
	}
	u := fmt.Sprintf("%s/route/v1/%s/%s?overview=false&alternatives=false&steps=false",
		o.BaseURL, url.PathEscape(profile), key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("osrm: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Routes  []struct {
			Distance float64 `json:"distance"` // metres
		} `json:"routes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0, fmt.Errorf("osrm: HTTP %d, unreadable response: %w", resp.StatusCode, err)
	}
	if body.Code != "Ok" {
		return 0, fmt.Errorf("osrm: %s: %s", body.Code, body.Message)
	}
	if len(body.Routes) == 0 {
		return 0, errors.New("osrm: no route")
	}
	km := body.Routes[0].Distance / 1000

	o.mu.Lock()
	if o.cache == nil || len(o.cache) >= maxCacheEntries {
		o.cache = make(map[string]float64)
	}
	o.cache[key] = km
	o.mu.Unlock()
	return km, nil
}

// DistanceMethod implements calc.DistanceMethoder.
func (o *OSRM) DistanceMethod(calc.Mode) string { return "osrm" }

// ByMode sends each mode to its own provider, and the rest to Fallback.
type ByMode struct {
	Providers map[calc.Mode]calc.DistanceProvider
	Fallback  calc.DistanceProvider
}

func (b ByMode) pick(m calc.Mode) calc.DistanceProvider {
	if p, ok := b.Providers[m]; ok {
		return p
	}
	return b.Fallback
}

// Distance implements calc.DistanceProvider.
func (b ByMode) Distance(ctx context.Context, m calc.Mode, from, to calc.Location) (float64, error) {
	p := b.pick(m)
	if p == nil {
		return 0, fmt.Errorf("no distance provider for mode %s", m)
	}
	return p.Distance(ctx, m, from, to)
}

// DistanceMethod implements calc.DistanceMethoder.
func (b ByMode) DistanceMethod(m calc.Mode) string {
	if dm, ok := b.pick(m).(calc.DistanceMethoder); ok {
		return dm.DistanceMethod(m)
	}
	return ""
}
