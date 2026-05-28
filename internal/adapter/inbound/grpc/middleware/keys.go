package middleware

import "context"

// Metadata keys for gRPC — same names as HTTP headers so gateway-injected values pass through.
const (
	MetaUserID      = "x-user-id"
	MetaTenantID    = "x-tenant-id"
	MetaTenantRoles = "x-tenant-roles"
	MetaRequestID   = "x-request-id"
	MetaTraceparent = "traceparent"
	MetaTracestate  = "tracestate"
)

type grpcRequestIDKey struct{}

// ContextWithRequestID stores the request ID in a Go context.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, grpcRequestIDKey{}, requestID)
}

// RequestIDFromContext retrieves the request ID from a Go context (set by RequestIDInterceptor).
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(grpcRequestIDKey{}).(string)
	return v
}
