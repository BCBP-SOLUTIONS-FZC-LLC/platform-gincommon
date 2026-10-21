package problem

import (
	"context"
	"errors"
	"maps"
	"net/http"
)

// Error is an error that renders as a problem: handlers return (or c.Error) one and
// the global error handler writes it. It is the Go form of the platform's
// BusinessException; the New*Error constructors are its specialisations
// (ValidationException, ResourceNotFoundException, ...).
//
// Only Type, Detail, Code, ValidationErrors and Extensions reach the client. Err,
// the cause, is for errors.Is/As and server-side logs: it is never rendered.
type Error struct {
	// Type is the registry entry; it fixes the title and status.
	Type Type
	// Detail is the occurrence-specific sentence the service wrote. Never put an
	// error's text, SQL, secrets or tenant information in it.
	Detail string
	// Code overrides Type.Code when set.
	Code string
	// ValidationErrors is the validationErrors extension.
	ValidationErrors []FieldError
	// Extensions are RFC 9457 extension members sent at the top level of the
	// problem (see WithExtension). They are client-visible, and an internal-error
	// never carries any.
	Extensions map[string]any
	// Err is the cause. It is never sent to the client.
	Err error

	// unclassified is set by FromError for an error it could not classify.
	unclassified bool
}

// NewError is an Error of any registered type, typically a domain type:
//
//	problem.NewError(TenderNotFound, "Tender 123 was not found.")
func NewError(t Type, detail string) *Error {
	return &Error{Type: t, Detail: detail}
}

// NewValidationError is a 400 validation-failed error listing the invalid fields.
func NewValidationError(fields ...FieldError) *Error {
	return &Error{Type: TypeValidationFailed, ValidationErrors: fields}
}

// NewBadRequestError is a 400 bad-request error (a malformed request that is not a
// field validation failure).
func NewBadRequestError(detail string) *Error { return NewError(TypeBadRequest, detail) }

// NewUnauthorizedError is a 401 unauthorized error (AuthenticationException).
func NewUnauthorizedError(detail string) *Error { return NewError(TypeUnauthorized, detail) }

// NewForbiddenError is a 403 forbidden error (AuthorizationException).
func NewForbiddenError(detail string) *Error { return NewError(TypeForbidden, detail) }

// NewNotFoundError is a 404 not-found error (ResourceNotFoundException).
func NewNotFoundError(detail string) *Error { return NewError(TypeNotFound, detail) }

// NewConflictError is a 409 conflict error (ConflictException).
func NewConflictError(detail string) *Error { return NewError(TypeConflict, detail) }

// NewBusinessRuleError is a 422 business-rule-violation error
// (BusinessRuleViolationException).
func NewBusinessRuleError(detail string) *Error {
	return NewError(TypeBusinessRuleViolation, detail)
}

// NewRateLimitError is a 429 rate-limit-exceeded error (RateLimitException).
func NewRateLimitError(detail string) *Error { return NewError(TypeRateLimitExceeded, detail) }

// NewNotImplementedError is a 501 not-implemented error (UnsupportedOperationException).
func NewNotImplementedError(detail string) *Error { return NewError(TypeNotImplemented, detail) }

// NewServiceUnavailableError is a 503 service-unavailable error. detail must be a fixed
// sentence ("The database is unavailable."), never an error's text.
func NewServiceUnavailableError(detail string) *Error {
	return NewError(TypeServiceUnavailable, detail)
}

// NewInternalError is the 500 internal-error for cause. The cause is kept for logs
// and errors.Is/As; the problem has no detail.
func NewInternalError(cause error) *Error {
	return &Error{Type: TypeInternalError, Err: cause}
}

// WithCode returns e with its errorCode set to code (e.g. "TENDER_NOT_FOUND").
func (e *Error) WithCode(code string) *Error {
	e.Code = code
	return e
}

// WithCause returns e with cause recorded for errors.Is/As and logs; it is never sent.
func (e *Error) WithCause(cause error) *Error {
	e.Err = cause
	return e
}

