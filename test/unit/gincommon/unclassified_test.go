package gincommon_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

type dbError struct{ query string }

func (e *dbError) Error() string { return "pq: duplicate key " + e.query }

type tenderAPI struct{}

func (tenderAPI) Create(c *gin.Context) {
	_ = c.Error(fmt.Errorf("create tender: %w",
		&dbError{query: `users_email_key email=john@example.com`}))
}

func unclassifiedEntries(l *problemLogger) []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []logEntry
	for _, e := range l.entries {
		if e.msg == "unclassified error" {
			out = append(out, e)
		}
	}
	return out
}

func unclassifiedRouter(t *testing.T, logs *problemLogger) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "problem-test", Logger: logs})...)
	r.POST("/tenders", tenderAPI{}.Create)
	r.GET("/joined", func(c *gin.Context) {
		_ = c.Error(errors.Join(errors.New("a"), &dbError{query: "x"}))
	})
	r.GET("/classified", func(c *gin.Context) {
		_ = c.Error(problem.NewInternalError(&dbError{query: "x"})) // deliberately mapped
	})
	r.GET("/conflict", func(c *gin.Context) { _ = c.Error(problem.NewConflictError("Taken.")) })
	r.GET("/panic", func(*gin.Context) { panic("boom") })
	return r
}

// An unclassified 500 is logged once at error level with where it happened and the
// error's types, never its text: the response stays a generic internal-error.
func TestUnclassifiedError_LoggedAsTypesOnly(t *testing.T) {
	logs := &problemLogger{}
	w := serve(unclassifiedRouter(t, logs), http.MethodPost, "/tenders", "{}", map[string]string{"x-request-id": "req-abc123"})
	p := mustProblem(t, w, http.StatusInternalServerError, problem.TypeInternalError)
	if p.Detail != "" || strings.Contains(w.Body.String(), "pq:") {
		t.Fatalf("response leaked the cause: %s", w.Body.String())
	}

	e := unclassifiedEntries(logs)
	if len(e) != 1 || e[0].level != "error" {
		t.Fatalf("unclassified entries = %+v, want one error-level entry", e)
	}
	f := e[0].fields
	want := map[string]any{
		"classified": false, "request_id": "req-abc123", "traceId": "req-abc123", "method": "POST",
		"route": "/tenders", "status": 500, "errorCode": "INTERNAL_ERROR", "service": "problem-test",
		"error_type": "*fmt.wrapError",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("%s = %v, want %v", k, f[k], v)
		}
	}
	if chain := f["error_chain"]; !reflect.DeepEqual(chain, []string{"*fmt.wrapError", "*gincommon_test.dbError"}) {
		t.Errorf("error_chain = %v", chain)
	}
	if op, _ := f["operation"].(string); op != "gincommon_test.tenderAPI.Create" {
		t.Errorf("operation = %q, want the handler", op)
	}
	if tid, _ := f["trace_id"].(string); len(tid) != 32 {
		t.Errorf("trace_id = %q, want the server span's", tid)
	}
	// No field of any entry carries the error text.
	for _, entry := range logs.entries {
		if s := fmt.Sprint(entry.fields); strings.Contains(s, "pq:") || strings.Contains(s, "john@example.com") || strings.Contains(s, "create tender") {
			t.Fatalf("%q leaked the error text: %s", entry.msg, s)
		}
	}
}

func TestUnclassifiedError_JoinedChain(t *testing.T) {
	logs := &problemLogger{}
	serve(unclassifiedRouter(t, logs), http.MethodGet, "/joined", "", nil)
	e := unclassifiedEntries(logs)
	if len(e) != 1 {
		t.Fatalf("entries = %+v", e)
	}
	if chain := e[0].fields["error_chain"]; !reflect.DeepEqual(chain, []string{"*errors.joinError", "*errors.errorString", "*gincommon_test.dbError"}) {
		t.Fatalf("error_chain = %v", chain)
	}
}

