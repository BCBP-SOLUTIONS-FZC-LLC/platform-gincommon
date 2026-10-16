package middleware_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// logEntry / recordingLogger capture every call (fixtures.MockLogger keeps only the last).
type logEntry struct {
	Level  string
	Msg    string
	Fields map[string]interface{}
}

type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (l *recordingLogger) add(level, msg string, f map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level, msg, f})
}
func (l *recordingLogger) Debug(m string, f map[string]interface{}) { l.add("debug", m, f) }
func (l *recordingLogger) Info(m string, f map[string]interface{})  { l.add("info", m, f) }
func (l *recordingLogger) Warn(m string, f map[string]interface{})  { l.add("warn", m, f) }
func (l *recordingLogger) Error(m string, f map[string]interface{}) { l.add("error", m, f) }

func (l *recordingLogger) byMsg(msg string) []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []logEntry
	for _, e := range l.entries {
		if e.Msg == msg {
			out = append(out, e)
		}
	}
	return out
}

func spanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

// ─── Finding 1: correlation headers reach the client ─────────────────────────

func TestCorrelationHeaders_PresentWhenHandlerWritesJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTestTracer(t)

	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), middleware.TracingMiddleware(""), middleware.CorrelationHeadersMiddleware())
	var handlerTraceID string
	r.GET("/json", func(c *gin.Context) {
		handlerTraceID = c.GetString(middleware.CtxTraceIDKey)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/json", nil))

	res := w.Result()
	if got := res.Header.Get(domain.HeaderTraceIDResponse); got == "" || got != handlerTraceID {
		t.Fatalf("X-Trace-ID on the wire = %q, want %q", got, handlerTraceID)
	}
	if res.Header.Get(domain.HeaderRequestIDResponse) == "" {
		t.Fatal("X-Request-ID missing on the wire")
	}
}

// ─── Finding 2: timeout semantics ────────────────────────────────────────────

func TestTimeoutMiddleware_ClientCancel_NotTreatedAsTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-timeout-cancel", "test", reg)

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(10 * time.Second))
	r.GET("/cancel", func(c *gin.Context) { <-c.Request.Context().Done() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client already gone
	req := httptest.NewRequest(http.MethodGet, "/cancel", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusServiceUnavailable {
		t.Fatal("client cancellation must not produce a 503 timeout")
	}
	c, err := metrics.HttpRequestTimeoutTotal.GetMetricWithLabelValues(http.MethodGet, "/cancel")
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if got := testutil.ToFloat64(c); got != 0 {
		t.Fatalf("timeouts = %v, want 0 for a client cancel", got)
	}
}

func TestTimeoutMiddleware_InsideObservability_RecordedStatusMatchesClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-timeout-inside", "test", reg)
	sr := spanRecorder(t)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(logger),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware("timeout-test"),
		middleware.CorrelationHeadersMiddleware(),
		middleware.MetricsMiddleware(logger),
		middleware.LoggingMiddleware(logger),
		middleware.TimeoutMiddleware(time.Nanosecond),
	)
	r.GET("/slow-inside", func(c *gin.Context) { <-c.Request.Context().Done() })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow-inside", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("client status = %d, want 503", w.Code)
	}
	if s, _ := logger.Fields["status"].(int); s != http.StatusServiceUnavailable {
		t.Fatalf("logged status = %v, want 503", logger.Fields["status"])
	}
	c, err := metrics.HttpRequestsTotal.GetMetricWithLabelValues(http.MethodGet, "/slow-inside", "5xx", metrics.RequestOutcome(503))
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if got := testutil.ToFloat64(c); got != 1 {
		t.Fatalf("5xx requests = %v, want 1", got)
	}
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	for _, a := range spans[0].Attributes() {
		if a.Key == "http.response.status_code" && a.Value.AsInt64() != 503 {
			t.Fatalf("span status code = %d, want 503", a.Value.AsInt64())
		}
	}
}

// ─── Finding 4: bounded span method, truncated host ──────────────────────────

