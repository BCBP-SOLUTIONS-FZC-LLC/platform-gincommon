package middleware

import (
	"errors"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/gin-gonic/gin"
)

// ContextMiddleware builds requestContext from validated gateway headers (requires RequireAuth).
func ContextMiddleware(ctxSvc port.ContextBuilder) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, tenantID, ok := identityFromRequest(c)
		if !ok {
			writeJSONError(c, http.StatusUnauthorized, "missing or invalid authentication headers")
			return
		}

		reqCtx, err := ctxSvc.BuildContext(
			tenantID,
			userID,
			domain.ParseRoles(c.GetHeader(domain.HeaderTenantRoles)),
			traceIDFromContext(c),
		)
		if err != nil {
			if errors.Is(err, domain.ErrMissingTenant) {
				writeJSONError(c, http.StatusBadRequest, "missing tenant id")
				return
			}
			if errors.Is(err, domain.ErrMissingUser) {
				writeJSONError(c, http.StatusBadRequest, "missing user id")
				return
			}
			writeJSONError(c, http.StatusBadRequest, "invalid request context")
			return
		}

		c.Set(CtxRequestContextKey, reqCtx)
		c.Next()
	}
}
