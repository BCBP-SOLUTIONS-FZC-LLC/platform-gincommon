package domain

// GinRequestContextKey is the Gin context key for *RequestContext.
const GinRequestContextKey = "requestContext"

// RequestContext carries tenant-scoped identity propagated from gateway headers.
type RequestContext struct {
	TenantID string   `json:"tenant_id"` // From gateway x-tenant-id
	UserID   string   `json:"user_id"`   // From gateway x-user-id
	Roles    []string `json:"roles"`     // From gateway x-tenant-roles
	TraceID  string   `json:"trace_id"`  // OpenTelemetry trace ID for the request
}
