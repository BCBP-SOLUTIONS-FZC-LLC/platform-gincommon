package middleware

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"

const (
	// CtxTraceIDKey stores the OpenTelemetry trace ID in Gin context.
	CtxTraceIDKey = "trace_id"
	// CtxRequestIDKey stores the request correlation ID in Gin context.
	CtxRequestIDKey = "request_id"
	// CtxRequestContextKey stores *domain.RequestContext after ContextMiddleware.
	CtxRequestContextKey = domain.GinRequestContextKey
	// ctxIdentityRejectedKey is set by ContextMiddleware when it rejects the request
	// after RequireAuth accepted the identity headers, so the access log does not
	// report that identity as verified.
	ctxIdentityRejectedKey = "gincommon.identity_rejected"
)
