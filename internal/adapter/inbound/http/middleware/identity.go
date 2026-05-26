package middleware

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
)

func identityFromRequest(c *gin.Context) (userID, tenantID string, ok bool) {
	if v, exists := c.Get(domain.HeaderUserID); exists {
		if s, typeOK := v.(string); typeOK {
			userID = s
		}
	}
	if v, exists := c.Get(domain.HeaderTenantID); exists {
		if s, typeOK := v.(string); typeOK {
			tenantID = s
		}
	}
	if userID == "" {
		userID, ok = domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderUserID))
		if !ok {
			return "", "", false
		}
	}
	if tenantID == "" {
		tenantID, ok = domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderTenantID))
		if !ok {
			return "", "", false
		}
	}
	return userID, tenantID, true
}
