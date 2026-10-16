package gincommon

import (
	"errors"
	"log"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
)

// logSyncer is the optional Sync() error capability exposed by buffered loggers
// (e.g. the Zap-backed port.Logger). Shutdown flushes it if implemented.
type logSyncer interface {
	Sync() error
}

// Shutdown performs graceful telemetry cleanup for an HTTP service. Call it from main()
// during shutdown (e.g. on SIGTERM) AFTER the HTTP server has stopped accepting requests:
// it stops the active OpenTelemetry TracerProvider — flushing any buffered spans to the
// collector — and, when logger implements Sync() error (the Zap logger does), flushes
// buffered log entries. Passing a nil logger flushes tracing only.
//
// It returns the logger's Sync error (if any) so callers can surface flush failures;
// the tracing provider is always flushed regardless of the logger result.
func Shutdown(logger port.Logger) error {
	otel.Shutdown()
	if s, ok := logger.(logSyncer); ok {
		return s.Sync()
	}
	return nil
}

// ErrServiceNameRequired is returned by InitTracingWithConfig when
// TracingConfig.ServiceName is empty.
var ErrServiceNameRequired = errors.New("gincommon: TracingConfig.ServiceName is required")

// TracingConfig configures InitTracingWithConfig. It carries the same identity
// fields as Config, so tracing started before ObservabilityMiddlewares exports
// spans under the same service.name, service.namespace (Domain) and
// deployment.environment.name (Environment).
type TracingConfig struct {
	// ServiceName is the OTel service.name. Required.
	ServiceName string
	// BuildVersion is service.version; empty uses BUILD_VERSION.
	BuildVersion string
	// Domain is the observability domain, exported as service.namespace;
	// empty uses OBSERVABILITY_DOMAIN.
	Domain string
	// Environment is exported as deployment.environment.name; empty uses
	// APP_ENV, then ENVIRONMENT.
	Environment string
	// Logger receives tracing init warnings, the effective configuration and
	// OpenTelemetry SDK errors; nil writes them to stderr.
	Logger port.Logger
	// Tracing overrides OTEL_* env when non-nil fields are set.
	Tracing *TracingOptions
}

// InitTracingWithConfig installs the OTLP/gRPC TracerProvider (replacing one
// installed earlier) and returns its shutdown function. Prefer it over
// InitTracing / InitTracingFromEnv when tracing must start before
// ObservabilityMiddlewares (background jobs, gRPC servers, startup work).
func InitTracingWithConfig(cfg TracingConfig) (func(), error) {
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return nil, ErrServiceNameRequired
	}
	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	tc := telemetryConfig(cfg.Domain, cfg.Environment, cfg.Logger, cfg.Tracing)
	return otel.InitTelemetry(cfg.ServiceName, version, tc), nil
}

// InitTracing configures OTLP/gRPC export from the OTEL_* environment and
// returns a shutdown function. Domain and environment resource attributes come
// from OBSERVABILITY_DOMAIN and APP_ENV/ENVIRONMENT. An empty service is
// exported as "unknown_service" with a stderr warning, and is replaced by the
// first ObservabilityMiddlewares / interceptor stack with a ServiceName.
func InitTracing(service, version, endpoint string) func() {
	tc := telemetryConfig("", "", nil, nil)
	tc.Endpoint = endpoint // explicit: "" disables export
	service, tc.Provisional = resolveServiceName(service)
	return otel.InitTelemetry(service, version, tc)
}

// EnsureTracing installs a TracerProvider when none is registered yet, with the
// same configuration as InitTracing.
func EnsureTracing(service, version, endpoint string) {
	tc := telemetryConfig("", "", nil, nil)
	tc.Endpoint = endpoint
	service, tc.Provisional = resolveServiceName(service)
	otel.EnsureInitTelemetry(service, version, tc)
}

// InitTracingFromEnv calls InitTracing with OTEL_SERVICE_NAME (then APP_NAME),
// BUILD_VERSION and OTEL_EXPORTER_OTLP_ENDPOINT. There is no built-in service
// name: when neither variable is set it warns on stderr, exports as
// "unknown_service", and the first ObservabilityMiddlewares (or gRPC
// interceptor stack) replaces the provider with its Config.ServiceName.
func InitTracingFromEnv() func() {
	return InitTracing(
		config.OtelServiceNameEnv(),
		config.BuildVersion(),
		config.OtelExporterEndpoint(),
	)
}

// resolveServiceName maps an empty service to the OTel-spec placeholder and
// reports whether the placeholder was used (the provider is then provisional).
func resolveServiceName(service string) (string, bool) {
	if strings.TrimSpace(service) != "" {
		return service, false
	}
	log.Printf("gincommon: WARNING tracing initialised without a service name " +
		"(set OTEL_SERVICE_NAME or APP_NAME, or use InitTracingWithConfig); spans use service.name=" +
		otel.UnknownServiceName + " until ObservabilityMiddlewares installs the configured ServiceName")
	return otel.UnknownServiceName, true
}

// ensureInitTelemetry installs tracing for ObservabilityMiddlewares: it keeps
// a provider already installed by InitTracing* (first configured caller wins)
// but replaces a provisional one that had no service name.
func ensureInitTelemetry(service, version string, cfg otel.TelemetryConfig) {
	otel.EnsureInitTelemetry(service, version, cfg)
}

// telemetryConfig resolves env defaults for the identity and OTEL_* settings,
// then applies non-nil TracingOptions overrides.
func telemetryConfig(domain, environment string, logger port.Logger, opts *TracingOptions) otel.TelemetryConfig {
	tc := otel.EnvConfig(domain, environment, logger)
	if opts == nil {
		return tc
	}
	if opts.Endpoint != "" {
		tc.Endpoint = opts.Endpoint
	}
	if opts.Insecure != nil {
		tc.Insecure = *opts.Insecure
	}
	if opts.SampleRatio != nil {
		tc.SampleRatio = *opts.SampleRatio
	}
	if opts.EnableBaggage != nil {
		tc.EnableBaggage = *opts.EnableBaggage
	}
	if opts.IgnoreRemoteParentSampled != nil {
		tc.IgnoreRemoteParentSampled = *opts.IgnoreRemoteParentSampled
	}
	return tc
}
