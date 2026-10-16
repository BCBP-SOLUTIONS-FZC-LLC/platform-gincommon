package service

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"

// ContextService builds domain.RequestContext from validated gateway identity.
type ContextService struct{}

// NewContextService constructs a ContextService.
func NewContextService() *ContextService {
	return &ContextService{}
}

// BuildContext assembles RequestContext or returns ErrMissingTenant / ErrMissingUser.
func (s *ContextService) BuildContext(tid, uid string, roles []string, trace string, clientIP string) (*domain.RequestContext, error) {

	if tid == "" {
		return nil, domain.ErrMissingTenant
	}

	if uid == "" {
		return nil, domain.ErrMissingUser
	}

	return &domain.RequestContext{
		TenantID: tid,
		UserID:   uid,
		Roles:    roles,
		TraceID:  trace,
		ClientIP: clientIP,
	}, nil
}
