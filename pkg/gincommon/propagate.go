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

// PropagateHeaders injects trace context, request ID, and identity headers from the inbound
// Gin request context into outbound, so downstream services can correlate the call.
// Call this before executing any outbound HTTP request from a handler.
// req must be non-nil; passing nil panics with a nil pointer dereference in http.Header.
func PropagateHeaders(c *gin.Context, req *http.Request) {
	if req == nil {
		return
	}
	// Inject W3C traceparent (and baggage if enabled) via OTel propagator.
	otel.GetTextMapPropagator().Inject(c.Request.Context(), propagation.HeaderCarrier(req.Header))

	// Propagate request correlation ID.
	if reqID := c.GetString(middleware.CtxRequestIDKey); reqID != "" {
		req.Header.Set(domain.HeaderRequestID, reqID)
	}

	// Propagate identity headers so the downstream service receives tenant context.
	if rc, ok := RequestContext(c); ok {
		req.Header.Set(domain.HeaderUserID, rc.UserID)
		req.Header.Set(domain.HeaderTenantID, rc.TenantID)
		if len(rc.Roles) > 0 {
			req.Header.Set(domain.HeaderTenantRoles, joinRoles(rc.Roles))
		}
	}
}

func joinRoles(roles []string) string {
	return strings.Join(roles, ",")
}
