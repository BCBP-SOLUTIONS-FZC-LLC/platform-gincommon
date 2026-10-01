package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

func TestCorrelationHeadersMiddleware_SetsHeadersOnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware(""),
		middleware.CorrelationHeadersMiddleware(),
	)
	// The handler writes a body, so headers are flushed with it: any header set after
	// c.Next() would not reach the client (only w.Result().Header shows that).
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Result().Header.Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id on success response")
	}
	if w.Result().Header.Get("X-Trace-Id") == "" {
		t.Fatal("expected X-Trace-Id on success response")
	}
	if w.Result().Header.Get("x-request-id") == "" {
		t.Fatal("expected x-request-id on success response")
	}
}

func TestWriteJSONError_ViaRecovery_IncludesCorrelationFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := &fixtures.MockLogger{}
	r := gin.New()
	r.Use(
		middleware.PanicRecoveryMiddleware(log),
		middleware.RequestIDMiddleware(),
	)
	r.GET("/boom", func(c *gin.Context) { panic("x") })

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}

	body, err := fixtures.DecodeErrorJSON(w.Body.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "internal server error" {
		t.Fatalf("error = %q", body.Error)
	}
	if body.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", body.Status, http.StatusInternalServerError)
	}
	if body.RequestID == "" {
		t.Fatal("request_id missing from body")
	}
}
