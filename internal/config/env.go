package config

import (
	"fmt"
	"os"
	"strconv"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// AppName returns APP_NAME (default platform-gincommon).
func AppName() string { return getenv("APP_NAME", "platform-gincommon") }

// BuildVersion returns BUILD_VERSION (default unknown).
func BuildVersion() string { return getenv("BUILD_VERSION", "unknown") }

// AppEnv returns APP_ENV. Returns empty string when not set so that callers such as
// OtelTraceSampleRatio default to production-safe values rather than dev defaults.
func AppEnv() string { return os.Getenv("APP_ENV") }

// IsDevEnv reports whether env is a local/development environment.
// Recognised values: "dev", "development", "local". Everything else (including
// an empty string) is treated as non-dev so production-safe defaults apply.
func IsDevEnv(env string) bool {
	return env == "dev" || env == "development" || env == "local"
}

// AppPort returns APP_PORT (default 8080).
func AppPort() int { return getenvInt("APP_PORT", 8080) }

// SmokeBaseURL returns SMOKE_BASE_URL for live smoke tests.
func SmokeBaseURL() string { return getenv("SMOKE_BASE_URL", "http://localhost:8080") }

// OtelServiceName returns OTEL_SERVICE_NAME (default AppName()).
func OtelServiceName() string {
	return getenv("OTEL_SERVICE_NAME", AppName())
}

// OtelExporterEndpoint returns OTEL_EXPORTER_OTLP_ENDPOINT (gRPC host:port).
func OtelExporterEndpoint() string {
	return getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
}

// OtelExporterInsecure enables plaintext OTLP/gRPC.
// Defaults to true only when APP_ENV is explicitly "dev", "development", or "local".
// An unset APP_ENV defaults to secure (false) to avoid accidental plaintext export.
func OtelExporterInsecure() bool {
	switch os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return IsDevEnv(os.Getenv("APP_ENV"))
	}
}

// OtelEnableBaggage enables W3C baggage propagation. Defaults to false.
func OtelEnableBaggage() bool {
	switch getenv("OTEL_PROPAGATOR_BAGGAGE", "") {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// OtelTraceSampleRatio returns a value in [0,1] for trace ID ratio sampling (1 = always).
// Precedence: OTEL_TRACES_SAMPLER_RATIO (this library's var, kept for backward compatibility),
// then the OpenTelemetry-spec OTEL_TRACES_SAMPLER_ARG, then env-based defaults
// (1.0 in dev, 0.1 in all other environments). Invalid or out-of-range values fall back
// to the env-based default.
func OtelTraceSampleRatio() float64 {
	v := os.Getenv("OTEL_TRACES_SAMPLER_RATIO")
	if v == "" {
		// Fall back to the standard OTel sampler argument used with
		// OTEL_TRACES_SAMPLER=traceidratio so services configured per the spec work.
		v = os.Getenv("OTEL_TRACES_SAMPLER_ARG")
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
		return defaultRatio
	}
	return n
}

// TestServiceName returns TEST_SERVICE_NAME for integration tests.
func TestServiceName() string { return getenv("TEST_SERVICE_NAME", "test-service") }

// E2EServiceName returns E2E_SERVICE_NAME for end-to-end tests.
func E2EServiceName() string { return getenv("E2E_SERVICE_NAME", "e2e-test") }

// PrometheusScrapeTarget returns PROMETHEUS_SCRAPE_TARGET for docker-compose templates.
func PrometheusScrapeTarget() string {
	return getenv("PROMETHEUS_SCRAPE_TARGET", "host.docker.internal:8080")
}

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
