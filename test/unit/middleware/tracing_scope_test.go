package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
)

func TestTracingMiddleware_ServiceNameScopeRecorded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := gin.New()
	r.Use(middleware.TracingMiddleware("my-service"))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	// The instrumentation scope name should match the service name.
	scope := spans[0].InstrumentationScope().Name
	if scope != "my-service" {
		t.Fatalf("scope = %q, want my-service", scope)
	}
}

// TestTracingMiddleware_TruncatesLongURLPath exercises truncateSpanAttr with a URL path
// that exceeds 512 bytes and contains a multi-byte UTF-8 rune at the truncation boundary.
func TestTracingMiddleware_TruncatesLongURLPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// Build a path: 510 ASCII bytes + "€" (U+20AC, 3 bytes) = 513 bytes total.
	// truncateSpanAttr must stop at byte 510 (before the euro sign), not at 512 (mid-rune).
	longPath := "/" + strings.Repeat("a", 509) + "€"
	if len(longPath) <= 512 {
		t.Fatalf("test setup: path len = %d, want > 512", len(longPath))
	}

	r := gin.New()
	r.Use(middleware.TracingMiddleware(""))
	r.GET("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, longPath, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}

	var urlPathAttr string
	for _, a := range spans[0].Attributes() {
		if string(a.Key) == "url.path" {
			urlPathAttr = a.Value.AsString()
		}
	}
	if urlPathAttr == "" {
		t.Fatal("url.path attribute not found on span")
	}
	if len(urlPathAttr) > 512 {
		t.Fatalf("url.path len = %d, want ≤ 512 after truncation", len(urlPathAttr))
	}
	if !isValidUTF8(urlPathAttr) {
		t.Fatalf("url.path %q is invalid UTF-8 after truncation", urlPathAttr)
	}
}

func isValidUTF8(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8Decode(s[i:])
		if r == utf8ReplacementChar && size == 1 {
			return false
		}
		i += size
	}
	return true
}

// utf8Decode is a minimal rune decoder to avoid importing unicode/utf8 in test helpers.
func utf8Decode(s string) (rune, int) {
	if len(s) == 0 {
		return -1, 0
	}
	b := s[0]
	if b < 0x80 {
		return rune(b), 1
	}
	if b < 0xC0 {
		return utf8ReplacementChar, 1
	}
	if b < 0xE0 && len(s) >= 2 {
		return rune(b&0x1F)<<6 | rune(s[1]&0x3F), 2
	}
	if b < 0xF0 && len(s) >= 3 {
		return rune(b&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	if len(s) >= 4 {
		return rune(b&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
	}
	return utf8ReplacementChar, 1
}

const utf8ReplacementChar = rune(0xFFFD)

func TestTracingMiddleware_DefaultScopeName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := gin.New()
	r.Use(middleware.TracingMiddleware(""))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	scope := spans[0].InstrumentationScope().Name
	if scope != "platform-gincommon/http" {
		t.Fatalf("scope = %q, want platform-gincommon/http", scope)
	}
}
