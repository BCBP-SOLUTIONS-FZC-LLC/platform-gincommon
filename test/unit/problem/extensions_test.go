package problem_test

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

func TestValidExtensionName(t *testing.T) {
	for _, name := range []string{"record_version", "activeWorkflows", "abc", "A1_", "retry_after_seconds"} {
		if !problem.ValidExtensionName(name) {
			t.Errorf("ValidExtensionName(%q) = false", name)
		}
	}
	bad := []string{"", "ab", "1abc", "_abc", "record-version", "a.b.c", "naïve", "has space"}
	for _, name := range bad {
		if problem.ValidExtensionName(name) {
			t.Errorf("ValidExtensionName(%q) = true", name)
		}
	}
	// Every standard member is reserved, in any letter case (encoding/json matches
	// struct members case-insensitively).
	for _, name := range jsonMembers(reflect.TypeOf(problem.Problem{})) {
		for _, n := range []string{name, strings.ToUpper(name), strings.ToUpper(name[:1]) + name[1:]} {
			if problem.ValidExtensionName(n) {
				t.Errorf("standard member %q must not be a valid extension name", n)
			}
		}
	}
}

func TestBuilder_Extensions(t *testing.T) {
	p := problem.NewBuilder(problem.TypeConflict).
		Detail("The tenant was modified concurrently.").
		Instance("/api/tenants/t1").
		TraceID("abc").
		Timestamp(time.Unix(0, 0)).
		Extension("record_version", 7).
		Extensions(map[string]any{"active_workflows": []string{"wf-1"}, "Zeta": true}).
		Build()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	// Standard members first, in the specification's order, then the extensions
	// sorted by name (byte order: upper case first).
	want := `{"type":"https://errors.company.com/platform/conflict","title":"Conflict","status":409,` +
		`"detail":"The tenant was modified concurrently.","instance":"/api/tenants/t1","traceId":"abc",` +
		`"errorCode":"CONFLICT","timestamp":"1970-01-01T00:00:00Z",` +
		`"Zeta":true,"active_workflows":["wf-1"],"record_version":7}`
	if string(b) != want {
		t.Fatalf("JSON = %s\nwant %s", b, want)
	}
	for range 20 { // map iteration order must not leak into the output
		if again, _ := json.Marshal(p); string(again) != want {
			t.Fatalf("non-deterministic JSON %s", again)
		}
	}
	if problem.NewBuilder(problem.TypeConflict).Build().Extensions != nil {
		t.Fatal("a problem without extensions must have nil Extensions")
	}
}

func TestError_WithExtension(t *testing.T) {
	e := problem.NewConflictError("The tenant was modified concurrently.").
		WithExtension("record_version", 6).
		WithExtensions(map[string]any{"record_version": 7, "licensed_seats": 10})
	if !reflect.DeepEqual(e.Extensions, map[string]any{"record_version": 7, "licensed_seats": 10}) {
		t.Fatalf("Extensions = %v", e.Extensions)
	}
	if got := e.Error(); strings.Contains(got, "record_version") {
		t.Fatalf("Error() = %q must not include extensions", got)
	}
	p := problem.BuilderFor(e).Build()
	if !reflect.DeepEqual(p.Extensions, e.Extensions) {
		t.Fatalf("BuilderFor extensions = %v", p.Extensions)
	}
	// The built problem owns its map: later changes to the error do not reach it.
	e.Extensions["licensed_seats"] = 11
	if p.Extensions["licensed_seats"] != 10 {
		t.Fatal("Build must copy the extensions")
	}
	if (&problem.Error{}).WithExtensions(nil).Extensions != nil {
		t.Fatal("WithExtensions(nil) must not allocate")
	}
}

