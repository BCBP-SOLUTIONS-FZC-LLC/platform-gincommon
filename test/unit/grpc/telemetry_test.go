package grpc_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
)

// TestObservabilityInterceptors_WithTracingOverrides exercises resolveGRPCTelemetry's
// override branches: every non-nil field of cfg.Tracing replaces the env-derived default.
func TestObservabilityInterceptors_WithTracingOverrides(t *testing.T) {
	insecure := true
	ratio := 0.5
	baggage := true

	cfg := grpccommon.Config{
		ServiceName:  "grpc-tracing-overrides",
		BuildVersion: "test",
		Tracing: &grpccommon.TracingOptions{
			Endpoint:      "localhost:4317",
			Insecure:      &insecure,
			SampleRatio:   &ratio,
			EnableBaggage: &baggage,
		},
	}

	if ics := grpccommon.ObservabilityUnaryInterceptors(cfg); len(ics) == 0 {
		t.Fatal("expected unary interceptors")
	}
	if ics := grpccommon.ObservabilityStreamInterceptors(cfg); len(ics) == 0 {
		t.Fatal("expected stream interceptors")
	}
}

// TestObservabilityInterceptors_EmptyTracingOptions exercises resolveGRPCTelemetry's
// no-override path where cfg.Tracing is non-nil but all fields are zero values.
func TestObservabilityInterceptors_EmptyTracingOptions(t *testing.T) {
	cfg := grpccommon.Config{
		ServiceName:  "grpc-tracing-empty",
		BuildVersion: "test",
		Tracing:      &grpccommon.TracingOptions{},
	}

	if ics := grpccommon.ObservabilityUnaryInterceptors(cfg); len(ics) == 0 {
		t.Fatal("expected unary interceptors")
	}
}
