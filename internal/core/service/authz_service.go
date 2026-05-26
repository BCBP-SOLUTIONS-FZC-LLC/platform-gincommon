package service

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"

// RBACService is a reference Authorizer for demos and tests. Production services should
// implement port.Authorizer with policy backed by IAM/OPA and must not rely on client headers alone.
type RBACService struct{}

// NewRBACService constructs the reference RBAC authorizer.
func NewRBACService() *RBACService {
	return &RBACService{}
}

// Authorize applies reference rules: read allowed; delete requires admin role; other actions denied.
func (s *RBACService) Authorize(userID string, roles []string, action string, resource string) error {
	_ = userID
	_ = resource

	switch action {
	case "read":
		return nil
	case "delete":
		for _, r := range roles {
			if r == "admin" {
				return nil
			}
		}
		return domain.ErrForbidden
	default:
		return domain.ErrForbidden
	}
}
