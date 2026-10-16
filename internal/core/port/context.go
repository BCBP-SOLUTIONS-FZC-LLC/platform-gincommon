package port

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"

// ContextBuilder assembles a RequestContext from gateway-provided identity values.
type ContextBuilder interface {
	BuildContext(tenantID, userID string, roles []string, traceID string, clientIP string) (*domain.RequestContext, error)
}
