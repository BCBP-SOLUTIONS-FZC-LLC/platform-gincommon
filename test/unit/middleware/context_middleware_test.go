package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
)

type mockCtxBuilder struct{ err error }

func (m *mockCtxBuilder) BuildContext(tenantID, userID string, roles []string, traceID string) (*domain.RequestContext, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domain.RequestContext{TenantID: tenantID, UserID: userID, Roles: roles, TraceID: traceID}, nil
}

func ctxMiddlewareRouter(b *mockCtxBuilder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContextMiddleware(b))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func authedRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("x-user-id", "u1")
	req.Header.Set("x-tenant-id", "t1")
	return req
}

func TestContextMiddleware_ErrMissingTenant_Returns400(t *testing.T) {
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{err: domain.ErrMissingTenant}).ServeHTTP(w, authedRequest())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestContextMiddleware_ErrMissingUser_Returns400(t *testing.T) {
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{err: domain.ErrMissingUser}).ServeHTTP(w, authedRequest())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestContextMiddleware_GenericError_Returns400(t *testing.T) {
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{err: errors.New("unexpected")}).ServeHTTP(w, authedRequest())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestContextMiddleware_MissingHeaders_Returns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	ctxMiddlewareRouter(&mockCtxBuilder{}).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestContextMiddleware_HappyPath_SetsContext(t *testing.T) {
	var gotCtx *domain.RequestContext
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ContextMiddleware(&mockCtxBuilder{}))
	r.GET("/x", func(c *gin.Context) {
		v, _ := c.Get(middleware.CtxRequestContextKey)
		gotCtx, _ = v.(*domain.RequestContext)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotCtx == nil || gotCtx.UserID != "u1" || gotCtx.TenantID != "t1" {
		t.Fatalf("context = %+v, want u1/t1", gotCtx)
	}
}
