package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/server"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/testenv"
)

func TestE2E_ServerHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger(config.AppEnv())
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := server.NewRouter(log, config.E2EServiceName())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health status: got %d want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestE2E_SecureRouteWithReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger(config.AppEnv())
	authz := gincommon.NewDemoRBAC()

	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: config.E2EServiceName(),
	}) {
		r.Use(m)
	}

	r.GET("/secure",
		gincommon.RequirePermission("read", "user", authz),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) },
	)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersUserRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestE2E_DeleteForbiddenForUserRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger(config.AppEnv())
	authz := gincommon.NewDemoRBAC()

	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: config.E2EServiceName(),
	}) {
		r.Use(m)
	}

	r.DELETE("/users/1",
		gincommon.RequirePermission("delete", "user", authz),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersUserRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}
}

func TestE2E_DeleteAllowedForAdminRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger(config.AppEnv())
	authz := gincommon.NewDemoRBAC()

	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:      log,
		ServiceName: config.E2EServiceName(),
	}) {
		r.Use(m)
	}

	r.DELETE("/users/1",
		gincommon.RequirePermission("delete", "user", authz),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersAdminRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestE2E_ObservabilityInitFromEnv(t *testing.T) {
	shutdown := gincommon.InitTracingFromEnv()
	if shutdown == nil {
		t.Fatal("expected shutdown func")
	}
	shutdown()
}
