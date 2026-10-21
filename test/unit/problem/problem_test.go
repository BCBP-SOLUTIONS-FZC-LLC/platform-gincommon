package problem_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

func TestPlatformTypes_RegistryInvariants(t *testing.T) {
	slug := regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	code := regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	ids := map[string]bool{}
	codes := map[string]bool{}
	for _, ty := range problem.PlatformTypes() {
		if ty.Domain != problem.PlatformDomain || !slug.MatchString(ty.Name) || !code.MatchString(ty.Code) {
			t.Errorf("invalid platform type %+v", ty)
		}
		if ty.Title == "" || ty.Status < 400 || ty.Status > 599 {
			t.Errorf("invalid title/status %+v", ty)
		}
		if ids[ty.ID()] || codes[ty.Code] {
			t.Errorf("duplicate %s / %s", ty.ID(), ty.Code)
		}
		ids[ty.ID()], codes[ty.Code] = true, true
		if strings.ContainsAny(ty.URI(""), "?#") || regexp.MustCompile(`/v\d+/`).MatchString(ty.URI("")) {
			t.Errorf("type URI %q must be stable and version-independent", ty.URI(""))
		}
	}
}

// The URIs the platform specification names must not change.
func TestPlatformTypes_SpecifiedURIs(t *testing.T) {
	want := map[string]problem.Type{
		"https://errors.company.com/platform/validation-failed":   problem.TypeValidationFailed,
		"https://errors.company.com/platform/unauthorized":        problem.TypeUnauthorized,
		"https://errors.company.com/platform/forbidden":           problem.TypeForbidden,
		"https://errors.company.com/platform/conflict":            problem.TypeConflict,
		"https://errors.company.com/platform/internal-error":      problem.TypeInternalError,
		"https://errors.company.com/platform/not-found":           problem.TypeNotFound,
		"https://errors.company.com/platform/service-unavailable": problem.TypeServiceUnavailable,
	}
	for uri, ty := range want {
		if got := ty.URI(""); got != uri {
			t.Errorf("URI = %q, want %q", got, uri)
		}
	}
	statuses := map[problem.Type]int{
		problem.TypeValidationFailed: 400, problem.TypeUnauthorized: 401, problem.TypeForbidden: 403,
		problem.TypeNotFound: 404, problem.TypeConflict: 409, problem.TypeBusinessRuleViolation: 422,
		problem.TypeRateLimitExceeded: 429, problem.TypeInternalError: 500, problem.TypeNotImplemented: 501,
	}
	for ty, st := range statuses {
		if ty.Status != st {
			t.Errorf("%s status = %d, want %d", ty.ID(), ty.Status, st)
		}
	}
	if problem.TypeValidationFailed.Title != "Validation failed" || problem.TypeInternalError.Title != "Internal Server Error" {
		t.Error("titles must match the platform specification")
	}
}

// docs/problem-details/registry.md is the registry of record; it must list every
// platform type with its status and errorCode.
func TestPlatformTypes_DocumentedInRegistry(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/problem-details/registry.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, ty := range problem.PlatformTypes() {
		row := fmt.Sprintf("| `%s` | %d | `%s` |", ty.ID(), ty.Status, ty.Code)
		if !bytes.Contains(doc, []byte(row)) {
			t.Errorf("registry.md is missing the row starting %q", row)
		}
	}
}

func TestTypeURI(t *testing.T) {
	cases := map[string]string{
		"":                           "https://errors.company.com/tender/not-found",
		"https://errors.example.com": "https://errors.example.com/tender/not-found",
		" https://e.example.com// ":  "https://e.example.com/tender/not-found",
	}
	for base, want := range cases {
		if got := problem.TypeURI(base, "tender", "not-found"); got != want {
			t.Errorf("TypeURI(%q) = %q, want %q", base, got, want)
		}
	}
	if got := (problem.Type{}).URI(""); got != "about:blank" {
		t.Errorf("zero Type URI = %q", got)
	}
}

