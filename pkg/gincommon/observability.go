package gincommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
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

// InitTracing configures OTLP/gRPC export and returns a shutdown function.
func InitTracing(service, version, endpoint string) func() {
	return otel.InitTelemetry(service, version, otel.TelemetryConfig{
		Endpoint:      endpoint,
		Insecure:      config.OtelExporterInsecure(),
		SampleRatio:   config.OtelTraceSampleRatio(),
		EnableBaggage: config.OtelEnableBaggage(),
	})
}

// EnsureTracing installs a TracerProvider when none is registered yet.
func EnsureTracing(service, version, endpoint string) {
	otel.EnsureInitTelemetry(service, version, otel.TelemetryConfig{
		Endpoint:      endpoint,
		Insecure:      config.OtelExporterInsecure(),
		SampleRatio:   config.OtelTraceSampleRatio(),
		EnableBaggage: config.OtelEnableBaggage(),
	})
}

// EnsureInitTelemetry installs tracing using TelemetryConfig (used by ObservabilityMiddlewares).
func EnsureInitTelemetry(service, version string, cfg otel.TelemetryConfig) {
	otel.EnsureInitTelemetry(service, version, cfg)
}

// InitTracingFromEnv calls InitTracing using OTEL_SERVICE_NAME, BUILD_VERSION, and OTEL_EXPORTER_OTLP_ENDPOINT.
func InitTracingFromEnv() func() {
	return InitTracing(
		config.OtelServiceName(),
		config.BuildVersion(),
		config.OtelExporterEndpoint(),
	)
}
