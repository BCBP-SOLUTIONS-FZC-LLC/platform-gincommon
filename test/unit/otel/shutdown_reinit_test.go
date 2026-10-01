package otel_test

import (
	"context"
	"testing"

	gootel "go.opentelemetry.io/otel"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
)

// A provider captured between Shutdown and the next Init (e.g. an otelgrpc
// handler or HTTP client built during a server restart) must come back to
// life after re-init instead of staying a dead no-op provider.
func TestShutdown_CapturedProviderLiveAfterReinit(t *testing.T) {
	otel.Init("shutdown-reinit-a", "test", "")
	otel.Shutdown()

	captured := gootel.GetTracerProvider()
	if !otel.IsGlobalProvider(captured) {
		t.Fatalf("global provider after Shutdown = %T, want the delegating provider", captured)
	}
	if _, span := captured.Tracer("t").Start(context.Background(), "before"); span.SpanContext().IsValid() {
		t.Fatal("span after Shutdown has a valid context, want a no-op span")
	}

	otel.Init("shutdown-reinit-b", "test", "")
	t.Cleanup(otel.Shutdown)
	_, span := captured.Tracer("t").Start(context.Background(), "after")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("span from provider captured after Shutdown is a no-op after re-init")
	}
}
