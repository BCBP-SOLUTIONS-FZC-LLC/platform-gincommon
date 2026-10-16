package grpc_test

// Tests for the gRPC production-readiness hardening: wire-accurate status
// classification, logging around auth, bounded method labels, client propagation,
// ambiguous identity rejection, request-ID echo, span status attributes, timeouts
// and health exemption.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func chainUnary(ics []grpc.UnaryServerInterceptor, method string, h grpc.UnaryHandler) grpc.UnaryHandler {
	info := &grpc.UnaryServerInfo{FullMethod: method}
	for i := len(ics) - 1; i >= 0; i-- {
		ic, next := ics[i], h
		h = func(ctx context.Context, req any) (any, error) { return ic(ctx, req, info, next) }
	}
	return h
}

func chainStream(ics []grpc.StreamServerInterceptor, method string, h grpc.StreamHandler) grpc.StreamHandler {
	info := &grpc.StreamServerInfo{FullMethod: method}
	for i := len(ics) - 1; i >= 0; i-- {
		ic, next := ics[i], h
		h = func(srv any, ss grpc.ServerStream) error { return ic(srv, ss, info, next) }
	}
	return h
}

func identityCtx(kv ...string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(kv...))
}

func okUnary(context.Context, any) (any, error) { return &emptypb.Empty{}, nil }

func newRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := gootel.GetTracerProvider()
	gootel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		gootel.SetTracerProvider(prev)
	})
	return sr
}

func spanAttr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// headerStream records SetHeader calls.
type headerStream struct {
	testServerStream
	mu     sync.Mutex
	header metadata.MD
}

func (s *headerStream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.header = metadata.Join(s.header, md)
	return nil
}

// ─── #1 context errors classified like the wire ───────────────────────────────

func TestMetrics_ContextErrorsClassifiedAsWireCodes(t *testing.T) {
	fixtures.InitMetrics("grpc-hardening", "test")
	ic := grpcmw.UnaryMetricsInterceptor()
	cases := map[string]struct {
		err  error
		code codes.Code
	}{
		"/test.Ctx/Deadline": {context.DeadlineExceeded, codes.DeadlineExceeded},
		"/test.Ctx/Canceled": {fmt.Errorf("wrapped: %w", context.Canceled), codes.Canceled},
		"/test.Ctx/Plain":    {errors.New("plain"), codes.Unknown},
	}
	for method, tc := range cases {
		_, _ = ic(context.Background(), &emptypb.Empty{}, &grpc.UnaryServerInfo{FullMethod: method},
			func(context.Context, any) (any, error) { return nil, tc.err })
		c, err := metrics.GrpcRequestsTotal.GetMetricWithLabelValues(method, tc.code.String(),
			metrics.GRPCStatusClass(tc.code), metrics.GRPCRequestOutcome(tc.code))
		if err != nil {
			t.Fatalf("%s: get metric: %v", method, err)
		}
		if got := testutil.ToFloat64(c); got != 1 {
			t.Fatalf("%s: count with code %v = %v, want 1", method, tc.code, got)
		}
	}
}

func TestLogging_ContextDeadlineLogsWarnDeadlineExceeded(t *testing.T) {
	log := &fixtures.MockLogger{}
	_, _ = grpcmw.UnaryLoggingInterceptor(log)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/svc/Method"},
		func(context.Context, any) (any, error) { return nil, context.DeadlineExceeded })
	if log.Level != "warn" || log.Fields["grpc_code"] != "DeadlineExceeded" {
		t.Fatalf("log = %s %v, want warn DeadlineExceeded", log.Level, log.Fields["grpc_code"])
	}
}

// ─── #7 span status attributes ────────────────────────────────────────────────

