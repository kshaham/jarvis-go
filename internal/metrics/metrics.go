// Package metrics owns the Prometheus registry for jarvis.
//
// All counters, gauges, and histograms hang off the package-level variables
// so call-sites are a single statement (metrics.SessionsCreated.Inc()).
// The registry is local rather than DefaultRegisterer so process-level
// churn (spawning another server in tests) doesn't collide with the global.
//
// The /metrics HTTP handler is built from this registry and mounted in
// internal/web/handler/routes.go.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the Prometheus registry used by the /metrics handler.
// Exported so tests can inspect it; production callers use the named
// counter/gauge variables directly.
var Registry = prometheus.NewRegistry()

// Session lifecycle counters. Labeled "reason" on Stopped so we can see
// which stop-paths dominate.
var (
	SessionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jarvis_sessions_active",
		Help: "Currently active sessions (PTY + SDK combined).",
	})
	SessionsCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "jarvis_sessions_created_total",
		Help: "Total sessions created since process start.",
	})
	SessionsStopped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jarvis_sessions_stopped_total",
		Help: "Total sessions stopped, labeled by reason.",
	}, []string{"reason"})
)

// SSE broker metrics. `event` label is the prefix (session, office,
// respawn, etc.) so the cardinality stays bounded.
var (
	SSEClients = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jarvis_sse_clients",
		Help: "Currently connected SSE clients.",
	})
	SSEEventsBroadcast = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jarvis_sse_events_broadcast_total",
		Help: "Total SSE events broadcast, labeled by event prefix.",
	}, []string{"event"})
	SSEEventsDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "jarvis_sse_events_dropped_total",
		Help: "SSE events dropped due to slow client / force-close.",
	})
)

// Cost + token counters. Cost is labeled by model so multi-provider
// setups can separate Claude Opus vs Sonnet vs Z.AI spend.
var (
	TokensInput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "jarvis_tokens_input_total",
		Help: "Total input tokens across all sessions.",
	})
	TokensOutput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "jarvis_tokens_output_total",
		Help: "Total output tokens across all sessions.",
	})
	CostUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jarvis_cost_usd_total",
		Help: "Cumulative USD cost, labeled by model.",
	}, []string{"model"})
)

// Scheduler execution results.
var SchedulerRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "jarvis_scheduler_runs_total",
	Help: "Scheduler executions labeled by action type and status.",
}, []string{"action", "status"})

// HTTP request observability.
var (
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jarvis_http_requests_total",
		Help: "Total HTTP requests, labeled by method, normalized route, and status code.",
	}, []string{"method", "route", "status"})
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "jarvis_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})
)

// Notifications dispatch counter added by Phase C.
var NotificationsDispatched = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "jarvis_notifications_dispatched_total",
	Help: "Notification channel dispatches labeled by channel type and status.",
}, []string{"channel", "status"})

func init() {
	Registry.MustRegister(
		SessionsActive, SessionsCreated, SessionsStopped,
		SSEClients, SSEEventsBroadcast, SSEEventsDropped,
		TokensInput, TokensOutput, CostUSD,
		SchedulerRuns,
		HTTPRequests, HTTPDuration,
		NotificationsDispatched,
	)
}

// Handler returns an http.Handler that serves the Prometheus text format.
// Mount at /metrics. Scrapes respect the process-local Registry so tests
// can exercise the real handler in isolation.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
