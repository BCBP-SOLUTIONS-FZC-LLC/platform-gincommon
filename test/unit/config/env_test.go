package config_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
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

// The OTel-spec OTEL_TRACES_SAMPLER_ARG is honored when the library-specific
// OTEL_TRACES_SAMPLER_RATIO is unset.
func TestOtelTraceSampleRatio_StandardSamplerArgFallback(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	if got := config.OtelTraceSampleRatio(); got != 0.25 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 0.25 from OTEL_TRACES_SAMPLER_ARG", got)
	}
}

// OTEL_TRACES_SAMPLER_RATIO takes precedence over the standard OTEL_TRACES_SAMPLER_ARG.
func TestOtelTraceSampleRatio_RatioTakesPrecedenceOverArg(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "0.9")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	if got := config.OtelTraceSampleRatio(); got != 0.9 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 0.9 (RATIO wins over ARG)", got)
	}
}

// An invalid OTEL_TRACES_SAMPLER_ARG falls back to the env-based default.
func TestOtelTraceSampleRatio_InvalidSamplerArgFallsBack(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "nonsense")
	if got := config.OtelTraceSampleRatio(); got != 0.1 {
		t.Fatalf("OtelTraceSampleRatio() = %v, want 0.1 fallback for invalid ARG", got)
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

func TestOtelExporterInsecure_ExplicitTrue(t *testing.T) {
	for _, v := range []string{"true", "1", "yes"} {
		t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", v)
		if !config.OtelExporterInsecure() {
			t.Fatalf("OtelExporterInsecure() = false for %q, want true", v)
		}
	}
}

func TestOtelExporterInsecure_ExplicitFalse(t *testing.T) {
	for _, v := range []string{"false", "0", "no"} {
		t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", v)
		if config.OtelExporterInsecure() {
			t.Fatalf("OtelExporterInsecure() = true for %q, want false", v)
		}
	}
}

func TestOtelEnableBaggage_ExplicitTrue(t *testing.T) {
	for _, v := range []string{"true", "1", "yes"} {
		t.Setenv("OTEL_PROPAGATOR_BAGGAGE", v)
		if !config.OtelEnableBaggage() {
			t.Fatalf("OtelEnableBaggage() = false for %q, want true", v)
		}
	}
}

func TestGetenvInt_EmptyEnvVar(t *testing.T) {
	t.Setenv("APP_PORT", "")
	if got := config.AppPort(); got != 8080 {
		t.Fatalf("AppPort() with empty env = %d, want default 8080", got)
	}
}

func TestOtelExporterEndpoint_Disable(t *testing.T) {
	cases := []struct {
		name, endpoint, exporter, want string
	}{
		{"unset defaults to localhost", "", "", "localhost:4317"},
		{"explicit endpoint", "collector:4317", "", "collector:4317"},
		{"endpoint none", "none", "", ""},
		{"endpoint OFF (case-insensitive)", " OFF ", "", ""},
		{"endpoint disabled", "disabled", "", ""},
		{"spec OTEL_TRACES_EXPORTER=none wins over an endpoint", "collector:4317", "none", ""},
		{"other exporter values are ignored", "collector:4317", "otlp", "collector:4317"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.endpoint)
			t.Setenv("OTEL_TRACES_EXPORTER", tc.exporter)
			if got := config.OtelExporterEndpoint(); got != tc.want {
				t.Fatalf("OtelExporterEndpoint() = %q, want %q", got, tc.want)
			}
			if got := config.OtelTraceExportDisabled(); got != (tc.want == "") {
				t.Fatalf("OtelTraceExportDisabled() = %v, want %v", got, tc.want == "")
			}
		})
	}
}
