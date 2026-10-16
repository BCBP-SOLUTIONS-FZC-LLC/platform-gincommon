package gincommon_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
)

func TestPropagateIdentityHeaders_InjectsIdentityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	rc := &domain.RequestContext{
		UserID:   "user-1",
		TenantID: "tenant-1",
		Roles:    []string{"admin", "user"},
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	c.Request = req
	c.Set(gincommon.RequestContextKey, rc)
	c.Set(middleware.CtxRequestIDKey, "req-abc")

	outbound, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
	gincommon.PropagateIdentityHeaders(c, outbound)

	if got := outbound.Header.Get("x-user-id"); got != "user-1" {
		t.Fatalf("x-user-id = %q, want user-1", got)
	}
	if got := outbound.Header.Get("x-tenant-id"); got != "tenant-1" {
		t.Fatalf("x-tenant-id = %q, want tenant-1", got)
	}
	if got := outbound.Header.Get("x-request-id"); got != "req-abc" {
		t.Fatalf("x-request-id = %q, want req-abc", got)
	}
	roles := outbound.Header.Get("x-tenant-roles")
	if roles == "" {
		t.Fatal("expected x-tenant-roles to be set")
	}
}

func TestPropagateHeaders_WithoutRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	c.Request = req
	// No RequestContext set, no request ID set.

	outbound, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
	gincommon.PropagateIdentityHeaders(c, outbound)

	// Identity headers should not be set.
	if got := outbound.Header.Get("x-user-id"); got != "" {
		t.Fatalf("x-user-id = %q, want empty when no RequestContext", got)
	}
	if got := outbound.Header.Get("x-tenant-id"); got != "" {
		t.Fatalf("x-tenant-id = %q, want empty when no RequestContext", got)
	}
}

func TestPropagateHeaders_RequestIDPropagated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	c.Request = req
	c.Set(middleware.CtxRequestIDKey, "correlation-123")

	outbound, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
	gincommon.PropagateHeaders(c, outbound)

	if got := outbound.Header.Get("x-request-id"); got != "correlation-123" {
		t.Fatalf("x-request-id = %q, want correlation-123", got)
	}
}

func TestPropagateIdentityHeaders_EmptyRoles_NoRolesHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	rc := &domain.RequestContext{
		UserID:   "user-1",
		TenantID: "tenant-1",
		Roles:    nil,
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	c.Request = req
	c.Set(gincommon.RequestContextKey, rc)

	outbound, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
	gincommon.PropagateIdentityHeaders(c, outbound)

	if got := outbound.Header.Get("x-tenant-roles"); got != "" {
		t.Fatalf("x-tenant-roles = %q, want empty when roles is nil", got)
	}
}

// A roles header already on the outbound request is removed when the caller has no
// verified roles, so an unverified value is never forwarded (parity with the gRPC
// identity client interceptor).
func TestPropagateIdentityHeaders_ClearsStaleRolesHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Set(gincommon.RequestContextKey, &domain.RequestContext{UserID: "u", TenantID: "t"})

	outbound, _ := http.NewRequest(http.MethodGet, "http://downstream/api", nil)
	outbound.Header.Set("x-tenant-roles", "admin")
	gincommon.PropagateIdentityHeaders(c, outbound)
	if got := outbound.Header.Get("x-tenant-roles"); got != "" {
		t.Fatalf("x-tenant-roles = %q, want removed", got)
	}
}
