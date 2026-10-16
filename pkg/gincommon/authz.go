package gincommon

import (
	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
)

// NewDemoRBAC returns a reference port.Authorizer for demos and tests only — not for production IAM.
// Logic: "read" is always permitted; "delete" requires the "admin" role; all other actions are denied.
func NewDemoRBAC() *service.RBACService {
	return service.NewRBACService()
}

// NewRBAC is a deprecated alias for NewDemoRBAC. Use NewDemoRBAC in new code.
//
// Deprecated: use NewDemoRBAC.
func NewRBAC() *service.RBACService {
	return NewDemoRBAC()
}

// RequirePermission returns Gin middleware that calls auth.Authorize for the route action/resource.
// Requires ProtectedMiddlewares (RequestContext in Gin context).
// Authz denials are not logged; use RequirePermissionWithLogger to emit audit log entries.
func RequirePermission(action, resource string, auth port.Authorizer) gin.HandlerFunc {
	return middleware.AuthZMiddleware(auth, action, resource, nil)
}

// RequirePermissionWithLogger is like RequirePermission but logs access denials and internal
// errors via the provided logger. Use cfg.Logger from gincommon.Config to share the same logger
// used by the rest of the middleware stack.
func RequirePermissionWithLogger(action, resource string, auth port.Authorizer, logger port.Logger) gin.HandlerFunc {
	return middleware.AuthZMiddleware(auth, action, resource, logger)
}
