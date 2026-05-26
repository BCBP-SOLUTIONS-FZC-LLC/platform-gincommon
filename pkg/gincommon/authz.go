package gincommon

import (
	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
)

// NewRBAC returns a reference port.Authorizer for demos and tests (not for production IAM).
func NewRBAC() *service.RBACService {
	return service.NewRBACService()
}

// RequirePermission returns Gin middleware that calls auth.Authorize for the route action/resource.
// Requires ProtectedMiddlewares (RequestContext in Gin context).
func RequirePermission(action, resource string, auth port.Authorizer) gin.HandlerFunc {
	return middleware.AuthZMiddleware(auth, action, resource)
}
