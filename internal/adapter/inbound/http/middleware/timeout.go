package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/gin-gonic/gin"
)

// TimeoutMiddleware sets a deadline on the request context. Downstream context-aware
// operations (DB queries, HTTP client calls) will fail with context.DeadlineExceeded
// when the deadline is exceeded. Does not forcibly kill the handler goroutine.
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if timeout <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		// If the context expired and the handler wrote nothing, emit a 503 so
		// callers get an explicit timeout signal rather than a silent 200 body.
		if ctx.Err() != nil && !c.Writer.Written() {
			metrics.RecordHTTPTimeout(c.Request.Method, label.HTTPRoute(c))
			writeJSONError(c, http.StatusServiceUnavailable, "request timeout")
		}
	}
}
