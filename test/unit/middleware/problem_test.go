package middleware_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

type tagged struct {
	Req      string            `json:"req" binding:"required"`
	MinS     string            `json:"min_s" binding:"min=3"`
	MaxN     int               `json:"max_n" binding:"max=5"`
	GtN      int               `json:"gt_n" binding:"gt=1"`
	LtN      int               `json:"lt_n" binding:"lt=1"`
	LenS     []int             `json:"len_s" binding:"len=2"`
	Eq       string            `json:"eq" binding:"eq=a"`
	Ne       string            `json:"ne" binding:"ne=b"`
	OneOf    string            `json:"one_of" binding:"oneof=x y"`
	Email    string            `json:"email" binding:"email"`
	URL      string            `json:"url" binding:"url"`
	UUID     string            `json:"uuid" binding:"uuid"`
	When     string            `json:"when" binding:"datetime=2006-01-02"`
	Alpha    string            `json:"alpha" binding:"alpha"`
	AlphaNum string            `json:"alpha_num" binding:"alphanum"`
	Numeric  string            `json:"numeric" binding:"numeric"`
	Bool     string            `json:"bool" binding:"boolean"`
	Other    string            `json:"other" binding:"hexcolor"`
	FormOnly string            `form:"form_only" binding:"required"`
	NoTag    string            `binding:"required"`
	Skipped  string            `json:"-" binding:"required"`
	MapMin   map[string]string `json:"map_min" binding:"min=1"`
}

// Registers the json tag-name function on Gin's validator (done once by ProblemMiddleware).
func init() { middleware.ProblemMiddleware(middleware.ProblemSettings{}) }

func TestBindingError_ValidationMessages(t *testing.T) {
	err := binding.Validator.ValidateStruct(&tagged{
		MinS: "ab", MaxN: 9, LtN: 5, LenS: []int{1}, Eq: "z", Ne: "b", OneOf: "q", Email: "nope",
		URL: "nope", UUID: "nope", When: "nope", Alpha: "a1", AlphaNum: "a-", Numeric: "x", Bool: "x", Other: "x",
	})
	pe := middleware.BindingError(err)
	if pe.Type != problem.TypeValidationFailed || pe.Err == nil {
		t.Fatalf("BindingError = %+v", pe)
	}
	want := map[string]string{
		"req": "is required", "min_s": "must be at least 3 characters", "max_n": "must be at most 5",
		"gt_n": "must be greater than 1", "lt_n": "must be less than 1", "len_s": "must be exactly 2 items",
		"eq": "must equal a", "ne": "must not equal b", "one_of": "must be one of: x, y",
		"email": "must be a valid email address", "url": "must be a valid URL", "uuid": "must be a valid UUID",
		"when": "must be a valid date-time", "alpha": "must contain only letters",
		"alpha_num": "must contain only letters and digits", "numeric": "must be numeric", "bool": "must be a boolean",
		"other": "is invalid", "form_only": "is required", "NoTag": "is required", "Skipped": "is required",
		"map_min": "must be at least 1 items",
	}
	got := map[string]string{}
	for _, f := range pe.ValidationErrors {
		got[f.Field] = f.Message
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("validationErrors:\n got %v\nwant %v", got, want)
	}
}

func TestBindingError_Variable(t *testing.T) {
	// validator.Var errors have no namespace: the field is empty, the tag still maps.
	err := validator.New().Var("", "required")
	pe := middleware.BindingError(err)
	if len(pe.ValidationErrors) != 1 || pe.ValidationErrors[0].Message != "is required" {
		t.Fatalf("Var = %+v", pe.ValidationErrors)
	}
}

