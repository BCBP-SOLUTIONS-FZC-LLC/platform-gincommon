package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	otelsetup "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type errAuthorizer struct{}

func (errAuthorizer) Authorize(string, []string, string, string) error {
	return errors.New("authorize failed")
}

func TestRequireAuth_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.RequireAuth())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestTracingMiddleware_NonZeroTraceIDAfterOtelInitWithoutExporter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	shutdown := otelsetup.Init("svc-trace", "test", "")
	defer shutdown()

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.TracingMiddleware(), middleware.LoggingMiddleware(log))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	traceID, _ := log.Fields["trace_id"].(string)
	if traceID == "00000000000000000000000000000000" {
		t.Fatalf("trace_id = %q, want valid OpenTelemetry trace ID after Init", traceID)
	}
	if !isValidOtelTraceID(traceID) {
		t.Fatalf("trace_id = %q is not valid", traceID)
	}
}

func TestLoggingMiddleware_LogsRequestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTestTracer(t)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.TracingMiddleware(), middleware.RequestIDMiddleware(), middleware.LoggingMiddleware(log))
	r.GET("/log", func(c *gin.Context) { c.Status(http.StatusCreated) })

	req := httptest.NewRequest(http.MethodGet, "/log", nil)
	req.Header.Set("x-request-id", "req-1")
	req.Header.Set("x-tenant-id", "tenant-1")
	req.Header.Set("x-user-id", "user-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if log.Msg != "http_request" {
		t.Fatalf("msg = %q", log.Msg)
	}
	if log.Fields["tenant_id"] != "tenant-1" || log.Fields["user_id"] != "user-1" {
		t.Fatalf("fields = %v", log.Fields)
	}
	if log.Fields["path"] != "/log" {
		t.Fatalf("path = %v", log.Fields["path"])
	}
	if log.Fields["status"] != http.StatusCreated {
		t.Fatalf("status = %v", log.Fields["status"])
	}
	if log.Fields["request_id"] != "req-1" {
		t.Fatalf("request_id = %v", log.Fields["request_id"])
	}
	traceID, _ := log.Fields["trace_id"].(string)
	if !isValidOtelTraceID(traceID) {
		t.Fatalf("trace_id = %v, want valid OpenTelemetry trace ID", log.Fields["trace_id"])
	}
	if log.Fields["duration"] == "" {
		t.Fatal("expected duration in log fields")
	}
}

func TestPanicRecoveryMiddleware_RecordsPanicAndReturns500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-panic", "test")

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(log),
		middleware.RequestIDMiddleware(),
		middleware.MetricsMiddleware(nil),
	)
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
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
	if status, ok := body["status"].(float64); !ok || int(status) != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %d", body["status"], http.StatusInternalServerError)
	}
	if body["request_id"] == "" || body["request_id"] == nil {
		t.Fatalf("request_id = %v, want non-empty", body["request_id"])
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id response header")
	}
	if log.Level != "error" || log.Msg != "panic recovered" {
		t.Fatalf("log = %q %q, want error panic recovered", log.Level, log.Msg)
	}
	if log.Fields["panic"] != "boom" {
		t.Fatalf("log panic = %v", log.Fields["panic"])
	}
	if log.Fields["request_id"] == nil || log.Fields["request_id"] == "" {
		t.Fatalf("log request_id = %v", log.Fields["request_id"])
	}

	counter, err := metrics.HttpPanicTotal.GetMetricWithLabelValues(http.MethodGet, "/panic")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_panic_total = %v, want 1", got)
	}
}

func TestPanicRecoveryMiddleware_NilLogger_ReturnsJSON500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(nil), middleware.RequestIDMiddleware())
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
}

func TestPanicRecoveryMiddleware_CorrelationHeadersMatchBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(log),
		middleware.RequestIDMiddleware(),
	)
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	body, err := fixtures.DecodeErrorJSON(w.Body.Bytes())
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RequestID != w.Header().Get("X-Request-Id") {
		t.Fatalf("body request_id %q != header %q", body.RequestID, w.Header().Get("X-Request-Id"))
	}
	if body.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", body.Status, http.StatusInternalServerError)
	}
}

func TestPanicRecoveryMiddleware_WithTracing_IncludesTraceIDInResponseAndLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTestTracer(t)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(log),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware(),
		middleware.MetricsMiddleware(nil),
	)
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	body, err := fixtures.DecodeErrorJSON(w.Body.Bytes())
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !isValidOtelTraceID(body.TraceID) {
		t.Fatalf("trace_id = %q, want valid non-zero OTel ID", body.TraceID)
	}
	if body.TraceID != w.Header().Get("X-Trace-Id") {
		t.Fatalf("body trace_id %q != header %q", body.TraceID, w.Header().Get("X-Trace-Id"))
	}
	if log.Fields["trace_id"] != body.TraceID {
		t.Fatalf("log trace_id = %v, want %q", log.Fields["trace_id"], body.TraceID)
	}
	if log.Fields["path"] != "/panic" {
		t.Fatalf("log path = %v, want /panic", log.Fields["path"])
	}
}

