package problem

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// PlatformDomain is the error domain of the cross-cutting types every service shares.
// Only this package defines types in it; services define types in their own domain.
const PlatformDomain = "platform"

// Type is an entry of the error type registry: a stable {domain}/{error} identifier
// with the fixed title, status and default error code of every occurrence. A Type
// carries no base URI, so the same Type renders under any organisation's error host
// (see URI); the identifier never contains a version.
type Type struct {
	// Domain is the owning error domain ("platform", "tender", "workflow", ...).
	Domain string
	// Name is the error slug within the domain ("not-found", "invalid-stage").
	Name string
	// Title is the fixed summary sent as the problem's title.
	Title string
	// Status is the HTTP status every occurrence is sent with.
	Status int
	// Code is the default errorCode (upper snake case).
	Code string
}

// ID is the type's version-independent identifier, "{domain}/{name}".
func (t Type) ID() string {
	if t.Domain == "" && t.Name == "" {
		return ""
	}
	return t.Domain + "/" + t.Name
}

// URI is the type URI under base ({base}/{domain}/{name}); an empty base means
// DefaultTypeBase and a trailing "/" on base is ignored. A zero Type (no domain and
// no name) renders as "about:blank".
func (t Type) URI(base string) string {
	if t.ID() == "" {
		return "about:blank"
	}
	return TypeURI(base, t.Domain, t.Name)
}

// TypeURI joins base, domain and error into a type URI, the way platform-pgcommon-py's
// type_uri does: {base}/{domain}/{error}, with an empty base meaning DefaultTypeBase.
func TypeURI(base, domain, name string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = DefaultTypeBase
	}
	return base + "/" + domain + "/" + name
}

