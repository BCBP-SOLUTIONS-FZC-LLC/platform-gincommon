package gincommon

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/logger"
)

// Metrics API for consuming services' own collectors, so they never import
// the Prometheus client themselves. Collectors built here carry the same const
// labels as gincommon's HTTP metrics (MetricsConstLabels: {domain, environment,
// service}) and register on the same
// registerer (MetricsRegisterer), so a single MetricsHandler scrape serves both.
// Call these after ObservabilityMiddlewares / DefaultMiddlewares.
//
// The Enterprise Platform Observability Standard is enforced at construction:
//   - NewShared* build Tier 1 (platform_*) and Tier 2 (<domain>_*) metrics from
//     their Platform Observability Registry entry: name, help, labels and
//     buckets are the registry's, so services cannot drift from the contract.
//   - NewCounterVec / NewCounter / NewGaugeVec / NewGauge / NewHistogramVec build
//     Tier 3 service-specific metrics. They reject registry and platform_* names,
//     wrong suffixes (_total for counters, _seconds for histograms), prohibited
//     high-cardinality labels and the centrally injected labels, and the name
//     must start with the service's <domain>_<service>_ prefix.
//
// Every constructor returns an error before ObservabilityMiddlewares (or the
// grpccommon interceptors) has run: the identity labels are not known yet.

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

// HistogramVec is a histogram partitioned by labels.
type HistogramVec = prometheus.HistogramVec

// Gatherer collects registered metrics, e.g. for obsregistry validation in tests.
type Gatherer = prometheus.Gatherer

// MetricsGatherer returns the Gatherer behind MetricsRegisterer (the default
// gatherer when the registerer is not one). Validate it in a service's tests
// with pkg/obsregistry.Validate.
func MetricsGatherer() Gatherer {
	if g, ok := MetricsRegisterer().(prometheus.Gatherer); ok {
		return g
	}
	return prometheus.DefaultGatherer
}

// MetricsHandler serves every collector registered on MetricsRegisterer —
// gincommon's own metrics plus any custom collectors — in the
// Prometheus exposition format. Mount it on the service's metrics port.
//
// A collector that fails during a scrape does not blank the page: the
// handler serves every metric that was gathered (promhttp.ContinueOnError)
// and logs the gather error through logger.Default() (stderr if that logger
// cannot be built). Each distinct error is logged when first seen and then at
// most once every 5 minutes, so a persistently broken collector does not log
// on every scrape; promhttp_metric_handler_errors_total{cause} counts every
// occurrence (exempt from the observability standard like go_* and process_*).
// One broken custom collector cannot hide gincommon's own metrics.
func MetricsHandler() http.Handler {
	opts := promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      metricsErrorLog,
	}
	// Register the error counter only when the registerer is the gatherer
	// this handler scrapes, so it is actually exposed; promhttp reuses an
	// already registered counter when MetricsHandler is called again.
	reg := MetricsRegisterer()
	if _, ok := reg.(prometheus.Gatherer); ok {
		opts.Registry = reg
	}
	return promhttp.HandlerFor(MetricsGatherer(), opts)
}

// metricsErrorInterval is how often MetricsHandler repeats one distinct error.
const metricsErrorInterval = 5 * time.Minute

// metricsErrorLog receives gather and encoding errors from MetricsHandler.
var metricsErrorLog = metrics.NewRateLimitedErrorLog(metricsErrorInterval, 64, nil, logMetricsHandlerError)

var metricsStderrLog = log.New(os.Stderr, "gincommon: metrics handler: ", log.LstdFlags)

func logMetricsHandlerError(msg string) {
	if l, err := logger.Default(); err == nil && l != nil {
		l.Error("metrics handler error", map[string]interface{}{"error": msg})
		return
	}
	metricsStderrLog.Println(msg)
}

// NewCounterVec builds and registers a service-specific counter vector.
// reg nil means MetricsRegisterer. The collector carries MetricsConstLabels.
func NewCounterVec(reg Registerer, name, help string, labelNames ...string) (*CounterVec, error) {
	if err := checkCustom(obsregistry.Counter, name, labelNames); err != nil {
		return nil, err
	}
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()}, labelNames)
	return registered(reg, c)
}

// NewCounter builds and registers a service-specific counter. reg nil means MetricsRegisterer.
func NewCounter(reg Registerer, name, help string) (Counter, error) {
	if err := checkCustom(obsregistry.Counter, name, nil); err != nil {
		return nil, err
	}
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()})
	return registered(reg, c)
}

// NewGaugeVec builds and registers a service-specific gauge vector. reg nil
// means MetricsRegisterer.
func NewGaugeVec(reg Registerer, name, help string, labelNames ...string) (*GaugeVec, error) {
	if err := checkCustom(obsregistry.Gauge, name, labelNames); err != nil {
		return nil, err
	}
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()}, labelNames)
	return registered(reg, g)
}

