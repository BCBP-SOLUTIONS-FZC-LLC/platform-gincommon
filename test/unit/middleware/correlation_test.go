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
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id on success response")
	}
	if w.Header().Get("X-Trace-Id") == "" {
		t.Fatal("expected X-Trace-Id on success response")
	}
	if w.Header().Get("x-request-id") == "" {
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
	if body.Error != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("error = %q", body.Error)
	}
	if body.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", body.Status, http.StatusInternalServerError)
	}
	if body.RequestID == "" {
		t.Fatal("request_id missing from body")
	}
}