// WithFields returns e with fields appended to its validationErrors.
func (e *Error) WithFields(fields ...FieldError) *Error {
	e.ValidationErrors = append(e.ValidationErrors, fields...)
	return e
}

// WithExtension returns e with the extension member name set to value, e.g. the
// version a client needs to retry an optimistic-lock conflict:
//
//	problem.NewConflictError("The tenant was modified concurrently.").
//		WithExtension("record_version", 7)
//
// The member is sent at the top level of the problem body, after the standard
// members. A name that is not a ValidExtensionName (a standard or platform member
// such as "status" or "traceId", or not a letter followed by letters, digits or
// "_", 3+ characters) is dropped when the problem is built, so it can never replace
// a standard member. value is marshalled with encoding/json and is client-visible:
// never put error text, SQL, secrets, tokens or tenant data in it. An
// internal-error drops every extension, as it drops its detail.
func (e *Error) WithExtension(name string, value any) *Error {
	if e.Extensions == nil {
		e.Extensions = make(map[string]any)
	}
	e.Extensions[name] = value
	return e
}

// WithExtensions returns e with every entry of ext set as an extension member (see
// WithExtension); later values replace earlier ones of the same name.
func (e *Error) WithExtensions(ext map[string]any) *Error {
	if len(ext) == 0 {
		return e
	}
	if e.Extensions == nil {
		e.Extensions = make(map[string]any, len(ext))
	}
	maps.Copy(e.Extensions, ext)
	return e
}

// ErrorCode is the errorCode the problem is sent with: Code, else Type.Code.
func (e *Error) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	return e.Type.Code
}

// Status is the HTTP status of the problem (Type.Status, 500 when unset).
func (e *Error) Status() int {
	if e.Type.Status == 0 {
		return http.StatusInternalServerError
	}
	return e.Type.Status
}

// Error implements error. It includes the cause, so it belongs in logs only.
func (e *Error) Error() string {
	msg := e.Type.Title
	if msg == "" {
		msg = http.StatusText(e.Status())
	}
	if code := e.ErrorCode(); code != "" {
		msg = code + ": " + msg
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// Unclassified reports whether FromError fell back to internal-error because it
// could not classify the error: an expected failure that reached the top-level
// handler unmapped, which the platform treats as a programming defect. An
// internal-error built deliberately with NewInternalError is classified.
func (e *Error) Unclassified() bool { return e.unclassified }

// Error codes the classifier uses for errors that are not *Error.
const (
	// CodeRequestTimeout is the errorCode of a 503 for an exceeded request deadline.
	CodeRequestTimeout = "REQUEST_TIMEOUT"
)

// Fixed details the classifier uses.
const (
	DetailRequestTimeout  = "The request timed out."
	DetailPayloadTooLarge = "The request body is too large."
)

// FromError classifies any error as a problem error (the global exception mapping):
//
//   - an *Error anywhere in the chain is used as it is;
//   - errors.ErrUnsupported is 501 not-implemented;
//   - context.DeadlineExceeded is 503 service-unavailable, errorCode REQUEST_TIMEOUT;
//   - *http.MaxBytesError is 413 payload-too-large;
//   - anything else is 500 internal-error with no detail, so no error text, SQL or
//     internal detail ever reaches the client, marked Unclassified.
//
// A nil err returns nil. Framework-specific errors (Gin binding errors, an
// Authorizer's ErrForbidden) are classified by the framework integration first.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) && pe != nil {
		return pe
	}
	var mbe *http.MaxBytesError
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return &Error{Type: TypeNotImplemented, Err: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Type: TypeServiceUnavailable, Code: CodeRequestTimeout, Detail: DetailRequestTimeout, Err: err}
	case errors.As(err, &mbe):
		return &Error{Type: TypePayloadTooLarge, Detail: DetailPayloadTooLarge, Err: err}
	}
	return &Error{Type: TypeInternalError, Err: err, unclassified: true}
}
