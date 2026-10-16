package gincommon_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

type logEntry struct {
	level, msg string
	fields     map[string]any
}

// problemLogger records every entry (fixtures.MockLogger keeps only the last).
type problemLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (l *problemLogger) add(level, msg string, f map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level, msg, f})
}
func (l *problemLogger) Debug(m string, f map[string]any) { l.add("debug", m, f) }
func (l *problemLogger) Info(m string, f map[string]any)  { l.add("info", m, f) }
func (l *problemLogger) Warn(m string, f map[string]any)  { l.add("warn", m, f) }
func (l *problemLogger) Error(m string, f map[string]any) { l.add("error", m, f) }

func (l *problemLogger) problems() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []logEntry
	for _, e := range l.entries {
		if e.msg == "problem response" {
			out = append(out, e)
		}
	}
	return out
}

type createTender struct {
	Title    string    `json:"title" binding:"required"`
	Deadline time.Time `json:"deadline" binding:"required"`
	Budget   int       `json:"budget" binding:"gte=1"`
	Stage    string    `json:"stage" binding:"omitempty,oneof=draft open"`
	Items    []struct {
		SKU string `json:"sku" binding:"required,max=8"`
	} `json:"items" binding:"dive"`
}

var tenderNotFound = problem.MustType("tender", "not-found", "Tender not found", http.StatusNotFound, "TENDER_NOT_FOUND")

func problemRouter(t *testing.T, cfg gincommon.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if cfg.ServiceName == "" {
		cfg.ServiceName = "problem-test"
	}
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gincommon.ObservabilityMiddlewares(cfg)...)

	r.GET("/public/conflict", func(c *gin.Context) { c.Status(http.StatusConflict) })
	r.GET("/public/written", func(c *gin.Context) {
		c.JSON(http.StatusTeapot, gin.H{"custom": true})
		_ = c.Error(errors.New("after write"))
	})
	r.GET("/public/canceled", func(c *gin.Context) { _ = c.Error(context.Canceled) })
	r.GET("/public/sql", func(c *gin.Context) {
		_ = c.Error(errors.New(`pq: duplicate key value violates unique constraint "users_email_key" (alice@example.com)`))
	})
	r.GET("/public/unsupported", func(c *gin.Context) { _ = c.Error(errors.ErrUnsupported) })
	r.GET("/public/deadline", func(c *gin.Context) { _ = c.Error(context.DeadlineExceeded) })
	r.POST("/public/bind", func(c *gin.Context) {
		var req createTender
		if err := gincommon.ShouldBindJSON(c, &req); err != nil {
			gincommon.AbortWithProblem(c, err)
			return
		}
		c.Status(http.StatusCreated)
	})
	r.POST("/public/raw-bind", func(c *gin.Context) {
		var req createTender
		if err := c.ShouldBindJSON(&req); err != nil {
			_ = c.Error(err) // validator errors are recognised without BindingError
			return
		}
		c.Status(http.StatusCreated)
	})
	r.POST("/public/bind-typed", func(c *gin.Context) {
		var req createTender
		if err := c.ShouldBindJSON(&req); err != nil {
			_ = c.Error(err).SetType(gin.ErrorTypeBind)
			return
		}
		c.Status(http.StatusCreated)
	})

	api := r.Group("/api", gincommon.ProtectedMiddlewares(cfg)...)
	api.GET("/tenders/:id", func(c *gin.Context) {
		_ = c.Error(problem.NewError(tenderNotFound, "Tender "+c.Param("id")+" was not found."))
	})
	api.GET("/panic", func(c *gin.Context) { panic("secret: db password hunter2") })
	api.DELETE("/items/:id", gincommon.RequirePermission("delete", "items", gincommon.NewDemoRBAC()), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return r
}

func serve(r http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

var identity = map[string]string{"x-user-id": "u1", "x-tenant-id": "tenant-001", "x-tenant-roles": "user"}

var timestampRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// mustProblem asserts the RFC 9457 contract every error response must meet and
// returns the decoded problem.
func mustProblem(t *testing.T, w *httptest.ResponseRecorder, status int, typ problem.Type) problem.Problem {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body %s", w.Code, status, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Fatalf("Content-Type = %q, want %s", ct, problem.ContentType)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for _, k := range []string{"type", "title", "status", "instance", "traceId", "errorCode", "timestamp"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("member %q missing from %s", k, w.Body.String())
		}
	}
	p, err := fixtures.DecodeProblem(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != status {
		t.Fatalf("body status = %d, want %d", p.Status, status)
	}
	if want := typ.URI(""); typ.ID() != "" && p.Type != want {
		t.Fatalf("type = %q, want %q", p.Type, want)
	}
	if p.Title == "" || !timestampRE.MatchString(p.Timestamp) {
		t.Fatalf("title %q / timestamp %q invalid", p.Title, p.Timestamp)
	}
	return p
}

