package gincommon_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// levelLogger records every entry's level and message (fixtures.MockLogger keeps the last).
type levelLogger struct {
	mu      sync.Mutex
	entries []struct{ level, msg string }
}

func (l *levelLogger) add(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, struct{ level, msg string }{level, msg})
}
func (l *levelLogger) Debug(m string, _ map[string]interface{}) { l.add("debug", m) }
func (l *levelLogger) Info(m string, _ map[string]interface{})  { l.add("info", m) }
func (l *levelLogger) Warn(m string, _ map[string]interface{})  { l.add("warn", m) }
func (l *levelLogger) Error(m string, _ map[string]interface{}) { l.add("error", m) }

// metricValue sums the counter samples of name whose route label is route.
func metricValue(t *testing.T, name, route string, labels map[string]string) float64 {
	t.Helper()
	families, err := gincommon.MetricsGatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var sum float64
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
	metric:
		for _, m := range f.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			if got["route"] != route {
				continue
			}
			for k, v := range labels {
				if got[k] != v {
					continue metric
				}
			}
			sum += m.GetCounter().GetValue()
		}
	}
	return sum
}

func TestObservabilityMiddlewares_ErrAbortHandler_FullChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := &levelLogger{}
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(gincommon.Config{Logger: log, ServiceName: "pkg-test", RequestTimeout: time.Minute})...)
	r.GET("/abort-chain", func(c *gin.Context) { panic(http.ErrAbortHandler) })

	var rec any
	func() {
		defer func() { rec = recover() }()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort-chain", nil))
	}()
	if rec != http.ErrAbortHandler {
		t.Fatalf("escaped value = %v, want http.ErrAbortHandler re-panicked for net/http", rec)
	}

	if got := metricValue(t, "platform_http_panics_total", "/abort-chain", nil); got != 0 {
		t.Fatalf("platform_http_panics_total = %v, want 0", got)
	}
	if got := metricValue(t, "platform_http_requests_total", "/abort-chain", map[string]string{"status_class": "5xx"}); got != 0 {
		t.Fatalf("5xx requests = %v, want 0", got)
	}
	if got := metricValue(t, "platform_http_requests_total", "/abort-chain", map[string]string{"error_class": "client_error"}); got != 1 {
		t.Fatalf("client_error requests = %v, want 1", got)
	}
	var debug int
	for _, e := range log.entries {
		if e.level == "error" {
			t.Fatalf("unexpected error log %q", e.msg)
		}
		if e.level == "debug" {
			debug++
		}
	}
	if debug != 2 { // the access log entry and PanicRecoveryMiddleware's "request aborted"
		t.Fatalf("debug entries = %d (%v), want 2", debug, log.entries)
	}
}

func TestProtectedMiddlewares_RepeatedIdentityHeader401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.ProtectedMiddlewares(gincommon.Config{Logger: &fixtures.MockLogger{}, ServiceName: "pkg-test"})...)
	r.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	req.Header.Add(gincommon.HeaderUserID, "admin-impersonation")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if body, _ := fixtures.DecodeProblem(w.Body.Bytes()); body.Detail != "Ambiguous x-user-id header." {
		t.Fatalf("detail = %q", body.Detail)
	}
}

func TestTimeoutMiddleware_KeepsStatusOnlyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.TimeoutMiddleware(time.Nanosecond))
	r.DELETE("/x", func(c *gin.Context) {
		<-c.Request.Context().Done()
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/x", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}