// A standard member always wins: a colliding or invalid name is dropped, never
// sent, whichever way it was set.
func TestExtensions_CollisionWithStandardMembers(t *testing.T) {
	e := problem.NewNotFoundError("Tender 1 was not found.").WithExtensions(map[string]any{
		"type": "x", "title": "x", "status": 200, "detail": "x", "instance": "x", "traceId": "x",
		"errorCode": "x", "tenantId": "other-tenant", "timestamp": "x", "validationErrors": "x",
		"Status": 200, "TRACEID": "x", "ab": 1, "bad-name": 1, "kept_member": 1,
	})
	p := problem.BuilderFor(e).TraceID("trace").Timestamp(time.Unix(0, 0)).Build()
	if !reflect.DeepEqual(p.Extensions, map[string]any{"kept_member": 1}) {
		t.Fatalf("Extensions = %v, want only kept_member", p.Extensions)
	}

	// A Problem assembled directly is guarded at serialisation too.
	p.Extensions = map[string]any{"status": 200, "Status": 200, "tenantId": "other-tenant", "kept_member": 1}
	b, _ := json.Marshal(p)
	want := `{"type":"https://errors.company.com/platform/not-found","title":"Not Found","status":404,` +
		`"detail":"Tender 1 was not found.","traceId":"trace","errorCode":"NOT_FOUND",` +
		`"timestamp":"1970-01-01T00:00:00Z","kept_member":1}`
	if string(b) != want {
		t.Fatalf("JSON = %s\nwant %s", b, want)
	}
	p.Extensions = map[string]any{"status": 200}
	b, _ = json.Marshal(p)
	if strings.Count(string(b), `"status"`) != 1 || !strings.Contains(string(b), `"status":404`) {
		t.Fatalf("JSON = %s: an all-invalid extension set must leave the standard members alone", b)
	}
}

// An internal-error drops its extensions as it drops its detail, so no internal
// state reaches the client through them.
func TestBuilder_InternalErrorNeverHasExtensions(t *testing.T) {
	e := problem.NewInternalError(errors.New("pq: deadlock detected")).WithExtension("sql_state", "40P01")
	if p := problem.BuilderFor(e).Extension("query_text", "SELECT").Build(); p.Extensions != nil {
		t.Fatalf("internal-error extensions = %v", p.Extensions)
	}
	if p := problem.NewBuilder(problem.Type{}).Extension("leaked", 1).Build(); p.Extensions != nil {
		t.Fatalf("zero type (sent as internal-error) extensions = %v", p.Extensions)
	}
	// Other 5xx types keep theirs, as they keep their (fixed) detail.
	e = problem.NewServiceUnavailableError("The catalog is unavailable.").WithExtension("retry_after_seconds", 30)
	if p := problem.BuilderFor(e).Build(); p.Extensions["retry_after_seconds"] != 30 {
		t.Fatalf("503 extensions = %v", p.Extensions)
	}
}

func TestExtensions_MarshalError(t *testing.T) {
	p := problem.NewBuilder(problem.TypeConflict).Extension("bad_value", math.NaN()).Build()
	if _, err := json.Marshal(p); err == nil || !strings.Contains(err.Error(), "bad_value") {
		t.Fatalf("Marshal err = %v, want the extension named", err)
	}
	p.Extensions = nil
	if _, err := json.Marshal(p); err != nil {
		t.Fatalf("without extensions: %v", err)
	}
}

