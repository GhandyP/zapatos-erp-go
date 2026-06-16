package observability

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Middleware struct {
	logger    *slog.Logger
	collector *Collector
}

type Collector struct {
	requestsTotal      atomic.Uint64
	inFlight           atomic.Int64
	totalDurationNanos atomic.Uint64
	status1xx          atomic.Uint64
	status2xx          atomic.Uint64
	status3xx          atomic.Uint64
	status4xx          atomic.Uint64
	status5xx          atomic.Uint64
}

func New(logger *slog.Logger) *Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &Middleware{logger: logger, collector: &Collector{}}
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := requestIDFrom(r)
		traceparent := traceparentFrom(r)

		w.Header().Set("X-Request-Id", requestID)
		if traceparent != "" {
			w.Header().Set("Traceparent", traceparent)
		}

		m.collector.inFlight.Add(1)
		defer m.collector.inFlight.Add(-1)

		rec := &recordingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		duration := time.Since(start)
		m.collector.record(rec.status, duration)
		m.logger.Info("request completed",
			"request_id", requestID,
			"traceparent", traceparent,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes_written", rec.bytes,
			"duration_ms", float64(duration.Milliseconds()),
		)
	})
}

func (m *Middleware) MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(m.collector.snapshot())
	}
}

func (c *Collector) record(status int, duration time.Duration) {
	c.requestsTotal.Add(1)
	c.totalDurationNanos.Add(uint64(duration.Nanoseconds()))
	switch {
	case status >= 100 && status < 200:
		c.status1xx.Add(1)
	case status >= 200 && status < 300:
		c.status2xx.Add(1)
	case status >= 300 && status < 400:
		c.status3xx.Add(1)
	case status >= 400 && status < 500:
		c.status4xx.Add(1)
	default:
		c.status5xx.Add(1)
	}
}

func (c *Collector) snapshot() map[string]any {
	requests := c.requestsTotal.Load()
	totalDuration := time.Duration(c.totalDurationNanos.Load())
	averageMs := 0.0
	if requests > 0 {
		averageMs = float64(totalDuration.Milliseconds()) / float64(requests)
	}
	return map[string]any{
		"requests_total": requests,
		"in_flight":      c.inFlight.Load(),
		"duration_ms": map[string]any{
			"total":   totalDuration.Milliseconds(),
			"average": averageMs,
		},
		"status_counts": map[string]uint64{
			"1xx": c.status1xx.Load(),
			"2xx": c.status2xx.Load(),
			"3xx": c.status3xx.Load(),
			"4xx": c.status4xx.Load(),
			"5xx": c.status5xx.Load(),
		},
	}
}

func requestIDFrom(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-Id")); value != "" {
		return value
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return "req-" + hex.EncodeToString(buf)
	}
	return "req-" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
}

func traceparentFrom(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Traceparent"))
}

type recordingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *recordingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}
