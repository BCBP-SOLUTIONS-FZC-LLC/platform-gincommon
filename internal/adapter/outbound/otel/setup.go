package otel

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
)

// UnknownServiceName is the OpenTelemetry-spec service.name used when no
// service name is configured.
const UnknownServiceName = "unknown_service"

// shutdownTimeout bounds flushing and shutting down a replaced or stopped
// SDK provider.
const shutdownTimeout = 5 * time.Second

// generation is one installed SDK provider.
type generation struct {
	tp          *sdktrace.TracerProvider
	shutdown    func()
	provisional bool
}

var (
	// shutdownMu serialises install/replace/stop decisions. It is never held
	// while a provider flushes: the replaced provider is shut down after the
	// lock is released.
	shutdownMu sync.Mutex
	active     *generation
)

// Init initializes OpenTelemetry tracing and always registers a TracerProvider so
// in-process spans get valid trace IDs even when the OTLP exporter cannot be created.
func Init(service, version, endpoint string) func() {
	return InitTelemetry(service, version, TelemetryConfig{Endpoint: endpoint})
}

// InitTelemetry initializes tracing with explicit export and sampling settings,
// replacing (and flushing) any provider installed by this package. Tracers
// obtained before the replacement switch to the new provider (see
// delegatingProvider).
func InitTelemetry(service, version string, cfg TelemetryConfig) func() {
	shutdownMu.Lock()
	stopOld := install(service, version, cfg)
	shutdownMu.Unlock()
	stopOld()

	return Shutdown
}

// Shutdown stops the active tracer provider, if any, flushing its buffered
// spans (bounded by a 5s timeout). The global provider is left in place and
// records nothing until the next Init.
func Shutdown() {
	shutdownMu.Lock()
	old := active
	active = nil
	// Keep the delegating provider installed with no inner provider (it then
	// behaves as a no-op): a provider captured via otel.GetTracerProvider()
	// between Shutdown and a later Init stays live after re-init, and a
	// consumer's own provider is never replaced.
	global.inner.Store(nil)
	shutdownMu.Unlock()
	if old != nil {
		old.shutdown()
	}
}

// EnsureInit installs a TracerProvider when the global provider is not the SDK implementation.
func EnsureInit(service, version, endpoint string) {
	EnsureInitTelemetry(service, version, TelemetryConfig{Endpoint: endpoint})
}

// EnsureInitTelemetry is like EnsureInit with full telemetry settings. A
// provider that is already installed is kept — this package's, or a
// *sdktrace.TracerProvider a consumer registered itself — unless this package
// installed it provisionally (no service name was configured) and this call
// is not provisional, in which case it is replaced so the configured service
// name wins. When the provider is kept and cfg.Logger is non-nil, it becomes
// the OpenTelemetry SDK error logger if none was configured yet.
//
// The check-and-init is performed under shutdownMu to prevent a TOCTOU race
// where two goroutines both see a noop provider and both install a new SDK provider,
// with the second clobbering the first and losing its buffered spans.
func EnsureInitTelemetry(service, version string, cfg TelemetryConfig) {
	shutdownMu.Lock()
	cur := otel.GetTracerProvider()
	switch {
	case IsGlobalProvider(cur) && active != nil:
		if !active.provisional || cfg.Provisional {
			upgradeErrorLogger(cfg.Logger)
			shutdownMu.Unlock()
			return
		}
	default:
		if _, ok := cur.(*sdktrace.TracerProvider); ok {
			upgradeErrorLogger(cfg.Logger)
			shutdownMu.Unlock()
			return
		}
	}
	stopOld := install(service, version, cfg)
	shutdownMu.Unlock()
	stopOld()
}

// install builds a provider, swaps it in behind the global delegating
// provider and returns a function that flushes and shuts down the replaced
// provider (a no-op when there was none). Callers hold shutdownMu and call
// the returned function after releasing it.
func install(service, version string, cfg TelemetryConfig) (stopOld func()) {
	tp, stop := initTracing(service, version, cfg)
	old := active
	active = &generation{tp: tp, shutdown: stop, provisional: cfg.Provisional}
	global.inner.Store(tp)
	if !IsGlobalProvider(otel.GetTracerProvider()) {
		otel.SetTracerProvider(global)
	}
	if old == nil {
		return func() {}
	}
	return old.shutdown
}

// Resource builds the trace resource: the SDK defaults plus service.name,
// service.version, and — when known — service.namespace (the observability
// domain) and deployment.environment.name.
func Resource(service, version string, cfg TelemetryConfig) *resource.Resource {
	if strings.TrimSpace(service) == "" {
		service = UnknownServiceName
	}
	attrs := []attribute.KeyValue{
		semconv.ServiceName(service),
		semconv.ServiceVersion(version),
	}
	if cfg.Domain != "" {
		attrs = append(attrs, semconv.ServiceNamespace(cfg.Domain))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(cfg.Environment))
	}
	// Schemaless so the merge with resource.Default() cannot fail on a
	// schema-URL mismatch between this semconv version and the SDK's.
	svcRes := resource.NewSchemaless(attrs...)
	res, err := resource.Merge(resource.Default(), svcRes)
	if err != nil {
		warn(cfg.Logger, "otel_resource_merge_failed", map[string]interface{}{"error": err.Error()})
		return svcRes
	}
	return res
}