// Classified errors, deliberate internal-errors and panics (logged with their stack
// by the recovery middleware) are not "unclassified".
func TestUnclassifiedError_NotForClassified(t *testing.T) {
	for _, path := range []string{"/classified", "/conflict", "/panic"} {
		logs := &problemLogger{}
		serve(unclassifiedRouter(t, logs), http.MethodGet, path, "", nil)
		if e := unclassifiedEntries(logs); len(e) != 0 {
			t.Errorf("%s: unexpected unclassified entries %+v", path, e)
		}
	}
}

func TestUnclassified_Flag(t *testing.T) {
	if !problem.FromError(errors.New("x")).Unclassified() {
		t.Error("FromError fallback must be unclassified")
	}
	for name, e := range map[string]*problem.Error{
		"NewInternalError":  problem.NewInternalError(errors.New("x")),
		"timeout":           problem.FromError(fmt.Errorf("q: %w", context.DeadlineExceeded)),
		"existing *Error":   problem.FromError(problem.NewConflictError("x")),
		"unsupported (501)": problem.FromError(errors.ErrUnsupported),
	} {
		if e.Unclassified() {
			t.Errorf("%s must be classified", name)
		}
	}
	if !gincommon.ClassifyError(errors.New("x")).Unclassified() {
		t.Error("ClassifyError fallback must be unclassified")
	}
}

// leafErr's Unwrap returns nil, as custom error types often do.
type leafErr struct{}

func (leafErr) Error() string { return "leaf secret=hunter2" }
func (leafErr) Unwrap() error { return nil }

// nestErr wraps itself n times.
type nestErr struct{ n int }

func (e nestErr) Error() string { return "nested" }
func (e nestErr) Unwrap() error {
	if e.n == 0 {
		return nil
	}
	return nestErr{n: e.n - 1}
}

func TestUnclassifiedError_ChainEdges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"nil Unwrap":         {leafErr{}, 1},
		"capped at 8 levels": {nestErr{n: 20}, 8},
	} {
		logs := &problemLogger{}
		r := gin.New()
		r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "problem-test", Logger: logs})...)
		r.GET("/x", func(c *gin.Context) { _ = c.Error(tc.err) })
		serve(r, http.MethodGet, "/x", "", nil)
		e := unclassifiedEntries(logs)
		if len(e) != 1 {
			t.Fatalf("%s: entries = %+v", name, e)
		}
		if chain, _ := e[0].fields["error_chain"].([]string); len(chain) != tc.want {
			t.Errorf("%s: error_chain = %v, want %d entries", name, chain, tc.want)
		}
		if strings.Contains(fmt.Sprint(e[0].fields), "hunter2") {
			t.Errorf("%s: error text leaked", name)
		}
	}
}

func unclassifiedCount(t *testing.T, transport string) float64 {
	t.Helper()
	return testutil.ToFloat64(metrics.UnclassifiedErrorsTotal.WithLabelValues(transport))
}

// platform_unclassified_errors_total{transport="http"} counts each unclassified 500,
// also without a logger, and nothing else.
func TestUnclassifiedError_Metric(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "problem-test"})...) // no logger
	r.GET("/plain", func(c *gin.Context) { _ = c.Error(errors.New("x")) })
	r.GET("/classified", func(c *gin.Context) { _ = c.Error(problem.NewInternalError(errors.New("x"))) })
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	before := unclassifiedCount(t, "http")
	serve(r, http.MethodGet, "/plain", "", nil)
	serve(r, http.MethodGet, "/plain", "", nil)
	if got := unclassifiedCount(t, "http") - before; got != 2 {
		t.Fatalf("http unclassified delta = %v, want 2", got)
	}
	before = unclassifiedCount(t, "http")
	serve(r, http.MethodGet, "/classified", "", nil)
	serve(r, http.MethodGet, "/panic", "", nil)
	if got := unclassifiedCount(t, "http") - before; got != 0 {
		t.Fatalf("classified/panic must not count: delta = %v", got)
	}
}

// Both transports are exported from initialization, at 0 until the first error, so
// increase() sees the first one.
func TestUnclassifiedErrorsMetric_PreInitialised(t *testing.T) {
	gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "problem-test"})
	if n := testutil.CollectAndCount(metrics.UnclassifiedErrorsTotal, "platform_unclassified_errors_total"); n != 2 {
		t.Fatalf("series = %d, want http and grpc", n)
	}
}
