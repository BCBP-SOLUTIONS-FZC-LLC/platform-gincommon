package gincommon

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"

// Request transport headers (set by a trusted gateway; validated by ProtectedMiddlewares).
const (
	// HeaderUserID identifies the authenticated user (x-user-id).
	HeaderUserID = domain.HeaderUserID
	// HeaderTenantID identifies the tenant (x-tenant-id).
	HeaderTenantID = domain.HeaderTenantID
	// HeaderTenantRoles carries comma-separated roles (x-tenant-roles).
	HeaderTenantRoles = domain.HeaderTenantRoles
	// HeaderRequestID is the client or generated correlation ID (x-request-id).
	HeaderRequestID = domain.HeaderRequestID
)

// Response correlation headers set by CorrelationHeadersMiddleware.
const (
	// HeaderTraceIDResponse echoes the OpenTelemetry trace ID (X-Trace-ID).
	HeaderTraceIDResponse = domain.HeaderTraceIDResponse
	// HeaderRequestIDResponse echoes the request ID (X-Request-ID).
	HeaderRequestIDResponse = domain.HeaderRequestIDResponse
)
