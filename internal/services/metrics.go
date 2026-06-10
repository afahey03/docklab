package services

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the Prometheus registry and all DockLab application metrics.
type Metrics struct {
	registry *prometheus.Registry

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	operationsTotal     *prometheus.CounterVec
	environmentsCreated *prometheus.CounterVec
	terminalSessions    prometheus.Gauge
	alertsSent          prometheus.Counter
	lifecycleActions    *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		registry: registry,
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "docklab_http_requests_total",
			Help: "HTTP requests by method, route, and status code.",
		}, []string{"method", "route", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "docklab_http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		operationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "docklab_operations_total",
			Help: "Async operations by type and final status.",
		}, []string{"type", "status"}),
		environmentsCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "docklab_environments_created_total",
			Help: "Environments created by target.",
		}, []string{"target"}),
		terminalSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "docklab_terminal_clients_active",
			Help: "Currently connected terminal websocket clients.",
		}),
		alertsSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "docklab_alerts_sent_total",
			Help: "Alert webhook deliveries attempted.",
		}),
		lifecycleActions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "docklab_lifecycle_actions_total",
			Help: "Automatic lifecycle actions by kind (workspace_stop, cloud_stop, cloud_terminate).",
		}, []string{"kind"}),
	}

	registry.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.operationsTotal,
		m.environmentsCreated,
		m.terminalSessions,
		m.alertsSent,
		m.lifecycleActions,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveHTTPRequest(method, route, status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.httpRequestsTotal.WithLabelValues(method, route, status).Inc()
	m.httpRequestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) RecordOperation(operationType, status string) {
	if m == nil {
		return
	}
	m.operationsTotal.WithLabelValues(operationType, status).Inc()
}

func (m *Metrics) RecordEnvironmentCreated(target string) {
	if m == nil {
		return
	}
	m.environmentsCreated.WithLabelValues(target).Inc()
}

func (m *Metrics) TerminalClientConnected() {
	if m == nil {
		return
	}
	m.terminalSessions.Inc()
}

func (m *Metrics) TerminalClientDisconnected() {
	if m == nil {
		return
	}
	m.terminalSessions.Dec()
}

func (m *Metrics) RecordAlertSent() {
	if m == nil {
		return
	}
	m.alertsSent.Inc()
}

func (m *Metrics) RecordLifecycleAction(kind string) {
	if m == nil {
		return
	}
	m.lifecycleActions.WithLabelValues(kind).Inc()
}
