// Package api exposes the calculator over HTTP.
package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
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
	mux.HandleFunc("POST /v1/calculate", s.calculate)
	mux.HandleFunc("POST /v1/calculate/batch", s.calculateBatch)
	if s.Log == nil {
		return mux
	}
	return s.logRequests(mux)
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
	res, err := s.Calc.Calculate(r.Context(), sh)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
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
			return // client went away
		}
		item := BatchItem{ShipmentID: sh.ID}
		if res, err := s.Calc.Calculate(r.Context(), sh); err != nil {
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
