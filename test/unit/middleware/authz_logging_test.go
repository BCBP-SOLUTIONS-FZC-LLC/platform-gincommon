package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

type failingAuthorizer struct{ err error }

func (a failingAuthorizer) Authorize(context.Context, *domain.RequestContext, string, string) error {
	return a.err
}

// authzRouter runs AuthZMiddleware after a stand-in for ContextMiddleware that binds a
// verified RequestContext.
func authzRouter(auth port.Authorizer, logger port.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxRequestContextKey, &domain.RequestContext{
			TenantID: "tenant-001", UserID: "u1", Roles: []string{"user"},
		})
	})
	r.DELETE("/items/:id", middleware.AuthZMiddleware(auth, "delete", "items", logger),
		func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func serveDelete(r http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/items/7", nil))
	return w
}

// A failing Authorizer is a 500 internal-error: its error is logged (sanitized, with the
// identity and the permission checked) and never sent to the client.
func TestAuthZ_AuthorizerError_LoggedNotLeaked(t *testing.T) {
	logs := &recordingLogger{}
	cause := errors.New("policy store: dial tcp 10.0.0.5:5432\nsecret=hunter2")
	w := serveDelete(authzRouter(failingAuthorizer{err: cause}, logs))

	p, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if w.Code != http.StatusInternalServerError || !fixtures.IsProblemType(p.Type, problem.TypeInternalError) {
		t.Fatalf("status %d, problem %+v", w.Code, p)
	}
	if p.Detail != "" || strings.Contains(w.Body.String(), "policy store") || strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("authorizer error leaked to the client: %s", w.Body.String())
	}
	e := logs.byMsg("authz: internal authorization error")
	if len(e) != 1 || e[0].Level != "error" {
		t.Fatalf("log = %+v, want one error entry", logs.entries)
	}
	f := e[0].Fields
	if f["tenant_id"] != "tenant-001" || f["user_id"] != "u1" || f["action"] != "delete" || f["resource"] != "items" {
		t.Fatalf("log fields = %v", f)
	}
	if msg, _ := f["error"].(string); strings.ContainsAny(msg, "\n\r") || !strings.Contains(msg, "policy store") {
		t.Fatalf("error field = %q, want the cause with control characters stripped", msg)
	}
}

// ErrForbidden wrapped by the Authorizer is still a 403 (errors.Is), not a 500.
func TestAuthZ_WrappedForbidden(t *testing.T) {
	logs := &recordingLogger{}
	w := serveDelete(authzRouter(failingAuthorizer{err: errors.Join(errors.New("role check"), domain.ErrForbidden)}, logs))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if e := logs.byMsg("authz: access denied"); len(e) != 1 || e[0].Level != "warn" {
		t.Fatalf("log = %+v, want one warn denial", logs.entries)
	}
}

// A nil Authorizer is a misconfiguration: 500 without detail, and an error log naming
// the permission that could not be checked.
func TestAuthZ_NilAuthorizer_Logged(t *testing.T) {
	logs := &recordingLogger{}
	w := serveDelete(authzRouter(nil, logs))
	p, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if w.Code != http.StatusInternalServerError || p.Detail != "" {
		t.Fatalf("status %d, problem %+v", w.Code, p)
	}
	e := logs.byMsg("authz: no authorizer configured")
	if len(e) != 1 || e[0].Level != "error" || e[0].Fields["action"] != "delete" || e[0].Fields["resource"] != "items" {
		t.Fatalf("log = %+v", logs.entries)
	}
}

// With a nil logger every failure path still answers, and nothing panics.
func TestAuthZ_NilLogger(t *testing.T) {
	for name, auth := range map[string]port.Authorizer{
		"nil authorizer": nil,
		"error":          failingAuthorizer{err: errors.New("boom")},
		"forbidden":      failingAuthorizer{err: domain.ErrForbidden},
	} {
		w := serveDelete(authzRouter(auth, nil))
		if w.Code != http.StatusInternalServerError && w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d", name, w.Code)
		}
	}
}