func TestTracing_StatusCodeAttributes(t *testing.T) {
	sr := newRecorder(t)
	ic := grpcmw.UnaryTracingInterceptor("attr-svc")
	errs := []error{nil, context.DeadlineExceeded, status.Error(codes.Code(99), "custom")}
	for _, e := range errs {
		_, _ = ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc.A/M"},
			func(context.Context, any) (any, error) { return nil, e })
	}
	want := []struct {
		name string
		num  int64
	}{{"OK", 0}, {"DEADLINE_EXCEEDED", 4}, {"99", 99}}
	spans := sr.Ended()
	if len(spans) != len(want) {
		t.Fatalf("spans = %d, want %d", len(spans), len(want))
	}
	for i, w := range want {
		if v, _ := spanAttr(spans[i], "rpc.response.status_code"); v.AsString() != w.name {
			t.Errorf("span %d rpc.response.status_code = %q, want %q", i, v.AsString(), w.name)
		}
		if v, _ := spanAttr(spans[i], "rpc.grpc.status_code"); v.AsInt64() != w.num {
			t.Errorf("span %d rpc.grpc.status_code = %d, want %d", i, v.AsInt64(), w.num)
		}
	}
}

func TestTracing_PanicRecordsInternalStatusCode(t *testing.T) {
	sr := newRecorder(t)
	ic := grpcmw.StreamTracingInterceptor("")
	func() {
		defer func() { _ = recover() }()
		_ = ic(nil, &testServerStream{}, &grpc.StreamServerInfo{FullMethod: "/svc.A/S"},
			func(any, grpc.ServerStream) error { panic("boom") })
	}()
	s := sr.Ended()[0]
	if v, _ := spanAttr(s, "rpc.response.status_code"); v.AsString() != "INTERNAL" {
		t.Fatalf("rpc.response.status_code = %q, want INTERNAL", v.AsString())
	}
}

// ─── #3 bounded method names ──────────────────────────────────────────────────

func TestTracing_UnknownMethodBoundedSpanName(t *testing.T) {
	sr := newRecorder(t)
	ic := grpcmw.UnaryTracingInterceptor("bounded")
	garbage := "/" + strings.Repeat("x", 300) + "/\nEvil"
	_, _ = ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: garbage}, okUnary)
	s := sr.Ended()[0]
	if s.Name() != label.Unknown {
		t.Fatalf("span name = %q, want %q", s.Name(), label.Unknown)
	}
	if v, _ := spanAttr(s, "rpc.method"); v.AsString() != "_OTHER" {
		t.Fatalf("rpc.method = %q, want _OTHER", v.AsString())
	}
	orig, _ := spanAttr(s, "rpc.method_original")
	if len(orig.AsString()) > 256 || strings.ContainsAny(orig.AsString(), "\n") {
		t.Fatalf("rpc.method_original not sanitized: %q", orig.AsString())
	}
}

type fakeServiceInfo map[string]grpc.ServiceInfo

func (f fakeServiceInfo) GetServiceInfo() map[string]grpc.ServiceInfo { return f }

func TestRegisterKnownMethods_UnregisteredMapToUnknown(t *testing.T) {
	t.Cleanup(label.ResetGRPCMethods)
	grpccommon.RegisterKnownMethods(fakeServiceInfo{
		"pkg.Svc": {Methods: []grpc.MethodInfo{{Name: "Get"}, {Name: "Watch", IsServerStream: true}}},
	})
	if got := label.GRPCMethod("/pkg.Svc/Get"); got != "/pkg.Svc/Get" {
		t.Fatalf("registered = %q", got)
	}
	if got := label.GRPCMethod("/pkg.Svc/Watch"); got != "/pkg.Svc/Watch" {
		t.Fatalf("registered stream = %q", got)
	}
	if got := label.GRPCMethod("/pkg.Svc/Scan"); got != label.Unknown {
		t.Fatalf("unregistered = %q, want unknown", got)
	}
}

// ─── #5 ambiguous identity ────────────────────────────────────────────────────

func TestRequireAuth_RejectsRepeatedIdentity(t *testing.T) {
	for _, key := range []string{"x-user-id", "x-tenant-id"} {
		ctx := identityCtx("x-user-id", "u1", "x-tenant-id", "t1", key, "attacker")
		_, err := grpcmw.UnaryRequireAuthInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, okUnary)
		if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "ambiguous "+key+" metadata" {
			t.Fatalf("%s: err = %v, want Unauthenticated ambiguous", key, err)
		}
	}
}

