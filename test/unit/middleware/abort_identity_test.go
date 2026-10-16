package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/codes"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// serveExpectingAbort serves req and returns the value that escaped ServeHTTP.
func serveExpectingAbort(r http.Handler, req *http.Request) (w *httptest.ResponseRecorder, rec any) {
	w = httptest.NewRecorder()
	func() {
		defer func() { rec = recover() }()
		r.ServeHTTP(w, req)
	}()
	return w, rec
}

func abortRouter(t *testing.T, svc string, logger *recordingLogger, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fixtures.InitMetricsWithRegisterer(svc, "test", prometheus.NewRegistry())
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(logger),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware("abort-test"),
		middleware.CorrelationHeadersMiddleware(),
		middleware.MetricsMiddleware(logger),
		middleware.LoggingMiddleware(logger),
	)
	r.GET("/abort", handler)
	return r
}

func TestObservability_ErrAbortHandler_NotAPanic(t *testing.T) {
	sr := spanRecorder(t)
	logger := &recordingLogger{}
	r := abortRouter(t, "svc-abort", logger, func(c *gin.Context) { panic(http.ErrAbortHandler) })

	_, rec := serveExpectingAbort(r, httptest.NewRequest(http.MethodGet, "/abort", nil))
	if rec != http.ErrAbortHandler {
		t.Fatalf("re-panicked value = %v, want http.ErrAbortHandler", rec)
	}

	if got := testutil.ToFloat64(metrics.HttpPanicTotal.WithLabelValues(http.MethodGet, "/abort")); got != 0 {
		t.Fatalf("panics_total = %v, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.HttpRequestsTotal.WithLabelValues(http.MethodGet, "/abort", "4xx", "client_error")); got != 1 {
		t.Fatalf("requests_total{4xx,client_error} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.HttpRequestsTotal.WithLabelValues(http.MethodGet, "/abort", "5xx", "server_error")); got != 0 {
		t.Fatalf("requests_total{5xx} = %v, want 0", got)
	}

	for _, e := range logger.entries {
		if e.Level == "error" {
			t.Fatalf("unexpected error log %q: %v", e.Msg, e.Fields)
		}
	}
	access := logger.byMsg("http_request")
	if len(access) != 1 || access[0].Level != "debug" || access[0].Fields["aborted"] != true ||
		access[0].Fields["status"] != middleware.StatusClientClosedRequest {
		t.Fatalf("access log = %+v, want one debug entry, aborted, status 499", access)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.Status().Code == codes.Error {
		t.Fatalf("span status = Error (%q), want unset for an abort", s.Status().Description)
	}
	aborted := false
	for _, kv := range s.Attributes() {
		if kv.Key == "http.aborted" && kv.Value.AsBool() {
			aborted = true
		}
	}
	if !aborted {
		t.Fatalf("http.aborted attribute missing: %v", s.Attributes())
	}
	if attrInt(s.Attributes(), "http.response.status_code") != middleware.StatusClientClosedRequest {
		t.Fatalf("status_code attr = %d, want 499", attrInt(s.Attributes(), "http.response.status_code"))
	}
	var event bool
	for _, ev := range s.Events() {
		if ev.Name == "http.aborted" {
			event = true
		}
		if ev.Name == "exception" {
			t.Fatal("abort must not be recorded as an exception")
		}
	}
	if !event {
		t.Fatal("http.aborted span event missing")
	}
}

func TestObservability_ErrAbortHandler_AfterWriteKeepsWrittenStatus(t *testing.T) {
	logger := &recordingLogger{}
	r := abortRouter(t, "svc-abort-written", logger, func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
		panic(http.ErrAbortHandler)
	})

	if _, rec := serveExpectingAbort(r, httptest.NewRequest(http.MethodGet, "/abort", nil)); rec != http.ErrAbortHandler {
		t.Fatalf("re-panicked value = %v", rec)
	}
	if got := testutil.ToFloat64(metrics.HttpRequestsTotal.WithLabelValues(http.MethodGet, "/abort", "2xx", "success")); got != 1 {
		t.Fatalf("requests_total{2xx} = %v, want 1 (status already written)", got)
	}
	if got := testutil.ToFloat64(metrics.HttpPanicTotal.WithLabelValues(http.MethodGet, "/abort")); got != 0 {
		t.Fatalf("panics_total = %v, want 0", got)
	}
}

func TestObservability_WrappedErrAbortHandler_NotAPanic(t *testing.T) {
	logger := &recordingLogger{}
	wrapped := errors.Join(errors.New("proxy copy failed"), http.ErrAbortHandler)
	r := abortRouter(t, "svc-abort-wrapped", logger, func(c *gin.Context) { panic(wrapped) })

	if _, rec := serveExpectingAbort(r, httptest.NewRequest(http.MethodGet, "/abort", nil)); rec != http.ErrAbortHandler {
		t.Fatalf("re-panicked value = %v, want the bare http.ErrAbortHandler (net/http compares by identity)", rec)
	}
	if got := testutil.ToFloat64(metrics.HttpPanicTotal.WithLabelValues(http.MethodGet, "/abort")); got != 0 {
		t.Fatalf("panics_total = %v, want 0", got)
	}
}

func TestObservability_RealPanicStillCounted(t *testing.T) {
	logger := &recordingLogger{}
	r := abortRouter(t, "svc-abort-real", logger, func(c *gin.Context) { panic("boom") })

	w, rec := serveExpectingAbort(r, httptest.NewRequest(http.MethodGet, "/abort", nil))
	if rec != nil || w.Code != http.StatusInternalServerError {
		t.Fatalf("rec = %v, code = %d, want recovered 500", rec, w.Code)
	}
	if got := testutil.ToFloat64(metrics.HttpPanicTotal.WithLabelValues(http.MethodGet, "/abort")); got != 1 {
		t.Fatalf("panics_total = %v, want 1", got)
	}
	if access := logger.byMsg("http_request"); len(access) != 1 || access[0].Level != "error" {
		t.Fatalf("access log = %+v, want one error entry", access)
	}
}

// ─── Repeated identity headers ───────────────────────────────────────────────

func TestRequireAuth_RepeatedIdentityHeader_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, h := range []string{domain.HeaderUserID, domain.HeaderTenantID} {
		t.Run(h, func(t *testing.T) {
			logger := &recordingLogger{}
			r := gin.New()
			r.Use(middleware.RequireAuth(logger))
			r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := authedRequest()
			req.Header.Add(h, "attacker")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			body, err := fixtures.DecodeProblem(w.Body.Bytes())
			want := "Ambiguous " + h + " header."
			if err != nil || body.Detail != want || !fixtures.IsProblemType(body.Type, problem.TypeUnauthorized) {
				t.Fatalf("body = %+v (%v), want unauthorized %q", body, err, want)
			}
			if e := logger.byMsg("auth: request rejected"); len(e) != 1 || e[0].Fields["reason"] != "ambiguous "+h+" header" {
				t.Fatalf("audit log = %+v", e)
			}
		})
	}
}

func TestContextMiddleware_RepeatedIdentityHeader_Rejected(t *testing.T) {
	req := authedRequest()
	req.Header.Add(domain.HeaderTenantID, "t2")
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{}).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if body, _ := fixtures.DecodeProblem(w.Body.Bytes()); body.Detail != "Ambiguous x-tenant-id header." {
		t.Fatalf("detail = %q", body.Detail)
	}
}