func TestBindingError_JSONKinds(t *testing.T) {
	cases := []struct {
		target any
		body   string
		want   string
	}{
		{&struct{ A bool }{}, `{"A":1}`, "must be a boolean"},
		{&struct{ A uint8 }{}, `{"A":"x"}`, "must be an integer"},
		{&struct{ A float64 }{}, `{"A":"x"}`, "must be a number"},
		{&struct{ A []int }{}, `{"A":1}`, "must be an array"},
		{&struct{ A map[string]int }{}, `{"A":1}`, "must be an object"},
		{&struct{ A *struct{} }{}, `{"A":1}`, "must be an object"},
		{&struct{ A chan int }{}, `{"A":1}`, "must be a valid value"},
	}
	for _, c := range cases {
		err := json.Unmarshal([]byte(c.body), c.target)
		pe := middleware.BindingError(err)
		if len(pe.ValidationErrors) != 1 || pe.ValidationErrors[0] != (problem.FieldError{Field: "A", Message: c.want}) {
			t.Errorf("%T: %+v", c.target, pe.ValidationErrors)
		}
	}
	// A type error at the root has no field: it is reported as invalid JSON.
	var n int
	pe := middleware.BindingError(json.Unmarshal([]byte(`"x"`), &n))
	if pe.Detail != middleware.DetailInvalidJSON || len(pe.ValidationErrors) != 0 {
		t.Fatalf("root type error = %+v", pe)
	}
	if got := middleware.BindingError(&json.UnmarshalTypeError{Field: "a"}); got.ValidationErrors[0].Message != "must be a valid value" {
		t.Fatalf("nil type = %+v", got.ValidationErrors)
	}
}

func TestBindingError_Other(t *testing.T) {
	if pe := middleware.BindingError(&http.MaxBytesError{Limit: 1}); pe.Type != problem.TypePayloadTooLarge {
		t.Fatalf("MaxBytesError = %+v", pe)
	}
	orig := problem.NewConflictError("x")
	if middleware.BindingError(orig) != orig {
		t.Fatal("a *problem.Error must pass through")
	}
	if pe := middleware.BindingError(errors.New("json: unknown field \"x\"")); pe.Detail != middleware.DetailInvalidBody {
		t.Fatalf("other = %+v", pe)
	}
	if middleware.BindingError(nil) != nil {
		t.Fatal("nil")
	}
}

func TestProblemMiddleware_BareStatusOnRegisteredRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ProblemMiddleware(middleware.ProblemSettings{}))
	r.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/items/1", nil))
	p, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if w.Code != 404 || p.ErrorCode != "NOT_FOUND" || p.Detail != "" {
		t.Fatalf("registered route bare 404 = %d %+v", w.Code, p)
	}
}

// Without TracingMiddleware, the problem still uses an OpenTelemetry span carried
// by the request context.
func TestWriteProblem_SpanFromRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	sid, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid})

	logs := &recordingLogger{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(trace.ContextWithSpanContext(c.Request.Context(), sc))
		c.Set(middleware.CtxRequestContextKey, &domain.RequestContext{UserID: "u"}) // verified, no tenant
	}, middleware.ProblemMiddleware(middleware.ProblemSettings{Logger: logs}))
	r.GET("/x", func(c *gin.Context) { middleware.WriteProblem(c, nil) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	p, _ := fixtures.DecodeProblem(w.Body.Bytes())
	if p.TraceID != tid.String() || p.Status != 500 || p.TenantID != "" {
		t.Fatalf("problem = %+v", p)
	}
	e := logs.byMsg("problem response")
	if len(e) != 1 || e[0].Fields["traceId"] != tid.String() {
		t.Fatalf("log = %+v", e)
	}
	if _, ok := e[0].Fields["service"]; ok {
		t.Fatal("no service field without a service name")
	}
}

type panickingLogger struct{ *recordingLogger }

func (panickingLogger) Warn(string, map[string]any) { panic("logger down") }

func TestWriteProblem_PanickingLoggerIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ProblemMiddleware(middleware.ProblemSettings{Logger: panickingLogger{&recordingLogger{}}}))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusConflict) })
	// An unmarshallable extension also logs "problem extensions dropped" through the same logger.
	r.GET("/nan", func(c *gin.Context) {
		middleware.WriteProblem(c, problem.NewConflictError("Conflict.").WithExtension("score", math.NaN()))
	})
	for _, path := range []string{"/x", "/nan"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusConflict || w.Header().Get("Content-Type") != problem.ContentType {
			t.Fatalf("%s: response = %d %q", path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}
