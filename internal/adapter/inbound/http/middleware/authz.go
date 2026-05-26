package middleware

import (
	"errors"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
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
func AuthZMiddleware(auth port.Authorizer, action, resource string) gin.HandlerFunc {
	return func(c *gin.Context) {

		ctxVal, ok := c.Get(CtxRequestContextKey)
		if !ok {
			writeJSONError(c, http.StatusInternalServerError, ErrCtxMissing)
			return
		}

		ctx, ok := ctxVal.(*domain.RequestContext)
		if !ok {
			writeJSONError(c, http.StatusInternalServerError, ErrCtxInvalid)
			return
		}

		if err := auth.Authorize(ctx.UserID, ctx.Roles, action, resource); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				writeJSONError(c, http.StatusForbidden, ErrForbidden)
				return
			}
			writeJSONError(c, http.StatusInternalServerError, ErrInternal)
			return
		}
		c.Next()
	}
}
