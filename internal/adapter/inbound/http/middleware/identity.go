package middleware

import (
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/gin-gonic/gin"
)

// ambiguousIdentityHeader returns the first identity header (x-user-id, then
// x-tenant-id) the request carries more than once. A client could append its own value
// next to the gateway's, and trusting the first would make the outcome depend on header
// order, so repeated identity headers are rejected (as gRPC rejects repeated metadata).
func ambiguousIdentityHeader(c *gin.Context) (string, bool) {
	for _, h := range []string{domain.HeaderUserID, domain.HeaderTenantID} {
		if len(c.Request.Header.Values(h)) > 1 {
			return h, true
		}
	}
	return "", false
}

// rolesHeader joins every x-tenant-roles value with ",", so a gateway that sends roles
// as repeated headers is handled like gRPC repeated metadata.
func rolesHeader(c *gin.Context) string {
	return strings.Join(c.Request.Header.Values(domain.HeaderTenantRoles), ",")
}

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
