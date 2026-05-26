package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"google.golang.org/grpc/codes"
)

func TestGRPCRequestOutcome(t *testing.T) {
	tests := []struct {
		code      codes.Code
		wantClass string
		wantError string
	}{
		{codes.OK, "success", "false"},
		{codes.InvalidArgument, "client_error", "true"},
		{codes.Unauthenticated, "client_error", "true"},
		{codes.Internal, "server_error", "true"},
		{codes.Unavailable, "server_error", "true"},
	}

	for _, tc := range tests {
		ec, isErr := metrics.GRPCRequestOutcome(tc.code)
		if ec != tc.wantClass || isErr != tc.wantError {
			t.Errorf("GRPCRequestOutcome(%v) = (%q, %q), want (%q, %q)",
				tc.code, ec, isErr, tc.wantClass, tc.wantError)
		}
	}
}

func TestGRPCStatusClass(t *testing.T) {
	if metrics.GRPCStatusClass(codes.OK) != "2xx" {
		t.Fatalf("OK = %q", metrics.GRPCStatusClass(codes.OK))
	}
	if metrics.GRPCStatusClass(codes.InvalidArgument) != "4xx" {
		t.Fatalf("InvalidArgument = %q", metrics.GRPCStatusClass(codes.InvalidArgument))
	}
	if metrics.GRPCStatusClass(codes.Internal) != "5xx" {
		t.Fatalf("Internal = %q", metrics.GRPCStatusClass(codes.Internal))
	}
}

func TestGRPCErrorClassAndIsError(t *testing.T) {
	if metrics.GRPCErrorClass(codes.OK) != "success" {
		t.Fatalf("GRPCErrorClass(OK) = %q", metrics.GRPCErrorClass(codes.OK))
	}
	if metrics.GRPCIsError(codes.OK) != "false" {
		t.Fatalf("GRPCIsError(OK) = %q", metrics.GRPCIsError(codes.OK))
	}
}
