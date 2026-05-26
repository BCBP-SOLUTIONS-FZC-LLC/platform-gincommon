package metrics

import "github.com/prometheus/client_golang/prometheus"

// gRPC Prometheus collectors; non-nil after Init.
var (
	// GrpcRequestsTotal counts RPCs by method, grpc_code, status_class, error_class.
	GrpcRequestsTotal *prometheus.CounterVec
	// GrpcRequestDuration observes RPC latency in seconds.
	GrpcRequestDuration *prometheus.HistogramVec
	// GrpcRequestSizeBytes observes encoded request message size.
	GrpcRequestSizeBytes *prometheus.HistogramVec
	// GrpcResponseSizeBytes observes encoded response message size.
	GrpcResponseSizeBytes *prometheus.HistogramVec
	// GrpcInflight tracks in-flight RPCs by method.
	GrpcInflight *prometheus.GaugeVec
	// GrpcRateLimitedRequestsTotal counts ResourceExhausted RPCs by method.
	GrpcRateLimitedRequestsTotal *prometheus.CounterVec
)

func initGRPCMetrics(constLabels prometheus.Labels) {
	grpcLabels := []string{"method", "grpc_code", "status_class", "error_class"}
	grpcOutcomeLabels := []string{"method", "status_class", "error_class"}

	GrpcRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "grpc_server_handled_total",
			Help:        "Total gRPC server RPCs handled",
			ConstLabels: constLabels,
		},
		grpcLabels,
	)

	GrpcRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "grpc_server_handling_seconds",
			Help:        "gRPC server RPC handling duration in seconds",
			Buckets:     HTTPRequestDurationBuckets,
			ConstLabels: constLabels,
		},
		grpcOutcomeLabels,
	)

	sizeOpts := prometheus.HistogramOpts{
		Buckets:     HTTPPayloadSizeBuckets,
		ConstLabels: constLabels,
	}

	GrpcRequestSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "grpc_server_request_size_bytes",
			Help:        "gRPC server request message size in bytes",
			Buckets:     sizeOpts.Buckets,
			ConstLabels: sizeOpts.ConstLabels,
		},
		grpcOutcomeLabels,
	)

	GrpcResponseSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "grpc_server_response_size_bytes",
			Help:        "gRPC server response message size in bytes",
			Buckets:     sizeOpts.Buckets,
			ConstLabels: sizeOpts.ConstLabels,
		},
		grpcOutcomeLabels,
	)

	GrpcInflight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "grpc_server_inflight_requests",
			Help:        "Inflight gRPC server RPCs",
			ConstLabels: constLabels,
		},
		[]string{"method"},
	)

	GrpcRateLimitedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "grpc_rate_limited_requests_total",
			Help:        "Total gRPC RPCs rejected by rate limiting (ResourceExhausted)",
			ConstLabels: constLabels,
		},
		[]string{"method"},
	)
}

// RecordGRPCRateLimited increments grpc_rate_limited_requests_total.
func RecordGRPCRateLimited(method string) {
	if GrpcRateLimitedRequestsTotal == nil {
		return
	}
	GrpcRateLimitedRequestsTotal.WithLabelValues(method).Inc()
}

// RecordGRPC observes server-side gRPC metrics for one completed RPC.
func RecordGRPC(
	method string,
	durationSeconds float64,
	grpcCode, statusClass, errorClass string,
	reqSize, respSize float64,
) {
	if GrpcRequestsTotal == nil {
		return
	}
	GrpcRequestsTotal.WithLabelValues(method, grpcCode, statusClass, errorClass).Inc()
	GrpcRequestDuration.WithLabelValues(method, statusClass, errorClass).Observe(durationSeconds)
	GrpcRequestSizeBytes.WithLabelValues(method, statusClass, errorClass).Observe(reqSize)
	GrpcResponseSizeBytes.WithLabelValues(method, statusClass, errorClass).Observe(respSize)
}
