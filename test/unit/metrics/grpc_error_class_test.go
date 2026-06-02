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
	}{
		{codes.OK, "success"},
		{codes.InvalidArgument, "client_error"},
		{codes.Unauthenticated, "client_error"},
		{codes.Internal, "server_error"},
		{codes.Unavailable, "server_error"},
	}

	for _, tc := range tests {
		ec := metrics.GRPCRequestOutcome(tc.code)
		if ec != tc.wantClass {
			t.Errorf("GRPCRequestOutcome(%v) = %q, want %q", tc.code, ec, tc.wantClass)
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
