package otel_test

import (
	"context"
	"testing"

	gootel "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
)

func TestInit_InvalidEndpointStillSetsTracerProvider(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	shutdown := otel.Init("test-service", "1.0.0-test", "")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown func")
	}

	tp, ok := gootel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("tracer provider type = %T", gootel.GetTracerProvider())
	}

	_, span := tp.Tracer("test").Start(context.Background(), "ping")
	defer span.End()

	traceID := span.SpanContext().TraceID().String()
	if traceID == "00000000000000000000000000000000" {
		t.Fatalf("trace_id = %q, want non-zero OpenTelemetry trace ID", traceID)
	}
}

func TestEnsureInit_SkipsWhenProviderAlreadyInstalled(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	otel.Init("first", "1.0.0", "")
	first := gootel.GetTracerProvider()

	otel.EnsureInit("second", "2.0.0", "")
	if gootel.GetTracerProvider() != first {
		t.Fatal("EnsureInit replaced an existing SDK TracerProvider")
	}
}

func TestInit_ValidConfigSetsTracerProvider(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	shutdown := otel.Init("test-service", "1.0.0-test", "localhost:4317")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown func")
	}
	if gootel.GetTracerProvider() == nil {
		t.Fatal("expected tracer provider to be set")
	}
}

func TestInitTelemetry_WithBaggage(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	shutdown := otel.InitTelemetry("baggage-svc", "1.0.0", otel.TelemetryConfig{
		Endpoint:      "",
		Insecure:      true,
		SampleRatio:   1.0,
		EnableBaggage: true,
	})
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown func")
	}
	if _, ok := gootel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("tracer provider type = %T, want *sdktrace.TracerProvider", gootel.GetTracerProvider())
	}
}
