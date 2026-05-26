package gincommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
)

// RequestContextKey is the Gin context key for *domain.RequestContext.
const RequestContextKey = domain.GinRequestContextKey

// RequestContext returns the domain request context built by ContextMiddleware.
func RequestContext(c *gin.Context) (*domain.RequestContext, bool) {
	v, ok := c.Get(RequestContextKey)
	if !ok {
		return nil, false
	}
	ctx, ok := v.(*domain.RequestContext)
	return ctx, ok
}
