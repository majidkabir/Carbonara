// Command osrmbench measures an OSRM server's route and table throughput
// with random points in a bounding box.
//
//	go run ./tools/osrmbench -url http://localhost:5000 -mode route -c 8 -d 30s -bbox 3.4,50.8,7.2,53.5
//
// Modes: route (one origin-destination pair per request, distance only)
// and table (an all-to-all distance matrix of -n points per request).
// Every request uses fresh random points, so nothing is cached.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	base := flag.String("url", "http://localhost:5000", "OSRM base URL")
	mode := flag.String("mode", "route", "route or table")
	conc := flag.Int("c", 8, "concurrent requests")
	dur := flag.Duration("d", 30*time.Second, "test duration")
	bboxS := flag.String("bbox", "3.4,50.8,7.2,53.5", "minLon,minLat,maxLon,maxLat")
	n := flag.Int("n", 25, "points per table request")
	label := flag.String("label", "", "label for the result line")
	flag.Parse()

	var bb [4]float64
	parts := strings.Split(*bboxS, ",")
	if len(parts) != 4 {
		fmt.Fprintln(os.Stderr, "bad -bbox")
		os.Exit(2)
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -bbox:", err)
			os.Exit(2)
		}
		bb[i] = v
	}
	if *mode != "route" && *mode != "table" {
		fmt.Fprintln(os.Stderr, "unknown -mode")
		os.Exit(2)
	}

	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: *conc}, Timeout: time.Minute}
	deadline := time.Now().Add(*dur)
	var (
		mu                sync.Mutex
		lat               []time.Duration
		ok, noRoute, errs atomic.Int64
		pairs             atomic.Int64
		wg                sync.WaitGroup
	)
	start := time.Now()
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			pt := func() string {
				return fmt.Sprintf("%.5f,%.5f", bb[0]+r.Float64()*(bb[2]-bb[0]), bb[1]+r.Float64()*(bb[3]-bb[1]))
			}
			var local []time.Duration
			for time.Now().Before(deadline) {
				var u string
				var np int64
				if *mode == "route" {
					u = fmt.Sprintf("%s/route/v1/driving/%s;%s?overview=false&steps=false&alternatives=false", *base, pt(), pt())
					np = 1
				} else {
					pts := make([]string, *n)
					for i := range pts {
						pts[i] = pt()
					}
					u = fmt.Sprintf("%s/table/v1/driving/%s?annotations=distance", *base, strings.Join(pts, ";"))
					np = int64(*n * *n)
				}
				t0 := time.Now()
				resp, err := client.Get(u)
				if err != nil {
					errs.Add(1)
					continue
				}
				var body struct {
					Code string `json:"code"`
				}
				_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&body)
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				switch body.Code {
				case "Ok":
					ok.Add(1)
					pairs.Add(np)
					local = append(local, time.Since(t0))
				case "NoRoute":
					noRoute.Add(1)
					local = append(local, time.Since(t0))
				default:
					errs.Add(1)
				}
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		}(int64(w + 1))
	}
	wg.Wait()
	el := time.Since(start).Seconds()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	q := func(p float64) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(p*float64(len(lat)-1))].Round(10 * time.Microsecond)
	}
	answered := float64(ok.Load()+noRoute.Load()) / el
	fmt.Printf("%s %s c=%d: %.0f requests/s, %.0f distances/s, p50 %v, p95 %v, p99 %v (%d ok, %d no route, %d errors)\n",
		*label, *mode, *conc, answered, float64(pairs.Load())/el, q(.5), q(.95), q(.99), ok.Load(), noRoute.Load(), errs.Load())
}
