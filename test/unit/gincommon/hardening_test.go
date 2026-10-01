package gincommon_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

// tracedContext returns a gin.Context whose request carries a sampled span and a
// baggage member, plus a RequestContext and request ID.
func tracedContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	ctx, span := tp.Tracer("t").Start(context.Background(), "parent")
	t.Cleanup(func() { span.End() })
	m, _ := baggage.NewMember("secret", "value")
	b, _ := baggage.New(m)
	ctx = baggage.ContextWithBaggage(ctx, b)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	c.Set(gincommon.RequestContextKey, &domain.RequestContext{UserID: "user-1", TenantID: "tenant-1", Roles: []string{"admin"}})
	c.Set("request_id", "req-1")
	return c
}

func TestPropagateHeaders_CorrelationOnly(t *testing.T) {
	c := tracedContext(t)
	out, _ := http.NewRequest(http.MethodGet, "https://third-party.example/api", nil)
	gincommon.PropagateHeaders(c, out)

	if out.Header.Get("traceparent") == "" {
		t.Fatal("traceparent not propagated")
	}
	if out.Header.Get(domain.HeaderRequestID) != "req-1" {
		t.Fatalf("x-request-id = %q", out.Header.Get(domain.HeaderRequestID))
	}
	for _, h := range []string{domain.HeaderUserID, domain.HeaderTenantID, domain.HeaderTenantRoles, "baggage"} {
		if v := out.Header.Get(h); v != "" {
			t.Fatalf("%s = %q must not be propagated by PropagateHeaders", h, v)
		}
	}
}

func TestPropagateIdentityHeaders_IdentityAndBaggage(t *testing.T) {
	c := tracedContext(t)
	out, _ := http.NewRequest(http.MethodGet, "http://internal/api", nil)
	gincommon.PropagateIdentityHeaders(c, out)

	want := map[string]string{
		domain.HeaderUserID:      "user-1",
		domain.HeaderTenantID:    "tenant-1",
		domain.HeaderTenantRoles: "admin",
		domain.HeaderRequestID:   "req-1",
		"baggage":                "secret=value",
	}
	for h, v := range want {
		if got := out.Header.Get(h); got != v {
			t.Fatalf("%s = %q, want %q", h, got, v)
		}
	}
	if out.Header.Get("traceparent") == "" {
		t.Fatal("traceparent not propagated")
	}
}

func TestPropagate_NilInputsAndNilHeader(t *testing.T) {
	c := tracedContext(t)
	gincommon.PropagateHeaders(nil, &http.Request{})
	gincommon.PropagateIdentityHeaders(nil, &http.Request{})
	gincommon.PropagateIdentityHeaders(c, nil)
	gincommon.PropagateHeaders(&gin.Context{}, &http.Request{}) // nil c.Request

	out := &http.Request{} // nil Header map must be allocated, not panic
	gincommon.PropagateHeaders(c, out)
	if out.Header.Get("traceparent") == "" {
		t.Fatal("traceparent not set on request with nil Header")
	}
	out = &http.Request{}
	gincommon.PropagateIdentityHeaders(c, out)
	if out.Header.Get(domain.HeaderUserID) != "user-1" {
		t.Fatal("identity not set on request with nil Header")
	}
}

func TestObservabilityMiddlewares_RequestTimeoutAppendedInnermost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "pkg-test"})
	withTimeout := gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "pkg-test", RequestTimeout: time.Nanosecond})
	if len(withTimeout) != len(base)+1 {
		t.Fatalf("len = %d, want %d", len(withTimeout), len(base)+1)
	}

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{Logger: log, ServiceName: "pkg-test", RequestTimeout: time.Nanosecond})...)
	r.GET("/slow-cfg", func(c *gin.Context) { <-c.Request.Context().Done() })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow-cfg", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if s, _ := log.Fields["status"].(int); s != http.StatusServiceUnavailable {
		t.Fatalf("access log status = %v, want 503 (same as client)", log.Fields["status"])
	}
	if w.Result().Header.Get(domain.HeaderTraceIDResponse) == "" {
		t.Fatal("X-Trace-ID missing on timeout response")
	}
}

func TestProtectedMiddlewares_Logs401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(gincommon.ProtectedMiddlewares(gincommon.Config{Logger: log, ServiceName: "pkg-test"})...)
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/p", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	if log.Level != "warn" || log.Msg != "auth: request rejected" {
		t.Fatalf("log = %s %q, want warn audit entry", log.Level, log.Msg)
	}
}