func TestTypeForStatus(t *testing.T) {
	if problem.TypeForStatus(400) != problem.TypeBadRequest {
		t.Error("a bare 400 is bad-request, not validation-failed")
	}
	for _, ty := range problem.PlatformTypes() {
		if ty == problem.TypeValidationFailed {
			continue
		}
		if got := problem.TypeForStatus(ty.Status); got != ty {
			t.Errorf("TypeForStatus(%d) = %+v, want %+v", ty.Status, got, ty)
		}
	}
	got := problem.TypeForStatus(http.StatusTeapot)
	if got.ID() != "" || got.Title != "I'm a teapot" || got.Code != "I_M_A_TEAPOT" || got.URI("") != "about:blank" {
		t.Errorf("TypeForStatus(418) = %+v", got)
	}
	if got := problem.TypeForStatus(499); got.Title != "Error" || got.Code != "ERROR" {
		t.Errorf("TypeForStatus(499) = %+v", got)
	}
}

func TestNewType(t *testing.T) {
	ty, err := problem.NewType("tender", "invalid-stage", "Invalid tender stage", 422, "TENDER_INVALID_STAGE")
	if err != nil || ty.URI("") != "https://errors.company.com/tender/invalid-stage" {
		t.Fatalf("NewType = %+v, %v", ty, err)
	}
	bad := []struct {
		domain, name, title string
		status              int
		code                string
	}{
		{"Tender", "x", "t", 400, "X"},
		{"platform", "x", "t", 400, "X"},
		{"tender", "v1/not-found", "t", 400, "X"},
		{"tender", "not_found", "t", 400, "X"},
		{"tender", "x", " ", 400, "X"},
		{"tender", "x", "t", 302, "X"},
		{"tender", "x", "t", 600, "X"},
		{"tender", "x", "t", 400, "lower"},
	}
	for _, b := range bad {
		if _, err := problem.NewType(b.domain, b.name, b.title, b.status, b.code); !errors.Is(err, problem.ErrInvalidType) {
			t.Errorf("NewType(%+v) err = %v, want ErrInvalidType", b, err)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("MustType must panic on an invalid type")
		}
	}()
	problem.MustType("platform", "x", "t", 400, "X")
}

func TestBuilder(t *testing.T) {
	ts := time.Date(2026, 10, 15, 14, 45, 22, 0, time.FixedZone("GST", 4*3600))
	p := problem.NewBuilder(problem.TypeNotFound).
		TypeBase("https://errors.example.com").
		Detail("Tender 123 was not found.").
		ErrorCode("TENDER_NOT_FOUND").
		Instance("/api/tenders/123").
		TraceID("abc").
		TenantID("tenant-001").
		Timestamp(ts).
		Build()
	want := problem.Problem{
		Type: "https://errors.example.com/platform/not-found", Title: "Not Found", Status: 404,
		Detail: "Tender 123 was not found.", Instance: "/api/tenders/123", TraceID: "abc",
		ErrorCode: "TENDER_NOT_FOUND", TenantID: "tenant-001", Timestamp: "2026-10-15T10:45:22Z",
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("Build = %+v\nwant %+v", p, want)
	}

	// Member order and omission follow the specification.
	b, _ := json.Marshal(p)
	wantJSON := `{"type":"https://errors.example.com/platform/not-found","title":"Not Found","status":404,` +
		`"detail":"Tender 123 was not found.","instance":"/api/tenders/123","traceId":"abc",` +
		`"errorCode":"TENDER_NOT_FOUND","tenantId":"tenant-001","timestamp":"2026-10-15T10:45:22Z"}`
	if string(b) != wantJSON {
		t.Fatalf("JSON = %s\nwant %s", b, wantJSON)
	}

	if got := problem.NewBuilder(problem.TypeConflict).Build(); got.Timestamp == "" || got.ErrorCode != "CONFLICT" {
		t.Fatalf("defaults = %+v", got)
	}
	if got := problem.NewBuilder(problem.Type{}).Build(); got.Status != 500 || got.Type != problem.TypeInternalError.URI("") {
		t.Fatalf("zero type = %+v, want internal-error", got)
	}
	if got := problem.NewBuilder(problem.TypeForStatus(418)).Build(); got.Type != "about:blank" || got.Title != "I'm a teapot" {
		t.Fatalf("about:blank = %+v", got)
	}
	custom := problem.Type{Domain: "x", Name: "y", Status: 418}
	if got := problem.NewBuilder(custom).Build(); got.Title != "I'm a teapot" {
		t.Fatalf("untitled type = %+v", got)
	}
}