func TestPanicRecoveryMiddleware_WithTracing_SetsSpanErrorOnPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(log),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware(),
	)
	r.GET("/panic", func(c *gin.Context) { panic("handler failure") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	st := spans[0].Status()
	if st.Code != codes.Error {
		t.Fatalf("span status code = %v, want Error", st.Code)
	}
	if st.Description != "panic" {
		t.Fatalf("span status description = %q, want panic", st.Description)
	}
	if len(spans[0].Events()) == 0 {
		t.Fatal("expected span error event after panic")
	}
}

func TestTracingMiddleware_SetsSpanAttributesOnPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(nil),
		middleware.TracingMiddleware(),
	)
	r.GET("/panic-attrs", func(c *gin.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic-attrs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	attrs := spans[0].Attributes()
	if got := attrValue(attrs, "http.route"); got == "" {
		t.Fatal("expected http.route attribute on panicked span")
	}
	if got := attrValue(attrs, "url.path"); got == "" {
		t.Fatal("expected url.path attribute on panicked span")
	}
	if got := attrInt(attrs, "http.response.status_code"); got == 0 {
		t.Fatalf("expected http.response.status_code on panicked span, got %d", got)
	}
	st := spans[0].Status()
	if st.Code != codes.Error {
		t.Fatalf("span status code = %v, want Error", st.Code)
	}
	if st.Description != "panic" {
		t.Fatalf("span status description = %q, want panic", st.Description)
	}
}

func TestMetricsMiddleware_RecordsRateLimitedOn429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-rl", "test")

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusTooManyRequests)
	})

	req := httptest.NewRequest(http.MethodGet, "/limited", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", w.Code)
	}

	counter, err := metrics.RateLimitedRequestsTotal.GetMetricWithLabelValues(http.MethodGet, "/limited")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("rate_limited_requests_total = %v, want 1", got)
	}
}

func TestMetricsMiddleware_KnownRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-metrics", "test")

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/known", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/known", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestMetricsMiddleware_RecordsPayloadSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-size", "test")

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.POST("/upload", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	body := "hello"
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected response body")
	}
}

func TestMetricsMiddleware_NoBodyResponseSizeNonNegative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-nobody", "test")

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/empty", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	w := httptest.NewRecorder()
	// Should not panic; gin.ResponseWriter.Size() returns -1 when no body is written.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	// Verify the request counter was incremented (confirms the middleware ran without panic).
	counter, err := metrics.HttpRequestsTotal.GetMetricWithLabelValues(
		http.MethodGet, "/empty", "2xx", "success",
	)
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_requests_total = %v, want 1", got)
	}
}

func TestMetricsMiddleware_NilSafe_WithoutInit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	// Should not panic even though metrics.Init has not been called in this Prometheus registry.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestMetricsMiddleware_UnknownRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics.Init("svc-unknown", "test")

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })

	req := httptest.NewRequest(http.MethodPost, "/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}

	counter, err := metrics.HttpRequestsTotal.GetMetricWithLabelValues(
		http.MethodPost, "/missing", "4xx", "client_error",
	)
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_requests_total unregistered route = %v, want 1", got)
	}
}

func TestAuthZMiddleware_ContextMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	auth := service.NewRBACService()
	r := gin.New()
	// AuthZMiddleware without ContextMiddleware → no request context in gin context
	r.Use(middleware.AuthZMiddleware(auth, "read", "doc", nil))
	r.GET("/doc", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (context missing)", w.Code)
	}
}

func TestAuthZMiddleware_ContextWrongType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	auth := service.NewRBACService()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxRequestContextKey, "wrong-type")
		c.Next()
	})
	r.Use(middleware.AuthZMiddleware(auth, "read", "doc", nil))
	r.GET("/doc", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (wrong context type)", w.Code)
	}
}

func TestMetricsMiddleware_NilActiveRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := metrics.HttpActiveRequests
	metrics.HttpActiveRequests = nil
	defer func() { metrics.HttpActiveRequests = saved }()

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func authzTestRouter(t *testing.T, auth port.Authorizer, action, resource, method, path string, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	r := gin.New()
	r.Use(
		middleware.RequireAuth(),
		middleware.ContextMiddleware(service.NewContextService()),
		middleware.AuthZMiddleware(auth, action, resource, nil),
	)
	r.Handle(method, path, handler)
	return r
}

