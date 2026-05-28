package domain

import "context"

// GinRequestContextKey is the Gin context key for *RequestContext.
const GinRequestContextKey = "requestContext"

// RequestContext carries tenant-scoped identity propagated from gateway headers.
type RequestContext struct {
	TenantID string   `json:"tenant_id"` // From gateway x-tenant-id
	UserID   string   `json:"user_id"`   // From gateway x-user-id
	Roles    []string `json:"roles"`     // From gateway x-tenant-roles
	TraceID  string   `json:"trace_id"`  // OpenTelemetry trace ID for the request
	ClientIP string   `json:"client_ip"` // Remote client IP (HTTP: c.ClientIP(); gRPC: peer.Addr)
}

type reqContextKey struct{}

// ContextWithRequestContext stores rc in a Go context.Context (used by gRPC interceptors).
func ContextWithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, reqContextKey{}, rc)
}

// RequestContextFromGoContext retrieves a RequestContext from a Go context.Context.
// Returns (nil, false) for both missing values and typed-nil pointers, preventing
// callers from dereferencing a nil *RequestContext when ok is true.
func RequestContextFromGoContext(ctx context.Context) (*RequestContext, bool) {
	rc, ok := ctx.Value(reqContextKey{}).(*RequestContext)
	if !ok || rc == nil {
		return nil, false
	}
	return rc, true
}
