package domain

// Gateway request headers (see package doc for trust boundary).
const (
	// HeaderUserID is the authenticated user id (x-user-id).
	HeaderUserID = "x-user-id"
	// HeaderTenantID is the tenant id (x-tenant-id).
	HeaderTenantID = "x-tenant-id"
	// HeaderTenantRoles is comma-separated roles (x-tenant-roles).
	HeaderTenantRoles = "x-tenant-roles"
	// HeaderRequestID is the correlation id (x-request-id).
	HeaderRequestID = "x-request-id"
)

// Response correlation headers echoed to clients.
const (
	// HeaderTraceIDResponse is the OpenTelemetry trace id (X-Trace-ID).
	HeaderTraceIDResponse = "X-Trace-ID"
	// HeaderRequestIDResponse is the request correlation id (X-Request-ID).
	HeaderRequestIDResponse = "X-Request-ID"
)

// Validation limits for transport header values.
const (
	// MaxHeaderValueLen is the maximum length for identity header values.
	MaxHeaderValueLen = 256
	// MaxRoleLen is the maximum length of a single role token in x-tenant-roles.
	MaxRoleLen = 64
	// MaxRolesHeaderLen is the maximum total length in bytes of x-tenant-roles (after
	// joining repeated gRPC metadata entries with ","). Longer values are rejected.
	MaxRolesHeaderLen = 4096
	// MaxRoles is the maximum number of distinct valid roles accepted from
	// x-tenant-roles. More roles than this are rejected, not truncated.
	MaxRoles = 64
	// MaxRequestIDLen is the maximum length of x-request-id when accepted from clients.
	MaxRequestIDLen = 128
)
