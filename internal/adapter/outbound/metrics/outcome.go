package metrics

import "google.golang.org/grpc/codes"

// HTTPStatusClass maps an HTTP status code to 2xx, 4xx, or 5xx (3xx counts as 2xx).
func HTTPStatusClass(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 400:
		return "2xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// GRPCStatusClass maps a gRPC code to 2xx, 4xx, or 5xx.
func GRPCStatusClass(code codes.Code) string {
	switch code {
	case codes.OK:
		return "2xx"
	case codes.Canceled, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition,
		codes.OutOfRange, codes.Aborted, codes.DeadlineExceeded, codes.ResourceExhausted:
		return "4xx"
	default:
		return "5xx"
	}
}

// RequestOutcome maps an HTTP status code to error_class: "success", "client_error", or
// "server_error". 3xx is classified as "success" (same bucket as 2xx).
func RequestOutcome(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 400:
		return "success"
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	default:
		return "server_error"
	}
}

// GRPCRequestOutcome maps a gRPC status code to error_class: "success", "client_error",
// or "server_error".
func GRPCRequestOutcome(code codes.Code) string {
	switch code {
	case codes.OK:
		return "success"
	case codes.Canceled, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition,
		codes.OutOfRange, codes.Aborted, codes.DeadlineExceeded, codes.ResourceExhausted:
		return "client_error"
	default:
		return "server_error"
	}
}
