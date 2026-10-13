// Command loadtest measures a running Carbonara server's throughput and
// latency.
//
//	go run ./tools/loadtest -url http://localhost:8080 -kind calc -c 32 -d 20s
//
// Kinds: calc (POST /v1/calculate, one shipment), batch (POST
// /v1/calculate/batch with -batch shipments) and report (POST /v1/report
// with -batch shipments). Shipments are random but repeatable (-seed):
// a road leg and a sea leg between points in Europe, distances by the
// server's distance provider (great-circle unless routing is set up).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type loc struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type leg struct {
	Mode        string `json:"mode"`
	VehicleType string `json:"vehicle_type"`
	From        loc    `json:"from"`
	To          loc    `json:"to"`
}

type shipment struct {
	ID       string  `json:"id"`
	WeightKg float64 `json:"weight_kg"`
	Legs     []leg   `json:"legs"`
}

func randLoc(r *rand.Rand) loc {
	return loc{Lat: 43 + r.Float64()*12, Lon: -2 + r.Float64()*22}
}

func randShipment(r *rand.Rand, i int) shipment {
	return shipment{ID: fmt.Sprintf("S%d", i), WeightKg: 500 + r.Float64()*20_000, Legs: []leg{
		{Mode: "road", VehicleType: "hgv", From: randLoc(r), To: randLoc(r)},
		{Mode: "sea", VehicleType: "container_ship", From: randLoc(r), To: randLoc(r)},
	}}
}

func main() {
	base := flag.String("url", "http://localhost:8080", "server base URL")
	kind := flag.String("kind", "calc", "calc, batch or report")
	conc := flag.Int("c", 16, "concurrent requests")
	dur := flag.Duration("d", 15*time.Second, "test duration")
	batch := flag.Int("batch", 100, "shipments per batch or report request")
	seed := flag.Int64("seed", 1, "random seed")
	flag.Parse()

	path, perReq := "/v1/calculate", 1
	switch *kind {
	case "calc":
	case "batch":
		path, perReq = "/v1/calculate/batch", *batch
	case "report":
		path, perReq = "/v1/report", *batch
	default:
		fmt.Fprintln(os.Stderr, "unknown -kind")
		os.Exit(2)
	}

	// Pre-build 256 request bodies so encoding doesn't load the client.
	r := rand.New(rand.NewSource(*seed))
	bodies := make([][]byte, 256)
	for i := range bodies {
		var v any
		if perReq == 1 {
			v = randShipment(r, i)
		} else {
			ss := make([]shipment, perReq)
			for j := range ss {
				ss[j] = randShipment(r, i*perReq+j)
			}
			v = map[string]any{"shipments": ss}
		}
		b, _ := json.Marshal(v)
		bodies[i] = b
	}

	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: *conc, MaxConnsPerHost: *conc}, Timeout: time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	var (
		mu        sync.Mutex
		latencies []time.Duration
		okN, errN atomic.Int64
		statuses  sync.Map
		wg        sync.WaitGroup
	)
	start := time.Now()
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var local []time.Duration
			for i := w; ctx.Err() == nil; i += *conc {
				t0 := time.Now()
				resp, err := client.Post(*base+path, "application/json", bytes.NewReader(bodies[i%len(bodies)]))
				if err != nil {
					if ctx.Err() == nil {
						errN.Add(1)
					}
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					okN.Add(1)
					local = append(local, time.Since(t0))
				} else {
					errN.Add(1)
					n, _ := statuses.LoadOrStore(resp.StatusCode, new(atomic.Int64))
					n.(*atomic.Int64).Add(1)
				}
			}
			mu.Lock()
			latencies = append(latencies, local...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	q := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[int(p*float64(len(latencies)-1))]
	}
	reqs := float64(okN.Load()) / elapsed
	fmt.Printf("%s, %d concurrent, %.0fs: %.0f requests/s (%.0f shipments/s), p50 %v, p95 %v, p99 %v, %d errors\n",
		*kind, *conc, elapsed, reqs, reqs*float64(perReq),
		q(.50).Round(10*time.Microsecond), q(.95).Round(10*time.Microsecond), q(.99).Round(10*time.Microsecond), errN.Load())
	statuses.Range(func(k, v any) bool {
		fmt.Printf("  HTTP %v: %d\n", k, v.(*atomic.Int64).Load())
		return true
	})
}
