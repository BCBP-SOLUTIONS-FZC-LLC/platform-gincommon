package middleware

import (
	"time"

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

		tenantID := c.GetHeader(domain.HeaderTenantID)
		userID := c.GetHeader(domain.HeaderUserID)
		if v, ok := c.Get(domain.HeaderTenantID); ok {
			if s, typeOK := v.(string); typeOK && s != "" {
				tenantID = s
			}
		}
		if v, ok := c.Get(domain.HeaderUserID); ok {
			if s, typeOK := v.(string); typeOK && s != "" {
				userID = s
			}
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
