package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"
)

func TestMiddlewareFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger(config.AppEnv())
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: config.TestServiceName(),
	}) {
		r.Use(m)
	}

	r.GET("/test", func(c *gin.Context) {
		ctx, exists := c.Get("requestContext")
		if !exists {
			t.Fatal("missing request context")
		}
		c.JSON(http.StatusOK, ctx)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
	req.Header.Set("x-request-id", "fixed-req-id")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("x-request-id"); got != "fixed-req-id" {
		t.Fatalf("x-request-id = %q want fixed-req-id", got)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["tenant_id"] != "tenant-1" || body["user_id"] != "user-1" {
		t.Fatalf("context body = %v", body)
	}
}

func TestMiddlewareFlow_GeneratesRequestIDWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger("dev")
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{Logger: log, ServiceName: "t"}) {
		r.Use(m)
	}
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("x-request-id") == "" {
		t.Fatal("expected generated x-request-id header")
	}
}

func TestMiddlewareFlow_GeneratesTraceIDWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger("dev")
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{Logger: log, ServiceName: "t"}) {
		r.Use(m)
	}
	r.GET("/test", func(c *gin.Context) {
		traceID, ok := c.Get("trace_id")
		if !ok || traceID == "" {
			t.Fatal("missing trace_id in context")
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}
