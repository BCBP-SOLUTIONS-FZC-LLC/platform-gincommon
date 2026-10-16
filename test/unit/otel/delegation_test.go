package otel_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	gootel "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
)

func serviceName(s tracetest.SpanStub) string {
	if v, ok := s.Resource.Set().Value(semconv.ServiceNameKey); ok {
		return v.AsString()
	}
	return ""
}

func TestReplacement_EarlierTracerExportsToNewProvider(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	otel.InitTelemetry(otel.UnknownServiceName, "1", otel.TelemetryConfig{Provisional: true, SampleRatio: 1})
	early := gootel.Tracer("early") // taken before the replacement
	provisional := otel.SDKProvider()

	otel.EnsureInitTelemetry("orders", "1", otel.TelemetryConfig{SampleRatio: 1})
	tp := otel.SDKProvider()
	if tp == nil || tp == provisional {
		t.Fatal("provisional provider was not replaced")
	}
	if !otel.IsGlobalProvider(gootel.GetTracerProvider()) {
		t.Fatalf("global provider = %T, want the delegating provider", gootel.GetTracerProvider())
	}
	exp := tracetest.NewInMemoryExporter()
	tp.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp))

	_, span := early.Start(context.Background(), "after-swap")
	if !span.SpanContext().IsValid() || !span.IsRecording() {
		t.Fatal("span from an earlier tracer is not recording after replacement")
	}
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "after-swap" {
		t.Fatalf("exported spans = %v, want one after-swap span", spans)
	}
	if got := serviceName(spans[0]); got != "orders" {
		t.Fatalf("service.name = %q, want orders", got)
	}
}

func TestShutdown_EarlierTracerBecomesNoop(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	otel.InitTelemetry("svc", "1", otel.TelemetryConfig{SampleRatio: 1})
	tr := otel.GlobalProvider().Tracer("x")
	otel.Shutdown()
	if otel.SDKProvider() != nil {
		t.Fatal("SDKProvider() non-nil after Shutdown")
	}
	_, span := tr.Start(context.Background(), "op")
	defer span.End()
	if span.IsRecording() {
		t.Fatal("span recording after Shutdown")
	}
	// Re-init: the same tracer records again.
	otel.InitTelemetry("svc2", "1", otel.TelemetryConfig{SampleRatio: 1})
	_, span2 := tr.Start(context.Background(), "op2")
	defer span2.End()
	if !span2.IsRecording() {
		t.Fatal("span not recording after re-init")
	}
}

func TestReplacement_ConcurrentStartsRace(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	otel.InitTelemetry("a", "1", otel.TelemetryConfig{SampleRatio: 1})
	tr := gootel.Tracer("race")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, s := tr.Start(context.Background(), "op")
					s.End()
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		otel.InitTelemetry("b", "1", otel.TelemetryConfig{SampleRatio: 1})
		otel.EnsureInitTelemetry("c", "1", otel.TelemetryConfig{Logger: &recLogger{}})
	}
	close(stop)
	wg.Wait()
}

func TestEnsureInitTelemetry_UpgradesNilErrorLogger(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	captureStderrLog(t)

	otel.InitTelemetry("svc", "1", otel.TelemetryConfig{}) // nil logger (InitTracing path)
	lg := &recLogger{}
	otel.EnsureInitTelemetry("svc", "1", otel.TelemetryConfig{Logger: lg}) // keeps provider
	gootel.Handle(errors.New("export failed"))
	if !lg.has("warn:otel_error") {
		t.Fatalf("SDK error not routed to the later logger, got %v", lg.entries)
	}

	// A configured logger is not replaced by a later one.
	other := &recLogger{}
	otel.EnsureInitTelemetry("svc", "1", otel.TelemetryConfig{Logger: other})
	gootel.Handle(errors.New("again"))
	if other.has("warn:otel_error") {
		t.Fatal("configured SDK error logger was replaced")
	}
}

func TestEnsureInitTelemetry_KeepsForeignSDKProvider(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	foreign := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = foreign.Shutdown(context.Background()) })
	gootel.SetTracerProvider(foreign)

	otel.EnsureInitTelemetry("svc", "1", otel.TelemetryConfig{Logger: &recLogger{}})
	if gootel.GetTracerProvider() != foreign {
		t.Fatalf("global provider = %T, want the consumer's SDK provider kept", gootel.GetTracerProvider())
	}
	if otel.SDKProvider() != nil {
		t.Fatal("package installed a provider over a consumer's SDK provider")
	}
}
