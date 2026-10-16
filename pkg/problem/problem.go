// Package problem is the platform's RFC 9457 Problem Details contract for Go: the
// model, the error type registry, the builder, the error types handlers return, the
// trace-id resolution, the client decoder and the shared OpenAPI components. It
// depends on the standard library only, so gateways, BFFs and clients can use it
// without Gin; pkg/gincommon renders it for Gin services.
//
// Every platform HTTP error is an application/problem+json body:
//
//	{
//	  "type": "https://errors.company.com/platform/not-found",
//	  "title": "Not Found",
//	  "status": 404,
//	  "detail": "Tender 123 was not found.",
//	  "instance": "/api/tenders/123",
//	  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
//	  "errorCode": "TENDER_NOT_FOUND",
//	  "tenantId": "tenant-001",
//	  "timestamp": "2026-10-15T10:45:22Z"
//	}
//
// The specification's exception names map to Go as follows: ProblemDetailsBuilder
// is Builder; BusinessException is Error (NewError with a domain Type);
// ValidationException, ResourceNotFoundException, ConflictException,
// UnauthorizedException and ForbiddenException are NewValidationError,
// NewNotFoundError, NewConflictError, NewUnauthorizedError and NewForbiddenError.
// See docs/problem-details/ for the registry and the full mapping.
package problem

import (
	"io"
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
)

// ContentType is the RFC 9457 media type, application/problem+json.
const ContentType = problem.ContentType

// DefaultTypeBase is the default type URI base, https://errors.company.com.
const DefaultTypeBase = problem.DefaultTypeBase

// TimestampLayout is the timestamp extension's format (RFC 3339 UTC, seconds).
const TimestampLayout = problem.TimestampLayout

// PlatformDomain is the reserved error domain of the platform types.
const PlatformDomain = problem.PlatformDomain

// CodeRequestTimeout is the errorCode of a 503 for an exceeded request deadline.
const CodeRequestTimeout = problem.CodeRequestTimeout

// MaxRequestIDLength is the longest x-request-id accepted as a traceId.
const MaxRequestIDLength = problem.MaxRequestIDLength

// Problem is an RFC 9457 problem details object with the platform extensions.
type Problem = problem.Problem

// FieldError is one entry of the validationErrors extension.
type FieldError = problem.FieldError

// Type is an error type registry entry ({domain}/{error}, title, status, code).
type Type = problem.Type

// Error is an error that renders as a problem (the platform's BusinessException).
type Error = problem.Error

// Builder assembles a Problem (the platform's ProblemDetailsBuilder).
type Builder = problem.Builder

// The platform error types.
var (
	TypeBadRequest            = problem.TypeBadRequest
	TypeValidationFailed      = problem.TypeValidationFailed
	TypeUnauthorized          = problem.TypeUnauthorized
	TypeForbidden             = problem.TypeForbidden
	TypeNotFound              = problem.TypeNotFound
	TypeMethodNotAllowed      = problem.TypeMethodNotAllowed
	TypeConflict              = problem.TypeConflict
	TypePayloadTooLarge       = problem.TypePayloadTooLarge
	TypeUnsupportedMediaType  = problem.TypeUnsupportedMediaType
	TypeBusinessRuleViolation = problem.TypeBusinessRuleViolation
	TypeRateLimitExceeded     = problem.TypeRateLimitExceeded
	TypeInternalError         = problem.TypeInternalError
	TypeNotImplemented        = problem.TypeNotImplemented
	TypeServiceUnavailable    = problem.TypeServiceUnavailable
	TypeGatewayTimeout        = problem.TypeGatewayTimeout
)

// ErrInvalidType is wrapped by every NewType error.
var ErrInvalidType = problem.ErrInvalidType

// ErrNotProblem is returned by FromResponse for a non-problem response.
var ErrNotProblem = problem.ErrNotProblem

// PlatformTypes returns the platform error types in registry order.
func PlatformTypes() []Type { return problem.PlatformTypes() }

// TypeForStatus is the platform type for a bare HTTP error status, or an
// about:blank type for a status without one.
func TypeForStatus(status int) Type { return problem.TypeForStatus(status) }

// TypeURI joins base, domain and error into {base}/{domain}/{error}.
func TypeURI(base, domain, name string) string { return problem.TypeURI(base, domain, name) }

// NewType defines a domain error type; see MustType.
func NewType(domain, name, title string, status int, code string) (Type, error) {
	return problem.NewType(domain, name, title, status, code)
}

// MustType is NewType that panics on an invalid definition:
//
//	var TenderNotFound = problem.MustType("tender", "not-found", "Tender not found", 404, "TENDER_NOT_FOUND")
func MustType(domain, name, title string, status int, code string) Type {
	return problem.MustType(domain, name, title, status, code)
}

