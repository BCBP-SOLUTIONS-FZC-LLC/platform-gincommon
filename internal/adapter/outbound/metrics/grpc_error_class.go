package metrics

import "google.golang.org/grpc/codes"

// GRPCErrorClass returns the error_class outcome label for a gRPC status code.
func GRPCErrorClass(code codes.Code) string {
	ec, _ := GRPCRequestOutcome(code)
	return ec
}

// GRPCIsError returns the is_error outcome label for a gRPC status code.
func GRPCIsError(code codes.Code) string {
	_, isErr := GRPCRequestOutcome(code)
	return isErr
}
