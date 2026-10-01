package metrics

import (
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
)

// Prometheus collectors under the Enterprise Platform Observability Standard;
// non-nil after initialization. Names, help, label names and buckets come from
// the Platform Observability Registry (internal/obsregistry/registry.json), and
// every collector carries the {domain, service, environment} const labels.
var (
	// HttpRequestsTotal is platform_http_requests_total{method, route, status_class, error_class}.
	HttpRequestsTotal *prometheus.CounterVec
	// HttpRequestDuration is platform_http_request_duration_seconds (request_duration buckets).
	HttpRequestDuration *prometheus.HistogramVec
	// HttpRequestSizeBytes is platform_http_request_size_bytes (payload_size buckets).
	HttpRequestSizeBytes *prometheus.HistogramVec
	// HttpResponseSizeBytes is platform_http_response_size_bytes (payload_size buckets).
	HttpResponseSizeBytes *prometheus.HistogramVec
	// HttpActiveRequests is platform_http_active_requests{route}.
	HttpActiveRequests *prometheus.GaugeVec
	// HttpPanicTotal is platform_http_panics_total{method, route}.
	HttpPanicTotal *prometheus.CounterVec
	// RateLimitedRequestsTotal is platform_http_rate_limited_requests_total{method, route}.
	RateLimitedRequestsTotal *prometheus.CounterVec
	// HttpRequestTimeoutTotal is platform_http_request_timeouts_total{method, route}.
	HttpRequestTimeoutTotal *prometheus.CounterVec

	// GrpcRequestsTotal is platform_grpc_server_handled_total{grpc_method, grpc_code, status_class, error_class}.
	GrpcRequestsTotal *prometheus.CounterVec
	// GrpcRequestDuration is platform_grpc_server_handling_seconds{grpc_method, status_class, error_class}.
	GrpcRequestDuration *prometheus.HistogramVec
	// GrpcRequestSizeBytes is platform_grpc_server_request_size_bytes.
	GrpcRequestSizeBytes *prometheus.HistogramVec
	// GrpcResponseSizeBytes is platform_grpc_server_response_size_bytes.
	GrpcResponseSizeBytes *prometheus.HistogramVec
	// GrpcInflight is platform_grpc_server_inflight_requests{grpc_method}.
	GrpcInflight *prometheus.GaugeVec
	// GrpcRateLimitedRequestsTotal is platform_grpc_rate_limited_requests_total{grpc_method}.
	GrpcRateLimitedRequestsTotal *prometheus.CounterVec
	// GrpcPanicTotal is platform_grpc_panics_total{grpc_method}.
	GrpcPanicTotal *prometheus.CounterVec

	// BuildInfo is platform_build_info{version}, always 1.
	BuildInfo *prometheus.GaugeVec

	initOnce sync.Once
	// initMu guards writes to the package-level collector vars.
	// InitWithOptions is protected by initOnce; InitWithRegistererOptions (tests)
	// uses this mutex to prevent a data race when called while other goroutines
	// read the vars.
	initMu sync.Mutex

	// currentRegisterer, currentConstLabels and currentOptions record the
	// registerer, const labels and options of the most recent Init* call.
	currentRegisterer  prometheus.Registerer
	currentConstLabels prometheus.Labels
	currentOptions     Options
)

const defaultVersion = "unknown"

// Options identifies the emitting service for the centrally injected labels.
type Options struct {
	// ServiceName is the service const label (required).
	ServiceName string
	// BuildVersion is the platform_build_info version label; empty becomes "unknown".
	BuildVersion string
	// Domain is the registry domain const label (required).
	Domain string
	// Environment is the environment const label (required; trimmed and
	// lowercased, then checked against the registry vocabulary).
	Environment string
}

// ErrDomainRequired is returned when no domain is configured: every metric
// carries the domain label, so there is nothing valid to emit without one.
var ErrDomainRequired = errors.New("metrics: a domain is required (set Config.Domain or OBSERVABILITY_DOMAIN to a registry domain)")

func (o Options) resolve() (Options, error) {
	if o.Domain == "" {
		return o, ErrDomainRequired
	}
	env, err := obsregistry.Default().CheckIdentity(o.Domain, o.ServiceName, o.Environment)
	if err != nil {
		return o, fmt.Errorf("metrics: %w", err)
	}
	o.Environment = env
	if o.BuildVersion == "" {
		o.BuildVersion = defaultVersion
	}
	return o, nil
}

// ApplyEnvDefaults fills the fields a pkg-level Config left empty: the build
// version from BUILD_VERSION (then "unknown"), the domain from
// OBSERVABILITY_DOMAIN, and the environment from APP_ENV, then ENVIRONMENT —
// the precedence platform-events and platform-pgcommon use.
func ApplyEnvDefaults(opts Options) Options {
	if opts.BuildVersion == "" {
		opts.BuildVersion = config.BuildVersion()
	}
	if opts.Domain == "" {
		opts.Domain = config.ObservabilityDomain()
	}
	if opts.Environment == "" {
		opts.Environment = config.ObservabilityEnvironment()
	}
	return opts
}

// MustInitFromConfig initializes metrics for a pkg-level Config with
// ApplyEnvDefaults. It panics on a missing domain or an identity the registry
// rejects (unknown domain, invalid service, unmappable environment), like the
// empty-ServiceName check, so misconfiguration fails at startup rather than
// corrupting cross-service aggregation.
func MustInitFromConfig(opts Options) {
	if err := InitWithOptions(ApplyEnvDefaults(opts)); err != nil {
		panic(err.Error())
	}
}

