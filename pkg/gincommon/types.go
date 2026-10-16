package gincommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
)

// Public aliases for the core types that appear in this package's signatures, so a
// consuming module can name them (implement Authorizer or Logger, declare fields)
// without importing the library's internal packages, which Go forbids.

// RequestContextData is the per-request identity built by ProtectedMiddlewares
// (TenantID, UserID, Roles, TraceID, ClientIP) and passed to Authorizer.Authorize.
// It is named RequestContextData because RequestContext is the accessor function;
// it is the same type as grpccommon.RequestContextData.
type RequestContextData = domain.RequestContext

// Authorizer is the route-level authorization port used by RequirePermission:
// Authorize(ctx, rc, action, resource) returns nil to allow, an error wrapping
// ErrForbidden to deny (403), and any other error for an internal failure (500).
type Authorizer = port.Authorizer

// Logger is the structured logging port accepted by Config.Logger,
// RequirePermissionWithLogger and Shutdown.
type Logger = port.Logger

// DemoRBAC is the reference Authorizer returned by NewDemoRBAC (demos and tests only).
type DemoRBAC = service.RBACService

// ErrForbidden is the sentinel an Authorizer wraps to deny access (403).
var ErrForbidden = domain.ErrForbidden
