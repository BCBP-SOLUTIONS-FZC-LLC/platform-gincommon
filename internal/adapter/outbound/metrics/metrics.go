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
)

const defaultVersion = "unknown"

// Init registers Prometheus collectors with const labels service and version.
// Empty buildVersion is stored as "unknown".
func Init(serviceName, buildVersion string) {
	initOnce.Do(func() {
		if buildVersion == "" {
			buildVersion = defaultVersion
		}
		initMetrics(serviceName, buildVersion)
	})
}

func initMetrics(serviceName, buildVersion string) {
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

	prometheus.MustRegister(
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
