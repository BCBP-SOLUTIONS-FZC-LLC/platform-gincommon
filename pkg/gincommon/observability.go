package gincommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
)

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
