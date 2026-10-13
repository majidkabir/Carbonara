// Package api exposes the calculator over HTTP.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/majidkabir/carbonara/pkg/calc"
)

//go:embed openapi.yaml
var openAPISpec []byte

//go:embed web/index.html
var indexHTML []byte

const (
	maxBodyBytes  = 10 << 20 // 10 MiB
	maxBatchItems = 10_000
)

// Server holds the handler dependencies.
type Server struct {
	Calc    calc.Calculator
	Factors any // served as-is on GET /v1/factors
	Log     *slog.Logger
	Limits  Limits

	sem chan struct{}
}

// Limits protect the server from overload. Zero values use the defaults.
type Limits struct {
	// MaxConcurrent is how many calculation requests run at once; more get
	// 503 with Retry-After. Default 4 x GOMAXPROCS.
	MaxConcurrent int
	// Timeout bounds each calculation request; past it the request gets
	// 504. Default 30s.
	Timeout time.Duration
	// MaxLegs bounds legs (and hubs) per shipment. Default 100.
	MaxLegs int
}

func (l Limits) withDefaults() Limits {
	if l.MaxConcurrent <= 0 {
		l.MaxConcurrent = 4 * runtime.GOMAXPROCS(0)
	}
	if l.Timeout <= 0 {
		l.Timeout = 30 * time.Second
	}
	if l.MaxLegs <= 0 {
		l.MaxLegs = 100
	}
	return l
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	s.Limits = s.Limits.withDefaults()
	s.sem = make(chan struct{}, s.Limits.MaxConcurrent)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPISpec)
	})
	mux.HandleFunc("GET /v1/factors", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Factors)
	})
	mux.HandleFunc("POST /v1/calculate", s.limited(s.calculate))
	mux.HandleFunc("POST /v1/calculate/batch", s.limited(s.calculateBatch))
	var h http.Handler = s.recoverPanics(secureHeaders(mux))
	if s.Log != nil {
		h = s.logRequests(h)
	}
	return h
}

// limited applies the concurrency cap and request timeout.
func (s *Server) limited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, errors.New("server busy, retry shortly"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.Limits.Timeout)
		defer cancel()
		w.Header().Set("Cache-Control", "no-store")
		h(w, r.WithContext(ctx))
	}
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				if s.Log != nil {
					s.Log.Error("panic", "path", r.URL.Path, "error", fmt.Sprint(v), "stack", string(debug.Stack()))
				}
				writeError(w, http.StatusInternalServerError, errors.New("internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// checkSize rejects shipments too large to calculate in one request.
func (s *Server) checkSize(sh calc.Shipment) error {
	if len(sh.Legs) > s.Limits.MaxLegs || len(sh.Hubs) > s.Limits.MaxLegs {
		return fmt.Errorf("shipment has %d legs and %d hubs; max is %d each", len(sh.Legs), len(sh.Hubs), s.Limits.MaxLegs)
	}
	return nil
}

// calcStatus maps a calculation error to an HTTP status.
func calcStatus(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return 499 // client closed request; nobody reads it
	default:
		return http.StatusUnprocessableEntity
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Log.Info("request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration_ms", float64(time.Since(start).Microseconds())/1000)
	})
}

func (s *Server) calculate(w http.ResponseWriter, r *http.Request) {
	var sh calc.Shipment
	if !s.decode(w, r, &sh) {
		return
	}
	if err := s.checkSize(sh); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	res, err := s.Calc.Calculate(r.Context(), sh)
	if err != nil {
		writeError(w, calcStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// BatchRequest is the body of POST /v1/calculate/batch.
type BatchRequest struct {
	Shipments []calc.Shipment `json:"shipments"`
}

// BatchItem is one entry in a batch response: a result or an error.
type BatchItem struct {
	ShipmentID string       `json:"shipment_id"`
	Result     *calc.Result `json:"result,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// BatchResponse is the body returned by POST /v1/calculate/batch.
type BatchResponse struct {
	Results []BatchItem `json:"results"`
	Failed  int         `json:"failed"`
}

func (s *Server) calculateBatch(w http.ResponseWriter, r *http.Request) {
	var req BatchRequest
	if !s.decode(w, r, &req) {
		return
	}
	if len(req.Shipments) > maxBatchItems {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Errorf("batch has %d shipments, max is %d", len(req.Shipments), maxBatchItems))
		return
	}
	resp := BatchResponse{Results: make([]BatchItem, 0, len(req.Shipments))}
	for _, sh := range req.Shipments {
		if err := r.Context().Err(); err != nil {
			writeError(w, calcStatus(err), fmt.Errorf("batch stopped after %d of %d shipments: %w",
				len(resp.Results), len(req.Shipments), err))
			return
		}
		item := BatchItem{ShipmentID: sh.ID}
		if err := s.checkSize(sh); err != nil {
			item.Error = err.Error()
			resp.Failed++
		} else if res, err := s.Calc.Calculate(r.Context(), sh); err != nil {
			if r.Context().Err() != nil { // timed out or client left: stop, don't blame the shipment
				writeError(w, calcStatus(r.Context().Err()), fmt.Errorf("batch stopped after %d of %d shipments: %w",
					len(resp.Results), len(req.Shipments), r.Context().Err()))
				return
			}
			item.Error = err.Error()
			resp.Failed++
		} else {
			item.Result = &res
		}
		resp.Results = append(resp.Results, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// decode reads a JSON body into v, writing a 4xx and returning false on failure.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, err)
		} else {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		}
		return false
	}
	return true
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorBody{Error: err.Error()})
}

// writeJSON encodes before writing, so an encoding failure becomes a 500
// instead of a truncated 200.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		b, _ = json.Marshal(errorBody{Error: "could not encode response: " + err.Error()})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
