package middleware

import "github.com/gin-gonic/gin"

// CorrelationHeadersMiddleware adds X-Trace-ID and X-Request-ID to every response.
// Place after RequestIDMiddleware and TracingMiddleware.
//
// The headers are set BEFORE c.Next(): response headers are flushed with the first
// body write, so headers added after the handler has written would never reach the
// client (they would only be visible on a test recorder's live header map).
func CorrelationHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		attachCorrelationHeaders(
			c,
			traceIDFromContext(c),
			requestIDFromContext(c),
		)
		c.Next()
	}
}
