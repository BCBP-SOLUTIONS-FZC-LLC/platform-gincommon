package otel_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
)

// recLogger records every call (fixtures.MockLogger keeps only the last).
type recLogger struct {
	mu      sync.Mutex
	entries []string
}

func (r *recLogger) add(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, level+":"+msg)
}
func (r *recLogger) Debug(msg string, _ map[string]interface{}) { r.add("debug", msg) }
func (r *recLogger) Info(msg string, _ map[string]interface{})  { r.add("info", msg) }
func (r *recLogger) Warn(msg string, _ map[string]interface{})  { r.add("warn", msg) }
func (r *recLogger) Error(msg string, _ map[string]interface{}) { r.add("error", msg) }

func (r *recLogger) has(entry string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e == entry {
			return true
		}
	}
	return false
}

func captureStderrLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

var (
	traceID = trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID  = trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
)

func parentCtx(remote, sampled bool) context.Context {
	flags := trace.TraceFlags(0)
	if sampled {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: flags, Remote: remote,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func decide(s sdktrace.Sampler, ctx context.Context) sdktrace.SamplingDecision {
	return s.ShouldSample(sdktrace.SamplingParameters{ParentContext: ctx, TraceID: traceID, Name: "op"}).Decision
}

func TestSampler_AlwaysParentBased(t *testing.T) {
	for _, ratio := range []float64{0, 0.5, 1} {
		s := otel.Sampler(ratio, false)
		if !strings.HasPrefix(s.Description(), "ParentBased") {
			t.Fatalf("ratio %v: sampler %q is not ParentBased", ratio, s.Description())
		}
	}
	// ratio 1: an unsampled parent is respected (not forced to sample).
	if d := decide(otel.Sampler(1, false), parentCtx(false, false)); d != sdktrace.Drop {
		t.Fatalf("ratio 1, unsampled parent: %v, want Drop", d)
	}
	// ratio 0: a sampled parent is respected.
	if d := decide(otel.Sampler(0, false), parentCtx(false, true)); d != sdktrace.RecordAndSample {
		t.Fatalf("ratio 0, sampled parent: %v, want RecordAndSample", d)
	}
	// Root spans follow the ratio.
	if d := decide(otel.Sampler(1, false), context.Background()); d != sdktrace.RecordAndSample {
		t.Fatalf("ratio 1 root: %v", d)
	}
	if d := decide(otel.Sampler(0, false), context.Background()); d != sdktrace.Drop {
		t.Fatalf("ratio 0 root: %v", d)
	}
}

func TestSampler_IgnoreRemoteParentSampled(t *testing.T) {
	// Default: a sampled remote parent forces sampling even at ratio 0.
	if d := decide(otel.Sampler(0, false), parentCtx(true, true)); d != sdktrace.RecordAndSample {
		t.Fatalf("default remote sampled: %v, want RecordAndSample", d)
	}
	// Opt-in: the ratio decides instead.
	s := otel.Sampler(0, true)
	if d := decide(s, parentCtx(true, true)); d != sdktrace.Drop {
		t.Fatalf("ignore remote sampled at ratio 0: %v, want Drop", d)
	}
	// Local sampled parents are still honoured.
	if d := decide(s, parentCtx(false, true)); d != sdktrace.RecordAndSample {
		t.Fatalf("local sampled parent: %v, want RecordAndSample", d)
	}
	// Ratio 1 with the option still samples remote sampled parents.
	if d := decide(otel.Sampler(1, true), parentCtx(true, true)); d != sdktrace.RecordAndSample {
		t.Fatalf("ratio 1 remote sampled: %v", d)
	}
	_ = otel.Sampler(0.25, true) // ratio branch
}

func resourceAttrs(service string, cfg otel.TelemetryConfig) map[attribute.Key]string {
	out := map[attribute.Key]string{}
	for _, kv := range otel.Resource(service, "1.2.3", cfg).Attributes() {
		out[kv.Key] = kv.Value.String()
	}
	return out
}

func TestResource_IdentityAttributes(t *testing.T) {
	got := resourceAttrs("orders", otel.TelemetryConfig{Domain: "iam", Environment: "prod"})
	want := map[attribute.Key]string{
		"service.name":                "orders",
		"service.version":             "1.2.3",
		"service.namespace":           "iam",
		"deployment.environment.name": "prod",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}

	bare := resourceAttrs("", otel.TelemetryConfig{})
	if bare["service.name"] != otel.UnknownServiceName {
		t.Fatalf("empty service.name = %q, want %q", bare["service.name"], otel.UnknownServiceName)
	}
	if _, ok := bare["service.namespace"]; ok {
		t.Fatal("service.namespace set without a domain")
	}
	if _, ok := bare["deployment.environment.name"]; ok {
		t.Fatal("deployment.environment.name set without an environment")
	}
}

func TestEnvConfig(t *testing.T) {
	t.Setenv("OBSERVABILITY_DOMAIN", " IAM ")
	t.Setenv("APP_ENV", "")
	t.Setenv("ENVIRONMENT", "Staging")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED", "true")
	lg := &recLogger{}

	tc := otel.EnvConfig("", "", lg)
	if tc.Domain != "iam" || tc.Environment != "staging" {
		t.Fatalf("identity = %q/%q, want iam/staging", tc.Domain, tc.Environment)
	}
	if tc.Endpoint != "collector:4317" || !tc.IgnoreRemoteParentSampled || tc.Logger != lg {
		t.Fatalf("unexpected config %+v", tc)
	}

	explicit := otel.EnvConfig("billing", "prod", nil)
	if explicit.Domain != "billing" || explicit.Environment != "prod" {
		t.Fatalf("explicit identity = %q/%q", explicit.Domain, explicit.Environment)
	}
}

func TestInitTelemetry_LogsEffectiveConfigAndRoutesErrors(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	lg := &recLogger{}

	otel.InitTelemetry("svc", "1", otel.TelemetryConfig{Logger: lg, Endpoint: "localhost:4317", Insecure: true})
	if !lg.has("info:otel_tracing_initialized") {
		t.Fatalf("missing init log, got %v", lg.entries)
	}
	gootel.Handle(errors.New("export failed"))
	gootel.Handle(nil)
	if !lg.has("warn:otel_error") {
		t.Fatalf("OTel error not routed to logger, got %v", lg.entries)
	}
}

func TestInitTelemetry_NoLoggerWritesStderr(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	buf := captureStderrLog(t)

	otel.InitTelemetry("svc", "1", otel.TelemetryConfig{SampleRatio: 0.5})
	gootel.Handle(errors.New("boom"))
	out := buf.String()
	if !strings.Contains(out, "otel_tracing_initialized") || !strings.Contains(out, "sample_ratio=0.5") {
		t.Fatalf("init line missing from stderr: %s", out)
	}
	if !strings.Contains(out, "warn otel_error error=boom") {
		t.Fatalf("OTel error missing from stderr: %s", out)
	}
}

func TestInitTelemetry_ExporterFailureWarnsAndFallsBack(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)
	lg := &recLogger{}

	// An endpoint that cannot be parsed as a gRPC target makes otlptracegrpc.New fail.
	otel.InitTelemetry("svc", "1", otel.TelemetryConfig{Logger: lg, Endpoint: "%zz", Insecure: true})
	if !lg.has("warn:otel_exporter_init_failed") {
		t.Fatalf("exporter failure not logged, got %v", lg.entries)
	}
	if otel.SDKProvider() == nil || !otel.IsGlobalProvider(gootel.GetTracerProvider()) {
		t.Fatal("in-process fallback provider not installed")
	}
}

func TestEnsureInitTelemetry_ReplacesProvisionalProvider(t *testing.T) {
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	otel.InitTelemetry(otel.UnknownServiceName, "1", otel.TelemetryConfig{Provisional: true})
	provisional := otel.SDKProvider()

	// Another provisional caller does not replace it.
	otel.EnsureInitTelemetry(otel.UnknownServiceName, "1", otel.TelemetryConfig{Provisional: true})
	if otel.SDKProvider() != provisional {
		t.Fatal("provisional EnsureInitTelemetry replaced a provisional provider")
	}

	// A configured service name wins.
	otel.EnsureInitTelemetry("orders", "1", otel.TelemetryConfig{})
	configured := otel.SDKProvider()
	if configured == provisional {
		t.Fatal("configured EnsureInitTelemetry kept the provisional provider")
	}

	// ...and is then kept.
	otel.EnsureInitTelemetry("other", "1", otel.TelemetryConfig{})
	if otel.SDKProvider() != configured {
		t.Fatal("EnsureInitTelemetry replaced a configured provider")
	}
}

// OTEL_EXPORTER_OTLP_ENDPOINT=none (or the spec's OTEL_TRACES_EXPORTER=none)
// reaches the TracerProvider as "no endpoint": spans stay in-process and no
// exporter is created, so a host without a collector does not log failed
// exports.
func TestEnvConfig_ExportDisabledReachesTelemetry(t *testing.T) {
	for _, env := range []map[string]string{
		{"OTEL_EXPORTER_OTLP_ENDPOINT": "none"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector:4317", "OTEL_TRACES_EXPORTER": "none"},
	} {
		t.Run(fmt.Sprint(env), func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			t.Setenv("OTEL_TRACES_EXPORTER", "")
			for k, v := range env {
				t.Setenv(k, v)
			}
			cfg := otel.EnvConfig("iam", "dev", nil)
			if cfg.Endpoint != "" {
				t.Fatalf("Endpoint = %q, want export disabled", cfg.Endpoint)
			}

			otel.Shutdown()
			t.Cleanup(otel.Shutdown)
			buf := captureStderrLog(t)
			otel.InitTelemetry("svc", "1", cfg)
			if out := buf.String(); !strings.Contains(out, "disabled") {
				t.Fatalf("init line does not report export disabled: %s", out)
			}
			if otel.SDKProvider() == nil {
				t.Fatal("in-process TracerProvider must still be installed")
			}
		})
	}
}
