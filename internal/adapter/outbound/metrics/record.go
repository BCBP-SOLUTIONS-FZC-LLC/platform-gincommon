package metrics

// Recording helpers used by the HTTP middleware and gRPC interceptors. Each
// loads the atomically published collector set and is a no-op before
// initialization, so consumers that skip metrics are unaffected and recording
// never races with (test) re-initialization.

// standardHTTPMethods bounds the method label; anything else (arbitrary tokens
// are valid HTTP methods) is reported as OTHER.
var standardHTTPMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// HTTPMethodLabel returns method if it is a standard HTTP method, else "OTHER".
func HTTPMethodLabel(method string) string {
	if standardHTTPMethods[method] {
		return method
	}
	return "OTHER"
}

// UnknownSize is the size RequestBodySize reports for an unknown body length
// (Content-Length -1, e.g. chunked transfer). RecordHTTP and RecordGRPC skip
// the size observation for negative sizes rather than recording a false 0.
const UnknownSize = -1

// RequestBodySize returns the request body size in bytes for histogram
// observation: the Content-Length when known (0 for an empty body), else
// UnknownSize. Streaming request sizes are not captured without body wrapping.
func RequestBodySize(contentLength int64) float64 {
	if contentLength >= 0 {
		return float64(contentLength)
	}
	return UnknownSize
}

// HTTPEnabled reports whether the HTTP collectors are registered.
func HTTPEnabled() bool {
	return active.Load() != nil
}

// HTTPActiveInc increments platform_http_active_requests for route.
func HTTPActiveInc(route string) {
	if cs := active.Load(); cs != nil {
		cs.httpActive.WithLabelValues(route).Inc()
	}
}

// HTTPActiveDec decrements platform_http_active_requests for route.
func HTTPActiveDec(route string) {
	if cs := active.Load(); cs != nil {
		cs.httpActive.WithLabelValues(route).Dec()
	}
}

// RecordHTTP observes one completed HTTP request. A negative reqSize or
// respSize (UnknownSize) is not observed.
func RecordHTTP(method, route, statusClass, errorClass string, seconds, reqSize, respSize float64) {
	cs := active.Load()
	if cs == nil {
		return
	}
	m := HTTPMethodLabel(method)
	cs.httpRequestsTotal.WithLabelValues(m, route, statusClass, errorClass).Inc()
	cs.httpRequestDuration.WithLabelValues(m, route, statusClass, errorClass).Observe(seconds)
	if reqSize >= 0 {
		cs.httpRequestSize.WithLabelValues(m, route, statusClass, errorClass).Observe(reqSize)
	}
	if respSize >= 0 {
		cs.httpResponseSize.WithLabelValues(m, route, statusClass, errorClass).Observe(respSize)
	}
}

// RecordHTTPPanic increments platform_http_panics_total.
func RecordHTTPPanic(method, route string) {
	if cs := active.Load(); cs != nil {
		cs.httpPanics.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// RecordHTTPRateLimited increments platform_http_rate_limited_requests_total.
func RecordHTTPRateLimited(method, route string) {
	if cs := active.Load(); cs != nil {
		cs.httpRateLimited.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// RecordHTTPTimeout increments platform_http_request_timeouts_total when TimeoutMiddleware fires.
func RecordHTTPTimeout(method, route string) {
	if cs := active.Load(); cs != nil {
		cs.httpTimeouts.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// GRPCEnabled reports whether the gRPC collectors are registered.
func GRPCEnabled() bool {
	return active.Load() != nil
}

// GRPCInflightInc increments platform_grpc_server_inflight_requests for method.
func GRPCInflightInc(method string) {
	if cs := active.Load(); cs != nil {
		cs.grpcInflight.WithLabelValues(method).Inc()
	}
}

// GRPCInflightDec decrements platform_grpc_server_inflight_requests for method.
func GRPCInflightDec(method string) {
	if cs := active.Load(); cs != nil {
		cs.grpcInflight.WithLabelValues(method).Dec()
	}
}

// Transport label values of platform_unclassified_errors_total.
const (
	TransportHTTP = "http"
	TransportGRPC = "grpc"
)

// RecordUnclassifiedError increments platform_unclassified_errors_total for
// transport (TransportHTTP or TransportGRPC): an error that reached the top-level
// handler unclassified. A no-op before metrics are initialized.
func RecordUnclassifiedError(transport string) {
	if cs := active.Load(); cs != nil {
		cs.unclassifiedErrors.WithLabelValues(transport).Inc()
	}
}

// RecordGRPCPanic increments platform_grpc_panics_total for method.
func RecordGRPCPanic(method string) {
	if cs := active.Load(); cs != nil {
		cs.grpcPanics.WithLabelValues(method).Inc()
	}
}

// RecordGRPCRateLimited increments platform_grpc_rate_limited_requests_total.
func RecordGRPCRateLimited(method string) {
	if cs := active.Load(); cs != nil {
		cs.grpcRateLimited.WithLabelValues(method).Inc()
	}
}

// RecordGRPC observes server-side gRPC metrics for one completed RPC. A
// negative reqSize or respSize (UnknownSize) is not observed.
func RecordGRPC(
	method string,
	durationSeconds float64,
	grpcCode, statusClass, errorClass string,
	reqSize, respSize float64,
) {
	cs := active.Load()
	if cs == nil {
		return
	}
	cs.grpcRequestsTotal.WithLabelValues(method, grpcCode, statusClass, errorClass).Inc()
	cs.grpcRequestDuration.WithLabelValues(method, statusClass, errorClass).Observe(durationSeconds)
	if reqSize >= 0 {
		cs.grpcRequestSize.WithLabelValues(method, statusClass, errorClass).Observe(reqSize)
	}
	if respSize >= 0 {
		cs.grpcResponseSize.WithLabelValues(method, statusClass, errorClass).Observe(respSize)
	}
}