// NewBuilder starts a problem of type t.
func NewBuilder(t Type) *Builder { return problem.NewBuilder(t) }

// BuilderFor starts a problem from e (type, detail, errorCode, validationErrors).
func BuilderFor(e *Error) *Builder { return problem.BuilderFor(e) }

// NewError is an Error of any registered type, typically a domain type.
func NewError(t Type, detail string) *Error { return problem.NewError(t, detail) }

// NewValidationError is a 400 validation-failed error (ValidationException).
func NewValidationError(fields ...FieldError) *Error { return problem.NewValidationError(fields...) }

// NewBadRequestError is a 400 bad-request error.
func NewBadRequestError(detail string) *Error { return problem.NewBadRequestError(detail) }

// NewUnauthorizedError is a 401 unauthorized error (UnauthorizedException).
func NewUnauthorizedError(detail string) *Error { return problem.NewUnauthorizedError(detail) }

// NewForbiddenError is a 403 forbidden error (ForbiddenException).
func NewForbiddenError(detail string) *Error { return problem.NewForbiddenError(detail) }

// NewNotFoundError is a 404 not-found error (ResourceNotFoundException).
func NewNotFoundError(detail string) *Error { return problem.NewNotFoundError(detail) }

// NewConflictError is a 409 conflict error (ConflictException).
func NewConflictError(detail string) *Error { return problem.NewConflictError(detail) }

// NewBusinessRuleError is a 422 business-rule-violation error.
func NewBusinessRuleError(detail string) *Error { return problem.NewBusinessRuleError(detail) }

// NewRateLimitError is a 429 rate-limit-exceeded error.
func NewRateLimitError(detail string) *Error { return problem.NewRateLimitError(detail) }

// NewNotImplementedError is a 501 not-implemented error (UnsupportedOperationException).
func NewNotImplementedError(detail string) *Error { return problem.NewNotImplementedError(detail) }

// NewServiceUnavailableError is a 503 service-unavailable error with a fixed detail.
func NewServiceUnavailableError(detail string) *Error {
	return problem.NewServiceUnavailableError(detail)
}

// NewInternalError is the 500 internal-error for cause; the cause is never sent.
func NewInternalError(cause error) *Error { return problem.NewInternalError(cause) }

// FromError classifies any error: an *Error as it is, errors.ErrUnsupported 501,
// context.DeadlineExceeded 503 REQUEST_TIMEOUT, *http.MaxBytesError 413, anything
// else 500 internal-error without detail.
func FromError(err error) *Error { return problem.FromError(err) }

// FormatTimestamp formats t with TimestampLayout.
func FormatTimestamp(t time.Time) string { return problem.FormatTimestamp(t) }

// TraceIDFromTraceparent is the trace-id of a valid W3C traceparent, or "".
func TraceIDFromTraceparent(v string) string { return problem.TraceIDFromTraceparent(v) }

// SanitizeRequestID is v trimmed when it is 1-128 characters of [A-Za-z0-9._:-], else "".
func SanitizeRequestID(v string) string { return problem.SanitizeRequestID(v) }

// ResolveTraceID is a problem's traceId: traceparent's trace-id, else the sanitized
// x-request-id, else spanTraceID, else a new random 32-hex id.
func ResolveTraceID(traceparent, requestID, spanTraceID string) string {
	return problem.ResolveTraceID(traceparent, requestID, spanTraceID)
}

// NewTraceID is a random 32-hex id, shaped like a W3C trace-id.
func NewTraceID() string { return problem.NewTraceID() }

// IsProblemContentType reports whether v is application/problem+json.
func IsProblemContentType(v string) bool { return problem.IsProblemContentType(v) }

// Decode parses a problem body (unknown members are ignored).
func Decode(r io.Reader) (*Problem, error) { return problem.Decode(r) }

// FromResponse parses resp's problem body; ErrNotProblem when it is not one. Branch
// on Status, Type and ErrorCode, never on the text of Title or Detail.
func FromResponse(resp *http.Response) (*Problem, error) { return problem.FromResponse(resp) }

// OpenAPIComponents returns the shared OpenAPI 3.1 components (YAML): the
// ProblemDetails and ValidationError schemas and the standard error responses.
func OpenAPIComponents() []byte { return problem.OpenAPIComponents() }

// StandardResponses maps each status every endpoint declares (400, 401, 403, 404,
// 409, 422, 429, 500) to its shared response component name.
func StandardResponses() map[int]string { return problem.StandardResponses() }
