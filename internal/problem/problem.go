// Package problem implements RFC 9457 Problem Details for HTTP APIs: the platform's
// single error contract. It depends on the standard library only, so the model, the
// type registry, the builder, the error types and the client decoder are usable from
// any Go HTTP code (Gin services, gateways, BFFs, clients). pkg/problem is its public
// facade; the Gin integration lives in pkg/gincommon.
package problem

import (
	"time"
)

// ContentType is the RFC 9457 media type of every platform error response.
const ContentType = "application/problem+json"

// DefaultTypeBase is the base of every type URI: {base}/{domain}/{error}. It is the
// same default platform-pgcommon-py uses (DEFAULT_PROBLEM_TYPE_BASE).
const DefaultTypeBase = "https://errors.company.com"

// TimestampLayout is RFC 3339 UTC with second precision, e.g. 2026-10-15T10:45:22Z.
const TimestampLayout = "2006-01-02T15:04:05Z"

// Problem is an RFC 9457 problem details object with the platform extensions.
//
// The standard members are Type, Title, Status, Detail and Instance. The platform
// extensions are TraceID, ErrorCode, TenantID, Timestamp and ValidationErrors.
// Optional members are omitted when empty, so a 5xx carries no detail. Field order
// matches the platform specification.
type Problem struct {
	// Type is the absolute, stable type URI ({base}/{domain}/{error}), or
	// "about:blank" for a status without a registered type (RFC 9457 §4.2.1).
	Type string `json:"type"`
	// Title is the short, fixed summary of the type. It never varies per occurrence.
	Title string `json:"title"`
	// Status is the HTTP status code; it always equals the response status.
	Status int `json:"status"`
	// Detail is a sentence the service wrote about this occurrence. It never
	// carries exception text, SQL, secrets or tenant information.
	Detail string `json:"detail,omitempty"`
	// Instance is the request path (never the query string, which may carry tokens).
	Instance string `json:"instance,omitempty"`
	// TraceID correlates the response with the server's logs and traces.
	TraceID string `json:"traceId,omitempty"`
	// ErrorCode is the stable, upper snake case, machine-readable code.
	ErrorCode string `json:"errorCode,omitempty"`
	// TenantID is set only for a request with a verified tenant context.
	TenantID string `json:"tenantId,omitempty"`
	// Timestamp is when the problem was created, formatted with TimestampLayout.
	Timestamp string `json:"timestamp,omitempty"`
	// ValidationErrors lists the invalid fields of a validation-failed problem.
	ValidationErrors []FieldError `json:"validationErrors,omitempty"`
}

// FieldError is one entry of the validationErrors extension.
type FieldError struct {
	// Field is the JSON path of the invalid field (e.g. "deadline", "items[0].sku").
	Field string `json:"field"`
	// Message is a fixed phrase ("is required", "must be in the future"). It never
	// echoes the rejected value, which may be PII.
	Message string `json:"message"`
}

// FormatTimestamp formats t (converted to UTC) with TimestampLayout.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}