func TestProblem_DomainErrorViaCError(t *testing.T) {
	logs := &problemLogger{}
	r := problemRouter(t, gincommon.Config{Logger: logs})
	w := serve(r, http.MethodGet, "/api/tenders/123?token=abc", "", identity)

	p := mustProblem(t, w, http.StatusNotFound, tenderNotFound)
	if p.Type != "https://errors.company.com/tender/not-found" || p.Title != "Tender not found" {
		t.Fatalf("type/title = %q / %q", p.Type, p.Title)
	}
	if p.Detail != "Tender 123 was not found." || p.ErrorCode != "TENDER_NOT_FOUND" {
		t.Fatalf("detail/code = %q / %q", p.Detail, p.ErrorCode)
	}
	if p.Instance != "/api/tenders/123" {
		t.Fatalf("instance = %q, want the path without the query string", p.Instance)
	}
	if p.TenantID != "tenant-001" {
		t.Fatalf("tenantId = %q, want the verified tenant", p.TenantID)
	}
	if strings.Contains(p.Title+p.Detail, "tenant-001") {
		t.Fatal("tenant must never appear in title or detail")
	}

	pl := logs.problems()
	if len(pl) != 1 || pl[0].level != "warn" {
		t.Fatalf("problem log entries = %+v, want one warn", pl)
	}
	f := pl[0].fields
	if f["traceId"] != p.TraceID || f["type"] != p.Type || f["status"] != 404 ||
		f["errorCode"] != "TENDER_NOT_FOUND" || f["service"] != "problem-test" || f["tenantId"] != "tenant-001" {
		t.Fatalf("problem log fields = %v", f)
	}
	for _, k := range []string{"detail", "instance", "user_id"} {
		if _, ok := f[k]; ok {
			t.Fatalf("problem log must not carry %q: %v", k, f)
		}
	}
}

func TestProblem_UnauthorizedHasNoTenant(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/api/tenders/1", "", map[string]string{"x-tenant-id": "tenant-001"})
	p := mustProblem(t, w, http.StatusUnauthorized, problem.TypeUnauthorized)
	if p.TenantID != "" {
		t.Fatalf("tenantId = %q on a 401: the identity was not verified", p.TenantID)
	}
	if p.Detail != "Missing or invalid authentication headers." || p.ErrorCode != "UNAUTHORIZED" {
		t.Fatalf("detail/code = %q / %q", p.Detail, p.ErrorCode)
	}
}

func TestProblem_ForbiddenFromRequirePermission(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodDelete, "/api/items/9", "", identity)
	p := mustProblem(t, w, http.StatusForbidden, problem.TypeForbidden)
	if p.ErrorCode != "FORBIDDEN" || p.TenantID != "tenant-001" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestProblem_PanicNeverLeaks(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/api/panic", "", identity)
	p := mustProblem(t, w, http.StatusInternalServerError, problem.TypeInternalError)
	if p.Title != "Internal Server Error" || p.Detail != "" || p.ErrorCode != "INTERNAL_ERROR" {
		t.Fatalf("problem = %+v", p)
	}
	if strings.Contains(w.Body.String(), "hunter2") || strings.Contains(w.Body.String(), "goroutine") {
		t.Fatalf("panic text leaked: %s", w.Body.String())
	}
}

func TestProblem_UnclassifiedErrorNeverLeaks(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/public/sql", "", nil)
	mustProblem(t, w, http.StatusInternalServerError, problem.TypeInternalError)
	for _, s := range []string{"pq:", "users_email_key", "alice@example.com", "detail"} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatalf("internal detail %q leaked: %s", s, w.Body.String())
		}
	}
}