func TestExtensions_RoundTrip(t *testing.T) {
	in := problem.NewBuilder(problem.TypeConflict).
		Detail("The tenant was modified concurrently.").
		TraceID("abc").
		Extension("record_version", int64(9007199254740993)). // 2^53 + 1: not a float64
		Extension("active_workflows", []map[string]any{{"id": "wf-1", "stage": "review"}}).
		Extension("nothing_here", nil).
		Build()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := problem.Decode(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	std := *out
	std.Extensions = nil
	wantStd := in
	wantStd.Extensions = nil
	if !reflect.DeepEqual(std, wantStd) {
		t.Fatalf("standard members = %+v\nwant %+v", std, wantStd)
	}
	if got := out.Extensions["record_version"]; got != json.Number("9007199254740993") {
		t.Fatalf("record_version = %#v, want json.Number", got)
	}
	var version int64
	if ok, err := out.Extension("record_version", &version); !ok || err != nil || version != 9007199254740993 {
		t.Fatalf("Extension(record_version) = %v, %v, %d", ok, err, version)
	}
	var wfs []struct{ ID, Stage string }
	if ok, err := out.Extension("active_workflows", &wfs); !ok || err != nil || len(wfs) != 1 || wfs[0].Stage != "review" {
		t.Fatalf("Extension(active_workflows) = %v, %v, %+v", ok, err, wfs)
	}
	if v, ok := out.Extensions["nothing_here"]; !ok || v != nil {
		t.Fatalf("null extension = %v, %v", v, ok)
	}
	again, _ := json.Marshal(out)
	if string(again) != string(b) {
		t.Fatalf("re-marshal = %s\nwant %s", again, b)
	}
}

func TestProblem_Extension(t *testing.T) {
	p := &problem.Problem{Extensions: map[string]any{"record_version": "seven", "bad_value": math.Inf(1)}}
	version := int64(-1)
	if ok, err := p.Extension("missing_member", &version); ok || err != nil || version != -1 {
		t.Fatalf("missing = %v, %v, %d", ok, err, version)
	}
	if ok, err := p.Extension("record_version", &version); !ok || err == nil {
		t.Fatalf("wrong type = %v, %v", ok, err)
	}
	if ok, err := p.Extension("bad_value", &version); !ok || err == nil {
		t.Fatalf("unmarshallable = %v, %v", ok, err)
	}
	if ok, err := (&problem.Problem{}).Extension("record_version", &version); ok || err != nil {
		t.Fatalf("no extensions = %v, %v", ok, err)
	}
}

// A third-party server's body: every member Decode does not recognise is kept,
// including names the builder would not send, and none overwrites a standard one.
func TestFromResponse_Extensions(t *testing.T) {
	body := `{"type":"https://example.net/probs/out-of-credit","title":"You do not have enough credit.",` +
		`"status":403,"Status":200,"detail":"Your current balance is 30, but that costs 50.",` +
		`"instance":"/account/12345/msgs/abc","balance":30,"accounts":["/account/12345","/account/67890"],` +
		`"x-vendor":{"a":1}}`
	resp := &http.Response{
		StatusCode: 403,
		Header:     http.Header{"Content-Type": {problem.ContentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	p, err := problem.FromResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "https://example.net/probs/out-of-credit" || p.Instance != "/account/12345/msgs/abc" {
		t.Fatalf("problem = %+v", p)
	}
	want := map[string]any{
		"balance":  json.Number("30"),
		"accounts": []any{"/account/12345", "/account/67890"},
		"x-vendor": map[string]any{"a": json.Number("1")},
	}
	if !reflect.DeepEqual(p.Extensions, want) {
		t.Fatalf("Extensions = %#v\nwant %#v", p.Extensions, want)
	}
	var balance int
	if ok, err := p.Extension("balance", &balance); !ok || err != nil || balance != 30 {
		t.Fatalf("Extension(balance) = %v, %v, %d", ok, err, balance)
	}

	// No unrecognised member: Extensions stays nil, so existing comparisons hold.
	p, err = problem.Decode(strings.NewReader(`{"type":"about:blank","title":"Not Found","status":404}`))
	if err != nil || p.Extensions != nil {
		t.Fatalf("Decode = %+v, %v", p, err)
	}
	for _, bad := range []string{`[]`, `{"status":"x","balance":1}`, `{"balance":}`} {
		if _, err := problem.Decode(strings.NewReader(bad)); err == nil {
			t.Errorf("Decode(%s) must fail", bad)
		}
	}
	var direct problem.Problem
	if err := direct.UnmarshalJSON([]byte(`"not an object"`)); err == nil {
		t.Error("UnmarshalJSON of a non-object must fail")
	}
}
