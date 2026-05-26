package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PanicRecoveryMiddleware recovers panics after MetricsMiddleware has recorded http_panic_total,
// logs the panic with trace/request correlation, and returns a JSON error response.
func PanicRecoveryMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			if err := recover(); err != nil {

				// Resolve path
				route := label.HTTPRoute(c)
				if route == "" {
					route = c.Request.URL.Path
				}
				path := c.Request.URL.Path

				// Log panic
				if logger != nil {
					fields := service.Sanitize(map[string]interface{}{
						"error_category": "runtime",
						"error_type":     "panic",
						"error_source":   "middleware",
						"panic":          fmt.Sprint(err),
						"stack":          string(debug.Stack()),
						"method":         c.Request.Method,
						"route":          route,
						"path":           path,
						"status":         http.StatusInternalServerError,
						"trace_id":       traceIDFromContext(c),
						"request_id":     requestIDFromContext(c),
						"duration":       time.Since(start),
					})
					logger.Error("panic recovered", fields)
				}

				span := trace.SpanFromContext(c.Request.Context())
				span.RecordError(fmt.Errorf("panic recovered: %v", err))
				span.SetStatus(codes.Error, "panic")
				span.SetAttributes(
					attribute.Int("http.status_code", http.StatusInternalServerError),
					attribute.String("http.method", c.Request.Method),
				)

				// Return JSON response
				if !c.IsAborted() {
					writeJSONError(
						c,
						http.StatusInternalServerError,
						http.StatusText(http.StatusInternalServerError),
					)
				}

			}
		}()

		c.Next()
	}
}
