package config_test

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestIsDevEnv_MatchesRegistryEnvironments(t *testing.T) {
	for _, env := range []string{"dev", "local"} {
		if !config.IsDevEnv(env) {
			t.Fatalf("IsDevEnv(%q) = false, want true", env)
		}
	}
	// "development" is not a registry environment, so it must not get dev defaults.
	for _, env := range []string{"", "development", "test", "staging", "prod", "production"} {
		if config.IsDevEnv(env) {
			t.Fatalf("IsDevEnv(%q) = true, want false", env)
		}
	}
}

func TestOtelServiceNameEnv_NoLibraryDefault(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("APP_NAME", "")
	if got := config.OtelServiceNameEnv(); got != "" {
		t.Fatalf("OtelServiceNameEnv() = %q, want empty", got)
	}
	t.Setenv("APP_NAME", "app")
	if got := config.OtelServiceNameEnv(); got != "app" {
		t.Fatalf("OtelServiceNameEnv() = %q, want app", got)
	}
	t.Setenv("OTEL_SERVICE_NAME", "otel")
	if got := config.OtelServiceNameEnv(); got != "otel" {
		t.Fatalf("OtelServiceNameEnv() = %q, want otel", got)
	}
}

func TestOtelIgnoreRemoteParentSampled(t *testing.T) {
	t.Setenv("OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED", "")
	if config.OtelIgnoreRemoteParentSampled() {
		t.Fatal("default must be false")
	}
	t.Setenv("OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED", "TRUE")
	if !config.OtelIgnoreRemoteParentSampled() {
		t.Fatal("TRUE must enable")
	}
}

func TestParseBool_InvalidWarnsOnceAndUsesFallback(t *testing.T) {
	buf := captureLog(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "maybe")
	for i := 0; i < 2; i++ {
		if config.OtelExporterInsecure() {
			t.Fatal("invalid value must use the secure fallback")
		}
	}
	if n := strings.Count(buf.String(), `invalid OTEL_EXPORTER_OTLP_INSECURE="maybe"`); n != 1 {
		t.Fatalf("warned %d times, want once: %s", n, buf.String())
	}
}

func TestOtelTraceSampleRatio_InvalidWarns(t *testing.T) {
	buf := captureLog(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_TRACES_SAMPLER_RATIO", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "7.5")
	if got := config.OtelTraceSampleRatio(); got != 0.1 {
		t.Fatalf("ratio = %v, want 0.1", got)
	}
	if !strings.Contains(buf.String(), `invalid OTEL_TRACES_SAMPLER_ARG="7.5"`) {
		t.Fatalf("no warning for invalid ratio: %q", buf.String())
	}
}
