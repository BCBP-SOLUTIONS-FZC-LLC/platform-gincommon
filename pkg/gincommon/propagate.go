package gincommon

import (
	"net/http"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// PropagateHeaders injects correlation headers only — the W3C trace context
// (traceparent/tracestate) and x-request-id — from the inbound Gin request into the
// outbound req. It is safe for calls to any destination, including third parties:
// no identity headers (x-user-id, x-tenant-id, x-tenant-roles) and no W3C baggage
// (which may carry application data) are sent.
//
// For calls to trusted internal services that need the caller's tenant context, use
// PropagateIdentityHeaders. A nil c or req is a no-op.
func PropagateHeaders(c *gin.Context, req *http.Request) {
	if c == nil || c.Request == nil || req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	propagation.TraceContext{}.Inject(c.Request.Context(), propagation.HeaderCarrier(req.Header))
	propagateRequestID(c, req)
}

// PropagateIdentityHeaders injects everything PropagateHeaders does plus the caller's
// identity — x-user-id, x-tenant-id and x-tenant-roles from the RequestContext built by
// ProtectedMiddlewares — and the full configured OpenTelemetry propagator output
// (including W3C baggage when enabled).
//
// Use it ONLY for calls to trusted internal services behind the same gateway trust
// boundary: the downstream service will treat these headers as authenticated identity.
// Never use it for third-party or user-supplied URLs. Without a RequestContext no
// identity header is set. A nil c or req is a no-op.
func PropagateIdentityHeaders(c *gin.Context, req *http.Request) {
	if c == nil || c.Request == nil || req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	otel.GetTextMapPropagator().Inject(c.Request.Context(), propagation.HeaderCarrier(req.Header))
	propagateRequestID(c, req)

	if rc, ok := RequestContext(c); ok {
		req.Header.Set(domain.HeaderUserID, rc.UserID)
		req.Header.Set(domain.HeaderTenantID, rc.TenantID)
		if len(rc.Roles) > 0 {
			req.Header.Set(domain.HeaderTenantRoles, strings.Join(rc.Roles, ","))
		} else {
			// Never forward a roles header the caller did not verify.
			req.Header.Del(domain.HeaderTenantRoles)
		}
	}
}

func propagateRequestID(c *gin.Context, req *http.Request) {
	if reqID := c.GetString(middleware.CtxRequestIDKey); reqID != "" {
		req.Header.Set(domain.HeaderRequestID, reqID)
	}
}
