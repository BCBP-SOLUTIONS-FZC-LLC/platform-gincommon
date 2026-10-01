package port

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
)

// Authorizer performs route-level authorization for the caller described by rc.
//
// ctx is the request context (deadline, cancellation, trace span), so policy lookups
// against IAM/OPA can honour request timeouts and appear in the request trace. rc is
// never nil when called by the gincommon/grpccommon middleware and carries the
// gateway-asserted TenantID, UserID and Roles, so policies can be tenant-scoped.
//
// Return nil to allow, an error wrapping domain.ErrForbidden to deny (403 /
// PermissionDenied), and any other error for an internal failure (500 / Internal).
type Authorizer interface {
	Authorize(ctx context.Context, rc *domain.RequestContext, action, resource string) error
}
