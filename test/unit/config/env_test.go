package config_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
)

func TestGetenvHelpers(t *testing.T) {
	t.Setenv("APP_NAME", "custom-app")
	t.Setenv("BUILD_VERSION", "2.0.0")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("SMOKE_BASE_URL", "http://smoke:9000")
	t.Setenv("OTEL_SERVICE_NAME", "otel-svc")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("TEST_SERVICE_NAME", "int-svc")
	t.Setenv("E2E_SERVICE_NAME", "e2e-svc")
	t.Setenv("PROMETHEUS_SCRAPE_TARGET", "host:9091")

	if got := config.AppName(); got != "custom-app" {
		t.Fatalf("AppName() = %q", got)
	}
	if got := config.BuildVersion(); got != "2.0.0" {
		t.Fatalf("BuildVersion() = %q", got)
	}
	if got := config.AppEnv(); got != "staging" {
		t.Fatalf("AppEnv() = %q", got)
	}
	if got := config.AppPort(); got != 9090 {
		t.Fatalf("AppPort() = %d", got)
	}
	if got := config.SmokeBaseURL(); got != "http://smoke:9000" {
		t.Fatalf("SmokeBaseURL() = %q", got)
	}
	if got := config.OtelServiceName(); got != "otel-svc" {
		t.Fatalf("OtelServiceName() = %q", got)
	}
	if got := config.OtelExporterEndpoint(); got != "collector:4317" {
		t.Fatalf("OtelExporterEndpoint() = %q", got)
	}
	if got := config.TestServiceName(); got != "int-svc" {
		t.Fatalf("TestServiceName() = %q", got)
	}
	if got := config.E2EServiceName(); got != "e2e-svc" {
		t.Fatalf("E2EServiceName() = %q", got)
	}
	if got := config.PrometheusScrapeTarget(); got != "host:9091" {
		t.Fatalf("PrometheusScrapeTarget() = %q", got)
	}
	if got := config.AppAddr(); got != ":9090" {
		t.Fatalf("AppAddr() = %q", got)
	}
}

func TestGetenvInt_InvalidFallsBack(t *testing.T) {
	t.Setenv("APP_PORT", "not-a-number")
	if got := config.AppPort(); got != 8080 {
		t.Fatalf("AppPort() = %d want default 8080", got)
	}
}

func TestOtelTraceSampleRatio_DevDefaultsTo1(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	if got := config.OtelTraceSampleRatio(); got != 1.0 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 1.0 in dev", got)
	}
}

func TestOtelTraceSampleRatio_NonDevDefaultsTo01(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	if got := config.OtelTraceSampleRatio(); got != 0.1 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 0.1 in prod", got)
	}
}

func TestOtelTraceSampleRatio_ExplicitOverride(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "0.5")
	if got := config.OtelTraceSampleRatio(); got != 0.5 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 0.5", got)
	}
}

func TestOtelTraceSampleRatio_InvalidFallsBack(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "bad")
	if got := config.OtelTraceSampleRatio(); got != 1.0 {
		t.Fatalf("OtelTraceSampleRatio() invalid in dev = %v, want fallback 1.0", got)
	}
}

func TestOtelTraceSampleRatio_InvalidInProdFallsBackTo01(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "bad")
	if got := config.OtelTraceSampleRatio(); got != 0.1 {
		t.Fatalf("OtelTraceSampleRatio() invalid in prod = %v, want fallback 0.1", got)
	}
}
