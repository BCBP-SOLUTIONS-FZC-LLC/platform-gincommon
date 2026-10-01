package otel

import (
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
)

// EnvConfig returns a TelemetryConfig populated from the OTEL_* environment
// (endpoint, TLS, sampling ratio, baggage, remote-parent sampling) with the
// given observability identity. An empty domain uses OBSERVABILITY_DOMAIN and
// an empty environment uses APP_ENV, then ENVIRONMENT — the same precedence as
// the metrics identity — both trimmed and lowercased.
func EnvConfig(domain, environment string, logger port.Logger) TelemetryConfig {
	if strings.TrimSpace(domain) == "" {
		domain = config.ObservabilityDomain()
	}
	if strings.TrimSpace(environment) == "" {
		environment = config.ObservabilityEnvironment()
	}
	return TelemetryConfig{
		Endpoint:                  config.OtelExporterEndpoint(),
		Insecure:                  config.OtelExporterInsecure(),
		SampleRatio:               config.OtelTraceSampleRatio(),
		EnableBaggage:             config.OtelEnableBaggage(),
		IgnoreRemoteParentSampled: config.OtelIgnoreRemoteParentSampled(),
		Domain:                    strings.ToLower(strings.TrimSpace(domain)),
		Environment:               strings.ToLower(strings.TrimSpace(environment)),
		Logger:                    logger,
	}
}
