package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CallOutcomeMetrics records all proxied calls, including those without a response.
// Labels only use the configured server and a fixed set of outcomes; request IDs,
// paths, query parameters and arbitrary JSON-RPC methods are never labels.
type CallOutcomeMetrics struct {
	started  *prometheus.CounterVec
	finished *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

var DefaultCallOutcomeMetrics = NewCallOutcomeMetrics(prometheus.DefaultRegisterer)

func NewCallOutcomeMetrics(registerer prometheus.Registerer) *CallOutcomeMetrics {
	m := &CallOutcomeMetrics{
		started: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_snooper_calls_started_total",
			Help: "Calls accepted for proxying, including calls that never receive an upstream response.",
		}, []string{"server"}),
		finished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_snooper_call_outcomes_total",
			Help: "Terminal proxy call outcomes; completed means HTTP body transfer, not Engine payload validity.",
		}, []string{"server", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rpc_snooper_call_elapsed_seconds",
			Help:    "Elapsed time until each proxy call outcome, including cancellation and errors, not upstream execution time.",
			Buckets: []float64{0.01, 0.1, 0.5, 1, 2, 5, 10, 15, 30, 60, 120},
		}, []string{"server", "outcome"}),
	}
	registerer.MustRegister(m.started, m.finished, m.duration)

	return m
}

func (m *CallOutcomeMetrics) Start(server string) {
	m.started.WithLabelValues(server).Inc()
}

func (m *CallOutcomeMetrics) Finish(server, outcome string, elapsed time.Duration) {
	m.finished.WithLabelValues(server, outcome).Inc()
	m.duration.WithLabelValues(server, outcome).Observe(elapsed.Seconds())
}