func TestTracingMiddleware_NonStandardMethodBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := spanRecorder(t)

	r := gin.New()
	r.Use(middleware.TracingMiddleware("method-test"))
	r.Handle("PURGEEVERYTHINGNOW", "/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("PURGEEVERYTHINGNOW", "/x", nil)
	req.Host = strings.Repeat("h", 1000)
	r.ServeHTTP(httptest.NewRecorder(), req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if got := spans[0].Name(); got != "HTTP /x" {
		t.Fatalf("span name = %q, want \"HTTP /x\"", got)
	}
	attrs := map[attribute.Key]attribute.Value{}
	for _, a := range spans[0].Attributes() {
		attrs[a.Key] = a.Value
	}
	if got := attrs["http.request.method"].AsString(); got != "_OTHER" {
		t.Fatalf("http.request.method = %q, want _OTHER", got)
	}
	if got := attrs["http.request.method_original"].AsString(); got != "PURGEEVERYTHINGNOW" {
		t.Fatalf("method_original = %q", got)
	}
	if got := attrs["server.address"].AsString(); len(got) != 256 {
		t.Fatalf("server.address len = %d, want 256", len(got))
	}
}

func TestTracingMiddleware_StandardMethodUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := spanRecorder(t)

	r := gin.New()
	r.Use(middleware.TracingMiddleware("method-test"))
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x", nil))

	spans := sr.Ended()
	if len(spans) != 1 || spans[0].Name() != "POST /x" {
		t.Fatalf("unexpected spans %v", spans)
	}
	for _, a := range spans[0].Attributes() {
		if a.Key == "http.request.method_original" {
			t.Fatal("method_original must only be set for non-standard methods")
		}
	}
}

func TestTracingMiddleware_PanicDescriptionSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := spanRecorder(t)

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(&fixtures.MockLogger{}), middleware.TracingMiddleware("panic-test"))
	r.GET("/p", func(c *gin.Context) { panic("line1\nline2" + strings.Repeat("x", 600)) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))

	spans := sr.Ended()
	if len(spans) != 1 || len(spans[0].Events()) == 0 {
		t.Fatal("expected one span with an exception event")
	}
	for _, a := range spans[0].Events()[0].Attributes {
		if a.Key == "exception.message" {
			msg := a.Value.AsString()
			if strings.Contains(msg, "\n") || len(msg) > 256 {
				t.Fatalf("exception.message not sanitized: len=%d", len(msg))
			}
			return
		}
	}
	t.Fatal("exception.message attribute missing")
}

// ─── Finding 6: panic recovery ───────────────────────────────────────────────

func TestPanicRecovery_PartialBodyNotAppendedTo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(logger))
	r.GET("/partial", func(c *gin.Context) {
		c.String(http.StatusOK, "partial")
		panic("after write")
	})

	w := httptest.NewRecorder()
	var rec any
	func() {
		defer func() { rec = recover() }()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/partial", nil))
	}()

	// The response is already on the wire: the connection must be aborted (bare
	// http.ErrAbortHandler) so the client sees truncation, not a complete 200.
	if rec != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler", rec)
	}
	if got := w.Body.String(); got != "partial" {
		t.Fatalf("body = %q, want only the partial write", got)
	}
	if logger.Level != "error" || logger.Msg != "panic recovered" {
		t.Fatalf("panic not logged: %s %q", logger.Level, logger.Msg)
	}
}

func TestPanicRecovery_AbortedBeforePanic_Still500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(nil))
	r.GET("/abort", func(c *gin.Context) {
		c.Abort()
		panic("after abort")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/abort", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestPanicRecovery_ErrAbortHandler_RepanicsAndLogsDebug(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(logger))
	r.GET("/abort", func(c *gin.Context) { panic(http.ErrAbortHandler) })

	defer func() {
		rec := recover()
		err, ok := rec.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want http.ErrAbortHandler re-panicked", rec)
		}
		if logger.Level != "debug" || logger.Msg != "request aborted" {
			t.Fatalf("log = %s %q, want debug \"request aborted\"", logger.Level, logger.Msg)
		}
		if _, hasStack := logger.Fields["stack"]; hasStack {
			t.Fatal("aborted request must not log a stack")
		}
	}()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
	t.Fatal("expected re-panic")
}

func TestPanicRecovery_ErrAbortHandler_NilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(nil))
	r.GET("/abort", func(c *gin.Context) { panic(http.ErrAbortHandler) })

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", rec)
		}
	}()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
}

