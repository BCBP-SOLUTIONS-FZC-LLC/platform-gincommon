package gincommon

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracing API for code outside the HTTP middleware (services, repositories,
// outbound clients), so consuming services never import OpenTelemetry
// themselves. Everything here uses exactly what InitTracing / EnsureTracing /
// ObservabilityMiddlewares installed: the global TracerProvider and the W3C
// text-map propagator.

// Tracer starts spans. It is an alias of the OpenTelemetry tracer interface,
// so callers use Start / span.End without importing OpenTelemetry.
type Tracer = trace.Tracer

// Span is a span started by a Tracer.
type Span = trace.Span

// NewTracer returns a tracer for scope (typically the service name) from the
// TracerProvider installed by InitTracing. Before InitTracing runs it returns
// OpenTelemetry's default no-op tracer. The tracer follows later provider
// replacements (ObservabilityMiddlewares replacing the provisional
// unknown_service provider, a second InitTracing): its spans always go to the
// provider that is current when they start.
func NewTracer(scope string) Tracer {
	return otel.Tracer(scope)
}

// NoopTracer returns a tracer that records nothing, for tests and tools.
func NoopTracer() Tracer {
	return noop.NewTracerProvider().Tracer("noop")
}

// SpanTraceID returns the trace ID of the span carried by ctx, or "" when ctx
// has no valid span. It is the context.Context counterpart of
// TraceIDFromContext (which reads a *gin.Context), for correlating log lines
// written outside a handler.
func SpanTraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// RecordSpanError records err on the span carried by ctx and marks the span
// failed with description. It is a no-op when ctx has no recording span.
func RecordSpanError(ctx context.Context, err error, description string) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, description)
}

// InjectTraceContext writes the W3C traceparent (and baggage, when enabled)
// from ctx onto header, using the propagator InitTracing installed. It is the
// context.Context counterpart of PropagateHeaders, for outbound calls made
// without a *gin.Context (background work, consumers, service-layer clients).
// Identity and request-ID headers are not set.
func InjectTraceContext(ctx context.Context, header http.Header) {
	if header == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// SpanTracer adapts NewTracer to the StartSpan(ctx, name) hook shape used by
// other platform libraries (platform-pgcommon's Config.Tracer), so their spans
// export through the same TracerProvider as HTTP spans.
type SpanTracer struct {
	tracer Tracer
}

// NewSpanTracer returns a SpanTracer for scope. An empty scope falls back to
// "platform-gincommon".
func NewSpanTracer(scope string) *SpanTracer {
	if scope == "" {
		scope = "platform-gincommon"
	}
	return &SpanTracer{tracer: NewTracer(scope)}
}

// StartSpan starts a child span named name and returns the derived context
// and a function that ends the span.
func (s *SpanTracer) StartSpan(ctx context.Context, name string) (spanCtx context.Context, end func()) {
	spanCtx, span := s.tracer.Start(ctx, name)
	return spanCtx, func() { span.End() }
}
