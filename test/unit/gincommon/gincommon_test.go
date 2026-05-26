package gincommon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	gootel "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	internalotel "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestNewRBAC(t *testing.T) {
	if gincommon.NewRBAC() == nil {
		t.Fatal("NewRBAC() returned nil")
	}
}

func TestDefaultMiddlewares_NotEmpty(t *testing.T) {
	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	mw := gincommon.DefaultMiddlewares(gincommon.Config{Logger: log, ServiceName: "pkg-test"})
	if len(mw) == 0 {
		t.Fatal("expected middleware chain")
	}
}

func TestDefaultMiddlewares_IncrementsHttpPanicTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:       log,
		ServiceName:  "pkg-panic",
		BuildVersion: "test",
	}) {
		r.Use(m)
	}
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "Internal Server Error" {
		t.Fatalf("error = %v", body["error"])
	}
	if body["request_id"] == nil || body["request_id"] == "" {
		t.Fatalf("request_id = %v, want non-empty in panic response", body["request_id"])
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id on panic response")
	}

	counter, err := metrics.HttpPanicTotal.GetMetricWithLabelValues(http.MethodGet, "/panic")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_panic_total = %v, want 1", got)
	}
}

func TestInitTracingFromEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "pkg-otel")
	t.Setenv("BUILD_VERSION", "9.9.9")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")

	shutdown := gincommon.InitTracingFromEnv()
	if shutdown == nil {
		t.Fatal("expected shutdown func")
	}
	shutdown()
}

func TestInitTracing_PassesServiceVersionEndpoint(t *testing.T) {
	shutdown := gincommon.InitTracing("my-svc", "1.2.3", "localhost:4317")
	if shutdown == nil {
		t.Fatal("expected shutdown func")
	}
	shutdown()
}

func TestEnsureTracing_InstallsWhenNoneRegistered(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	gootel.SetTracerProvider(tp)
	_ = tp.Shutdown(context.Background())

	gincommon.EnsureTracing("ensure-svc", "1.0.0", "")
	if _, ok := gootel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("tracer provider type = %T, want *sdktrace.TracerProvider", gootel.GetTracerProvider())
	}
}

func TestEnsureInitTelemetry_SkipsWhenAlreadyInstalled(t *testing.T) {
	shutdown := gincommon.InitTracing("base-svc", "1.0.0", "")
	defer shutdown()

	first := gootel.GetTracerProvider()
	gincommon.EnsureInitTelemetry("other-svc", "2.0.0", internalotel.TelemetryConfig{})
	if gootel.GetTracerProvider() != first {
		t.Fatal("EnsureInitTelemetry replaced an already-installed TracerProvider")
	}
}

func TestRequestContext_ReturnsContextWhenPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	want := &domain.RequestContext{TenantID: "t1", UserID: "u1", Roles: []string{"admin"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(gincommon.RequestContextKey, want)

	got, ok := gincommon.RequestContext(c)
	if !ok {
		t.Fatal("RequestContext() ok = false, want true")
	}
	if got != want {
		t.Fatalf("RequestContext() = %v, want %v", got, want)
	}
}

func TestRequestContext_ReturnsFalseWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	got, ok := gincommon.RequestContext(c)
	if ok {
		t.Fatal("RequestContext() ok = true, want false")
	}
	if got != nil {
		t.Fatalf("RequestContext() = %v, want nil", got)
	}
}

func TestRequestContext_ReturnsFalseOnTypeMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(gincommon.RequestContextKey, "not-a-request-context")

	got, ok := gincommon.RequestContext(c)
	if ok {
		t.Fatal("RequestContext() ok = true for wrong type, want false")
	}
	if got != nil {
		t.Fatalf("RequestContext() = %v, want nil for type mismatch", got)
	}
}

func TestObservabilityMiddlewares_WithTracingOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	insecure := true
	ratio := 0.5
	baggage := true

	mw := gincommon.ObservabilityMiddlewares(gincommon.Config{
		Logger:       log,
		ServiceName:  "tracing-overrides-test",
		BuildVersion: "test",
		Tracing: &gincommon.TracingOptions{
			Endpoint:      "localhost:4317",
			Insecure:      &insecure,
			SampleRatio:   &ratio,
			EnableBaggage: &baggage,
		},
	})
	if len(mw) == 0 {
		t.Fatal("expected middleware chain")
	}
}

func TestObservabilityMiddlewares_WithEmptyTracingOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	mw := gincommon.ObservabilityMiddlewares(gincommon.Config{
		Logger:       log,
		ServiceName:  "empty-tracing-options",
		BuildVersion: "test",
		Tracing:      &gincommon.TracingOptions{},
	})
	if len(mw) == 0 {
		t.Fatal("expected middleware chain")
	}
}
