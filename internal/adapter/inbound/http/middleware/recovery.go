package middleware

import (
	"fmt"
	"log"
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

				// Resolve path — use the registered route template for the log field to
				// prevent log injection from attacker-controlled raw URL paths.
				route := label.HTTPRoute(c)
				// sanitizeLogValue strips control chars and truncates; apply to the
				// raw path for human-readable context only when no template matched.
				rawPath := sanitizeLogValue(c.Request.URL.Path)

				// Log panic; fall back to stderr when no structured logger is configured.
				stack := debug.Stack()
				if logger != nil {
					fields := service.Sanitize(map[string]interface{}{
						"error_category": "runtime",
						"error_type":     "panic",
						"error_source":   "middleware",
						"panic":          sanitizeLogValue(fmt.Sprint(err)),
						"stack":          string(stack),
						"method":         c.Request.Method,
						"route":          route,
						"path":           rawPath,
						"status":         http.StatusInternalServerError,
						"trace_id":       traceIDFromContext(c),
						"request_id":     requestIDFromContext(c),
						"duration_ms":    float64(time.Since(start).Nanoseconds()) / 1e6,
					})
					logger.Error("panic recovered", fields)
				} else {
					log.Printf("panic recovered: %v\n%s", err, stack)
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
