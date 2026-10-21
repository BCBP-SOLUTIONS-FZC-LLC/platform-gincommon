package problem

import (
	"time"
)

// Builder assembles a Problem (the platform's ProblemDetailsBuilder). The zero
// values are safe: an empty base is DefaultTypeBase and an unset timestamp is the
// time Build runs.
//
//	p := problem.NewBuilder(problem.TypeNotFound).
//		Detail("Tender 123 was not found.").
//		Instance(r.URL.Path).
//		TraceID(traceID).
//		Build()
type Builder struct {
	base      string
	t         Type
	detail    string
	code      string
	instance  string
	traceID   string
	tenantID  string
	fields    []FieldError
	ext       map[string]any
	timestamp time.Time
}

// NewBuilder starts a problem of type t.
func NewBuilder(t Type) *Builder {
	return &Builder{t: t}
}

// BuilderFor starts a problem from e: its type, detail, errorCode, validationErrors
// and extension members. A nil e is a 500 internal-error.
func BuilderFor(e *Error) *Builder {
	if e == nil {
		return NewBuilder(TypeInternalError)
	}
	b := &Builder{t: e.Type, detail: e.Detail, code: e.Code, fields: e.ValidationErrors}
	return b.Extensions(e.Extensions)
}

// TypeBase sets the type URI base (default DefaultTypeBase).
func (b *Builder) TypeBase(base string) *Builder { b.base = base; return b }

// Detail sets the occurrence-specific sentence.
func (b *Builder) Detail(detail string) *Builder { b.detail = detail; return b }

// ErrorCode overrides the type's default errorCode.
func (b *Builder) ErrorCode(code string) *Builder { b.code = code; return b }

// Instance sets the instance member: the request path, without the query string.
func (b *Builder) Instance(path string) *Builder { b.instance = path; return b }

// TraceID sets the traceId extension.
func (b *Builder) TraceID(id string) *Builder { b.traceID = id; return b }

// TenantID sets the tenantId extension. Set it only from a verified tenant context.
func (b *Builder) TenantID(id string) *Builder { b.tenantID = id; return b }

// ValidationErrors appends to the validationErrors extension.
func (b *Builder) ValidationErrors(fields ...FieldError) *Builder {
	b.fields = append(b.fields, fields...)
	return b
}

// Extension sets the extension member name to value (RFC 9457 §3.2), replacing an
// earlier value of the same name. A name that is not a ValidExtensionName (a
// standard or platform member, or not letter + [A-Za-z0-9_], 3+ characters) is
// dropped by Build. value is sent to the client as JSON: never error text, SQL,
// secrets, tokens or tenant data.
func (b *Builder) Extension(name string, value any) *Builder {
	if b.ext == nil {
		b.ext = make(map[string]any)
	}
	b.ext[name] = value
	return b
}

// Extensions sets every entry of ext as an extension member (see Extension).
func (b *Builder) Extensions(ext map[string]any) *Builder {
	for name, v := range ext {
		b.Extension(name, v)
	}
	return b
}

// Timestamp sets the timestamp extension (default: the time Build runs).
func (b *Builder) Timestamp(t time.Time) *Builder { b.timestamp = t; return b }

// Build returns the problem. A type without a status is sent as 500. An
// internal-error never carries a detail, validationErrors or extension members,
// whatever was set, so an exception message cannot leak through it. Extensions
// whose name is not a ValidExtensionName are dropped.
func (b *Builder) Build() Problem {
	t := b.t
	if t.Status == 0 {
		t = TypeInternalError
	}
	title := t.Title
	if title == "" {
		title = TypeForStatus(t.Status).Title
	}
	code := b.code
	if code == "" {
		code = t.Code
	}
	ts := b.timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	p := Problem{
		Type:      t.URI(b.base),
		Title:     title,
		Status:    t.Status,
		Detail:    b.detail,
		Instance:  b.instance,
		TraceID:   b.traceID,
		ErrorCode: code,
		TenantID:  b.tenantID,
		Timestamp: FormatTimestamp(ts),
	}
	if len(b.fields) > 0 {
		p.ValidationErrors = append([]FieldError(nil), b.fields...)
	}
	p.Extensions = validExtensions(b.ext)
	if t.ID() == TypeInternalError.ID() {
		p.Detail = ""
		p.ValidationErrors = nil
		p.Extensions = nil
	}
	return p
}
