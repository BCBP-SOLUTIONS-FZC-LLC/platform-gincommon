package service

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
)

// RBACService is a reference Authorizer for demos and tests. Production services should
// implement port.Authorizer with policy backed by IAM/OPA and must not rely on client headers alone.
type RBACService struct{}

// NewRBACService constructs the reference RBAC authorizer.
func NewRBACService() *RBACService {
	return &RBACService{}
}

// Authorize applies reference rules: read allowed; delete requires admin role; other actions denied.
// Tenant, user and resource are ignored by the reference rules. A nil rc is denied.
func (s *RBACService) Authorize(_ context.Context, rc *domain.RequestContext, action, _ string) error {
	if rc == nil {
		return domain.ErrForbidden
	}
	switch action {
	case "read":
		return nil
	case "delete":
		for _, r := range rc.Roles {
			if r == "admin" {
				return nil
			}
		}
		return domain.ErrForbidden
	default:
		return domain.ErrForbidden
	}
}
