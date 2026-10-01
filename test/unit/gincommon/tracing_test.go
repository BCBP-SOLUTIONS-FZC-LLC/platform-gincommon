package gincommon_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return sr
}

func TestNewTracer_UsesInstalledProvider(t *testing.T) {
	sr := installRecorder(t)
	ctx, span := gincommon.NewTracer("svc-scope").Start(context.Background(), "op")
	if gincommon.SpanTraceID(ctx) == "" {
		t.Fatal("SpanTraceID empty for a recording span")
	}
	span.End()
	ended := sr.Ended()
	if len(ended) != 1 || ended[0].Name() != "op" || ended[0].InstrumentationScope().Name != "svc-scope" {
		t.Fatalf("unexpected spans: %+v", ended)
	}
}

func TestSpanTraceID_NoSpan(t *testing.T) {
	if id := gincommon.SpanTraceID(context.Background()); id != "" {
		t.Fatalf("SpanTraceID = %q, want empty", id)
	}
}

func TestRecordSpanError_MarksSpanFailed(t *testing.T) {
	sr := installRecorder(t)
	ctx, span := gincommon.NewTracer("svc").Start(context.Background(), "op")
	gincommon.RecordSpanError(ctx, errors.New("boom"), "internal_server_error")
	span.End()
	got := sr.Ended()[0]
	if got.Status().Code != codes.Error || got.Status().Description != "internal_server_error" {
		t.Fatalf("status = %+v", got.Status())
	}
	if len(got.Events()) == 0 || got.Events()[0].Name != "exception" {
		t.Fatalf("expected a recorded exception event, got %+v", got.Events())
	}
	// No span in context: must not panic.
	gincommon.RecordSpanError(context.Background(), errors.New("x"), "y")
}

func TestInjectTraceContext(t *testing.T) {
	installRecorder(t)
	ctx, span := gincommon.NewTracer("svc").Start(context.Background(), "op")
	defer span.End()
	h := http.Header{}
	gincommon.InjectTraceContext(ctx, h)
	if h.Get("traceparent") == "" {
		t.Fatal("traceparent not injected")
	}
	gincommon.InjectTraceContext(ctx, nil) // nil header: no-op, no panic
}

func TestSpanTracer_StartSpan(t *testing.T) {
	sr := installRecorder(t)
	for _, scope := range []string{"db", ""} {
		ctx, end := gincommon.NewSpanTracer(scope).StartSpan(context.Background(), "db.query")
		if ctx == nil {
			t.Fatal("nil context")
		}
		end()
	}
	ended := sr.Ended()
	if len(ended) != 2 || ended[1].InstrumentationScope().Name != "platform-gincommon" {
		t.Fatalf("unexpected spans: %+v", ended)
	}
}

func TestNoopTracer(t *testing.T) {
	ctx, span := gincommon.NoopTracer().Start(context.Background(), "op")
	span.End()
	if gincommon.SpanTraceID(ctx) != "" {
		t.Fatal("noop tracer must not produce a trace id")
	}
}