func TestPanicRecovery_StderrFallbackSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	r := gin.New()
	r.Use(middleware.PanicRecoveryMiddleware(nil))
	r.GET("/p", func(c *gin.Context) { panic("evil\nFAKE LOG LINE") })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))

	first := strings.SplitN(buf.String(), "\n", 2)[0]
	if first != "panic recovered: evilFAKE LOG LINE" {
		t.Fatalf("first stderr line = %q, want sanitized panic value", first)
	}
}

// ─── Finding 7: sampled 429 log ──────────────────────────────────────────────

func TestMetricsMiddleware_RateLimitedLogSampled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-429-sampled", "test", reg)
	logger := &recordingLogger{}

	r := gin.New()
	r.Use(middleware.MetricsMiddleware(logger))
	r.GET("/limited-sampled", func(c *gin.Context) { c.Status(http.StatusTooManyRequests) })

	hit := func() {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/limited-sampled", nil))
	}
	for range 5 {
		hit()
	}

	if got := len(logger.byMsg("rate limited")); got != 1 {
		t.Fatalf("rate-limited log entries = %d, want 1 within the interval", got)
	}
	c, err := metrics.RateLimitedRequestsTotal.GetMetricWithLabelValues(http.MethodGet, "/limited-sampled")
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if got := testutil.ToFloat64(c); got != 5 {
		t.Fatalf("rate-limited counter = %v, want every 429 counted (5)", got)
	}

	time.Sleep(middleware.RateLimitLogInterval + 50*time.Millisecond)
	hit()
	entries := logger.byMsg("rate limited")
	if len(entries) != 2 {
		t.Fatalf("entries after interval = %d, want 2", len(entries))
	}
	if got := entries[1].Fields["suppressed"]; got != int64(4) {
		t.Fatalf("suppressed = %v, want 4", got)
	}
}

// ─── Finding 8: verified vs claimed identity in access log ───────────────────

func TestLoggingMiddleware_VerifiedIdentityFromRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(
		middleware.LoggingMiddleware(logger),
		middleware.RequireAuth(nil),
		middleware.ContextMiddleware(service.NewContextService(), nil),
	)
	r.GET("/me", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	r.ServeHTTP(httptest.NewRecorder(), req)

	if logger.Fields["user_id"] != "user-1" || logger.Fields["tenant_id"] != "tenant-1" {
		t.Fatalf("verified identity missing: %v", logger.Fields)
	}
	if _, ok := logger.Fields["claimed_user_id"]; ok {
		t.Fatal("claimed_user_id must not be logged for verified identity")
	}
}

func TestLoggingMiddleware_IdentityValidatedByRequireAuthOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.LoggingMiddleware(logger), middleware.RequireAuth(nil))
	r.GET("/me", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	r.ServeHTTP(httptest.NewRecorder(), req)

	if logger.Fields["user_id"] != "user-1" || logger.Fields["tenant_id"] != "tenant-1" {
		t.Fatalf("RequireAuth-validated identity missing: %v", logger.Fields)
	}
}

func TestLoggingMiddleware_NoIdentityFieldsWithoutHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.LoggingMiddleware(logger))
	r.GET("/public", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/public", nil))

	for _, k := range []string{"user_id", "tenant_id", "claimed_user_id", "claimed_tenant_id"} {
		if _, ok := logger.Fields[k]; ok {
			t.Fatalf("unexpected %s field: %v", k, logger.Fields)
		}
	}
}

func TestLoggingMiddleware_RejectedRequestLogsClaimedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &recordingLogger{}

	r := gin.New()
	r.Use(middleware.LoggingMiddleware(logger), middleware.RequireAuth(nil))
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set(domain.HeaderUserID, "spoofed-user") // tenant missing → 401
	r.ServeHTTP(httptest.NewRecorder(), req)

	e := logger.byMsg("http_request")
	if len(e) != 1 {
		t.Fatalf("http_request entries = %d", len(e))
	}
	if e[0].Fields["claimed_user_id"] != "spoofed-user" {
		t.Fatalf("claimed_user_id = %v", e[0].Fields["claimed_user_id"])
	}
	if _, ok := e[0].Fields["user_id"]; ok {
		t.Fatal("unverified identity must not be logged as user_id")
	}
}

