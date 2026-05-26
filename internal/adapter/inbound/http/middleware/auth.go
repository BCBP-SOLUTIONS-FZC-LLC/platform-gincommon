package middleware

import (
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/gin-gonic/gin"
)

// RequireAuth rejects requests without valid gateway identity headers (x-user-id, x-tenant-id).
// Identity must be injected by a trusted API gateway; see domain transport header documentation.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, okUser := domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderUserID))
		tenantID, okTenant := domain.NormalizeHeaderValue(c.GetHeader(domain.HeaderTenantID))
		if !okUser || !okTenant {
			writeJSONError(c, http.StatusUnauthorized, "missing or invalid authentication headers")
			return
		}
		c.Set(domain.HeaderUserID, userID)
		c.Set(domain.HeaderTenantID, tenantID)
		c.Next()
	}
}