func TestContextMiddleware_RepeatedRolesHeadersJoined(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var roles []string
	r := gin.New()
	r.Use(middleware.ContextMiddleware(service.NewContextService(), nil))
	r.GET("/x", func(c *gin.Context) {
		v, _ := c.Get(middleware.CtxRequestContextKey)
		roles = v.(*domain.RequestContext).Roles
		c.Status(http.StatusOK)
	})

	req := authedRequest()
	req.Header.Add(domain.HeaderTenantRoles, "user")
	req.Header.Add(domain.HeaderTenantRoles, "admin,auditor")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if len(roles) != 3 || roles[0] != "user" || roles[1] != "admin" || roles[2] != "auditor" {
		t.Fatalf("roles = %v, want [user admin auditor]", roles)
	}
}

// ─── Verified identity only when the RequestContext was built ────────────────

func TestLoggingMiddleware_ContextRejectionLogsClaimedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &recordingLogger{}
	r := gin.New()
	r.Use(
		middleware.LoggingMiddleware(logger),
		middleware.RequireAuth(nil),
		middleware.ContextMiddleware(service.NewContextService(), nil),
	)
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := authedRequest()
	req.Header.Set(domain.HeaderTenantRoles, strings.Repeat("r", domain.MaxRolesHeaderLen+1))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	access := logger.byMsg("http_request")
	if len(access) != 1 {
		t.Fatalf("access entries = %d", len(access))
	}
	f := access[0].Fields
	if _, ok := f["tenant_id"]; ok {
		t.Fatalf("tenant_id logged as verified after ContextMiddleware rejected: %v", f)
	}
	if _, ok := f["user_id"]; ok {
		t.Fatalf("user_id logged as verified after ContextMiddleware rejected: %v", f)
	}
	if f["claimed_tenant_id"] != "t1" || f["claimed_user_id"] != "u1" {
		t.Fatalf("claimed identity = %v", f)
	}
}

