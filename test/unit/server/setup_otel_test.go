package server_test

import (
	"testing"

	"go.opentelemetry.io/otel"

	otelsetup "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/server"
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
	// The global is the package's delegating provider; the SDK provider sits behind it.
	if !otelsetup.IsGlobalProvider(otel.GetTracerProvider()) {
		t.Fatalf("global tracer provider = %T, want the otel package's delegating provider", otel.GetTracerProvider())
	}
	if otelsetup.SDKProvider() == nil {
		t.Fatal("no SDK tracer provider installed behind the global")
	}
}
