package metrics

// Recording helpers used by the HTTP middleware and gRPC interceptors. Each is
// a no-op before initialization (nil collectors), so consumers that skip
// metrics are unaffected.

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

// RequestBodySize returns the request body size in bytes for histogram observation.
// Both Content-Length: -1 (unknown/chunked transfer) and Content-Length: 0 (empty body)
// return 0; streaming request sizes are not captured without body wrapping.
func RequestBodySize(contentLength int64) float64 {
	if contentLength > 0 {
		return float64(contentLength)
	}
	return 0
}

// HTTPEnabled reports whether the HTTP collectors are registered.
func HTTPEnabled() bool {
	return HttpRequestsTotal != nil && HttpActiveRequests != nil
}

// HTTPActiveInc increments platform_http_active_requests for route.
func HTTPActiveInc(route string) {
	if HttpActiveRequests != nil {
		HttpActiveRequests.WithLabelValues(route).Inc()
	}
}

// HTTPActiveDec decrements platform_http_active_requests for route.
func HTTPActiveDec(route string) {
	if HttpActiveRequests != nil {
		HttpActiveRequests.WithLabelValues(route).Dec()
	}
}

// RecordHTTP observes one completed HTTP request.
func RecordHTTP(method, route, statusClass, errorClass string, seconds, reqSize, respSize float64) {
	if HttpRequestsTotal == nil {
		return
	}
	m := HTTPMethodLabel(method)
	HttpRequestsTotal.WithLabelValues(m, route, statusClass, errorClass).Inc()
	HttpRequestDuration.WithLabelValues(m, route, statusClass, errorClass).Observe(seconds)
	HttpRequestSizeBytes.WithLabelValues(m, route, statusClass, errorClass).Observe(reqSize)
	HttpResponseSizeBytes.WithLabelValues(m, route, statusClass, errorClass).Observe(respSize)
}

// RecordHTTPPanic increments platform_http_panics_total.
func RecordHTTPPanic(method, route string) {
	if HttpPanicTotal != nil {
		HttpPanicTotal.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// RecordHTTPRateLimited increments platform_http_rate_limited_requests_total.
func RecordHTTPRateLimited(method, route string) {
	if RateLimitedRequestsTotal != nil {
		RateLimitedRequestsTotal.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// RecordHTTPTimeout increments platform_http_request_timeouts_total when TimeoutMiddleware fires.
func RecordHTTPTimeout(method, route string) {
	if HttpRequestTimeoutTotal != nil {
		HttpRequestTimeoutTotal.WithLabelValues(HTTPMethodLabel(method), route).Inc()
	}
}

// GRPCEnabled reports whether the gRPC collectors are registered.
func GRPCEnabled() bool {
	return GrpcRequestsTotal != nil && GrpcInflight != nil
}

// GRPCInflightInc increments platform_grpc_server_inflight_requests for method.
func GRPCInflightInc(method string) {
	if GrpcInflight != nil {
		GrpcInflight.WithLabelValues(method).Inc()
	}
}

// GRPCInflightDec decrements platform_grpc_server_inflight_requests for method.
func GRPCInflightDec(method string) {
	if GrpcInflight != nil {
		GrpcInflight.WithLabelValues(method).Dec()
	}
}

// RecordGRPCPanic increments platform_grpc_panics_total for method.
func RecordGRPCPanic(method string) {
	if GrpcPanicTotal != nil {
		GrpcPanicTotal.WithLabelValues(method).Inc()
	}
}

// RecordGRPCRateLimited increments platform_grpc_rate_limited_requests_total.
func RecordGRPCRateLimited(method string) {
	if GrpcRateLimitedRequestsTotal != nil {
		GrpcRateLimitedRequestsTotal.WithLabelValues(method).Inc()
	}
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
