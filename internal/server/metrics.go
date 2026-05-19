package server

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Metrics struct {
	requestsTotal            atomic.Uint64
	inspectedTotal           atomic.Uint64
	blockedTotal             atomic.Uint64
	skippedTotal             atomic.Uint64
	extractErrorsTotal       atomic.Uint64
	proxyErrorsTotal         atomic.Uint64
	inspectionDurationMicros atomic.Uint64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

type Snapshot struct {
	RequestsTotal            uint64 `json:"requests_total"`
	InspectedTotal           uint64 `json:"inspected_total"`
	BlockedTotal             uint64 `json:"blocked_total"`
	SkippedTotal             uint64 `json:"skipped_total"`
	ExtractErrorsTotal       uint64 `json:"extract_errors_total"`
	ProxyErrorsTotal         uint64 `json:"proxy_errors_total"`
	InspectionDurationMicros uint64 `json:"inspection_duration_micros"`
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		RequestsTotal:            m.requestsTotal.Load(),
		InspectedTotal:           m.inspectedTotal.Load(),
		BlockedTotal:             m.blockedTotal.Load(),
		SkippedTotal:             m.skippedTotal.Load(),
		ExtractErrorsTotal:       m.extractErrorsTotal.Load(),
		ProxyErrorsTotal:         m.proxyErrorsTotal.Load(),
		InspectionDurationMicros: m.inspectionDurationMicros.Load(),
	}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_requests_total Total HTTP requests handled.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_requests_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_requests_total %d\n", m.requestsTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_inspected_total Total requests inspected.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_inspected_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_inspected_total %d\n", m.inspectedTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_blocked_total Total requests blocked.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_blocked_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_blocked_total %d\n", m.blockedTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_skipped_total Total requests skipped from inspection.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_skipped_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_skipped_total %d\n", m.skippedTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_extract_errors_total Total inspection extract errors.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_extract_errors_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_extract_errors_total %d\n", m.extractErrorsTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_proxy_errors_total Total proxy errors.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_proxy_errors_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_proxy_errors_total %d\n", m.proxyErrorsTotal.Load())
		_, _ = fmt.Fprintf(w, "# HELP prompt_guard_inspection_duration_seconds_total Total inspection duration in seconds.\n")
		_, _ = fmt.Fprintf(w, "# TYPE prompt_guard_inspection_duration_seconds_total counter\n")
		_, _ = fmt.Fprintf(w, "prompt_guard_inspection_duration_seconds_total %.6f\n", float64(m.inspectionDurationMicros.Load())/1_000_000)
	})
}
