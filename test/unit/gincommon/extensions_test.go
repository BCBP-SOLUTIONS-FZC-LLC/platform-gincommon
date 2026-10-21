package gincommon_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

func extensionRouter(t *testing.T, cfg gincommon.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg.ServiceName = "problem-test"
	r := gin.New()
	r.Use(gincommon.ObservabilityMiddlewares(cfg)...)
	api := r.Group("/api", gincommon.ProtectedMiddlewares(cfg)...)
	api.PUT("/tenants/:id", func(c *gin.Context) {
		gincommon.AbortWithProblem(c, problem.NewConflictError("The tenant was modified concurrently.").
			WithCode("VERSION_CONFLICT").
			WithExtension("record_version", 7).
			WithExtension("tenantId", "other-tenant"). // a standard member: dropped
			WithExtension("traceId", "forged"))
	})
	api.DELETE("/users/:id", func(c *gin.Context) {
		_ = c.Error(problem.NewBusinessRuleError("The user is a delegate on active workflows.").
			WithExtensions(map[string]any{"active_workflows": []string{"wf-1", "wf-2"}, "licensed_seats": 10}))
	})
	api.GET("/internal", func(c *gin.Context) {
		gincommon.AbortWithProblem(c, problem.NewInternalError(errors.New("pq: password=hunter2")).
			WithExtension("query_text", "SELECT secret FROM vault"))
	})
	api.GET("/nan", func(c *gin.Context) {
		gincommon.AbortWithProblem(c, problem.NewConflictError("Conflict.").WithExtension("score", math.NaN()))
	})
	return r
}

// AbortWithProblem carries the extensions to the body as top-level members, after
// the request's traceId, instance and verified tenantId, which they never replace.
func TestAbortWithProblem_Extensions(t *testing.T) {
	logs := &problemLogger{}
	r := extensionRouter(t, gincommon.Config{Logger: logs})
	w := serve(r, http.MethodPut, "/api/tenants/t1", "", identity)

	p := mustProblem(t, w, http.StatusConflict, problem.TypeConflict)
	if p.ErrorCode != "VERSION_CONFLICT" || p.Instance != "/api/tenants/t1" || p.TenantID != "tenant-001" {
		t.Fatalf("standard members = %+v", p)
	}
	if p.TraceID == "forged" || p.TraceID == "" {
		t.Fatalf("traceId = %q: an extension must never replace it", p.TraceID)
	}
	var version int64
	if ok, err := p.Extension("record_version", &version); !ok || err != nil || version != 7 {
		t.Fatalf("record_version = %v, %v, %d; body %s", ok, err, version, w.Body.String())
	}
	if len(p.Extensions) != 1 {
		t.Fatalf("Extensions = %v, want only record_version", p.Extensions)
	}
	body := w.Body.String()
	if !strings.HasSuffix(body, `,"record_version":7}`) || strings.Count(body, `"tenantId"`) != 1 || strings.Count(body, `"traceId"`) != 1 {
		t.Fatalf("body = %s", body)
	}

	// Logging is unchanged: one warn entry with the usual fields and no extensions.
	pl := logs.problems()
	if len(pl) != 1 || pl[0].level != "warn" {
		t.Fatalf("problem log entries = %+v", pl)
	}
	if _, ok := pl[0].fields["record_version"]; ok || pl[0].fields["tenantId"] != "tenant-001" || pl[0].fields["errorCode"] != "VERSION_CONFLICT" {
		t.Fatalf("problem log fields = %v", pl[0].fields)
	}
}

// The deferred path (c.Error) renders extensions the same way.
func TestProblem_ExtensionsViaCError(t *testing.T) {
	r := extensionRouter(t, gincommon.Config{})
	w := serve(r, http.MethodDelete, "/api/users/u2", "", identity)
	p := mustProblem(t, w, http.StatusUnprocessableEntity, problem.TypeBusinessRuleViolation)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["active_workflows"]) != `["wf-1","wf-2"]` || string(raw["licensed_seats"]) != "10" {
		t.Fatalf("body = %s", w.Body.String())
	}
	if p.Detail != "The user is a delegate on active workflows." {
		t.Fatalf("detail = %q", p.Detail)
	}
}

// An internal-error sends no extension, as it sends no detail.
func TestProblem_InternalErrorExtensionsNeverLeak(t *testing.T) {
	r := extensionRouter(t, gincommon.Config{})
	w := serve(r, http.MethodGet, "/api/internal", "", identity)
	p := mustProblem(t, w, http.StatusInternalServerError, problem.TypeInternalError)
	if p.Extensions != nil || p.Detail != "" || strings.Contains(w.Body.String(), "SELECT") || strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

// An extension value that cannot be marshalled is dropped, the problem is still
// sent, and the drop is logged without the value.
func TestProblem_UnmarshallableExtensionDropped(t *testing.T) {
	logs := &problemLogger{}
	r := extensionRouter(t, gincommon.Config{Logger: logs})
	w := serve(r, http.MethodGet, "/api/nan", "", identity)
	p := mustProblem(t, w, http.StatusConflict, problem.TypeConflict)
	if p.Extensions != nil || p.Detail != "Conflict." {
		t.Fatalf("problem = %+v", p)
	}
	var dropped []logEntry
	for _, e := range logs.entries {
		if e.msg == "problem extensions dropped" {
			dropped = append(dropped, e)
		}
	}
	if len(dropped) != 1 || dropped[0].level != "warn" || dropped[0].fields["errorCode"] != "CONFLICT" ||
		dropped[0].fields["traceId"] != p.TraceID || dropped[0].fields["service"] != "problem-test" {
		t.Fatalf("dropped entries = %+v", dropped)
	}
	if _, ok := dropped[0].fields["score"]; ok {
		t.Fatal("the value must not be logged")
	}
	if len(logs.problems()) != 1 {
		t.Fatal("the problem response must still be logged")
	}

	// Without a logger the problem is still written.
	w = serve(extensionRouter(t, gincommon.Config{}), http.MethodGet, "/api/nan", "", identity)
	mustProblem(t, w, http.StatusConflict, problem.TypeConflict)
}
