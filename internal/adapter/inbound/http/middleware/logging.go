package middleware

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware emits a structured http_request log after each request (sanitized fields).
// Uses a panicked-sentinel defer so that panicking requests are always logged (as status 500)
// rather than silently dropped when c.Next() unwinds via panic.
// A panic with http.ErrAbortHandler (a deliberate abort) is logged at Debug with
// "aborted": true and the written status, else 499, never at Error.
// Nil logger is a no-op — the middleware passes through without logging or panicking.
func LoggingMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if logger == nil {
			c.Next()
			return
		}

		start := time.Now()
		panicked := true // set to false on normal return; stays true if c.Next() panics
		aborted := false // panicked with http.ErrAbortHandler (client abort, not a bug)

		defer func() {
			traceID, _ := c.Get(CtxTraceIDKey)
			requestID, _ := c.Get(CtxRequestIDKey)

			status := c.Writer.Status()
			// During panic unwind PanicRecoveryMiddleware hasn't written the 500 yet,
			// so c.Writer.Status() returns Gin's default 200. Override to 500 here so
			// panicking requests are logged at Error level rather than Info.
			switch {
			case aborted:
				status = abortedStatus(c)
			case panicked:
				status = http.StatusInternalServerError
			}
			fields := map[string]interface{}{
				"trace_id":    traceID,
				"request_id":  requestID,
				"method":      sanitizeLogValue(c.Request.Method),
				"path":        label.HTTPRoute(c),
				"status":      status,
				"duration_ms": float64(time.Since(start).Nanoseconds()) / 1e6,
			}
			addIdentityLogFields(c, fields)

			safe := service.Sanitize(fields)
			switch {
			case aborted:
				// A deliberate abort (typically a client disconnect) is not an error.
				safe["aborted"] = true
				logger.Debug("http_request", safe)
			case status >= 500:
				logger.Error("http_request", safe)
			case status >= 400:
				logger.Warn("http_request", safe)
			default:
				logger.Info("http_request", safe)
			}
		}()
		// Runs first on panic: flags an http.ErrAbortHandler abort, then re-panics.
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

// addIdentityLogFields sets the identity fields of the http_request log entry.
//
// tenant_id / user_id are only ever the identity the protected middleware accepted:
// the RequestContext built by ContextMiddleware, else the values RequireAuth
// validated when ContextMiddleware did not reject the request. On routes without the protected middleware (public routes, or requests
// RequireAuth or ContextMiddleware rejected) the raw gateway headers are untrusted client input, so they
// are logged — sanitized and truncated — under claimed_tenant_id / claimed_user_id,
// never under the verified field names, and omitted when absent.
func addIdentityLogFields(c *gin.Context, fields map[string]interface{}) {
	if v, ok := c.Get(CtxRequestContextKey); ok {
		if rc, typeOK := v.(*domain.RequestContext); typeOK && rc != nil {
			fields["tenant_id"] = rc.TenantID
			fields["user_id"] = rc.UserID
			return
		}
	}
	setIdentityField(c, fields, domain.HeaderTenantID, "tenant_id", "claimed_tenant_id")
	setIdentityField(c, fields, domain.HeaderUserID, "user_id", "claimed_user_id")
}

func setIdentityField(c *gin.Context, fields map[string]interface{}, header, verifiedKey, claimedKey string) {
	// ContextMiddleware rejected the request after RequireAuth accepted the headers:
	// no RequestContext was built, so the identity is not verified.
	rejected := c.GetBool(ctxIdentityRejectedKey)
	if v, ok := c.Get(header); ok && !rejected {
		if s, typeOK := v.(string); typeOK && s != "" {
			fields[verifiedKey] = s
			return
		}
	}
	if raw := sanitizeLogValue(c.GetHeader(header)); raw != "" {
		fields[claimedKey] = raw
	}
}

// sanitizeLogValue strips control characters and truncates to 256 bytes at a
// rune boundary to prevent log injection and ensure valid UTF-8 in log output.
func sanitizeLogValue(v string) string {
	const maxLen = 256
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	if len(v) <= maxLen {
		return v
	}
	// Walk back from the byte limit to the nearest rune-start boundary so the
	// result is always valid UTF-8 (multi-byte sequences are at most 4 bytes).
	i := maxLen
	for i > 0 && !utf8.RuneStart(v[i]) {
		i--
	}
	return v[:i]
}
