package middleware

import (
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
)

// RequireAuth rejects requests without valid gateway identity headers (x-user-id, x-tenant-id).
// Identity must be injected by a trusted API gateway; see domain transport header documentation.
// A repeated x-user-id or x-tenant-id header is rejected with 401 "ambiguous <header> header".
// A non-nil logger receives a warn-level "auth: request rejected" audit entry for every 401;
// nil disables the audit log.
func RequireAuth(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h, ambiguous := ambiguousIdentityHeader(c); ambiguous {
			logAuthRejection(c, logger, "ambiguous "+h+" header")
			writeJSONError(c, http.StatusUnauthorized, "ambiguous "+h+" header")
			return
		}
		userID, okUser := domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderUserID))
		tenantID, okTenant := domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderTenantID))
		if !okUser || !okTenant {
			reason := "missing or invalid " + domain.HeaderTenantID
			if !okUser {
				reason = "missing or invalid " + domain.HeaderUserID
			}
			logAuthRejection(c, logger, reason)
			writeJSONError(c, http.StatusUnauthorized, "missing or invalid authentication headers")
			return
		}
		c.Set(domain.HeaderUserID, userID)
		c.Set(domain.HeaderTenantID, tenantID)
		c.Next()
	}
}

// logAuthRejection emits the 401 audit entry. Header values are client input: they are
// sanitized (control characters stripped, truncated to 256 bytes) and logged under
// claimed_* names so they are never mistaken for verified identity.
func logAuthRejection(c *gin.Context, logger port.Logger, reason string) {
	if logger == nil {
		return
	}
	logger.Warn("auth: request rejected", service.Sanitize(rejectionFields(c, reason, http.StatusUnauthorized)))
}

// rejectionFields builds the audit fields shared by the 401 and 400 rejection entries.
func rejectionFields(c *gin.Context, reason string, status int) map[string]interface{} {
	fields := map[string]interface{}{
		"reason":     sanitizeLogValue(reason),
		"method":     sanitizeLogValue(c.Request.Method),
		"route":      label.HTTPRoute(c),
		"status":     status,
		"client_ip":  c.ClientIP(),
		"trace_id":   traceIDFromContext(c),
		"request_id": requestIDFromContext(c),
	}
	if v := sanitizeLogValue(c.GetHeader(domain.HeaderUserID)); v != "" {
		fields["claimed_user_id"] = v
	}
	if v := sanitizeLogValue(c.GetHeader(domain.HeaderTenantID)); v != "" {
		fields["claimed_tenant_id"] = v
	}
	return fields
}
