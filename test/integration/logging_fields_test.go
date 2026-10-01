package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"
)

// integrationLogger captures log calls in tests.
type integrationLogger struct {
	Level  string
	Msg    string
	Fields map[string]interface{}
}

func (l *integrationLogger) Debug(msg string, fields map[string]interface{}) {
	l.record("debug", msg, fields)
}
func (l *integrationLogger) Info(msg string, fields map[string]interface{}) {
	l.record("info", msg, fields)
}
func (l *integrationLogger) Warn(msg string, fields map[string]interface{}) {
	l.record("warn", msg, fields)
}
func (l *integrationLogger) Error(msg string, fields map[string]interface{}) {
	l.record("error", msg, fields)
}
func (l *integrationLogger) Fatal(msg string, fields map[string]interface{}) {
	l.record("fatal", msg, fields)
}

func (l *integrationLogger) record(level, msg string, fields map[string]interface{}) {
	l.Level = level
	l.Msg = msg
	l.Fields = fields
}

func buildTestRouter(log *integrationLogger, handler func(c *gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: "integration-test",
	}) {
		r.Use(m)
	}
	r.GET("/test", handler)
	return r
}

func TestLoggingMiddleware_Integration_MethodFieldPresent(t *testing.T) {
	log := &integrationLogger{}
	r := buildTestRouter(log, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if log.Fields["method"] != http.MethodGet {
		t.Fatalf("method = %v, want GET", log.Fields["method"])
	}
}

func TestLoggingMiddleware_Integration_DurationMsIsFloat64(t *testing.T) {
	log := &integrationLogger{}
	r := buildTestRouter(log, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	v, ok := log.Fields["duration_ms"]
	if !ok {
		t.Fatal("expected duration_ms in log fields")
	}
	if _, isFloat := v.(float64); !isFloat {
		t.Fatalf("duration_ms type = %T, want float64", v)
	}
}

func TestLoggingMiddleware_Integration_WarnFor4xx(t *testing.T) {
	log := &integrationLogger{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	for _, m := range gincommon.ObservabilityMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: "integration-4xx",
	}) {
		r.Use(m)
	}
	r.GET("/fail", func(c *gin.Context) { c.Status(http.StatusBadRequest) })

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "warn" {
		t.Fatalf("log level = %q, want warn for 400", log.Level)
	}
}

func TestLoggingMiddleware_Integration_ErrorFor5xx(t *testing.T) {
	log := &integrationLogger{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	for _, m := range gincommon.ObservabilityMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: "integration-5xx",
	}) {
		r.Use(m)
	}
	r.GET("/err", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "error" {
		t.Fatalf("log level = %q, want error for 500", log.Level)
	}
}

func TestPropagateIdentityHeaders_Integration(t *testing.T) {
	log := &integrationLogger{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: "propagate-test",
	}) {
		r.Use(m)
	}

	var outboundReq *http.Request
	r.GET("/proxy", func(c *gin.Context) {
		out, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
		gincommon.PropagateIdentityHeaders(c, out)
		outboundReq = out
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	req.Header.Set("x-request-id", "integration-req-id")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if outboundReq == nil {
		t.Fatal("outbound request not captured")
	}
	if outboundReq.Header.Get("x-user-id") != "user-1" {
		t.Fatalf("x-user-id = %q, want user-1", outboundReq.Header.Get("x-user-id"))
	}
	if outboundReq.Header.Get("x-tenant-id") != "tenant-1" {
		t.Fatalf("x-tenant-id = %q, want tenant-1", outboundReq.Header.Get("x-tenant-id"))
	}
	if outboundReq.Header.Get("x-request-id") != "integration-req-id" {
		t.Fatalf("x-request-id = %q, want integration-req-id", outboundReq.Header.Get("x-request-id"))
	}
}
