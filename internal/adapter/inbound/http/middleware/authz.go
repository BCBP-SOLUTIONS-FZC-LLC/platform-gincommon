package middleware

import (
	"errors"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
	"github.com/gin-gonic/gin"
)

// AuthZMiddleware authorizes the request using requestContext (requires ContextMiddleware).
// A denial is a 403 forbidden problem; a missing context, a missing Authorizer or an
// Authorizer error is a 500 internal-error problem without detail (the cause is logged).
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
			WriteProblem(c, problem.NewInternalError(nil))
			return
		}

		ctx, ok := ctxVal.(*domain.RequestContext)
		if !ok || ctx == nil {
			if logger != nil {
				logger.Error("authz: invalid request context type", map[string]any{
					"action": action, "resource": resource,
				})
			}
			WriteProblem(c, problem.NewInternalError(nil))
			return
		}

		if auth == nil {
			if logger != nil {
				logger.Error("authz: no authorizer configured", map[string]any{
					"action": action, "resource": resource,
				})
			}
			WriteProblem(c, problem.NewInternalError(nil))
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
				WriteProblem(c, problem.NewForbiddenError(DetailForbidden).WithCause(err))
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
			WriteProblem(c, problem.NewInternalError(err))
			return
		}
		c.Next()
	}
}
