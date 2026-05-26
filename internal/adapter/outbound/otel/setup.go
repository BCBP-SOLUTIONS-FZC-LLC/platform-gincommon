package otel

import (
	"context"
	"log"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

var (
	shutdownMu     sync.Mutex
	activeShutdown func()
)

// Init initializes OpenTelemetry tracing and always registers a TracerProvider so
// in-process spans get valid trace IDs even when the OTLP exporter cannot be created.
func Init(service, version, endpoint string) func() {
	return InitTelemetry(service, version, TelemetryConfig{Endpoint: endpoint})
}

// InitTelemetry initializes tracing with explicit export and sampling settings.
func InitTelemetry(service, version string, cfg TelemetryConfig) func() {
	shutdownMu.Lock()
	if activeShutdown != nil {
		activeShutdown()
		activeShutdown = nil
	}
	activeShutdown = initTracing(service, version, cfg)
	shutdownMu.Unlock()

	return Shutdown
}

// Shutdown stops the active tracer provider, if any.
func Shutdown() {
	shutdownMu.Lock()
	if activeShutdown != nil {
		activeShutdown()
		activeShutdown = nil
	}
	otel.SetTracerProvider(nooptrace.NewTracerProvider())
	shutdownMu.Unlock()
}

// EnsureInit installs a TracerProvider when the global provider is not the SDK implementation.
func EnsureInit(service, version, endpoint string) {
	EnsureInitTelemetry(service, version, TelemetryConfig{Endpoint: endpoint})
}

// EnsureInitTelemetry is like EnsureInit with full telemetry settings.
func EnsureInitTelemetry(service, version string, cfg TelemetryConfig) {
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		return
	}
	InitTelemetry(service, version, cfg)
}

func initTracing(service, version string, cfg TelemetryConfig) func() {
	ctx := context.Background()

	svcRes, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(service),
			semconv.ServiceVersion(version),
		),
	)
	res, err := resource.Merge(resource.Default(), svcRes)
	if err != nil {
		log.Printf("otel: resource merge failed: %v; service.name/version may be missing from spans", err)
		res = svcRes
	}

	sampler := traceSampler(cfg.SampleRatio)

	var tp *sdktrace.TracerProvider
	if cfg.Endpoint != "" {
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if exporter, expErr := otlptracegrpc.New(ctx, opts...); expErr == nil {
			tp = sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exporter),
				sdktrace.WithResource(res),
				sdktrace.WithSampler(sampler),
			)
		}
	}
	if tp == nil {
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
		)
	}

	otel.SetTracerProvider(tp)
	propagators := []propagation.TextMapPropagator{propagation.TraceContext{}}
	if cfg.EnableBaggage {
		propagators = append(propagators, propagation.Baggage{})
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagators...))

	return func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			log.Printf("otel: shutdown flush failed: %v", err)
		}
	}
}

func traceSampler(ratio float64) sdktrace.Sampler {
	if ratio >= 1.0 {
		return sdktrace.AlwaysSample()
	}
	if ratio <= 0 {
		return sdktrace.NeverSample()
	}
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}