func TestBuilder_ValidationErrors(t *testing.T) {
	e := problem.NewValidationError(problem.FieldError{Field: "deadline", Message: "must be in the future"}).
		WithFields(problem.FieldError{Field: "title", Message: "is required"})
	b, _ := json.Marshal(problem.BuilderFor(e).Timestamp(time.Unix(0, 0)).Build())
	direct := problem.NewBuilder(problem.TypeValidationFailed).ValidationErrors(e.ValidationErrors...).Build()
	if len(direct.ValidationErrors) != 2 {
		t.Fatalf("Builder.ValidationErrors = %+v", direct.ValidationErrors)
	}
	want := `{"type":"https://errors.company.com/platform/validation-failed","title":"Validation failed","status":400,` +
		`"errorCode":"VALIDATION_FAILED","timestamp":"1970-01-01T00:00:00Z","validationErrors":[` +
		`{"field":"deadline","message":"must be in the future"},{"field":"title","message":"is required"}]}`
	if string(b) != want {
		t.Fatalf("JSON = %s\nwant %s", b, want)
	}
}

// An internal-error never exposes a detail, however it was built.
func TestBuilder_InternalErrorNeverHasDetail(t *testing.T) {
	e := problem.NewError(problem.TypeInternalError, "pq: relation \"users\" does not exist")
	e.ValidationErrors = []problem.FieldError{{Field: "x", Message: "y"}}
	p := problem.BuilderFor(e).Detail("still secret").Build()
	if p.Detail != "" || p.ValidationErrors != nil {
		t.Fatalf("internal-error = %+v", p)
	}
	if p := problem.BuilderFor(nil).Build(); p.Status != 500 {
		t.Fatalf("BuilderFor(nil) = %+v", p)
	}
}

func TestErrorConstructors(t *testing.T) {
	cases := []struct {
		err    *problem.Error
		typ    problem.Type
		status int
	}{
		{problem.NewValidationError(), problem.TypeValidationFailed, 400},
		{problem.NewBadRequestError("d"), problem.TypeBadRequest, 400},
		{problem.NewUnauthorizedError("d"), problem.TypeUnauthorized, 401},
		{problem.NewForbiddenError("d"), problem.TypeForbidden, 403},
		{problem.NewNotFoundError("d"), problem.TypeNotFound, 404},
		{problem.NewConflictError("d"), problem.TypeConflict, 409},
		{problem.NewBusinessRuleError("d"), problem.TypeBusinessRuleViolation, 422},
		{problem.NewRateLimitError("d"), problem.TypeRateLimitExceeded, 429},
		{problem.NewInternalError(nil), problem.TypeInternalError, 500},
		{problem.NewNotImplementedError("d"), problem.TypeNotImplemented, 501},
		{problem.NewServiceUnavailableError("d"), problem.TypeServiceUnavailable, 503},
	}
	for _, c := range cases {
		if c.err.Type != c.typ || c.err.Status() != c.status {
			t.Errorf("%s: type %s status %d", c.typ.ID(), c.err.Type.ID(), c.err.Status())
		}
	}
	if (&problem.Error{}).Status() != 500 {
		t.Error("an Error without a type is a 500")
	}
}

func TestError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("row locked")
	e := problem.NewConflictError("The tender is being edited.").WithCode("TENDER_LOCKED").WithCause(cause)
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap must expose the cause")
	}
	if e.ErrorCode() != "TENDER_LOCKED" {
		t.Fatalf("ErrorCode = %q", e.ErrorCode())
	}
	if got := e.Error(); got != "TENDER_LOCKED: Conflict: The tender is being edited.: row locked" {
		t.Fatalf("Error() = %q", got)
	}
	if got := (&problem.Error{Type: problem.Type{Status: 418}}).Error(); got != "I'm a teapot" {
		t.Fatalf("untitled Error() = %q", got)
	}
	wrapped := fmt.Errorf("handler: %w", e)
	if got := problem.FromError(wrapped); got != e {
		t.Fatalf("FromError(wrapped) = %+v, want the wrapped *Error", got)
	}
}

