package gincommon_test

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestInitTracingWithConfig_RequiresServiceName(t *testing.T) {
	shutdown, err := gincommon.InitTracingWithConfig(gincommon.TracingConfig{ServiceName: "  "})
	if !errors.Is(err, gincommon.ErrServiceNameRequired) || shutdown != nil {
		t.Fatalf("got (%v, %v), want (nil, ErrServiceNameRequired)", shutdown != nil, err)
	}
}

func TestInitTracingWithConfig_AppliesOptions(t *testing.T) {
	yes, ratio := true, 0.5
	shutdown, err := gincommon.InitTracingWithConfig(gincommon.TracingConfig{
		ServiceName: "orders",
		Domain:      "iam",
		Environment: "prod",
		Tracing: &gincommon.TracingOptions{
			Endpoint:                  "localhost:4317",
			Insecure:                  &yes,
			SampleRatio:               &ratio,
			EnableBaggage:             &yes,
			IgnoreRemoteParentSampled: &yes,
		},
	})
	if err != nil || shutdown == nil {
		t.Fatalf("InitTracingWithConfig: %v", err)
	}
	shutdown()
}

func TestInitTracingFromEnv_NoServiceName_IsProvisional(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("APP_NAME", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	shutdown := gincommon.InitTracingFromEnv()
	defer shutdown()
	if !strings.Contains(buf.String(), "without a service name") {
		t.Fatalf("expected a loud warning, got %q", buf.String())
	}
	provisional := otel.SDKProvider()

	// The first stack with a configured service name replaces the placeholder.
	gincommon.EnsureTracing("orders", "1.0.0", "")
	if otel.SDKProvider() == provisional {
		t.Fatal("configured service name did not replace the provisional provider")
	}
}
