package gincommon

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics API for consuming services' own collectors, so they never import
// the Prometheus client themselves. Collectors built here carry the same
// {service, version} const labels as gincommon's HTTP metrics and register on
// the same registerer (MetricsRegisterer), so a single MetricsHandler scrape
// serves both. Call these after ObservabilityMiddlewares / DefaultMiddlewares.

// Registerer is where collectors are registered (MetricsRegisterer by
// default; an isolated prometheus.Registry in tests).
type Registerer = prometheus.Registerer

// CounterVec is a counter partitioned by labels.
type CounterVec = prometheus.CounterVec

// Counter is a single counter.
type Counter = prometheus.Counter

// GaugeVec is a gauge partitioned by labels.
type GaugeVec = prometheus.GaugeVec

// Gauge is a single gauge.
type Gauge = prometheus.Gauge

// MetricsHandler serves every collector registered on MetricsRegisterer —
// gincommon's own http_* metrics plus any custom collectors — in the
// Prometheus exposition format. Mount it on the service's metrics port.
func MetricsHandler() http.Handler {
	if g, ok := MetricsRegisterer().(prometheus.Gatherer); ok {
		return promhttp.HandlerFor(g, promhttp.HandlerOpts{})
	}
	return promhttp.Handler()
}

// NewCounterVec builds and registers a counter vector named name with the
// given label names. reg nil means MetricsRegisterer. The collector carries
// MetricsConstLabels.
func NewCounterVec(reg Registerer, name, help string, labelNames ...string) (*CounterVec, error) {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()}, labelNames)
	if err := registerer(reg).Register(c); err != nil {
		return nil, err
	}
	return c, nil
}

// NewCounter builds and registers a counter. reg nil means MetricsRegisterer.
func NewCounter(reg Registerer, name, help string) (Counter, error) {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()})
	if err := registerer(reg).Register(c); err != nil {
		return nil, err
	}
	return c, nil
}

// NewGaugeVec builds and registers a gauge vector. reg nil means
// MetricsRegisterer.
func NewGaugeVec(reg Registerer, name, help string, labelNames ...string) (*GaugeVec, error) {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()}, labelNames)
	if err := registerer(reg).Register(g); err != nil {
		return nil, err
	}
	return g, nil
}

// NewGauge builds and registers a gauge. reg nil means MetricsRegisterer.
func NewGauge(reg Registerer, name, help string) (Gauge, error) {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()})
	if err := registerer(reg).Register(g); err != nil {
		return nil, err
	}
	return g, nil
}

func registerer(reg Registerer) Registerer {
	if reg == nil {
		return MetricsRegisterer()
	}
	return reg
}

// constLabelsOrNil returns MetricsConstLabels, or nil before metrics are
// initialized (no const labels to apply yet).
func constLabelsOrNil() prometheus.Labels {
	labels := MetricsConstLabels()
	if len(labels) == 0 {
		return nil
	}
	return labels
}