// NewGauge builds and registers a service-specific gauge. reg nil means MetricsRegisterer.
func NewGauge(reg Registerer, name, help string) (Gauge, error) {
	if err := checkCustom(obsregistry.Gauge, name, nil); err != nil {
		return nil, err
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: constLabelsOrNil()})
	return registered(reg, g)
}

// NewHistogramVec builds and registers a service-specific histogram vector
// (name must end in _seconds). nil buckets means the registry's
// request_duration preset. reg nil means MetricsRegisterer.
func NewHistogramVec(reg Registerer, name, help string, buckets []float64, labelNames ...string) (*HistogramVec, error) {
	if err := checkCustom(obsregistry.Histogram, name, labelNames); err != nil {
		return nil, err
	}
	if buckets == nil {
		buckets = obsregistry.Default().Buckets("request_duration")
	}
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: name, Help: help, Buckets: buckets, ConstLabels: constLabelsOrNil(),
	}, labelNames)
	return registered(reg, h)
}

// NewSharedCounterVec builds and registers the registry counter name (e.g.
// platform_messages_processed_total or iam_auth_events_total) with the
// registry's help text and label names, in registry order. Call it after
// ObservabilityMiddlewares. reg nil means MetricsRegisterer.
func NewSharedCounterVec(reg Registerer, name string) (*CounterVec, error) {
	m, labels, err := sharedEntry(name, obsregistry.Counter)
	if err != nil {
		return nil, err
	}
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: m.Name, Help: m.Help, ConstLabels: labels}, m.Labels)
	return registered(reg, c)
}

// NewSharedGaugeVec is NewSharedCounterVec for registry gauges (e.g. platform_queue_depth).
func NewSharedGaugeVec(reg Registerer, name string) (*GaugeVec, error) {
	m, labels, err := sharedEntry(name, obsregistry.Gauge)
	if err != nil {
		return nil, err
	}
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: m.Name, Help: m.Help, ConstLabels: labels}, m.Labels)
	return registered(reg, g)
}

// NewSharedHistogramVec is NewSharedCounterVec for registry histograms (e.g.
// platform_dependency_request_seconds), using the registry's bucket preset so
// quantiles aggregate across services.
func NewSharedHistogramVec(reg Registerer, name string) (*HistogramVec, error) {
	m, labels, err := sharedEntry(name, obsregistry.Histogram)
	if err != nil {
		return nil, err
	}
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: m.Name, Help: m.Help, Buckets: obsregistry.Default().Buckets(m.Buckets), ConstLabels: labels,
	}, m.Labels)
	return registered(reg, h)
}

// LabeledRegisterer returns a Registerer that adds MetricsConstLabels to every
// collector registered through it, then registers onto reg (nil means
// MetricsRegisterer). The labels and target registerer are resolved when each
// collector is registered, so it may be created before ObservabilityMiddlewares
// runs. Hand it to third-party collectors that know nothing about the
// standard. Do NOT hand it to platform-events or platform-pgcommon: they inject
// the identity labels themselves (give them MetricsRegisterer()), and wrapping
// them again makes registration fail on the duplicate label names.
func LabeledRegisterer(reg Registerer) Registerer {
	return labeledRegisterer{reg: reg}
}

type labeledRegisterer struct{ reg Registerer }

func (l labeledRegisterer) current() Registerer {
	return prometheus.WrapRegistererWith(MetricsConstLabels(), registerer(l.reg))
}

func (l labeledRegisterer) Register(c prometheus.Collector) error { return l.current().Register(c) }

func (l labeledRegisterer) MustRegister(cs ...prometheus.Collector) { l.current().MustRegister(cs...) }

func (l labeledRegisterer) Unregister(c prometheus.Collector) bool { return l.current().Unregister(c) }

func checkCustom(t obsregistry.Type, name string, labelNames []string) error {
	domain, service := metrics.Identity()
	if domain == "" {
		return fmt.Errorf("metric %q: %w", name, errNotInitialized)
	}
	return obsregistry.Default().CheckCustom(t, name, labelNames, domain, service)
}

var errNotInitialized = errors.New("metrics are not initialized yet; call ObservabilityMiddlewares (or the grpccommon interceptors) first")

func sharedEntry(name string, t obsregistry.Type) (*obsregistry.Metric, prometheus.Labels, error) {
	domain, _ := metrics.Identity()
	if domain == "" {
		return nil, nil, fmt.Errorf("metric %q: %w", name, errNotInitialized)
	}
	m, err := obsregistry.Default().SharedMetric(name, t, domain)
	if err != nil {
		return nil, nil, err
	}
	return m, MetricsConstLabels(), nil
}

// registered registers c, returning the zero value (nil) on failure.
func registered[C prometheus.Collector](reg Registerer, c C) (C, error) {
	if err := registerer(reg).Register(c); err != nil {
		var zero C
		return zero, err
	}
	return c, nil
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
