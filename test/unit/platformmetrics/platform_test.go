// Package platformmetrics_test checks gincommon's own instrumentation against
// the Enterprise Platform Observability Standard. It lives in its own package
// because it re-initializes the process-wide metrics state.
package platformmetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	httpmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/obsregistry"
)

var iamOpts = metrics.Options{
	ServiceName: "event-consumer", BuildVersion: "v1.2.3", Domain: "iam", Environment: " PROD ",
}

func initPlatform(t *testing.T, opts metrics.Options) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	if err := metrics.InitWithRegistererOptions(opts, reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

type stream struct{ grpc.ServerStream }

func (stream) Context() context.Context { return context.Background() }

// exercise drives every instrumented code path once.
func exercise(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpmw.PanicRecoveryMiddleware(nil), httpmw.MetricsMiddleware(nil))
	r.GET("/items/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.POST("/items", func(c *gin.Context) { c.Status(http.StatusTooManyRequests) })
	r.Handle("PURGE", "/items", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/boom", func(*gin.Context) { panic("boom") })
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/items/42", http.NoBody),
		httptest.NewRequest(http.MethodPost, "/items", strings.NewReader("{}")),
		httptest.NewRequest("PURGE", "/items", http.NoBody),
		httptest.NewRequest(http.MethodGet, "/nope", http.NoBody),
		httptest.NewRequest(http.MethodGet, "/boom", http.NoBody),
	} {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	metrics.RecordHTTPTimeout(http.MethodGet, "/items/:id")

	unary := grpcmw.UnaryMetricsInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/iam.v1.Events/Publish"}
	for _, err := range []error{nil, status.Error(codes.ResourceExhausted, "slow down"), status.Error(codes.Internal, "x")} {
		_, _ = unary(context.Background(), &emptypb.Empty{}, info, func(context.Context, any) (any, error) {
			return &emptypb.Empty{}, err
		})
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = unary(context.Background(), &emptypb.Empty{}, info, func(context.Context, any) (any, error) { panic("boom") })
	}()
	_ = grpcmw.StreamMetricsInterceptor()(nil, stream{}, &grpc.StreamServerInfo{FullMethod: "/iam.v1.Events/Watch"},
		func(any, grpc.ServerStream) error { return nil })
}

func TestLibraryMetrics_ComplyWithStandard(t *testing.T) {
	reg := initPlatform(t, iamOpts)
	exercise(t)

	rep, err := obsregistry.Validate(reg, obsregistry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Errors() {
		t.Error(f)
	}
	for name, tier := range rep.Tiers {
		if strings.HasPrefix(name, "platform_") && tier != obsregistry.TierPlatform {
			t.Errorf("%s classified as %s", name, tier)
		}
	}
	// No compatibility period: only platform_* families, and every removed
	// legacy name's platform successor is emitted.
	for name, tier := range rep.Tiers {
		if tier != obsregistry.TierPlatform {
			t.Errorf("%s emitted with tier %s; gincommon must emit platform_* only", name, tier)
		}
	}
	for _, m := range obsregistry.Default().Metrics {
		if m.Tier == obsregistry.TierLegacy && m.Owner == "platform-gincommon" {
			if m.Status != obsregistry.StatusRemoved {
				t.Errorf("legacy %s must be status removed", m.Name)
			}
			if rep.Tiers[m.Replacements[0]] != obsregistry.TierPlatform {
				t.Errorf("successor %s of removed %s not emitted", m.Replacements[0], m.Name)
			}
		}
	}

	// The method label is bounded: PURGE is recorded as OTHER.
	if got := testutil.ToFloat64(metrics.HttpRequestsTotal.WithLabelValues("OTHER", "/items", "2xx", "success")); got != 1 {
		t.Errorf("PURGE (as OTHER) count = %v", got)
	}
	body := scrape(t, reg)
	for _, want := range []string{
		`platform_http_requests_total{domain="iam",environment="prod",error_class="success",method="OTHER",route="/items",service="event-consumer",status_class="2xx"} 1`,
		`platform_http_panics_total{domain="iam",environment="prod",method="GET",route="/boom",service="event-consumer"} 1`,
		`platform_http_rate_limited_requests_total{domain="iam",environment="prod",method="POST",route="/items",service="event-consumer"} 1`,
		`platform_http_request_timeouts_total{domain="iam",environment="prod",method="GET",route="/items/:id",service="event-consumer"} 1`,
		`platform_grpc_panics_total{domain="iam",environment="prod",grpc_method="/iam.v1.Events/Publish",service="event-consumer"} 1`,
		`platform_grpc_rate_limited_requests_total{domain="iam",environment="prod",grpc_method="/iam.v1.Events/Publish",service="event-consumer"} 1`,
		`platform_grpc_server_inflight_requests{domain="iam",environment="prod",grpc_method="/iam.v1.Events/Watch",service="event-consumer"} 0`,
		`platform_build_info{domain="iam",environment="prod",service="event-consumer",version="v1.2.3"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %s", want)
		}
	}
}

// The exported collector vars are a read-only view: recorders use the
// atomically published set, so clobbering a var cannot disable (or race with)
// recording. Pre-initialization no-ops are pinned in test/unit/uninit.
func TestRecordersIgnoreExportedVars(t *testing.T) {
	reg := initPlatform(t, iamOpts)
	prevHTTP, prevActive := metrics.HttpRequestsTotal, metrics.HttpActiveRequests
	metrics.HttpRequestsTotal, metrics.HttpActiveRequests = nil, nil
	defer func() { metrics.HttpRequestsTotal, metrics.HttpActiveRequests = prevHTTP, prevActive }()
	if !metrics.HTTPEnabled() || !metrics.GRPCEnabled() {
		t.Fatal("enabled state follows the published set, not the exported vars")
	}
	metrics.RecordHTTP("GET", "/x", "2xx", "success", 0.1, 0, 0)
	metrics.HTTPActiveInc("/x")
	metrics.HTTPActiveDec("/x")
	if !strings.Contains(scrape(t, reg), `route="/x"`) {
		t.Error("recording must not depend on the exported vars")
	}
}

func TestUnknownSizesAreNotObserved(t *testing.T) {
	reg := initPlatform(t, iamOpts)
	metrics.RecordHTTP("POST", "/chunked", "2xx", "success", 0.1, metrics.RequestBodySize(-1), 10)
	metrics.RecordGRPC("/a.B/C", 0.1, "OK", "2xx", "success", metrics.UnknownSize, metrics.UnknownSize)
	counts := map[string]uint64{}
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fams {
		for _, m := range f.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				counts[f.GetName()] += h.GetSampleCount()
			}
		}
	}
	for name, want := range map[string]uint64{
		"platform_http_request_size_bytes":         0,
		"platform_http_response_size_bytes":        1,
		"platform_http_request_duration_seconds":   1,
		"platform_grpc_server_request_size_bytes":  0,
		"platform_grpc_server_response_size_bytes": 0,
		"platform_grpc_server_handling_seconds":    1,
	} {
		if counts[name] != want {
			t.Errorf("%s sample count = %d, want %d", name, counts[name], want)
		}
	}
}

// Re-initialization (tests) while requests are being recorded must be
// race-free: run with -race.
func TestRecordingConcurrentWithReinit(t *testing.T) {
	initPlatform(t, iamOpts)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			metrics.RecordHTTP("GET", "/r", "2xx", "success", 0.01, 1, 1)
			metrics.HTTPActiveInc("/r")
			metrics.HTTPActiveDec("/r")
			metrics.RecordGRPC("/a.B/C", 0.01, "OK", "2xx", "success", 1, 1)
			_ = metrics.ConstLabels()
			_, _ = metrics.Identity()
		}
	}()
	for range 20 {
		initPlatform(t, iamOpts)
	}
	<-done
}

func TestFailedRegistrationRollsBack(t *testing.T) {
	initPlatform(t, iamOpts)
	reg := prometheus.NewRegistry()
	// Identical shape (AlreadyRegisteredError): a registry remembers a name's
	// help and labels even after Unregister.
	m, _ := obsregistry.Default().Lookup("platform_http_panics_total")
	conflict := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: m.Name, Help: m.Help,
		ConstLabels: prometheus.Labels{"domain": "iam", "environment": "test", "service": "rollback-svc"},
	}, m.Labels)
	reg.MustRegister(conflict)
	other := metrics.Options{ServiceName: "rollback-svc", Domain: "iam", Environment: "test"}
	if err := metrics.InitWithRegistererOptions(other, reg); err == nil || !strings.Contains(err.Error(), "register collectors") {
		t.Fatalf("want a registration error, got %v", err)
	}
	if _, s := metrics.Identity(); s != "event-consumer" {
		t.Errorf("a failed init must keep the previous collectors active; Identity service = %q", s)
	}
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	// The conflicting vec has no children, so anything gathered (e.g.
	// platform_build_info, always 1) was left behind by the failed init.
	for _, f := range fams {
		t.Errorf("%s left on the registry after a failed init", f.GetName())
	}
	reg.Unregister(conflict)
	if err := metrics.InitWithRegistererOptions(other, reg); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, s := metrics.Identity(); s != "rollback-svc" {
		t.Errorf("Identity service = %q after retry", s)
	}
}

func TestIdentityAndConstLabels(t *testing.T) {
	initPlatform(t, iamOpts)
	if !metrics.Initialized() {
		t.Fatal("want initialized metrics")
	}
	if d, s := metrics.Identity(); d != "iam" || s != "event-consumer" {
		t.Errorf("Identity = %q, %q", d, s)
	}
	want := prometheus.Labels{"domain": "iam", "environment": "prod", "service": "event-consumer"}
	got := metrics.ConstLabels()
	if len(got) != len(want) {
		t.Errorf("ConstLabels = %v: version must not be a platform const label", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ConstLabels[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestOptionsValidation(t *testing.T) {
	for _, tc := range []struct {
		opts metrics.Options
		want string
	}{
		{metrics.Options{ServiceName: "s", Environment: "prod"}, "a domain is required"},
		{metrics.Options{ServiceName: "s", Domain: "hr", Environment: "prod"}, "domain"},
		{metrics.Options{ServiceName: "Bad Name", Domain: "iam", Environment: "prod"}, "service"},
		{metrics.Options{ServiceName: "s1", Domain: "iam", Environment: "qa"}, "environment"},
		{metrics.Options{ServiceName: "s1", Domain: "iam", Environment: "production"}, "environment"},
		{metrics.Options{ServiceName: "s1", Domain: "iam"}, "environment"},
		// Versions are sanitised, not rejected; only nothing usable fails.
		{metrics.Options{ServiceName: "s1", Domain: "iam", Environment: "prod", BuildVersion: "///"}, "build version"},
		{metrics.Options{ServiceName: "s1", Domain: "iam", Environment: "prod", BuildVersion: "refs/tags/"}, "build version"},
	} {
		if err := metrics.InitWithRegistererOptions(tc.opts, prometheus.NewRegistry()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("InitWithRegistererOptions(%+v) = %v, want %q", tc.opts, err, tc.want)
		}
		if err := metrics.InitWithOptions(tc.opts); err == nil {
			t.Errorf("InitWithOptions(%+v): want error", tc.opts)
		}
	}
	// Common BUILD_VERSION shapes are valid; surrounding whitespace is trimmed.
	for _, v := range []string{"", "unknown", "dev", "v1.2.3", "1.2.3-rc.1+build.5", "v1.2.3-4-gabc1234", "abc1234", " v1.0.0\n"} {
		o := iamOpts
		o.BuildVersion = v
		if err := metrics.InitWithRegistererOptions(o, prometheus.NewRegistry()); err != nil {
			t.Errorf("BuildVersion %q: %v", v, err)
		}
	}
	// A valid call once metrics are initialized is a no-op (same identity).
	initPlatform(t, iamOpts)
	if err := metrics.InitWithOptions(iamOpts); err != nil {
		t.Fatal(err)
	}
}

func TestMustInitFromConfig_EnvFallback(t *testing.T) {
	t.Setenv("OBSERVABILITY_DOMAIN", "iam")
	t.Setenv("APP_ENV", "production")
	func() {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), `"production"`) {
				t.Errorf("want panic naming the bad environment, got %v", r)
			}
		}()
		metrics.MustInitFromConfig(metrics.Options{ServiceName: "event-consumer"})
	}()
	t.Setenv("APP_ENV", "")
	t.Setenv("ENVIRONMENT", "prod")
	// Valid; the first identity is kept because testenv enables
	// AllowIdentityChangeForTesting (the mismatch panic is pinned in test/unit/uninit).
	metrics.MustInitFromConfig(metrics.Options{ServiceName: "event-consumer"})
	t.Setenv("OBSERVABILITY_DOMAIN", "")
	func() {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), "OBSERVABILITY_DOMAIN") {
				t.Errorf("want panic naming OBSERVABILITY_DOMAIN, got %v", r)
			}
		}()
		metrics.MustInitFromConfig(metrics.Options{ServiceName: "event-consumer"})
	}()
}

func TestHTTPMethodLabel(t *testing.T) {
	for in, want := range map[string]string{"GET": "GET", "TRACE": "TRACE", "get": "OTHER", "PURGE": "OTHER", "": "OTHER"} {
		if got := metrics.HTTPMethodLabel(in); got != want {
			t.Errorf("HTTPMethodLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func scrape(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range fams {
		for _, m := range f.GetMetric() {
			b.WriteString(f.GetName())
			b.WriteByte('{')
			for i, lp := range m.GetLabel() {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(lp.GetName() + `="` + lp.GetValue() + `"`)
			}
			b.WriteString("} ")
			switch {
			case m.Counter != nil:
				b.WriteString(strconv.FormatFloat(m.GetCounter().GetValue(), 'g', -1, 64))
			case m.Gauge != nil:
				b.WriteString(strconv.FormatFloat(m.GetGauge().GetValue(), 'g', -1, 64))
			default:
				b.WriteString("h")
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestApplyEnvDefaults(t *testing.T) {
	t.Setenv("BUILD_VERSION", "v9.9.9")
	t.Setenv("OBSERVABILITY_DOMAIN", "iam")
	t.Setenv("APP_ENV", "staging")
	got := metrics.ApplyEnvDefaults(metrics.Options{ServiceName: "event-consumer"})
	if got.BuildVersion != "v9.9.9" || got.Domain != "iam" || got.Environment != "staging" {
		t.Errorf("ApplyEnvDefaults = %+v", got)
	}
	explicit := metrics.Options{ServiceName: "s", BuildVersion: "v1", Domain: "billing", Environment: "prod"}
	if got := metrics.ApplyEnvDefaults(explicit); got != explicit {
		t.Errorf("explicit values must win over env: %+v", got)
	}
	t.Setenv("BUILD_VERSION", "")
	if got := metrics.ApplyEnvDefaults(metrics.Options{ServiceName: "s"}); got.BuildVersion != "unknown" {
		t.Errorf("BuildVersion = %q, want unknown", got.BuildVersion)
	}
}
