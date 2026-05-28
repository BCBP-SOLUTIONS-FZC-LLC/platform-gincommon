package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"
)

func TestContextMiddleware_MissingTenantUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(
		middleware.TracingMiddleware(""),
		middleware.ContextMiddleware(service.NewContextService()),
	)
	r.GET("/ctx", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	req.Header.Set("x-user-id", "user-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestContextMiddleware_ParseRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(
		middleware.TracingMiddleware(""),
		middleware.ContextMiddleware(service.NewContextService()),
	)
	r.GET("/ctx", func(c *gin.Context) {
		ctx, _ := c.Get(middleware.CtxRequestContextKey)
		c.JSON(http.StatusOK, ctx)
	})

	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	req.Header.Set("x-user-id", "user-1")
	req.Header.Set("x-tenant-id", "tenant-1")
	req.Header.Set("x-tenant-roles", " admin , user , , ")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
}
