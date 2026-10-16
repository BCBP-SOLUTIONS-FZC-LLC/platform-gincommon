package middleware

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
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
