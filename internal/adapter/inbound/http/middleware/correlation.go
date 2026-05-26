package middleware

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
)

func traceIDFromContext(c *gin.Context) string {

	if v, ok := c.Get(CtxTraceIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""

}

func requestIDFromContext(c *gin.Context) string {

	if v, ok := c.Get(CtxRequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""

}

func attachCorrelationHeaders(c *gin.Context, traceID, requestID string) {

	if traceID != "" {
		c.Header(domain.HeaderTraceIDResponse, traceID)
	}
	if requestID != "" {
		c.Header(domain.HeaderRequestIDResponse, requestID)
	}

}

func writeJSONError(c *gin.Context, status int, message string) {

	traceID := traceIDFromContext(c)
	requestID := requestIDFromContext(c)

	attachCorrelationHeaders(c, traceID, requestID)

	c.AbortWithStatusJSON(status, gin.H{
		"error":      message,
		"status":     status,
		"trace_id":   traceID,
		"request_id": requestID,
	})

}
