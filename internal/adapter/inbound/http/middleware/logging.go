package middleware

import (
	"strings"
	"time"
	"unicode"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware emits a structured http_request log after each request (sanitized fields).
func LoggingMiddleware(logger port.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {

		start := time.Now()
		c.Next()

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

		fields := map[string]interface{}{
			"trace_id":   traceID,
			"request_id": requestID,
			"tenant_id":  tenantID,
			"user_id":    userID,
			"path":       label.HTTPRoute(c),
			"status":     c.Writer.Status(),
			"duration":   time.Since(start).String(),
		}

		safe := service.Sanitize(fields)
		logger.Info("http_request", safe)
	}
}

// sanitizeLogValue strips control characters and truncates to 256 bytes
// to prevent log injection from untrusted header values.
func sanitizeLogValue(v string) string {
	const maxLen = 256
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	if len(v) > maxLen {
		return v[:maxLen]
	}
	return v
}
