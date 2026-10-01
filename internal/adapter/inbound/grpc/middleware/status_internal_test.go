package middleware

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestStatusCodeName_AllCanonical(t *testing.T) {
	want := map[codes.Code]string{
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
	if len(want) != 17 {
		t.Fatalf("table has %d codes, want 17", len(want))
	}
	for c := codes.Code(0); c <= codes.Unauthenticated; c++ {
		if got := statusCodeName(c); got != want[c] {
			t.Errorf("statusCodeName(%d) = %q, want %q", c, got, want[c])
		}
	}
	for c, w := range map[codes.Code]string{17: "17", 99: "99", 1 << 31: "2147483648"} {
		if got := statusCodeName(c); got != w {
			t.Errorf("statusCodeName(%d) = %q, want %q", c, got, w)
		}
	}
}

func TestIsHealthMethod_ExactMatchOnly(t *testing.T) {
	for _, m := range []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/Watch",
		"/grpc.health.v1.Health/List",
	} {
		if !isHealthMethod(m) {
			t.Errorf("isHealthMethod(%q) = false, want true", m)
		}
	}
	for _, m := range []string{
		"", "/grpc.health.v1.Health/", "/grpc.health.v1.Health/CheckX",
		"/grpc.health.v1.HealthX/Check", "/grpc.health.v1.Health/x/a.B/C",
		"/grpc.health.v1.Health/Check/", "grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/check", "//grpc.health.v1.Health/Check",
	} {
		if isHealthMethod(m) {
			t.Errorf("isHealthMethod(%q) = true, want false", m)
		}
	}
}
