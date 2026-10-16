package config_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
)

func TestIsDevEnv_Normalises(t *testing.T) {
	for _, env := range []string{"Dev", "DEV", " dev ", "Local", " LOCAL\t"} {
		if !config.IsDevEnv(env) {
			t.Fatalf("IsDevEnv(%q) = false, want true", env)
		}
	}
}

// APP_ENV=Dev must select the dev defaults, matching the metrics identity
// (which trims and lowercases it to "dev").
func TestAppEnv_MixedCaseDevGetsDevDefaults(t *testing.T) {
	t.Setenv("APP_ENV", " Dev ")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	if got := config.AppEnv(); got != "dev" {
		t.Fatalf("AppEnv() = %q, want dev", got)
	}
	if !config.OtelExporterInsecure() {
		t.Fatal("OtelExporterInsecure() = false for APP_ENV=Dev, want true")
	}
	if got := config.OtelTraceSampleRatio(); got != 1.0 {
		t.Fatalf("OtelTraceSampleRatio() = %v for APP_ENV=Dev, want 1", got)
	}
}