func TestProblem_StatusMapping(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	cases := []struct {
		name, method, path string
		status             int
		typ                problem.Type
		code               string
	}{
		{"bare status", http.MethodGet, "/public/conflict", 409, problem.TypeConflict, "CONFLICT"},
		{"unsupported", http.MethodGet, "/public/unsupported", 501, problem.TypeNotImplemented, "NOT_IMPLEMENTED"},
		{"deadline", http.MethodGet, "/public/deadline", 503, problem.TypeServiceUnavailable, "REQUEST_TIMEOUT"},
		{"unknown route", http.MethodGet, "/no/such/route", 404, problem.TypeNotFound, "ROUTE_NOT_FOUND"},
		{"wrong method", http.MethodPut, "/public/conflict", 405, problem.TypeMethodNotAllowed, "METHOD_NOT_ALLOWED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(r, tc.method, tc.path, "", nil)
			if p := mustProblem(t, w, tc.status, tc.typ); p.ErrorCode != tc.code {
				t.Fatalf("errorCode = %q, want %q", p.ErrorCode, tc.code)
			}
		})
	}
}

func TestProblem_WrittenResponseUntouched(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/public/written", "", nil)
	if w.Code != http.StatusTeapot || w.Body.String() != `{"custom":true}` {
		t.Fatalf("handler response changed: %d %s", w.Code, w.Body.String())
	}
}

func TestProblem_ClientCanceledHasNoBody(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/public/canceled", "", nil)
	if w.Code != 499 || w.Body.Len() != 0 {
		t.Fatalf("canceled = %d %q, want 499 without body", w.Code, w.Body.String())
	}
}

func TestProblem_ValidationErrors(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	for _, path := range []string{"/public/bind", "/public/raw-bind"} {
		t.Run(path, func(t *testing.T) {
			w := serve(r, http.MethodPost, path, `{"budget":0,"stage":"closed","items":[{"sku":"TOO-LONG-SKU"}]}`, nil)
			p := mustProblem(t, w, http.StatusBadRequest, problem.TypeValidationFailed)
			if p.Title != "Validation failed" || p.ErrorCode != "VALIDATION_FAILED" {
				t.Fatalf("title/code = %q / %q", p.Title, p.ErrorCode)
			}
			want := []problem.FieldError{
				{Field: "title", Message: "is required"},
				{Field: "deadline", Message: "is required"},
				{Field: "budget", Message: "must be at least 1"},
				{Field: "stage", Message: "must be one of: draft, open"},
				{Field: "items[0].sku", Message: "must be at most 8 characters"},
			}
			if len(p.ValidationErrors) != len(want) {
				t.Fatalf("validationErrors = %+v, want %+v", p.ValidationErrors, want)
			}
			for i := range want {
				if p.ValidationErrors[i] != want[i] {
					t.Fatalf("validationErrors[%d] = %+v, want %+v", i, p.ValidationErrors[i], want[i])
				}
			}
			if strings.Contains(w.Body.String(), "TOO-LONG-SKU") || strings.Contains(w.Body.String(), "closed") {
				t.Fatal("rejected values must never be echoed")
			}
		})
	}
	valid := `{"title":"Bridge","deadline":"2027-01-01T00:00:00Z","budget":5,"stage":"open","items":[{"sku":"A1"}]}`
	if w := serve(r, http.MethodPost, "/public/bind", valid, nil); w.Code != http.StatusCreated {
		t.Fatalf("valid body = %d %s", w.Code, w.Body.String())
	}
}

func TestProblem_MalformedBodies(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})
	cases := []struct {
		path, body, detail string
		fields             []problem.FieldError
	}{
		{"/public/bind", `{"title":`, "The request body is not valid JSON.", nil},
		{"/public/bind", `{"title":"x"`, "The request body is not valid JSON.", nil},
		{"/public/bind", `{"title":1}`, "", []problem.FieldError{{Field: "title", Message: "must be a string"}}},
		{"/public/bind-typed", `not json`, "The request body is not valid JSON.", nil},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			w := serve(r, http.MethodPost, tc.path, tc.body, nil)
			p := mustProblem(t, w, http.StatusBadRequest, problem.TypeValidationFailed)
			if p.Detail != tc.detail || len(p.ValidationErrors) != len(tc.fields) {
				t.Fatalf("problem = %+v", p)
			}
			for i := range tc.fields {
				if p.ValidationErrors[i] != tc.fields[i] {
					t.Fatalf("validationErrors = %+v, want %+v", p.ValidationErrors, tc.fields)
				}
			}
		})
	}

	// An empty body: gin hands json.Decoder an empty reader (io.EOF).
	req := httptest.NewRequest(http.MethodPost, "/public/bind", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if p := mustProblem(t, w, http.StatusBadRequest, problem.TypeValidationFailed); p.Detail != "The request body is empty." {
		t.Fatalf("empty body detail = %q", p.Detail)
	}
}