// The platform error types. Every service uses these for the cross-cutting errors
// and defines its own types (NewType) only for its domain's errors.
var (
	// TypeBadRequest is a malformed request that is not a field validation failure.
	TypeBadRequest = Type{PlatformDomain, "bad-request", "Bad Request", http.StatusBadRequest, "BAD_REQUEST"}
	// TypeValidationFailed is a request whose fields failed validation (validationErrors).
	TypeValidationFailed = Type{PlatformDomain, "validation-failed", "Validation failed", http.StatusBadRequest, "VALIDATION_FAILED"}
	// TypeUnauthorized is a request without a valid identity (authentication failure).
	TypeUnauthorized = Type{PlatformDomain, "unauthorized", "Unauthorized", http.StatusUnauthorized, "UNAUTHORIZED"}
	// TypeForbidden is an authenticated request the identity may not perform.
	TypeForbidden = Type{PlatformDomain, "forbidden", "Forbidden", http.StatusForbidden, "FORBIDDEN"}
	// TypeNotFound is a resource (or route) that does not exist.
	TypeNotFound = Type{PlatformDomain, "not-found", "Not Found", http.StatusNotFound, "NOT_FOUND"}
	// TypeMethodNotAllowed is a route that exists without the requested method.
	TypeMethodNotAllowed = Type{PlatformDomain, "method-not-allowed", "Method Not Allowed", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"}
	// TypeConflict is a request that conflicts with the resource's current state.
	TypeConflict = Type{PlatformDomain, "conflict", "Conflict", http.StatusConflict, "CONFLICT"}
	// TypePayloadTooLarge is a request body over the accepted size.
	TypePayloadTooLarge = Type{PlatformDomain, "payload-too-large", "Payload Too Large", http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"}
	// TypeUnsupportedMediaType is a request body in a media type the endpoint does not accept.
	TypeUnsupportedMediaType = Type{PlatformDomain, "unsupported-media-type", "Unsupported Media Type", http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"}
	// TypeBusinessRuleViolation is a well-formed request that a business rule rejects.
	TypeBusinessRuleViolation = Type{PlatformDomain, "business-rule-violation", "Business rule violation", http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION"}
	// TypeRateLimitExceeded is a request over the caller's rate limit.
	TypeRateLimitExceeded = Type{PlatformDomain, "rate-limit-exceeded", "Too Many Requests", http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED"}
	// TypeInternalError is any unhandled server error. It never carries a detail.
	TypeInternalError = Type{PlatformDomain, "internal-error", "Internal Server Error", http.StatusInternalServerError, "INTERNAL_ERROR"}
	// TypeNotImplemented is an operation the service does not support.
	TypeNotImplemented = Type{PlatformDomain, "not-implemented", "Not Implemented", http.StatusNotImplemented, "NOT_IMPLEMENTED"}
	// TypeServiceUnavailable is a temporary inability to serve (overload, a dependency
	// down, a request deadline exceeded inside the service).
	TypeServiceUnavailable = Type{PlatformDomain, "service-unavailable", "Service Unavailable", http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"}
	// TypeGatewayTimeout is an upstream that did not answer a gateway in time.
	TypeGatewayTimeout = Type{PlatformDomain, "gateway-timeout", "Gateway Timeout", http.StatusGatewayTimeout, "GATEWAY_TIMEOUT"}
)

// PlatformTypes returns the platform error types in registry order.
func PlatformTypes() []Type {
	return []Type{
		TypeBadRequest, TypeValidationFailed, TypeUnauthorized, TypeForbidden, TypeNotFound,
		TypeMethodNotAllowed, TypeConflict, TypePayloadTooLarge, TypeUnsupportedMediaType,
		TypeBusinessRuleViolation, TypeRateLimitExceeded, TypeInternalError,
		TypeNotImplemented, TypeServiceUnavailable, TypeGatewayTimeout,
	}
}

// TypeForStatus is the platform type for a bare HTTP error status (one written
// without a problem, e.g. Gin's 404 for an unregistered route). A 400 maps to
// TypeBadRequest, not TypeValidationFailed. A status without a platform type gets
// an about:blank type titled with the status text (RFC 9457 §4.2.1).
func TypeForStatus(status int) Type {
	if status == http.StatusBadRequest {
		return TypeBadRequest
	}
	for _, t := range PlatformTypes() {
		if t.Status == status {
			return t
		}
	}
	title := http.StatusText(status)
	if title == "" {
		title = "Error"
	}
	return Type{Title: title, Status: status, Code: codeFromTitle(title)}
}

func codeFromTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(title) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteByte('_')
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

var (
	slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
)

// ErrInvalidType is wrapped by every NewType error.
var ErrInvalidType = errors.New("problem: invalid error type")

// NewType defines a domain error type, e.g.
//
//	TenderNotFound = problem.MustType("tender", "not-found", "Tender not found", 404, "TENDER_NOT_FOUND")
//
// domain and name are lower-case kebab slugs (no version), status is a 4xx or 5xx,
// title is non-empty and code is upper snake case. The "platform" domain is
// reserved for PlatformTypes. Register every domain type in the error type registry
// (docs/problem-details/registry.md) so its URI stays stable.
func NewType(domain, name, title string, status int, code string) (Type, error) {
	switch {
	case !slugPattern.MatchString(domain):
		return Type{}, fmt.Errorf("%w: domain %q is not a lower-case kebab slug", ErrInvalidType, domain)
	case domain == PlatformDomain:
		return Type{}, fmt.Errorf("%w: domain %q is reserved for the platform types", ErrInvalidType, domain)
	case !slugPattern.MatchString(name):
		return Type{}, fmt.Errorf("%w: name %q is not a lower-case kebab slug", ErrInvalidType, name)
	case strings.TrimSpace(title) == "":
		return Type{}, fmt.Errorf("%w: %s/%s has an empty title", ErrInvalidType, domain, name)
	case status < 400 || status > 599:
		return Type{}, fmt.Errorf("%w: %s/%s status %d is not a 4xx or 5xx", ErrInvalidType, domain, name, status)
	case !codePattern.MatchString(code):
		return Type{}, fmt.Errorf("%w: %s/%s code %q is not upper snake case", ErrInvalidType, domain, name, code)
	}
	return Type{Domain: domain, Name: name, Title: title, Status: status, Code: code}, nil
}

// MustType is NewType that panics on an invalid definition; use it for package-level
// type variables so a bad definition fails at startup.
func MustType(domain, name, title string, status int, code string) Type {
	t, err := NewType(domain, name, title, status, code)
	if err != nil {
		panic(err)
	}
	return t
}