func initTracing(service, version string, cfg TelemetryConfig) (*sdktrace.TracerProvider, func()) {
	ctx := context.Background()
	installErrorHandler(cfg.Logger)

	if strings.TrimSpace(service) == "" {
		service = UnknownServiceName
	}
	res := Resource(service, version, cfg)
	sampler := Sampler(cfg.SampleRatio, cfg.IgnoreRemoteParentSampled)

	exporterState := "disabled (no endpoint)"
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}
	if cfg.Endpoint != "" {
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, expErr := otlptracegrpc.New(ctx, opts...)
		if expErr != nil {
			exporterState = "failed (in-process only)"
			warn(cfg.Logger, "otel_exporter_init_failed", map[string]interface{}{
				"error":    expErr.Error(),
				"endpoint": cfg.Endpoint,
				"fallback": "in-process TracerProvider; spans are not exported",
			})
		} else {
			exporterState = "otlp-grpc"
			tpOpts = append(tpOpts, sdktrace.WithBatcher(exporter))
		}
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)

	propagators := []propagation.TextMapPropagator{propagation.TraceContext{}}
	if cfg.EnableBaggage {
		propagators = append(propagators, propagation.Baggage{})
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagators...))

	info(cfg.Logger, "otel_tracing_initialized", map[string]interface{}{
		"service":                      service,
		"version":                      version,
		"domain":                       cfg.Domain,
		"environment":                  cfg.Environment,
		"endpoint":                     cfg.Endpoint,
		"insecure":                     cfg.Insecure,
		"exporter":                     exporterState,
		"sample_ratio":                 cfg.SampleRatio,
		"ignore_remote_parent_sampled": cfg.IgnoreRemoteParentSampled,
		"baggage":                      cfg.EnableBaggage,
		"provisional":                  cfg.Provisional,
	})

	return tp, func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		if err := tp.ForceFlush(shutdownCtx); err != nil {
			warn(cfg.Logger, "otel_shutdown_flush_failed", map[string]interface{}{"error": err.Error()})
		}
		if err := tp.Shutdown(shutdownCtx); err != nil {
			warn(cfg.Logger, "otel_shutdown_flush_failed", map[string]interface{}{"error": err.Error()})
		}
	}
}

// Sampler returns the trace sampler for ratio: always ParentBased, so a
// sampled or unsampled parent decision is respected, with the root decision
// AlwaysSample (ratio >= 1), NeverSample (ratio <= 0) or TraceIDRatioBased.
// With ignoreRemoteParentSampled, a sampled remote parent is re-sampled with
// the root sampler rather than always sampled (an unsampled remote parent is
// still never sampled), so external callers cannot force sampling.
func Sampler(ratio float64, ignoreRemoteParentSampled bool) sdktrace.Sampler {
	var root sdktrace.Sampler
	switch {
	case ratio >= 1.0:
		root = sdktrace.AlwaysSample()
	case ratio <= 0:
		root = sdktrace.NeverSample()
	default:
		root = sdktrace.TraceIDRatioBased(ratio)
	}
	var opts []sdktrace.ParentBasedSamplerOption
	if ignoreRemoteParentSampled {
		opts = append(opts, sdktrace.WithRemoteParentSampled(root))
	}
	return sdktrace.ParentBased(root, opts...)
}

// errLoggerRef holds the logger OpenTelemetry SDK errors are routed to.
type errLoggerRef struct{ logger port.Logger }

// errLogger is nil until installErrorHandler runs; it is read on every SDK
// error and written under shutdownMu.
var errLogger atomic.Pointer[errLoggerRef]

// installErrorHandler routes OpenTelemetry SDK errors (export failures,
// dropped spans, ...) to logger, or stderr when logger is nil.
func installErrorHandler(logger port.Logger) {
	errLogger.Store(&errLoggerRef{logger: logger})
	otel.SetErrorHandler(otel.ErrorHandlerFunc(handleOtelError))
}

// upgradeErrorLogger sets logger as the SDK error logger when this package's
// handler is installed without one (InitTracing / EnsureTracing pass nil, a
// later ObservabilityMiddlewares carries Config.Logger). Callers hold
// shutdownMu.
func upgradeErrorLogger(logger port.Logger) {
	if logger == nil {
		return
	}
	if cur := errLogger.Load(); cur != nil && cur.logger == nil {
		errLogger.Store(&errLoggerRef{logger: logger})
	}
}

func handleOtelError(err error) {
	if err == nil {
		return
	}
	var logger port.Logger
	if ref := errLogger.Load(); ref != nil {
		logger = ref.logger
	}
	warn(logger, "otel_error", map[string]interface{}{"error": err.Error()})
}

func warn(logger port.Logger, msg string, fields map[string]interface{}) {
	if logger != nil {
		logger.Warn(msg, fields)
		return
	}
	log.Printf("otel: warn %s %s", msg, formatFields(fields))
}

func info(logger port.Logger, msg string, fields map[string]interface{}) {
	if logger != nil {
		logger.Info(msg, fields)
		return
	}
	log.Printf("otel: %s %s", msg, formatFields(fields))
}

// formatFields renders fields as sorted key=value pairs for stderr output.
func formatFields(fields map[string]interface{}) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}
	return strings.Join(parts, " ")
}