func TestProblem_TraceIDPropagation(t *testing.T) {
	r := problemRouter(t, gincommon.Config{})

	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	w := serve(r, http.MethodGet, "/public/conflict", "", map[string]string{"traceparent": tp, "x-request-id": "req-1"})
	if p := mustProblem(t, w, 409, problem.TypeConflict); p.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceId = %q, want the traceparent trace-id", p.TraceID)
	}
	if got := w.Header().Get("X-Trace-Id"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("X-Trace-Id = %q", got)
	}

	w = serve(r, http.MethodGet, "/public/conflict", "", map[string]string{"x-request-id": "req-7f3a9c"})
	if p := mustProblem(t, w, 409, problem.TypeConflict); p.TraceID != "req-7f3a9c" {
		t.Fatalf("traceId = %q, want the x-request-id", p.TraceID)
	}

	w = serve(r, http.MethodGet, "/public/conflict", "", nil)
	p := mustProblem(t, w, 409, problem.TypeConflict)
	if p.TraceID != w.Header().Get("X-Trace-Id") || len(p.TraceID) != 32 {
		t.Fatalf("traceId = %q, want the server span's trace id %q", p.TraceID, w.Header().Get("X-Trace-Id"))
	}
}

func TestProblem_TypeBaseOverride(t *testing.T) {
	r := problemRouter(t, gincommon.Config{ProblemTypeBase: "https://errors.example.com/"})
	w := serve(r, http.MethodGet, "/public/conflict", "", nil)
	if p := mustProblem(t, w, 409, problem.Type{}); p.Type != "https://errors.example.com/platform/conflict" {
		t.Fatalf("type = %q", p.Type)
	}
}

func TestProblem_TimeoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "problem-test", RequestTimeout: 5 * time.Millisecond})...)
	r.GET("/slow", func(c *gin.Context) { <-c.Request.Context().Done() })
	w := serve(r, http.MethodGet, "/slow", "", nil)
	p := mustProblem(t, w, http.StatusServiceUnavailable, problem.TypeServiceUnavailable)
	if p.ErrorCode != problem.CodeRequestTimeout || p.Detail != "The request timed out." {
		t.Fatalf("problem = %+v", p)
	}
}

func TestAbortWithProblem_Edges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/nil", func(c *gin.Context) { gincommon.AbortWithProblem(c, nil) })
	r.GET("/written", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
		gincommon.AbortWithProblem(c, problem.NewConflictError("late"))
	})
	r.GET("/forbidden", func(c *gin.Context) {
		gincommon.AbortWithProblem(c, errors.Join(errors.New("ctx"), gincommon.ErrForbidden))
	})

	// No ObservabilityMiddlewares: defaults apply (base, generated traceId).
	w := serve(r, http.MethodGet, "/nil", "", nil)
	mustProblem(t, w, 500, problem.TypeInternalError)
	if w := serve(r, http.MethodGet, "/written", "", nil); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("written response changed: %d %q", w.Code, w.Body.String())
	}
	mustProblem(t, serve(r, http.MethodGet, "/forbidden", "", nil), 403, problem.TypeForbidden)

	if gincommon.ClassifyError(nil) != nil || gincommon.BindingError(nil) != nil {
		t.Fatal("nil errors must classify as nil")
	}
	if e := gincommon.ClassifyError(errors.New("x")); e.Type != problem.TypeInternalError {
		t.Fatalf("ClassifyError = %+v", e)
	}
}

func TestShouldBind_Form(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type query struct {
		Page int `form:"page" binding:"required,min=1"`
	}
	r := gin.New()
	r.GET("/q", func(c *gin.Context) {
		var q query
		if err := gincommon.ShouldBind(c, &q); err != nil {
			gincommon.AbortWithProblem(c, err)
			return
		}
		c.Status(http.StatusOK)
	})
	p := mustProblem(t, serve(r, http.MethodGet, "/q?page=0", "", nil), 400, problem.TypeValidationFailed)
	if len(p.ValidationErrors) != 1 || p.ValidationErrors[0] != (problem.FieldError{Field: "page", Message: "is required"}) {
		t.Fatalf("validationErrors = %+v", p.ValidationErrors)
	}
	if w := serve(r, http.MethodGet, "/q?page=2", "", nil); w.Code != 200 {
		t.Fatalf("valid query = %d", w.Code)
	}
}