func TestContextMiddleware_BuilderError_400WithAuditLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &recordingLogger{}
	r := gin.New()
	r.Use(middleware.ContextMiddleware(&mockCtxBuilder{err: errors.New("store\nunavailable")}, logger))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if body.Detail != "Invalid request context." || body.ErrorCode != "INVALID_REQUEST_CONTEXT" ||
		!fixtures.IsProblemType(body.Type, problem.TypeBadRequest) {
		t.Fatalf("problem = %+v, builder detail must not leak", body)
	}
	e := logger.byMsg("context: request rejected")
	if len(e) != 1 || e[0].Level != "warn" || e[0].Fields["status"] != http.StatusBadRequest {
		t.Fatalf("audit log = %+v, want one warn entry with status 400", e)
	}
	if reason, _ := e[0].Fields["reason"].(string); reason != "invalid request context: storeunavailable" {
		t.Fatalf("reason = %q, want sanitized builder error", reason)
	}
	if e[0].Fields["claimed_user_id"] != "u1" {
		t.Fatalf("claimed_user_id = %v", e[0].Fields["claimed_user_id"])
	}
}

func TestContextMiddleware_BuilderError_NilLogger(t *testing.T) {
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{err: errors.New("x")}).ServeHTTP(w, authedRequest())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ─── Timeout keeps a status-only response ────────────────────────────────────

func TestTimeoutMiddleware_StatusOnlyResponseNotOverwritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixtures.InitMetricsWithRegisterer("svc-timeout-204", "test", prometheus.NewRegistry())

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(time.Nanosecond))
	r.DELETE("/item", func(c *gin.Context) {
		<-c.Request.Context().Done()
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/item", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want the handler's 204", w.Code)
	}
	if got := testutil.ToFloat64(metrics.HttpRequestTimeoutTotal.WithLabelValues(http.MethodDelete, "/item")); got != 0 {
		t.Fatalf("timeouts = %v, want 0", got)
	}
}

func TestTimeoutMiddleware_NothingSet_Still503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixtures.InitMetricsWithRegisterer("svc-timeout-none", "test", prometheus.NewRegistry())

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(time.Nanosecond))
	r.GET("/slow", func(c *gin.Context) { <-c.Request.Context().Done() })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if got := testutil.ToFloat64(metrics.HttpRequestTimeoutTotal.WithLabelValues(http.MethodGet, "/slow")); got != 1 {
		t.Fatalf("timeouts = %v, want 1", got)
	}
}