// ─── Finding 9: 401 audit log ────────────────────────────────────────────────

func TestRequireAuth_LogsRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name, user, tenant, reason string
	}{
		{"missing user", "", "tenant-1", "missing or invalid x-user-id"},
		{"missing tenant", "user-1", "", "missing or invalid x-tenant-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := &fixtures.MockLogger{}
			r := gin.New()
			r.Use(middleware.RequestIDMiddleware(), middleware.RequireAuth(logger))
			r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/p", nil)
			if tc.user != "" {
				req.Header.Set(domain.HeaderUserID, tc.user)
			}
			if tc.tenant != "" {
				req.Header.Set(domain.HeaderTenantID, tc.tenant)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", w.Code)
			}
			if logger.Level != "warn" || logger.Msg != "auth: request rejected" {
				t.Fatalf("log = %s %q", logger.Level, logger.Msg)
			}
			if logger.Fields["reason"] != tc.reason || logger.Fields["status"] != http.StatusUnauthorized {
				t.Fatalf("fields = %v", logger.Fields)
			}
			if logger.Fields["request_id"] == "" {
				t.Fatal("request_id missing from audit entry")
			}
		})
	}
}

func TestRequireAuth_LogSanitizesClaimedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.RequireAuth(logger))
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set(domain.HeaderUserID, strings.Repeat("u", 1000)) // too long → invalid
	req.Header.Set(domain.HeaderTenantID, "tenant-1")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if s, _ := logger.Fields["claimed_user_id"].(string); len(s) != 256 {
		t.Fatalf("claimed_user_id len = %d, want truncated to 256", len(s))
	}
	if logger.Fields["claimed_tenant_id"] != "tenant-1" {
		t.Fatalf("claimed_tenant_id = %v", logger.Fields["claimed_tenant_id"])
	}
}

// ─── Finding 5: x-tenant-roles caps ──────────────────────────────────────────

func TestContextMiddleware_TooManyRoles_401AndLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.RequireAuth(nil), middleware.ContextMiddleware(service.NewContextService(), logger))
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

	roles := make([]string, domain.MaxRoles+1)
	for i := range roles {
		roles[i] = fmt.Sprintf("role%d", i)
	}
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	req.Header.Set(domain.HeaderTenantRoles, strings.Join(roles, ","))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if body.Detail != "Invalid tenant roles." {
		t.Fatalf("detail = %q", body.Detail)
	}
	if logger.Fields["reason"] != "invalid x-tenant-roles" {
		t.Fatalf("audit reason = %v", logger.Fields["reason"])
	}
}

func TestContextMiddleware_MissingIdentity_Logged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &fixtures.MockLogger{}

	r := gin.New()
	r.Use(middleware.ContextMiddleware(service.NewContextService(), logger))
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/p", nil))

	if w.Code != http.StatusUnauthorized || logger.Msg != "auth: request rejected" {
		t.Fatalf("status = %d, log = %q", w.Code, logger.Msg)
	}
}

// ─── Finding 12: Authorizer receives ctx and the full RequestContext ─────────

type capturingAuthorizer struct {
	ctx context.Context
	rc  *domain.RequestContext
}

func (a *capturingAuthorizer) Authorize(ctx context.Context, rc *domain.RequestContext, _, _ string) error {
	a.ctx, a.rc = ctx, rc
	return nil
}

type ctxKey struct{}

func TestAuthZMiddleware_PassesContextAndTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &capturingAuthorizer{}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxKey{}, "marker"))
		c.Next()
	})
	r.Use(middleware.RequireAuth(nil), middleware.ContextMiddleware(service.NewContextService(), nil))
	r.GET("/doc", middleware.AuthZMiddleware(auth, "read", "doc", nil), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/doc", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if auth.ctx == nil || auth.ctx.Value(ctxKey{}) != "marker" {
		t.Fatal("Authorize did not receive the request context")
	}
	if auth.rc == nil || auth.rc.TenantID != "tenant-1" || auth.rc.UserID != "user-1" {
		t.Fatalf("Authorize rc = %+v", auth.rc)
	}
}
