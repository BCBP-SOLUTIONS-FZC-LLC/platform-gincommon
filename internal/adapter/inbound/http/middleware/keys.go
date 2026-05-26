package middleware

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"

const (
	// CtxTraceIDKey stores the OpenTelemetry trace ID in Gin context.
	CtxTraceIDKey = "trace_id"
	// CtxRequestIDKey stores the request correlation ID in Gin context.
	CtxRequestIDKey = "request_id"
	// CtxRequestContextKey stores *domain.RequestContext after ContextMiddleware.
	CtxRequestContextKey = domain.GinRequestContextKey
)