func TestAuthZMiddleware_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	auth := service.NewRBACService()
	r := authzTestRouter(t, auth, "delete", "doc", http.MethodDelete, "/doc",
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	req := httptest.NewRequest(http.MethodDelete, "/doc", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")
	req.Header.Set("x-tenant-roles", "user")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAuthZMiddleware_Allowed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	auth := service.NewRBACService()
	r := authzTestRouter(t, auth, "read", "doc", http.MethodGet, "/doc",
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")
	req.Header.Set("x-tenant-roles", "user")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAuthZMiddleware_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := authzTestRouter(t, errAuthorizer{}, "read", "doc", http.MethodGet, "/doc",
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestRequestIDMiddleware_PropagatesHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.RequestIDMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("x-request-id", "rid-123")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Header().Get("x-request-id") != "rid-123" {
		t.Fatalf("header = %q", w.Header().Get("x-request-id"))
	}
}

func TestTracingMiddleware_SetsOtelTraceIDInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTestTracer(t)

	r := gin.New()
	r.Use(middleware.TracingMiddleware())
	r.GET("/x", func(c *gin.Context) {
		if !isValidOtelTraceID(c.GetString("trace_id")) {
			t.Fatalf("trace_id = %q, want valid OpenTelemetry trace ID", c.GetString("trace_id"))
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestTracingMiddleware_SetsSpanAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := gin.New()
	r.Use(middleware.TracingMiddleware())
	r.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/items/42", nil)
	req.Host = "api.example.com"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	span := spans[0]
	if span.SpanKind() != trace.SpanKindServer {
		t.Fatalf("span kind = %v, want Server", span.SpanKind())
	}
	if span.Name() != "GET /items/:id" {
		t.Fatalf("span name = %q, want GET /items/:id", span.Name())
	}

	attrs := span.Attributes()
	if got := attrValue(attrs, "http.request.method"); got != http.MethodGet {
		t.Fatalf("http.request.method = %q, want GET", got)
	}
	if got := attrValue(attrs, "http.route"); got != "/items/:id" {
		t.Fatalf("http.route = %q, want /items/:id", got)
	}
	if got := attrValue(attrs, "url.path"); got != "/items/42" {
		t.Fatalf("url.path = %q, want /items/42", got)
	}
	if got := attrInt(attrs, "http.response.status_code"); got != http.StatusOK {
		t.Fatalf("http.response.status_code = %d, want 200", got)
	}
	if got := attrValue(attrs, "server.address"); got != "api.example.com" {
		t.Fatalf("server.address = %q, want api.example.com", got)
	}
	if attrValue(attrs, "client.address") == "" {
		t.Fatal("expected client.address attribute")
	}
}

func TestTracingMiddleware_SetsSpanErrorOn5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := gin.New()
	r.Use(middleware.TracingMiddleware())
	r.GET("/fail", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	st := spans[0].Status()
	if st.Code != codes.Error {
		t.Fatalf("span status code = %v, want Error", st.Code)
	}
	wantDesc := http.StatusText(http.StatusInternalServerError)
	if st.Description != wantDesc {
		t.Fatalf("span status description = %q, want %q", st.Description, wantDesc)
	}
	if got := attrInt(spans[0].Attributes(), "http.response.status_code"); got != http.StatusInternalServerError {
		t.Fatalf("http.response.status_code = %d, want 500", got)
	}
}

func TestTracingMiddleware_PropagatesW3CTraceContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const wantTraceID = "4bf92f3577b34da6a3ce929d0e0e4737"

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var ginTraceID string
	r := gin.New()
	r.Use(middleware.TracingMiddleware())
	r.GET("/x", func(c *gin.Context) {
		ginTraceID = c.GetString("trace_id")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", "00-"+wantTraceID+"-00f067aa0ba902b7-01")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	spanTraceID := spans[0].SpanContext().TraceID().String()
	if spanTraceID != wantTraceID {
		t.Fatalf("span trace_id = %q, want %q", spanTraceID, wantTraceID)
	}
	if ginTraceID != wantTraceID {
		t.Fatalf("gin trace_id = %q, want %q", ginTraceID, wantTraceID)
	}
}

func TestTracingMiddleware_TraceIDMatchesSpanContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var ginTraceID string
	r := gin.New()
	r.Use(middleware.TracingMiddleware())
	r.GET("/x", func(c *gin.Context) {
		ginTraceID = c.GetString("trace_id")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	spanTraceID := spans[0].SpanContext().TraceID().String()
	if ginTraceID != spanTraceID {
		t.Fatalf("gin trace_id = %q, span context trace_id = %q", ginTraceID, spanTraceID)
	}
	if !isValidOtelTraceID(ginTraceID) {
		t.Fatalf("trace_id %q is not a valid OTel trace ID", ginTraceID)
	}
}

func attrValue(attrs []attribute.KeyValue, key string) string {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func attrInt(attrs []attribute.KeyValue, key string) int64 {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsInt64()
		}
	}
	return 0
}

func useTestTracer(t *testing.T) {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func isValidOtelTraceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return id != "00000000000000000000000000000000"
}
