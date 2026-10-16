package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestLoggingMiddleware_NilLogger_DoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.LoggingMiddleware(nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	// Must not panic.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestLoggingMiddleware_MethodFieldPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/method", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/method", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Fields["method"] != http.MethodGet {
		t.Fatalf("method = %v, want GET", log.Fields["method"])
	}
}

func TestLoggingMiddleware_InfoFor2xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "info" {
		t.Fatalf("log level = %q, want info for 200", log.Level)
	}
}

func TestLoggingMiddleware_WarnFor4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/notfound", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "warn" {
		t.Fatalf("log level = %q, want warn for 404", log.Level)
	}
}

func TestLoggingMiddleware_ErrorFor5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/err", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "error" {
		t.Fatalf("log level = %q, want error for 500", log.Level)
	}
}

func TestLoggingMiddleware_DurationMsIsFloat64(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/dur", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/dur", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	v, ok := log.Fields["duration_ms"]
	if !ok {
		t.Fatal("expected duration_ms field in log")
	}
	if _, isFloat := v.(float64); !isFloat {
		t.Fatalf("duration_ms type = %T, want float64", v)
	}
}

func TestLoggingMiddleware_WarnFor400(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/bad", func(c *gin.Context) { c.Status(http.StatusBadRequest) })

	req := httptest.NewRequest(http.MethodGet, "/bad", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "warn" {
		t.Fatalf("log level = %q, want warn for 400", log.Level)
	}
}

func TestLoggingMiddleware_ErrorFor503(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/unavail", func(c *gin.Context) { c.Status(http.StatusServiceUnavailable) })

	req := httptest.NewRequest(http.MethodGet, "/unavail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if log.Level != "error" {
		t.Fatalf("log level = %q, want error for 503", log.Level)
	}
}

// TestLoggingMiddleware_SanitizesLongHeaderWithRuneBoundary exercises the
// UTF-8 rune-boundary walk in sanitizeLogValue: 254 ASCII bytes + "€" (3 bytes)
// = 257 bytes total, so the truncation loop must step back past the continuation
// bytes of "€" to land on a valid rune boundary.
func TestLoggingMiddleware_SanitizesLongHeaderWithRuneBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 254 ASCII bytes + "€" (U+20AC, 3 bytes) = 257 bytes total
	headerVal := strings.Repeat("a", 254) + "€"

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("x-user-id", headerVal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	uid, ok := log.Fields["claimed_user_id"]
	if !ok {
		t.Fatal("expected claimed_user_id field in log")
	}
	s, _ := uid.(string)
	if len(s) > 256 {
		t.Fatalf("user_id len = %d, want ≤ 256 after truncation", len(s))
	}
	if !utf8.ValidString(s) {
		t.Fatalf("user_id %q is invalid UTF-8 after truncation", s)
	}
}

func TestLoggingMiddleware_SanitizesLongHeaderValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	// Use logging without RequireAuth so context values are absent — sanitizeLogValue runs.
	r.Use(middleware.LoggingMiddleware(log))
	r.GET("/san", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Pass a header value longer than 256 bytes — exercises the truncation branch.
	longValue := string(make([]byte, 300))
	for i := range longValue {
		_ = i
	}
	longValue = "A"
	for i := 0; i < 300; i++ {
		longValue += "x"
	}

	req := httptest.NewRequest(http.MethodGet, "/san", nil)
	req.Header.Set("x-user-id", longValue)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	uid, ok := log.Fields["claimed_user_id"]
	if !ok {
		t.Fatal("expected claimed_user_id field")
	}
	if s, _ := uid.(string); len(s) > 256 {
		t.Fatalf("user_id len = %d, want ≤ 256", len(s))
	}
}
