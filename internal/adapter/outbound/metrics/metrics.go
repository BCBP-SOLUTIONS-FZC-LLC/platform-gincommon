package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus collectors; non-nil after Init. Label semantics match IAM-style platform metrics.
var (
	// HttpRequestsTotal counts requests by method, route, status_class, error_class.
	HttpRequestsTotal *prometheus.CounterVec
	// HttpRequestDuration observes latency in seconds with IAM-tuned buckets.
	HttpRequestDuration *prometheus.HistogramVec
	// HttpRequestSizeBytes observes request body size in bytes.
	HttpRequestSizeBytes *prometheus.HistogramVec
	// HttpResponseSizeBytes observes response body size in bytes.
	HttpResponseSizeBytes *prometheus.HistogramVec
	// HttpActiveRequests tracks in-flight requests by route template.
	HttpActiveRequests *prometheus.GaugeVec
	// HttpPanicTotal counts recovered panics by method and route.
	HttpPanicTotal *prometheus.CounterVec
	// RateLimitedRequestsTotal counts HTTP 429 responses by method and route.
	RateLimitedRequestsTotal *prometheus.CounterVec
	// BuildInfo is a gauge set to 1 with service/version const labels.
	BuildInfo *prometheus.GaugeVec

	initOnce sync.Once
	// initMu guards writes to the package-level collector vars.
	// Init is protected by initOnce; InitWithRegisterer (test-only) uses this mutex
	// to prevent a data race when called while other goroutines read the vars.
	initMu sync.Mutex
)

const defaultVersion = "unknown"

// Init registers Prometheus collectors with const labels service and version.
// It is idempotent via sync.Once: the first call wins. All reads of the package-level
// vars are safe after Init returns because sync.Once provides the required happens-before.
// Empty buildVersion is stored as "unknown".
func Init(serviceName, buildVersion string) {
	initOnce.Do(func() {
		if buildVersion == "" {
			buildVersion = defaultVersion
		}
		initMu.Lock()
		defer initMu.Unlock()
		initMetricsWithRegisterer(serviceName, buildVersion, prometheus.DefaultRegisterer)
	})
}

// InitWithRegisterer initializes metrics using the provided Prometheus registerer.
// Unlike Init, it does not use sync.Once, allowing test packages to use isolated
// registries (prometheus.NewRegistry()) to avoid cross-test pollution.
// Must be called before any middleware goroutines start — concurrent calls with
// in-flight requests are not safe even with the mutex because readers do not lock.
// It panics on duplicate registration just like prometheus.MustRegister.
func InitWithRegisterer(serviceName, buildVersion string, reg prometheus.Registerer) {
	if buildVersion == "" {
		buildVersion = defaultVersion
	}
	// Consume initOnce so a subsequent Init call does not overwrite the custom registry.
	// Without this, Init (which uses initOnce.Do) fires after InitWithRegisterer and
	// replaces all package-level vars with DefaultRegisterer collectors.
	initOnce.Do(func() {})
	initMu.Lock()
	defer initMu.Unlock()
	initMetricsWithRegisterer(serviceName, buildVersion, reg)
}

func initMetricsWithRegisterer(serviceName, buildVersion string, reg prometheus.Registerer) {
	constLabels := prometheus.Labels{
		"service": serviceName,
		"version": buildVersion,
	}

	HttpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "http_requests_total",
			Help:        "Total HTTP requests",
			ConstLabels: constLabels,
		},
		[]string{"method", "route", "status_class", "error_class"},
	)

	httpOutcomeLabels := []string{"method", "route", "status_class", "error_class"}

	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "http_request_duration_seconds",
			Help:        "HTTP request duration",
			Buckets:     HTTPRequestDurationBuckets,
			ConstLabels: constLabels,
		},
		httpOutcomeLabels,
	)

	HttpPanicTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "http_panic_total",
			Help:        "Total HTTP handler panics recovered",
			ConstLabels: constLabels,
		},
		[]string{"method", "route"},
	)

	sizeOpts := prometheus.HistogramOpts{
		Buckets:     HTTPPayloadSizeBuckets,
		ConstLabels: constLabels,
	}
	HttpRequestSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "http_request_size_bytes",
			Help:        "HTTP request body size in bytes",
			Buckets:     sizeOpts.Buckets,
			ConstLabels: sizeOpts.ConstLabels,
		},
		httpOutcomeLabels,
	)

	HttpResponseSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "http_response_size_bytes",
			Help:        "HTTP response body size in bytes",
			Buckets:     sizeOpts.Buckets,
			ConstLabels: sizeOpts.ConstLabels,
		},
		httpOutcomeLabels,
	)

	HttpActiveRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "http_active_requests",
			Help:        "Active HTTP requests being handled",
			ConstLabels: constLabels,
		},
		[]string{"route"},
	)

	RateLimitedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "rate_limited_requests_total",
			Help:        "Total HTTP requests rejected by rate limiting (429)",
			ConstLabels: constLabels,
		},
		[]string{"method", "route"},
	)

	BuildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "build_info",
			Help:        "Build information",
			ConstLabels: constLabels,
		},
		[]string{},
	)
	BuildInfo.WithLabelValues().Set(1)

	initGRPCMetrics(constLabels)

	reg.MustRegister(
		BuildInfo,
		HttpRequestsTotal,
		HttpRequestDuration,
		HttpRequestSizeBytes,
		HttpResponseSizeBytes,
		HttpActiveRequests,
		HttpPanicTotal,
		RateLimitedRequestsTotal,
		GrpcRequestsTotal,
		GrpcRequestDuration,
		GrpcRequestSizeBytes,
		GrpcResponseSizeBytes,
		GrpcInflight,
		GrpcRateLimitedRequestsTotal,
		GrpcPanicTotal,
	)
}

// RequestBodySize returns the request body size in bytes for histogram observation.
// Unknown or missing Content-Length is recorded as 0.
func RequestBodySize(contentLength int64) float64 {
	if contentLength > 0 {
		return float64(contentLength)
	}
	return 0
}

// ErrorClass returns the error_class outcome label for an HTTP status code.
func ErrorClass(statusCode int) string {
	ec, _ := RequestOutcome(statusCode)
	return ec
}

// HTTPIsError returns the is_error outcome label ("true" or "false") for an HTTP status code.
func HTTPIsError(statusCode int) string {
	_, isErr := RequestOutcome(statusCode)
	return isErr
}

// RecordHTTPRateLimited increments rate_limited_requests_total for explicit rate-limit paths.
func RecordHTTPRateLimited(method, route string) {
	if RateLimitedRequestsTotal == nil {
		return
	}
	RateLimitedRequestsTotal.WithLabelValues(method, route).Inc()
}
