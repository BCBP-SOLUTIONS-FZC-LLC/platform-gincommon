package middleware

import (
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcStatus converts a handler error into the status grpc-go actually sends to
// the client: a status error keeps its status; anything else goes through
// status.FromContextError, so a plain context.DeadlineExceeded / context.Canceled
// (or an error wrapping one) becomes DeadlineExceeded / Canceled rather than
// Unknown. A nil error is OK.
//
// metrics, logging and tracing all classify through this one helper so the three
// signals agree with each other and with the wire.
func grpcStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}
	st, ok := status.FromError(err)
	if !ok {
		st = status.FromContextError(err)
	}
	if st.Code() == codes.OK {
		// A non-nil error that claims OK is a handler bug; never report it as success.
		return status.New(codes.Unknown, err.Error())
	}
	return st
}

// grpcCode returns the gRPC status code that err produces on the wire (see grpcStatus).
func grpcCode(err error) codes.Code {
	return grpcStatus(err).Code()
}

// canonicalCodeNames are the canonical upper-snake gRPC status code names
// (grpc/grpc doc/statuscodes.md), as used by OpenTelemetry
// rpc.response.status_code. Note "CANCELLED" (double L): codes.Canceled.String()
// is "Canceled", so the names cannot be derived from the Go identifiers.
var canonicalCodeNames = [...]string{
	codes.OK:                 "OK",
	codes.Canceled:           "CANCELLED",
	codes.Unknown:            "UNKNOWN",
	codes.InvalidArgument:    "INVALID_ARGUMENT",
	codes.DeadlineExceeded:   "DEADLINE_EXCEEDED",
	codes.NotFound:           "NOT_FOUND",
	codes.AlreadyExists:      "ALREADY_EXISTS",
	codes.PermissionDenied:   "PERMISSION_DENIED",
	codes.ResourceExhausted:  "RESOURCE_EXHAUSTED",
	codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.Aborted:            "ABORTED",
	codes.OutOfRange:         "OUT_OF_RANGE",
	codes.Unimplemented:      "UNIMPLEMENTED",
	codes.Internal:           "INTERNAL",
	codes.Unavailable:        "UNAVAILABLE",
	codes.DataLoss:           "DATA_LOSS",
	codes.Unauthenticated:    "UNAUTHENTICATED",
}

// statusCodeName returns the OpenTelemetry rpc.response.status_code value for a
// gRPC code: the canonical upper-snake name ("OK", "CANCELLED",
// "DEADLINE_EXCEEDED"), or the decimal value for codes outside the defined range.
func statusCodeName(c codes.Code) string {
	if int(c) < len(canonicalCodeNames) {
		return canonicalCodeNames[c]
	}
	return strconv.FormatUint(uint64(c), 10)
}
