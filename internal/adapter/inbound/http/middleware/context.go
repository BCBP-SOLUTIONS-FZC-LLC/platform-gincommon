package middleware

import (
	"errors"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
)

// ContextMiddleware builds requestContext from validated gateway headers (requires RequireAuth).
//
// x-tenant-roles is parsed with domain.ParseRolesStrict: individually invalid role tokens
// are dropped, but a header longer than domain.MaxRolesHeaderLen or carrying more than
// domain.MaxRoles roles is rejected with 401, like any other invalid identity header.
// Repeated x-tenant-roles headers are joined with "," before parsing (parity with gRPC
// repeated metadata); a repeated x-user-id / x-tenant-id is rejected with 401.
// A non-nil logger receives a warn-level audit entry for every 401 ("auth: request
// rejected") and for every 400 from a ContextBuilder failure ("context: request
// rejected"); nil disables it.
func ContextMiddleware(ctxSvc port.ContextBuilder, logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Any rejection below means no RequestContext is built: flag it so the access
		// log keeps the identity RequireAuth accepted under claimed_* names.
		reject := func(status int, reason, msg string) {
			c.Set(ctxIdentityRejectedKey, true)
			if status == http.StatusUnauthorized {
				logAuthRejection(c, logger, reason)
			} else {
				logContextRejection(c, logger, status, reason)
			}
			writeJSONError(c, status, msg)
		}

		if h, ambiguous := ambiguousIdentityHeader(c); ambiguous {
			reject(http.StatusUnauthorized, "ambiguous "+h+" header", "ambiguous "+h+" header")
			return
		}
		userID, tenantID, ok := identityFromRequest(c)
		if !ok {
			reject(http.StatusUnauthorized, "missing or invalid authentication headers", "missing or invalid authentication headers")
			return
		}

		roles, err := domain.ParseRolesStrict(rolesHeader(c))
		if err != nil {
			reject(http.StatusUnauthorized, "invalid "+domain.HeaderTenantRoles, "invalid tenant roles")
			return
		}

		reqCtx, err := ctxSvc.BuildContext(
			tenantID,
			userID,
			roles,
			traceIDFromContext(c),
			c.ClientIP(),
		)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrMissingTenant):
				reject(http.StatusUnauthorized, "missing tenant id", "missing tenant id")
			case errors.Is(err, domain.ErrMissingUser):
				reject(http.StatusUnauthorized, "missing user id", "missing user id")
			default:
				// The builder's error text is logged (sanitized) but never sent to the client.
				reject(http.StatusBadRequest, "invalid request context: "+err.Error(), "invalid request context")
			}
			return
		}

		c.Set(CtxRequestContextKey, reqCtx)
		c.Next()
	}
}

// logContextRejection emits the warn-level audit entry for a non-401 rejection (a
// ContextBuilder failure), with the same correlation and claimed-identity fields as
// logAuthRejection.
func logContextRejection(c *gin.Context, logger port.Logger, status int, reason string) {
	if logger == nil {
		return
	}
	fields := rejectionFields(c, reason, status)
	logger.Warn("context: request rejected", service.Sanitize(fields))
}
