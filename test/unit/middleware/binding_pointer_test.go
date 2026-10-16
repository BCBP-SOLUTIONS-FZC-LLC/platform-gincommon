package middleware_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
)

// A type error on a pointer field names the pointed-to JSON type (**int → integer).
func TestBindingError_PointerFieldKind(t *testing.T) {
	var x **int
	pe := middleware.BindingError(&json.UnmarshalTypeError{Field: "count", Type: reflect.TypeOf(x)})
	if len(pe.ValidationErrors) != 1 || pe.ValidationErrors[0].Message != "must be an integer" ||
		pe.ValidationErrors[0].Field != "count" {
		t.Fatalf("validationErrors = %+v", pe.ValidationErrors)
	}
}
