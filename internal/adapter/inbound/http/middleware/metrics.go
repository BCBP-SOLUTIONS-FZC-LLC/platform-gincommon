package middleware

import (
	"math"
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/gin-gonic/gin"
)

// MetricsMiddleware records Prometheus HTTP metrics (duration, sizes, panics, 429).
// A single deferred closure covers both normal and panic paths so that panicking
// requests are counted in http_requests_total / platform_http_requests_total (as 5xx),
// not only in the panic counters.
// Pass a non-nil logger to emit a warn-level log entry for every 429 response.
func MetricsMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !metrics.HTTPEnabled() {
			c.Next()
			return
		}

		method := c.Request.Method
		route := label.HTTPRoute(c)

		metrics.HTTPActiveInc(route)
		start := time.Now()
		panicked := true // set to false on normal return; remains true if c.Next() panics

		defer func() {
			metrics.HTTPActiveDec(route)

			elapsed := time.Since(start).Seconds()
			code := c.Writer.Status()
			// During panic unwind PanicRecoveryMiddleware (outer) hasn't written
			// the 500 yet, so c.Writer.Status() returns Gin's default 200 (http.StatusOK).
			// Without the panicked guard the panicking request would be recorded as 2xx
			// success — inflating error-budget counters incorrectly. Only override for
			// the panic path; a deliberate c.Abort() must keep its actual status code.
			if panicked {
				code = http.StatusInternalServerError
			}
			statusClass := metrics.HTTPStatusClass(code)
			errorClass := metrics.RequestOutcome(code)

			if panicked {
				metrics.RecordHTTPPanic(method, route)
			}

			metrics.RecordHTTP(method, route, statusClass, errorClass, elapsed,
				metrics.RequestBodySize(c.Request.ContentLength), math.Max(0, float64(c.Writer.Size())))

			if code == http.StatusTooManyRequests {
				metrics.RecordHTTPRateLimited(method, route)
				if logger != nil {
					logger.Warn("rate limited", map[string]any{
						"method": method,
						"route":  route,
						"status": code,
					})
				}
			}
		}()

		c.Next()
		panicked = false
	}
}
