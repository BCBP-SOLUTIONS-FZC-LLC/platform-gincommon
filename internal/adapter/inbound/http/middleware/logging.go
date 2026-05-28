package middleware

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware emits a structured http_request log after each request (sanitized fields).
// Uses a panicked-sentinel defer so that panicking requests are always logged (as status 500)
// rather than silently dropped when c.Next() unwinds via panic.
// Nil logger is a no-op — the middleware passes through without logging or panicking.
func LoggingMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if logger == nil {
			c.Next()
			return
		}

		start := time.Now()
		panicked := true // set to false on normal return; stays true if c.Next() panics

		defer func() {
			traceID, _ := c.Get(CtxTraceIDKey)
			requestID, _ := c.Get(CtxRequestIDKey)

			// Prefer context values (validated by RequireAuth/ContextMiddleware).
			// Fall back to raw header only for unauthenticated requests (e.g. 401 paths);
			// raw values are sanitized to prevent log injection.
			var tenantID, userID string
			if v, ok := c.Get(domain.HeaderTenantID); ok {
				if s, typeOK := v.(string); typeOK && s != "" {
					tenantID = s
				}
			}
			if tenantID == "" {
				tenantID = sanitizeLogValue(c.GetHeader(domain.HeaderTenantID))
			}
			if v, ok := c.Get(domain.HeaderUserID); ok {
				if s, typeOK := v.(string); typeOK && s != "" {
					userID = s
				}
			}
			if userID == "" {
				userID = sanitizeLogValue(c.GetHeader(domain.HeaderUserID))
			}

			status := c.Writer.Status()
			// During panic unwind PanicRecoveryMiddleware hasn't written the 500 yet,
			// so c.Writer.Status() returns Gin's default 200. Override to 500 here so
			// panicking requests are logged at Error level rather than Info.
			if panicked {
				status = http.StatusInternalServerError
			}
			fields := map[string]interface{}{
				"trace_id":    traceID,
				"request_id":  requestID,
				"tenant_id":   tenantID,
				"user_id":     userID,
				"method":      c.Request.Method,
				"path":        label.HTTPRoute(c),
				"status":      status,
				"duration_ms": float64(time.Since(start).Nanoseconds()) / 1e6,
			}

			safe := service.Sanitize(fields)
			switch {
			case status >= 500:
				logger.Error("http_request", safe)
			case status >= 400:
				logger.Warn("http_request", safe)
			default:
				logger.Info("http_request", safe)
			}
		}()

		c.Next()
		panicked = false
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
