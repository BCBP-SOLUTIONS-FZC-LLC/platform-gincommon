package middleware

import (
	"errors"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
)

// JSON error message constants returned by AuthZMiddleware.
const (
	ErrCtxMissing = "request context missing"
	ErrCtxInvalid = "invalid request context"
	ErrForbidden  = "forbidden"
	ErrInternal   = "internal server error"
)

// AuthZMiddleware authorizes the request using requestContext (requires ContextMiddleware).
// Pass a non-nil logger to emit audit log entries for denials; pass nil to skip logging.
func AuthZMiddleware(auth port.Authorizer, action, resource string, logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {

		ctxVal, ok := c.Get(CtxRequestContextKey)
		if !ok {
			if logger != nil {
				logger.Error("authz: request context missing", map[string]any{
					"action": action, "resource": resource,
				})
			}
			writeJSONError(c, http.StatusInternalServerError, ErrCtxMissing)
			return
		}

		ctx, ok := ctxVal.(*domain.RequestContext)
		if !ok || ctx == nil {
			if logger != nil {
				logger.Error("authz: invalid request context type", map[string]any{
					"action": action, "resource": resource,
				})
			}
			writeJSONError(c, http.StatusInternalServerError, ErrCtxInvalid)
			return
		}

		if auth == nil {
			writeJSONError(c, http.StatusInternalServerError, ErrInternal)
			return
		}
		if err := auth.Authorize(c.Request.Context(), ctx, action, resource); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				if logger != nil {
					logger.Warn("authz: access denied", map[string]any{
						"tenant_id": ctx.TenantID,
						"user_id":   ctx.UserID,
						"roles":     ctx.Roles,
						"action":    action,
						"resource":  resource,
					})
				}
				writeJSONError(c, http.StatusForbidden, ErrForbidden)
				return
			}
			if logger != nil {
				logger.Error("authz: internal authorization error", map[string]any{
					"tenant_id": ctx.TenantID,
					"user_id":   ctx.UserID,
					"action":    action,
					"resource":  resource,
					"error":     service.SanitizeString(err.Error()),
				})
			}
			writeJSONError(c, http.StatusInternalServerError, ErrInternal)
			return
		}
		c.Next()
	}
}
