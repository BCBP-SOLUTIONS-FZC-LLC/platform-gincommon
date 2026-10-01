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
// requests are counted in platform_http_requests_total (as 5xx), not only in the panic
// counter. A panic with http.ErrAbortHandler (a deliberate abort, typically a client
// disconnect) is not a panic: it is recorded with the written status, else 499
// (StatusClientClosedRequest, error_class client_error), and the panic counter is not
// incremented.
// Every 429 is counted. Pass a non-nil logger to also emit a warn-level "rate limited"
// log entry, sampled to at most one per route per RateLimitLogInterval (the entry's
// "suppressed" field counts the 429s on that route not logged since the previous
// entry), so a burst of throttled traffic cannot flood the log pipeline.
func MetricsMiddleware(logger port.Logger) gin.HandlerFunc {
	limiter := newRouteLogLimiter(RateLimitLogInterval)
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
		aborted := false // panicked with http.ErrAbortHandler (client abort, not a bug)

		defer func() {
			metrics.HTTPActiveDec(route)

			elapsed := time.Since(start).Seconds()
			code := c.Writer.Status()
			// During panic unwind PanicRecoveryMiddleware (outer) hasn't written
			// the 500 yet, so c.Writer.Status() returns Gin's default 200 (http.StatusOK).
			// Without the panicked guard the panicking request would be recorded as 2xx
			// success — inflating error-budget counters incorrectly. Only override for
			// the panic path; a deliberate c.Abort() must keep its actual status code.
			// An http.ErrAbortHandler abort is not a server failure: it is recorded with
			// the written status, else 499 (client_error), and never as a panic.
			switch {
			case aborted:
				code = abortedStatus(c)
			case panicked:
				code = http.StatusInternalServerError
			}
			statusClass := metrics.HTTPStatusClass(code)
			errorClass := metrics.RequestOutcome(code)

			if panicked && !aborted {
				metrics.RecordHTTPPanic(method, route)
			}

			metrics.RecordHTTP(method, route, statusClass, errorClass, elapsed,
				metrics.RequestBodySize(c.Request.ContentLength), math.Max(0, float64(c.Writer.Size())))

			if code == http.StatusTooManyRequests {
				metrics.RecordHTTPRateLimited(method, route)
				if logger != nil {
					if suppressed, ok := limiter.allow(route, time.Now()); ok {
						logger.Warn("rate limited", map[string]any{
							"method":     metrics.HTTPMethodLabel(method),
							"route":      route,
							"status":     code,
							"suppressed": suppressed,
						})
					}
				}
			}
		}()
		// Runs first on panic: flags an http.ErrAbortHandler abort for the recording
		// defer above, then re-panics the same value for PanicRecoveryMiddleware.
		defer func() {
			if panicked {
				if r := recover(); r != nil {
					aborted = isAbortPanic(r)
					panic(r)
				}
			}
		}()

		c.Next()
		panicked = false
	}
}
