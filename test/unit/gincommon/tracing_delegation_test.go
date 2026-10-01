package gincommon_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"testing"

	gootel "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

// A tracer handed out after InitTracingFromEnv (e.g. platform-pgcommon's
// Config.Tracer) must keep exporting after ObservabilityMiddlewares replaces
// the provisional unknown_service provider.
func TestNewTracer_SurvivesProvisionalReplacement(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("APP_NAME", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "1")
	prev := log.Writer()
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(prev) })

	shutdown := gincommon.InitTracingFromEnv()
	t.Cleanup(shutdown)
	tracer := gincommon.NewTracer("repo")
	spanTracer := gincommon.NewSpanTracer("pgcommon")
	provisional := otel.SDKProvider()

	gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "orders"})
	tp := otel.SDKProvider()
	if tp == nil || tp == provisional {
		t.Fatal("provisional provider was not replaced")
	}
	exp := tracetest.NewInMemoryExporter()
	tp.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp))

	_, span := tracer.Start(context.Background(), "query")
	span.End()
	_, end := spanTracer.StartSpan(context.Background(), "pg.exec")
	end()

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("exported %d spans, want 2: %v", len(spans), spans)
	}
	for _, s := range spans {
		v, _ := s.Resource.Set().Value(semconv.ServiceNameKey)
		if v.AsString() != "orders" {
			t.Fatalf("span %q service.name = %q, want orders", s.Name, v.AsString())
		}
	}
}

// InitTracing installs the SDK error handler without a logger; a later
// ObservabilityMiddlewares that keeps the provider routes SDK errors to
// Config.Logger.
func TestObservabilityMiddlewares_UpgradesTracingErrorLogger(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	prev := log.Writer()
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(prev) })

	shutdown := gincommon.InitTracing("orders", "1", "")
	t.Cleanup(shutdown)
	before := otel.SDKProvider()

	lg := &fixtures.MockLogger{}
	gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "orders", Logger: lg})
	if otel.SDKProvider() != before {
		t.Fatal("ObservabilityMiddlewares replaced a configured provider")
	}
	gootel.Handle(errors.New("export failed"))
	if lg.Msg != "otel_error" || lg.Level != "warn" {
		t.Fatalf("last log = %s %q, want warn otel_error", lg.Level, lg.Msg)
	}
}