func TestFromError(t *testing.T) {
	if problem.FromError(nil) != nil {
		t.Fatal("FromError(nil) must be nil")
	}
	cases := []struct {
		err    error
		typ    problem.Type
		code   string
		detail string
	}{
		{errors.ErrUnsupported, problem.TypeNotImplemented, "NOT_IMPLEMENTED", ""},
		{fmt.Errorf("query: %w", context.DeadlineExceeded), problem.TypeServiceUnavailable, "REQUEST_TIMEOUT", "The request timed out."},
		{&http.MaxBytesError{Limit: 10}, problem.TypePayloadTooLarge, "PAYLOAD_TOO_LARGE", "The request body is too large."},
		{errors.New("pq: syntax error at or near SELECT"), problem.TypeInternalError, "INTERNAL_ERROR", ""},
	}
	for _, c := range cases {
		got := problem.FromError(c.err)
		if got.Type != c.typ || got.ErrorCode() != c.code || got.Detail != c.detail || !errors.Is(got, c.err) {
			t.Errorf("FromError(%v) = %+v", c.err, got)
		}
	}
}

// Vectors shared with platform-pgcommon-py (tests/unit/test_fastapi_problem_details.py),
// so a Go and a Python service resolve the same traceId for the same request.
func TestTraceIDFromTraceparent(t *testing.T) {
	const id = "4bf92f3577b34da6a3ce929d0e0e4736"
	cases := map[string]string{
		"00-" + id + "-00f067aa0ba902b7-01":                       id,
		"  00-" + id + "-00f067aa0ba902b7-00 ":                    id,
		"01-" + id + "-00f067aa0ba902b7-01-future":                id,
		"00-" + id + "-00f067aa0ba902b7-01-extra":                 "",
		"ff-" + id + "-00f067aa0ba902b7-01":                       "",
		"00-" + strings.ToUpper(id) + "-00f067aa0ba902b7-01":      "",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01": "",
		"00-" + id + "-0000000000000000-01":                       "",
		"00-" + id[:31] + "-00f067aa0ba902b7-01":                  "",
		"":                                                        "",
		"garbage":                                                 "",
	}
	for in, want := range cases {
		if got := problem.TraceIDFromTraceparent(in); got != want {
			t.Errorf("TraceIDFromTraceparent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeRequestID(t *testing.T) {
	cases := map[string]string{
		"req-7f3a9c":             "req-7f3a9c",
		" svc-1234:abc.def_1 ":   "svc-1234:abc.def_1",
		strings.Repeat("a", 128): strings.Repeat("a", 128),
		strings.Repeat("a", 129): "",
		"bad id":                 "",
		"evil\ninjected":         "",
		"<script>":               "",
		"":                       "",
	}
	for in, want := range cases {
		if got := problem.SanitizeRequestID(in); got != want {
			t.Errorf("SanitizeRequestID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveTraceID(t *testing.T) {
	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	const span = "0af7651916cd43dd8448eb211c80319c"
	if got := problem.ResolveTraceID(tp, "req-1", span); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("traceparent first: %q", got)
	}
	if got := problem.ResolveTraceID("bad", "req-1", span); got != "req-1" {
		t.Errorf("x-request-id second: %q", got)
	}
	if got := problem.ResolveTraceID("", "bad id", span); got != span {
		t.Errorf("span third: %q", got)
	}
	got := problem.ResolveTraceID("", "", "00000000000000000000000000000000")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got) || got == problem.NewTraceID() {
		t.Errorf("generated: %q", got)
	}
}

func TestFromResponse(t *testing.T) {
	body := `{"type":"https://errors.company.com/tender/not-found","title":"Tender not found","status":404,` +
		`"detail":"Tender 123 was not found","traceId":"01JABC","errorCode":"TENDER_NOT_FOUND","futureExtension":{"a":1}}`
	resp := &http.Response{
		StatusCode: 404,
		Header:     http.Header{"Content-Type": {"application/problem+json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	p, err := problem.FromResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != 404 || p.Type != "https://errors.company.com/tender/not-found" || p.Title != "Tender not found" ||
		p.Detail != "Tender 123 was not found" || p.TraceID != "01JABC" || p.ErrorCode != "TENDER_NOT_FOUND" {
		t.Fatalf("problem = %+v", p)
	}

	resp = &http.Response{StatusCode: 502, Header: http.Header{"Content-Type": {problem.ContentType}},
		Body: io.NopCloser(strings.NewReader(`{"title":"Bad Gateway"}`))}
	if p, err := problem.FromResponse(resp); err != nil || p.Status != 502 {
		t.Fatalf("status fallback = %+v, %v", p, err)
	}
	for _, ct := range []string{"application/json", "", "text/html; charset"} {
		resp = &http.Response{Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader("{}"))}
		if _, err := problem.FromResponse(resp); !errors.Is(err, problem.ErrNotProblem) {
			t.Errorf("Content-Type %q: err = %v, want ErrNotProblem", ct, err)
		}
	}
	if _, err := problem.FromResponse(nil); !errors.Is(err, problem.ErrNotProblem) {
		t.Error("nil response must be ErrNotProblem")
	}
	resp = &http.Response{Header: http.Header{"Content-Type": {problem.ContentType}}, Body: io.NopCloser(strings.NewReader("{"))}
	if _, err := problem.FromResponse(resp); err == nil {
		t.Error("malformed body must fail")
	}
	if _, err := problem.Decode(strings.NewReader(`{"status":"x"}`)); err == nil {
		t.Error("Decode must reject a wrong member type")
	}
}

// The shared OpenAPI schema must describe exactly the members Problem serialises.
func TestOpenAPIComponents(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string                  `yaml:"required"`
				Properties map[string]map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]string `yaml:"schema"`
				} `yaml:"content"`
			} `yaml:"responses"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(problem.OpenAPIComponents(), &doc); err != nil {
		t.Fatal(err)
	}
	pd, ok := doc.Components.Schemas["ProblemDetails"]
	if !ok {
		t.Fatal("ProblemDetails schema missing")
	}
	if !reflect.DeepEqual(pd.Required, []string{"type", "title", "status"}) {
		t.Errorf("required = %v", pd.Required)
	}
	if got, want := sortedKeys(pd.Properties), jsonMembers(reflect.TypeOf(problem.Problem{})); !reflect.DeepEqual(got, want) {
		t.Errorf("ProblemDetails properties = %v, Problem members = %v", got, want)
	}
	ve := doc.Components.Schemas["ValidationError"]
	if got, want := sortedKeys(ve.Properties), jsonMembers(reflect.TypeOf(problem.FieldError{})); !reflect.DeepEqual(got, want) {
		t.Errorf("ValidationError properties = %v, FieldError members = %v", got, want)
	}

	std := problem.StandardResponses()
	for _, st := range []int{400, 401, 403, 404, 409, 422, 429, 500} {
		name, ok := std[st]
		if !ok {
			t.Errorf("no standard response for %d", st)
			continue
		}
		resp, ok := doc.Components.Responses[name]
		if !ok {
			t.Errorf("response component %s missing", name)
			continue
		}
		if ref := resp.Content[problem.ContentType].Schema["$ref"]; ref != "#/components/schemas/ProblemDetails" {
			t.Errorf("%s: application/problem+json schema = %q", name, ref)
		}
	}
	problem.OpenAPIComponents()[0] = 'X'
	if problem.OpenAPIComponents()[0] == 'X' {
		t.Error("OpenAPIComponents must return a copy")
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return sortStrings(out)
}

func jsonMembers(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "-" {
			continue // Extensions: serialised as top-level members by MarshalJSON
		}
		out = append(out, name)
	}
	return sortStrings(out)
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

func TestFormatTimestampAndContentType(t *testing.T) {
	if got := problem.FormatTimestamp(time.Date(2026, 1, 2, 3, 4, 5, 999, time.UTC)); got != "2026-01-02T03:04:05Z" {
		t.Errorf("FormatTimestamp = %q", got)
	}
	if !problem.IsProblemContentType("Application/Problem+JSON") || problem.IsProblemContentType("application/json") {
		t.Error("IsProblemContentType")
	}
	if problem.DefaultTypeBase != "https://errors.company.com" || problem.MaxRequestIDLength != 128 || problem.TimestampLayout == "" {
		t.Error("constants changed")
	}
}
