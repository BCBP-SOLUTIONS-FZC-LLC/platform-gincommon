package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
)

// Fixed details of binding problems. None of them echoes the rejected input.
const (
	DetailEmptyBody   = "The request body is empty."
	DetailInvalidJSON = "The request body is not valid JSON."
	DetailInvalidBody = "The request could not be bound."
)

var registerFieldNamesOnce sync.Once

// registerValidationFieldNames makes Gin's default validator report fields by their
// json (else form, else Go) name, so validationErrors name the field the client sent.
// It runs once per process; a binding.Validator replaced by the service is left alone.
func registerValidationFieldNames() {
	registerFieldNamesOnce.Do(func() {
		if binding.Validator == nil {
			return
		}
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(fieldName)
	})
}

func fieldName(f reflect.StructField) string {
	for _, tag := range []string{"json", "form"} {
		name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
		if name != "" && name != "-" {
			return name
		}
	}
	return f.Name
}

// BindingError maps an error from c.ShouldBind* to a problem error:
//
//   - validator.ValidationErrors are 400 validation-failed with one validationErrors
//     entry per field (JSON path, fixed message);
//   - a *json.UnmarshalTypeError is 400 validation-failed naming the field;
//   - an empty body, malformed JSON or any other binding failure is 400
//     validation-failed with a fixed detail;
//   - an *http.MaxBytesError is 413 payload-too-large.
//
// A nil err returns nil; a *problem.Error is returned unchanged.
func BindingError(err error) *problem.Error {
	if err == nil {
		return nil
	}
	var (
		pe  *problem.Error
		ve  validator.ValidationErrors
		ute *json.UnmarshalTypeError
		se  *json.SyntaxError
		mbe *http.MaxBytesError
	)
	switch {
	case errors.As(err, &pe) && pe != nil:
		return pe
	case errors.As(err, &mbe):
		return problem.FromError(err)
	case errors.As(err, &ve):
		fields := make([]problem.FieldError, 0, len(ve))
		for _, fe := range ve {
			fields = append(fields, problem.FieldError{Field: fieldPath(fe), Message: validationMessage(fe)})
		}
		return problem.NewValidationError(fields...).WithCause(err)
	case errors.As(err, &ute):
		field := ute.Field
		if field == "" {
			return validationDetail(DetailInvalidJSON, err)
		}
		return problem.NewValidationError(problem.FieldError{
			Field: field, Message: "must be " + jsonKind(ute.Type),
		}).WithCause(err)
	case errors.Is(err, io.EOF):
		return validationDetail(DetailEmptyBody, err)
	case errors.As(err, &se), errors.Is(err, io.ErrUnexpectedEOF):
		return validationDetail(DetailInvalidJSON, err)
	}
	return validationDetail(DetailInvalidBody, err)
}

func validationDetail(detail string, cause error) *problem.Error {
	e := problem.NewValidationError()
	e.Detail = detail
	return e.WithCause(cause)
}

// fieldPath is the field's path without the root struct name ("items[0].sku").
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, rest, ok := strings.Cut(ns, "."); ok && rest != "" {
		return rest
	}
	return fe.Field()
}

// validationMessage is a fixed phrase for the failed tag. It uses the tag's
// parameter (a bound or an allowed set), never the rejected value.
func validationMessage(fe validator.FieldError) string {
	p := fe.Param()
	unit := ""
	switch fe.Kind() {
	case reflect.String:
		unit = " characters"
	case reflect.Slice, reflect.Array, reflect.Map:
		unit = " items"
	}
	switch fe.Tag() {
	case "required", "required_if", "required_unless", "required_with", "required_without", "required_with_all", "required_without_all":
		return "is required"
	case "min", "gte":
		return "must be at least " + p + unit
	case "max", "lte":
		return "must be at most " + p + unit
	case "gt":
		return "must be greater than " + p + unit
	case "lt":
		return "must be less than " + p + unit
	case "len":
		return "must be exactly " + p + unit
	case "eq":
		return "must equal " + p
	case "ne":
		return "must not equal " + p
	case "oneof":
		return "must be one of: " + strings.Join(strings.Fields(p), ", ")
	case "email":
		return "must be a valid email address"
	case "url", "http_url", "uri":
		return "must be a valid URL"
	case "uuid", "uuid3", "uuid4", "uuid5", "uuid_rfc4122":
		return "must be a valid UUID"
	case "datetime":
		return "must be a valid date-time"
	case "alpha":
		return "must contain only letters"
	case "alphanum":
		return "must contain only letters and digits"
	case "numeric", "number":
		return "must be numeric"
	case "boolean":
		return "must be a boolean"
	}
	return "is invalid"
}

// jsonKind names a Go type as the JSON type the client should have sent.
func jsonKind(t reflect.Type) string {
	if t == nil {
		return "a valid value"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Map, reflect.Struct:
		return "an object"
	}
	return "a valid value"
}
