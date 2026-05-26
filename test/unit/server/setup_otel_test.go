package server_test

import (
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
)

func TestSetup_InitializesTracerProviderWithoutExporter(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "setup-otel-test")
	t.Setenv("BUILD_VERSION", "test")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	router, shutdown, err := server.Setup()
	if err != nil {
		t.Fatalf("Setup() err = %v", err)
	}
	defer shutdown()

	if router == nil {
		t.Fatal("expected router")
	}
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("tracer provider type = %T, want *sdktrace.TracerProvider", otel.GetTracerProvider())
	}
}
