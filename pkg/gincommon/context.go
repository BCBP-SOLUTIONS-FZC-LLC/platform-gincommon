package gincommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
)

// RequestContextKey is the Gin context key for *domain.RequestContext.
const RequestContextKey = domain.GinRequestContextKey

// RequestContext returns the domain request context built by ContextMiddleware.
// Returns (nil, false) for both absent and typed-nil values, preventing callers
// from dereferencing a nil *RequestContext when ok is true.
func RequestContext(c *gin.Context) (*domain.RequestContext, bool) {
	v, ok := c.Get(RequestContextKey)
	if !ok {
		return nil, false
	}
	rc, ok := v.(*domain.RequestContext)
	if !ok || rc == nil {
		return nil, false
	}
	return rc, true
}

// TraceIDFromContext returns the OpenTelemetry trace ID stored by TracingMiddleware.
// Returns an empty string when tracing is not enabled or before TracingMiddleware runs.
func TraceIDFromContext(c *gin.Context) string {
	return c.GetString(middleware.CtxTraceIDKey)
}

// RequestIDFromContext returns the correlation request ID stored by RequestIDMiddleware.
// Returns an empty string when the middleware has not run.
func RequestIDFromContext(c *gin.Context) string {
	return c.GetString(middleware.CtxRequestIDKey)
}
