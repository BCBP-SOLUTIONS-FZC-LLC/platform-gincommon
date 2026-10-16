package middleware

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
	"github.com/gin-gonic/gin"
)

// PanicRecoveryMiddleware recovers panics after the inner middleware (metrics, logging,
// tracing) has recorded them, logs the panic with trace/request correlation, and returns
// a 500 internal-error problem (application/problem+json, no detail).
//
//   - If the handler had already started writing the response, no problem body is
//     appended (it would corrupt the partial response); the request is only aborted.
//   - http.ErrAbortHandler is re-panicked after being logged at Debug level without a
//     stack, so net/http aborts the connection silently as it documents (it is the
//     sentinel handlers and httputil.ReverseProxy use to abort a response, not a bug).
//
// The server span is annotated by TracingMiddleware, which has already ended it by the
// time this deferred recovery runs.
func PanicRecoveryMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			err := recover()
			if err == nil {
				return
			}

			// Use the registered route template for the route field to prevent log
			// injection and cardinality from attacker-controlled raw URL paths;
			// the sanitized raw path is kept for human-readable context only.
			route := label.HTTPRoute(c)
			rawPath := sanitizeLogValue(c.Request.URL.Path)
			panicMsg := sanitizeLogValue(fmt.Sprint(err))

			if isAbortPanic(err) {
				if logger != nil {
					logger.Debug("request aborted", service.Sanitize(map[string]interface{}{
						"method":     sanitizeLogValue(c.Request.Method),
						"route":      route,
						"path":       rawPath,
						"trace_id":   traceIDFromContext(c),
						"request_id": requestIDFromContext(c),
					}))
				}
				// Re-panic the bare sentinel: net/http compares the panic value by
				// identity, so a wrapped ErrAbortHandler would otherwise be logged by
				// the server with a stack trace.
				panic(http.ErrAbortHandler)
			}

			// Log panic; fall back to stderr when no structured logger is configured.
			stack := debug.Stack()
			if logger != nil {
				fields := service.Sanitize(map[string]interface{}{
					"error_category": "runtime",
					"error_type":     "panic",
					"error_source":   "middleware",
					"panic":          panicMsg,
					"stack":          string(stack),
					"method":         sanitizeLogValue(c.Request.Method),
					"route":          route,
					"path":           rawPath,
					"status":         http.StatusInternalServerError,
					"trace_id":       traceIDFromContext(c),
					"request_id":     requestIDFromContext(c),
					"duration_ms":    float64(time.Since(start).Nanoseconds()) / 1e6,
				})
				logger.Error("panic recovered", fields)
			} else {
				log.Printf("panic recovered: %s\n%s", panicMsg, stack)
			}

			if c.Writer.Written() {
				// Headers (and possibly part of the body) are already on the wire;
				// appending a JSON error would corrupt the response, and returning
				// normally would let net/http finish it as a complete 200. Abort the
				// connection instead so the client sees a truncated response.
				c.Abort()
				panic(http.ErrAbortHandler)
			}
			// Written even when the handler called c.Abort() before panicking:
			// otherwise the client would get Gin's default 200 while metrics, logs
			// and the span all record 500.
			WriteProblem(c, problem.NewInternalError(nil))
		}()

		c.Next()
	}
}

// StatusClientClosedRequest is the non-standard status (nginx's 499) that metrics,
// the access log and the server span record for a request aborted with
// http.ErrAbortHandler before any response was written. It classifies the abort as
// a client_error / 4xx outcome, never as a server panic.
const StatusClientClosedRequest = 499

// isAbortPanic reports whether a recovered panic value is http.ErrAbortHandler (or
// wraps it): the sentinel net/http documents for aborting a response, which handlers
// and httputil.ReverseProxy use when the client went away. It is not a bug, so the
// observability middleware must not count it as a panic or a 5xx.
func isAbortPanic(v any) bool {
	e, ok := v.(error)
	return ok && errors.Is(e, http.ErrAbortHandler)
}

// abortedStatus is the status recorded for an aborted request: the status the
// handler already wrote, else StatusClientClosedRequest.
func abortedStatus(c *gin.Context) int {
	if c.Writer.Written() {
		return c.Writer.Status()
	}
	return StatusClientClosedRequest
}
