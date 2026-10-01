package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

func TestNewRouter_Health(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := server.NewRouter(log, "server-test")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Trace-Id") == "" {
		t.Fatal("expected X-Trace-Id on /health response")
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id on /health response")
	}
}

func TestNewRouter_MetricsWithoutAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := server.NewRouter(log, "server-test")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Fatal("expected Content-Type on metrics response")
	}
	if w.Header().Get("X-Trace-Id") == "" {
		t.Fatal("expected X-Trace-Id on /metrics response")
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id on /metrics response")
	}
}

func TestNewRouter_Panic_Returns500WithCorrelation(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	gin.SetMode(gin.TestMode)
	fixtures.InitMetrics("server-panic-test", "test")

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := server.NewRouter(log, "server-panic-test")

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}

	body, err := fixtures.DecodeErrorJSON(w.Body.Bytes())
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("error = %q", body.Error)
	}
	if body.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", body.Status, http.StatusInternalServerError)
	}
	if body.RequestID == "" {
		t.Fatal("expected non-empty request_id in body")
	}
	if body.RequestID != w.Header().Get("X-Request-Id") {
		t.Fatalf("body request_id %q != header %q", body.RequestID, w.Header().Get("X-Request-Id"))
	}
	if body.TraceID == "" || body.TraceID == "00000000000000000000000000000000" {
		t.Fatalf("trace_id = %q, want non-zero OTel trace ID", body.TraceID)
	}
	if body.TraceID != w.Header().Get("X-Trace-Id") {
		t.Fatalf("body trace_id %q != header %q", body.TraceID, w.Header().Get("X-Trace-Id"))
	}

	counter, err := metrics.HttpPanicTotal.GetMetricWithLabelValues(http.MethodGet, "/panic")
	if err != nil {
		t.Fatalf("get http_panic_total: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_panic_total = %v, want 1", got)
	}
}
