package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// AppName returns APP_NAME (default platform-gincommon). The default is the
// demo server's identity only; library code never uses it as a service name
// (see OtelServiceNameEnv).
func AppName() string { return getenv("APP_NAME", "platform-gincommon") }

// BuildVersion returns BUILD_VERSION (default unknown).
func BuildVersion() string { return getenv("BUILD_VERSION", "unknown") }

// AppEnv returns APP_ENV, trimmed and lowercased — the same normalisation the
// observability identity applies, so APP_ENV=Dev selects the dev defaults and
// the "dev" metrics environment consistently. Returns empty string when not set
// so that callers such as OtelTraceSampleRatio default to production-safe
// values rather than dev defaults.
func AppEnv() string { return NormalizeEnv(os.Getenv("APP_ENV")) }

// NormalizeEnv trims and lowercases an environment name.
func NormalizeEnv(env string) string { return strings.ToLower(strings.TrimSpace(env)) }

// IsDevEnv reports whether env is a local/development environment.
// Recognised values are the registry's developer environments, "local" and
// "dev". Everything else — "test", "staging", "prod", an empty string, and
// aliases such as "development" that the observability registry rejects — is
// treated as non-dev so production-safe defaults apply. env is compared after
// NormalizeEnv, so "Dev" and " LOCAL " are dev.
func IsDevEnv(env string) bool {
	env = NormalizeEnv(env)
	return env == "dev" || env == "local"
}

// ObservabilityDomain returns OBSERVABILITY_DOMAIN, the registry domain (e.g. iam)
// used for the platform_* metrics' domain label when Config.Domain is empty.
func ObservabilityDomain() string { return os.Getenv("OBSERVABILITY_DOMAIN") }

// ObservabilityEnvironment returns the environment label source when
// Config.Environment is empty: APP_ENV, then ENVIRONMENT. platform-events and
// platform-pgcommon resolve their metrics identity with the same precedence.
func ObservabilityEnvironment() string {
	return getenv("APP_ENV", os.Getenv("ENVIRONMENT"))
}

// AppPort returns APP_PORT (default 8080).
func AppPort() int { return getenvInt("APP_PORT", 8080) }

// SmokeBaseURL returns SMOKE_BASE_URL for live smoke tests.
func SmokeBaseURL() string { return getenv("SMOKE_BASE_URL", "http://localhost:8080") }

// OtelServiceName returns OTEL_SERVICE_NAME (default AppName()). It is the demo
// server's service name; library code uses OtelServiceNameEnv, which has no
// built-in default.
func OtelServiceName() string {
	return getenv("OTEL_SERVICE_NAME", AppName())
}

// OtelServiceNameEnv returns OTEL_SERVICE_NAME, then APP_NAME, or "" when
// neither is set. Unlike OtelServiceName it never falls back to a library
// default, so spans are never exported under the library's own name.
func OtelServiceNameEnv() string {
	return getenv("OTEL_SERVICE_NAME", os.Getenv("APP_NAME"))
}

// OtelExporterEndpoint returns OTEL_EXPORTER_OTLP_ENDPOINT (gRPC host:port),
// default localhost:4317. It returns "" — trace export disabled, spans kept
// in-process — when export is switched off explicitly:
// OTEL_EXPORTER_OTLP_ENDPOINT=none (or off / disabled), or the OpenTelemetry
// spec's OTEL_TRACES_EXPORTER=none. An empty OTEL_EXPORTER_OTLP_ENDPOINT is
// still treated as unset, as before.
func OtelExporterEndpoint() string {
	if OtelTraceExportDisabled() {
		return ""
	}
	return getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
}

// OtelTraceExportDisabled reports whether trace export is switched off:
// OTEL_EXPORTER_OTLP_ENDPOINT is none, off or disabled, or
// OTEL_TRACES_EXPORTER is none (case-insensitive). Any other
// OTEL_TRACES_EXPORTER value is ignored: the library always exports OTLP/gRPC.
func OtelTraceExportDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))) {
	case "none", "off", "disabled":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")), "none")
}

// OtelExporterInsecure enables plaintext OTLP/gRPC.
// Defaults to true only when APP_ENV is explicitly "dev" or "local".
// An unset APP_ENV defaults to secure (false) to avoid accidental plaintext export.
func OtelExporterInsecure() bool {
	return parseBool("OTEL_EXPORTER_OTLP_INSECURE", IsDevEnv(AppEnv()))
}

// OtelEnableBaggage enables W3C baggage propagation. Defaults to false.
func OtelEnableBaggage() bool {
	return parseBool("OTEL_PROPAGATOR_BAGGAGE", false)
}

// OtelIgnoreRemoteParentSampled reports OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED
// (default false). When true, a remote parent's sampled flag does not force
// sampling: those spans are re-sampled with the configured ratio, so callers
// cannot push an edge service to 100% sampling by sending a sampled traceparent.
func OtelIgnoreRemoteParentSampled() bool {
	return parseBool("OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED", false)
}

// parseBool reads a boolean env var: "true"/"1"/"yes" and "false"/"0"/"no"
// (case-insensitive). Unset returns fallback; an unrecognised value returns
// fallback with a one-time warning on stderr.
func parseBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	case "":
		return fallback
	default:
		warnInvalidOnce(key, v, strconv.FormatBool(fallback))
		return fallback
	}
}

// invalidWarned records key=value pairs already reported, so a bad value is
// warned about once per process rather than on every lookup.
var invalidWarned sync.Map

func warnInvalidOnce(key, value, using string) {
	if _, loaded := invalidWarned.LoadOrStore(key+"="+value, struct{}{}); loaded {
		return
	}
	log.Printf("gincommon: invalid %s=%q; using %s", key, value, using)
}

// OtelTraceSampleRatio returns a value in [0,1] for trace ID ratio sampling (1 = always).
// Precedence: OTEL_TRACES_SAMPLER_RATIO (this library's var, kept for backward compatibility),
// then the OpenTelemetry-spec OTEL_TRACES_SAMPLER_ARG, then env-based defaults
// (1.0 in dev, 0.1 in all other environments). Invalid or out-of-range values fall back
// to the env-based default with a one-time warning on stderr.
func OtelTraceSampleRatio() float64 {
	key := "OTEL_TRACES_SAMPLER_RATIO"
	v := os.Getenv(key)
	if v == "" {
		// Fall back to the standard OTel sampler argument used with
		// OTEL_TRACES_SAMPLER=traceidratio so services configured per the spec work.
		key = "OTEL_TRACES_SAMPLER_ARG"
		v = os.Getenv(key)
	}

	defaultRatio := 0.1
	if IsDevEnv(AppEnv()) {
		defaultRatio = 1.0
	}

	if v == "" {
		return defaultRatio
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 || n > 1 {
		warnInvalidOnce(key, v, fmt.Sprintf("default ratio %g (want a number in [0,1])", defaultRatio))
		return defaultRatio
	}
	return n
}

// TestServiceName returns TEST_SERVICE_NAME for integration tests.
func TestServiceName() string { return getenv("TEST_SERVICE_NAME", "test-service") }

// E2EServiceName returns E2E_SERVICE_NAME for end-to-end tests.
func E2EServiceName() string { return getenv("E2E_SERVICE_NAME", "e2e-test") }

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// AppAddr returns the listen address :APP_PORT for the example server.
func AppAddr() string {
	return fmt.Sprintf(":%d", AppPort())
}
