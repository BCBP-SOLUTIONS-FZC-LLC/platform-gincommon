package middleware

import "github.com/gin-gonic/gin"

// CorrelationHeadersMiddleware adds X-Trace-ID and X-Request-ID to every response.
// Place after RequestIDMiddleware and TracingMiddleware.
func CorrelationHeadersMiddleware() gin.HandlerFunc {

	return func(c *gin.Context) {
		c.Next()

		attachCorrelationHeaders(
			c,
			traceIDFromContext(c),
			requestIDFromContext(c),
		)
	}

}
