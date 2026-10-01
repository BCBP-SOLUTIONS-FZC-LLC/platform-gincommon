package grpccommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
)

// Public aliases for the core types that appear in this package's signatures, so a
// consuming module can name them (implement Authorizer or Logger, declare fields)
// without importing the library's internal packages, which Go forbids.

// RequestContextData is the per-request identity built by the Protected/Full interceptors
// (TenantID, UserID, Roles, TraceID, ClientIP), returned by RequestContextFromContext
// and passed to Authorizer.Authorize. It is the same type as
// gincommon.RequestContextData (same name in both packages), so one Authorizer
// serves both stacks.
type RequestContextData = domain.RequestContext

// Authorizer is the per-RPC authorization port used by RequirePermission and
// RequireStreamPermission: Authorize(ctx, rc, action, resource) returns nil to allow,
// an error wrapping ErrForbidden to deny (PermissionDenied), and any other error for
// an internal failure (Internal).
type Authorizer = port.Authorizer

// Logger is the structured logging port accepted by Config.Logger,
// RequirePermissionWithLogger and Shutdown.
type Logger = port.Logger

// DemoRBAC is the reference Authorizer returned by NewDemoRBAC (demos and tests only).
type DemoRBAC = service.RBACService

// ErrForbidden is the sentinel an Authorizer wraps to deny access (PermissionDenied).
var ErrForbidden = domain.ErrForbidden