// InitWithOptions validates the identity against the Platform Observability
// Registry and registers the collectors on prometheus.DefaultRegisterer. It is
// idempotent via sync.Once: the first successful caller wins. A validation
// error is returned without consuming the once guard.
func InitWithOptions(opts Options) error {
	opts, err := opts.resolve()
	if err != nil {
		return err
	}
	initOnce.Do(func() {
		initMu.Lock()
		defer initMu.Unlock()
		initCollectors(opts, prometheus.DefaultRegisterer)
	})
	return nil
}

// InitWithRegistererOptions is InitWithOptions on reg without the once guard,
// so tests can use isolated registries (prometheus.NewRegistry()). It consumes
// the once guard so a later InitWithOptions does not replace the collectors.
// Must be called before any middleware goroutines start. It panics on
// duplicate registration just like prometheus.MustRegister.
func InitWithRegistererOptions(opts Options, reg prometheus.Registerer) error {
	opts, err := opts.resolve()
	if err != nil {
		return err
	}
	initOnce.Do(func() {})
	initMu.Lock()
	defer initMu.Unlock()
	initCollectors(opts, reg)
	return nil
}

func initCollectors(opts Options, reg prometheus.Registerer) {
	constLabels := prometheus.Labels{
		"domain":      opts.Domain,
		"environment": opts.Environment,
		"service":     opts.ServiceName,
	}
	currentRegisterer = reg
	currentConstLabels = constLabels
	currentOptions = opts

	b := registryBuilder{reg: obsregistry.Default(), constLabels: constLabels}
	BuildInfo = b.gaugeVec("platform_build_info")
	BuildInfo.WithLabelValues(opts.BuildVersion).Set(1)

	HttpRequestsTotal = b.counterVec("platform_http_requests_total")
	HttpRequestDuration = b.histogramVec("platform_http_request_duration_seconds")
	HttpRequestSizeBytes = b.histogramVec("platform_http_request_size_bytes")
	HttpResponseSizeBytes = b.histogramVec("platform_http_response_size_bytes")
	HttpActiveRequests = b.gaugeVec("platform_http_active_requests")
	HttpPanicTotal = b.counterVec("platform_http_panics_total")
	RateLimitedRequestsTotal = b.counterVec("platform_http_rate_limited_requests_total")
	HttpRequestTimeoutTotal = b.counterVec("platform_http_request_timeouts_total")

	GrpcRequestsTotal = b.counterVec("platform_grpc_server_handled_total")
	GrpcRequestDuration = b.histogramVec("platform_grpc_server_handling_seconds")
	GrpcRequestSizeBytes = b.histogramVec("platform_grpc_server_request_size_bytes")
	GrpcResponseSizeBytes = b.histogramVec("platform_grpc_server_response_size_bytes")
	GrpcInflight = b.gaugeVec("platform_grpc_server_inflight_requests")
	GrpcRateLimitedRequestsTotal = b.counterVec("platform_grpc_rate_limited_requests_total")
	GrpcPanicTotal = b.counterVec("platform_grpc_panics_total")

	reg.MustRegister(b.built...)
}

// registryBuilder builds collectors from registry entries. A missing or
// mistyped entry is a defect in this module (covered by tests) and panics.
type registryBuilder struct {
	reg         *obsregistry.Registry
	constLabels prometheus.Labels
	built       []prometheus.Collector
}

func (b *registryBuilder) entry(name string, t obsregistry.Type) *obsregistry.Metric {
	m, ok := b.reg.Lookup(name)
	if !ok || m.Type != t {
		panic(fmt.Sprintf("metrics: registry has no %s %q", t, name))
	}
	return m
}

func (b *registryBuilder) counterVec(name string) *prometheus.CounterVec {
	m := b.entry(name, obsregistry.Counter)
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: m.Name, Help: m.Help, ConstLabels: b.constLabels}, m.Labels)
	b.built = append(b.built, c)
	return c
}

func (b *registryBuilder) gaugeVec(name string) *prometheus.GaugeVec {
	m := b.entry(name, obsregistry.Gauge)
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: m.Name, Help: m.Help, ConstLabels: b.constLabels}, m.Labels)
	b.built = append(b.built, g)
	return g
}

func (b *registryBuilder) histogramVec(name string) *prometheus.HistogramVec {
	m := b.entry(name, obsregistry.Histogram)
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: m.Name, Help: m.Help, ConstLabels: b.constLabels, Buckets: b.reg.Buckets(m.Buckets),
	}, m.Labels)
	b.built = append(b.built, h)
	return h
}

// Registerer returns the prometheus.Registerer the collectors were registered
// to by the most recent Init* call, or prometheus.DefaultRegisterer before any.
func Registerer() prometheus.Registerer {
	initMu.Lock()
	defer initMu.Unlock()
	if currentRegisterer == nil {
		return prometheus.DefaultRegisterer
	}
	return currentRegisterer
}

// ConstLabels returns a copy of the {domain, environment, service} const labels
// every collector carries (version is only on platform_build_info), so custom
// collectors can match them. Empty before initialization.
func ConstLabels() prometheus.Labels {
	initMu.Lock()
	defer initMu.Unlock()
	out := make(prometheus.Labels, len(currentConstLabels))
	maps.Copy(out, currentConstLabels)
	return out
}

// Identity returns the domain and service of the most recent Init* call (empty
// before initialization).
func Identity() (domain, service string) {
	initMu.Lock()
	defer initMu.Unlock()
	return currentOptions.Domain, currentOptions.ServiceName
}

// Initialized reports whether the collectors are registered.
func Initialized() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return BuildInfo != nil
}
