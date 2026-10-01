package otel

import (
	"context"
	"sync/atomic"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// delegatingProvider is the single TracerProvider this package installs as
// the OpenTelemetry global. It never changes identity: replacing the SDK
// provider (InitTelemetry, or EnsureInitTelemetry replacing a provisional
// one) swaps the inner *sdktrace.TracerProvider atomically, so tracers
// obtained earlier — gincommon.NewTracer / NewSpanTracer, a package-level
// otel.Tracer bound through OpenTelemetry's global delegate (which binds only
// once), instrumentation that captured the provider at construction — keep
// exporting through whichever SDK provider is current.
//
// Spans started before a swap belong to the old provider; they end on it,
// and if they end after it was shut down they are dropped. That only affects
// spans in flight at swap time, which normally happens once at startup.
type delegatingProvider struct {
	embedded.TracerProvider

	inner atomic.Pointer[sdktrace.TracerProvider]
}

// global is the package-owned delegating provider.
var global = &delegatingProvider{}

var noopTracer = nooptrace.NewTracerProvider().Tracer("")

// Tracer returns a tracer that resolves the current SDK provider on every
// Start. With no SDK provider installed (before init, after Shutdown) it
// behaves like the no-op tracer.
func (p *delegatingProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &delegatingTracer{p: p, name: name, opts: opts}
}

// SDKProvider returns the SDK TracerProvider currently behind the global
// provider installed by this package, or nil when none is active. Use it
// instead of asserting otel.GetTracerProvider() to *sdktrace.TracerProvider:
// the global is a delegating provider whose identity never changes.
func SDKProvider() *sdktrace.TracerProvider {
	return global.inner.Load()
}

// GlobalProvider returns the package-owned delegating TracerProvider (the
// value otel.GetTracerProvider() returns while this package's tracing is
// active).
func GlobalProvider() trace.TracerProvider { return global }

// IsGlobalProvider reports whether tp is this package's delegating provider.
func IsGlobalProvider(tp trace.TracerProvider) bool {
	p, ok := tp.(*delegatingProvider)
	return ok && p == global
}

// tracerGen caches the SDK tracer for one inner-provider generation.
type tracerGen struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
}

type delegatingTracer struct {
	embedded.Tracer

	p     *delegatingProvider
	name  string
	opts  []trace.TracerOption
	cache atomic.Pointer[tracerGen]
}

// Start starts the span on the SDK provider that is current at call time.
func (t *delegatingTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	tp := t.p.inner.Load()
	if tp == nil {
		return noopTracer.Start(ctx, spanName, opts...)
	}
	g := t.cache.Load()
	if g == nil || g.tp != tp {
		g = &tracerGen{tp: tp, tracer: tp.Tracer(t.name, t.opts...)}
		t.cache.Store(g)
	}
	return g.tracer.Start(ctx, spanName, opts...)
}
