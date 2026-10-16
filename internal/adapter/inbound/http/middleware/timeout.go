package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
	"github.com/gin-gonic/gin"
)

// TimeoutMiddleware sets a deadline on the request context. Downstream context-aware
// operations (DB queries, HTTP client calls) fail with context.DeadlineExceeded once
// the deadline passes.
//
// It does NOT interrupt the handler goroutine: the handler keeps running until it
// returns, so handlers must honour c.Request.Context() for the timeout to take effect.
// When the deadline expired (context.DeadlineExceeded — a client disconnect,
// context.Canceled, is not a timeout) and the handler returned without writing a
// response, it writes a 503 service-unavailable problem (errorCode REQUEST_TIMEOUT)
// and increments the timeout counter. A response
// the handler already wrote is left untouched, and so is one whose status the handler
// set without a body (e.g. c.Status(http.StatusNoContent)): see responseStarted.
//
// Placement: put it AFTER (inside) the observability middleware — gincommon
// Config.RequestTimeout does this — so that metrics, the access log and the server
// span, which read the status after their c.Next() returns, record the same 503 the
// client receives. Placed outside them it would write the 503 after they had already
// recorded the handler's (unwritten, default 200) status.
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && !responseStarted(c) {
			metrics.RecordHTTPTimeout(c.Request.Method, label.HTTPRoute(c))
			WriteProblem(c, problem.FromError(ctx.Err()))
		}
	}
}

// responseStarted reports whether the handler produced a response: it wrote a body or
// flushed headers (Written), or it set a status other than Gin's default 200 without
// writing (c.Status(204), c.AbortWithStatus(...)): Gin flushes that status after the
// chain returns, so it is the handler's response. Gin cannot distinguish an explicit
// c.Status(200) or response headers set without a status from "nothing set"; those
// still get the 503.
func responseStarted(c *gin.Context) bool {
	return c.Writer.Written() || c.Writer.Status() != http.StatusOK
}