func TestContextInterceptor_RejectsRepeatedIdentity(t *testing.T) {
	for _, key := range []string{"x-user-id", "x-tenant-id"} {
		ctx := identityCtx("x-user-id", "u1", "x-tenant-id", "t1", key, "attacker")
		_, err := grpcmw.UnaryContextInterceptor(service.NewContextService())(ctx, nil,
			&grpc.UnaryServerInfo{FullMethod: "/svc/M"}, okUnary)
		if status.Code(err) != codes.Unauthenticated || !strings.Contains(err.Error(), "ambiguous "+key) {
			t.Fatalf("%s: err = %v, want Unauthenticated ambiguous", key, err)
		}
	}
}

// ─── #2 logging wraps auth ────────────────────────────────────────────────────

func TestFullUnaryInterceptors_RejectedCallIsLoggedWarn(t *testing.T) {
	log := &fixtures.MockLogger{}
	ics := grpccommon.FullUnaryInterceptors(grpccommon.Config{ServiceName: "full-log", Logger: log})
	h := chainUnary(ics, "/svc.A/M", okUnary)

	if _, err := h(context.Background(), &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
	if log.Msg != "grpc_request" || log.Level != "warn" || log.Fields["grpc_code"] != "Unauthenticated" {
		t.Fatalf("log = %s %s %v, want warn grpc_request Unauthenticated", log.Level, log.Msg, log.Fields)
	}
	if _, ok := log.Fields["user_id"]; ok {
		t.Fatal("rejected call must not log a user_id")
	}

	if _, err := h(identityCtx("x-user-id", "u1", "x-tenant-id", "t1"), &emptypb.Empty{}); err != nil {
		t.Fatalf("authenticated call err = %v", err)
	}
	if log.Level != "info" || log.Fields["user_id"] != "u1" || log.Fields["tenant_id"] != "t1" {
		t.Fatalf("log = %s %v, want info with identity", log.Level, log.Fields)
	}
}

func TestFullStreamInterceptors_RejectedAndAuthenticatedLogged(t *testing.T) {
	log := &fixtures.MockLogger{}
	ics := grpccommon.FullStreamInterceptors(grpccommon.Config{ServiceName: "full-log-stream", Logger: log})
	h := chainStream(ics, "/svc.A/S", func(any, grpc.ServerStream) error { return nil })

	if err := h(nil, &testServerStream{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
	if log.Level != "warn" || log.Fields["grpc_code"] != "Unauthenticated" {
		t.Fatalf("log = %s %v", log.Level, log.Fields)
	}
	if err := h(nil, &testServerStream{ctx: identityCtx("x-user-id", "u2", "x-tenant-id", "t2")}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if log.Level != "info" || log.Fields["user_id"] != "u2" {
		t.Fatalf("log = %s %v, want info user u2", log.Level, log.Fields)
	}
}

// ─── #6 request ID echo ───────────────────────────────────────────────────────

func TestStreamRequestID_EchoedInHeader(t *testing.T) {
	ss := &headerStream{testServerStream: testServerStream{ctx: identityCtx("x-request-id", "req-123")}}
	var seen string
	err := grpcmw.StreamRequestIDInterceptor()(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc/S"},
		func(_ any, s grpc.ServerStream) error { seen = grpcmw.RequestIDFromContext(s.Context()); return nil })
	if err != nil || seen != "req-123" {
		t.Fatalf("err=%v seen=%q", err, seen)
	}
	if got := ss.header.Get("x-request-id"); len(got) != 1 || got[0] != "req-123" {
		t.Fatalf("header x-request-id = %v, want [req-123]", got)
	}
}

// ─── #8 timeout + health exemption ────────────────────────────────────────────

func TestTimeoutUnaryInterceptor(t *testing.T) {
	ic := grpccommon.TimeoutUnaryInterceptor(time.Second)
	remaining := func(ctx context.Context) time.Duration {
		var got time.Duration
		_, _ = ic(ctx, nil, &grpc.UnaryServerInfo{}, func(c context.Context, _ any) (any, error) {
			dl, ok := c.Deadline()
			if !ok {
				got = -1
				return nil, nil
			}
			got = time.Until(dl)
			return nil, nil
		})
		return got
	}
	if d := remaining(context.Background()); d <= 0 || d > time.Second {
		t.Fatalf("no deadline: remaining = %v, want (0,1s]", d)
	}
	late, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if d := remaining(late); d > time.Second {
		t.Fatalf("later deadline not capped: %v", d)
	}
	early, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if d := remaining(early); d > 100*time.Millisecond {
		t.Fatalf("earlier deadline extended: %v", d)
	}

	_, _ = grpccommon.TimeoutUnaryInterceptor(0)(context.Background(), nil, &grpc.UnaryServerInfo{},
		func(c context.Context, _ any) (any, error) {
			if _, ok := c.Deadline(); ok {
				t.Fatal("zero timeout must not set a deadline")
			}
			return nil, nil
		})
}

func TestTimeoutStreamInterceptor(t *testing.T) {
	err := grpccommon.TimeoutStreamInterceptor(time.Second)(nil, &testServerStream{}, &grpc.StreamServerInfo{},
		func(_ any, ss grpc.ServerStream) error {
			if _, ok := ss.Context().Deadline(); !ok {
				t.Fatal("expected deadline on stream context")
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	orig := &testServerStream{}
	_ = grpccommon.TimeoutStreamInterceptor(-1)(nil, orig, &grpc.StreamServerInfo{},
		func(_ any, ss grpc.ServerStream) error {
			if ss != orig {
				t.Fatal("negative timeout must pass the stream through unchanged")
			}
			return nil
		})
}

func TestProtected_HealthMethodsExemptFromAuth(t *testing.T) {
	cfg := grpccommon.Config{ServiceName: "health-exempt"}
	h := chainUnary(grpccommon.ProtectedUnaryInterceptors(cfg), "/grpc.health.v1.Health/Check", okUnary)
	if _, err := h(context.Background(), nil); err != nil {
		t.Fatalf("unary health err = %v", err)
	}
	sh := chainStream(grpccommon.ProtectedStreamInterceptors(cfg), "/grpc.health.v1.Health/Watch",
		func(any, grpc.ServerStream) error { return nil })
	if err := sh(nil, &testServerStream{}); err != nil {
		t.Fatalf("stream health err = %v", err)
	}
}

func TestRequirePermission_HealthMethodsExempt(t *testing.T) {
	// No RequestContext in ctx: a non-health method would get FailedPrecondition.
	auth := grpccommon.NewDemoRBAC()
	h := chainUnary([]grpc.UnaryServerInterceptor{grpccommon.RequirePermission("write", "items", auth)},
		"/grpc.health.v1.Health/Check", okUnary)
	if _, err := h(context.Background(), nil); err != nil {
		t.Fatalf("unary health err = %v", err)
	}
	sh := chainStream([]grpc.StreamServerInterceptor{grpccommon.RequireStreamPermission("write", "items", auth)},
		"/grpc.health.v1.Health/Watch", func(any, grpc.ServerStream) error { return nil })
	if err := sh(nil, &testServerStream{}); err != nil {
		t.Fatalf("stream health err = %v", err)
	}
	h = chainUnary([]grpc.UnaryServerInterceptor{grpccommon.RequirePermission("write", "items", auth)},
		"/pkg.Svc/Method", okUnary)
	if _, err := h(context.Background(), nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("non-health err = %v, want FailedPrecondition", err)
	}
}

func TestRegisterHealth_RegistersService(t *testing.T) {
	srv := grpc.NewServer()
	h := grpccommon.RegisterHealth(srv)
	if h == nil {
		t.Fatal("RegisterHealth returned nil")
	}
	if _, ok := srv.GetServiceInfo()["grpc.health.v1.Health"]; !ok {
		t.Fatal("health service not registered")
	}
}

// ─── #4 client interceptors ───────────────────────────────────────────────────

// serverCtx simulates a handler context: request ID, verified identity and an active span.
func serverCtx(t *testing.T, outgoing ...string) (context.Context, trace.SpanContext) {
	t.Helper()
	newRecorder(t)
	prev := gootel.GetTextMapPropagator()
	gootel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { gootel.SetTextMapPropagator(prev) })

	ctx := grpcmw.ContextWithRequestID(context.Background(), "req-abc")
	ctx = domain.ContextWithRequestContext(ctx, &domain.RequestContext{UserID: "u1", TenantID: "t1", Roles: []string{"admin", "user"}})
	if len(outgoing) > 0 {
		ctx = metadata.AppendToOutgoingContext(ctx, outgoing...)
	}
	ctx, span := gootel.Tracer("client-test").Start(ctx, "handler")
	t.Cleanup(func() { span.End() })
	return ctx, span.SpanContext()
}

func captureUnary(t *testing.T, ic grpc.UnaryClientInterceptor, ctx context.Context) metadata.MD {
	t.Helper()
	var md metadata.MD
	err := ic(ctx, "/svc.B/M", nil, nil, nil, func(c context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		md, _ = metadata.FromOutgoingContext(c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return md
}

func captureStream(t *testing.T, ic grpc.StreamClientInterceptor, ctx context.Context) metadata.MD {
	t.Helper()
	var md metadata.MD
	_, err := ic(ctx, &grpc.StreamDesc{}, nil, "/svc.B/S", func(c context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		md, _ = metadata.FromOutgoingContext(c)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return md
}

func TestClientInterceptor_PropagatesTraceAndRequestIDOnly(t *testing.T) {
	ctx, sc := serverCtx(t)
	for name, md := range map[string]metadata.MD{
		"unary":  captureUnary(t, grpccommon.UnaryClientInterceptor(), ctx),
		"stream": captureStream(t, grpccommon.StreamClientInterceptor(), ctx),
	} {
		tp := md.Get("traceparent")
		if len(tp) != 1 || !strings.Contains(tp[0], sc.TraceID().String()) {
			t.Fatalf("%s: traceparent = %v, want trace %s", name, tp, sc.TraceID())
		}
		if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "req-abc" {
			t.Fatalf("%s: x-request-id = %v", name, got)
		}
		if len(md.Get("x-user-id")) != 0 {
			t.Fatalf("%s: propagation interceptor must not forward identity", name)
		}
	}
}

func TestClientInterceptor_KeepsExplicitRequestID(t *testing.T) {
	ctx, _ := serverCtx(t, "x-request-id", "explicit")
	md := captureUnary(t, grpccommon.UnaryClientInterceptor(), ctx)
	if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "explicit" {
		t.Fatalf("x-request-id = %v, want [explicit]", got)
	}
}

func TestClientIdentityInterceptor_ForwardsVerifiedIdentity(t *testing.T) {
	ctx, _ := serverCtx(t, "x-user-id", "stale", "x-tenant-roles", "stale")
	for name, md := range map[string]metadata.MD{
		"unary":  captureUnary(t, grpccommon.UnaryClientIdentityInterceptor(), ctx),
		"stream": captureStream(t, grpccommon.StreamClientIdentityInterceptor(), ctx),
	} {
		if got := md.Get("x-user-id"); len(got) != 1 || got[0] != "u1" {
			t.Fatalf("%s: x-user-id = %v, want [u1] (replaced, never repeated)", name, got)
		}
		if got := md.Get("x-tenant-id"); len(got) != 1 || got[0] != "t1" {
			t.Fatalf("%s: x-tenant-id = %v", name, got)
		}
		if got := md.Get("x-tenant-roles"); len(got) != 1 || got[0] != "admin,user" {
			t.Fatalf("%s: x-tenant-roles = %v", name, got)
		}
	}
}

func TestClientIdentityInterceptor_NoRolesAndNoContext(t *testing.T) {
	ctx := domain.ContextWithRequestContext(
		metadata.AppendToOutgoingContext(context.Background(), "x-tenant-roles", "stale"),
		&domain.RequestContext{UserID: "u1", TenantID: "t1"})
	md := captureUnary(t, grpccommon.UnaryClientIdentityInterceptor(), ctx)
	if len(md.Get("x-tenant-roles")) != 0 {
		t.Fatalf("roles must be cleared when RequestContext has none: %v", md.Get("x-tenant-roles"))
	}
	md = captureUnary(t, grpccommon.UnaryClientIdentityInterceptor(), context.Background())
	if len(md) != 0 {
		t.Fatalf("no RequestContext: outgoing metadata = %v, want none", md)
	}
}
