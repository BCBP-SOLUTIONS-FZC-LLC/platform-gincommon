package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/testenv"
)

func newAuthzRouter(t *testing.T, action, resource string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	authz := gincommon.NewDemoRBAC()
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{Logger: log, ServiceName: "authz-test"}) {
		r.Use(m)
	}
	r.DELETE("/resource",
		gincommon.RequirePermission(action, resource, authz),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)
	r.GET("/resource",
		gincommon.RequirePermission("read", resource, authz),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)
	return r
}

func TestAuthZ_DeleteForbiddenForUserRole(t *testing.T) {
	r := newAuthzRouter(t, "delete", "doc")

	req := httptest.NewRequest(http.MethodDelete, "/resource", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersUserRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusForbidden)
	}
}

func TestAuthZ_DeleteAllowedForAdminRole(t *testing.T) {
	r := newAuthzRouter(t, "delete", "doc")

	req := httptest.NewRequest(http.MethodDelete, "/resource", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersAdminRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusOK)
	}
}

func TestAuthZ_ReadAllowedForUserRole(t *testing.T) {
	r := newAuthzRouter(t, "read", "doc")

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	fixtures.ApplyHeaders(req, fixtures.HeadersUserRole())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusOK)
	}
}
