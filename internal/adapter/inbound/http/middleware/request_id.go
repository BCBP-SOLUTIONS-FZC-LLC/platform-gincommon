package middleware

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDMiddleware reads x-request-id or generates a UUID; invalid client values are replaced.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID, ok := domain.NormalizeRequestID(c.GetHeader(domain.HeaderRequestID))
		if !ok {
			reqID = uuid.New().String()
		}

		c.Set(CtxRequestIDKey, reqID)
		c.Writer.Header().Set(domain.HeaderRequestID, reqID)
		c.Next()
	}
}
