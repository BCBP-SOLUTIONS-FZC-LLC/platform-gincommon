package integration

// End-to-end gRPC tests over an in-process bufconn server running the full
// grpccommon interceptor stack: auth rejections are logged, the request ID is
// echoed, health probes bypass auth, unknown methods are bounded, and the client
// interceptors carry trace context, request ID and (opt-in) identity downstream.

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// downstreamSeen captures what the Inspect handler observed.
type downstreamSeen struct {
	mu        sync.Mutex
	requestID string
	userID    string
	traceID   string
}

type grpcHarness struct {
	conn    *grpc.ClientConn
	log     *fixtures.MockLogger
	seen    *downstreamSeen
	upTrace string // trace ID observed in Ping, for the propagation assertion
}

func unaryMethod(name string, fn func(ctx context.Context) error) grpc.MethodDesc {
	full := "/test.Echo/" + name
	return grpc.MethodDesc{
		MethodName: name,
		Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
			in := new(emptypb.Empty)
			if err := dec(in); err != nil {
				return nil, err
			}
			h := func(ctx context.Context, _ any) (any, error) { return &emptypb.Empty{}, fn(ctx) }
			if ic == nil {
				return h(ctx, in)
			}
			return ic(ctx, in, &grpc.UnaryServerInfo{FullMethod: full}, h)
		},
	}
}

func newGRPCHarness(t *testing.T) *grpcHarness {
	t.Helper()
	prev := gootel.GetTextMapPropagator()
	gootel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { gootel.SetTextMapPropagator(prev) })
	t.Cleanup(label.ResetGRPCMethods)

	h := &grpcHarness{log: &fixtures.MockLogger{}, seen: &downstreamSeen{}}
	cfg := grpccommon.Config{ServiceName: "grpc-integration", Logger: h.log}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpccommon.FullUnaryInterceptors(cfg)...),
		grpc.ChainStreamInterceptor(grpccommon.FullStreamInterceptors(cfg)...),
		grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
			return status.Error(codes.Unimplemented, "no such method")
		}),
	)

	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Echo",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			unaryMethod("Ping", func(ctx context.Context) error {
				h.upTrace = trace.SpanContextFromContext(ctx).TraceID().String()
				// Outbound call to a trusted internal service (here: ourselves).
				return h.conn.Invoke(ctx, "/test.Echo/Inspect", &emptypb.Empty{}, &emptypb.Empty{})
			}),
			unaryMethod("Inspect", func(ctx context.Context) error {
				h.seen.mu.Lock()
				defer h.seen.mu.Unlock()
				h.seen.requestID = grpccommon.RequestIDFromContext(ctx)
				if rc, ok := grpccommon.RequestContextFromContext(ctx); ok {
					h.seen.userID = rc.UserID
				}
				h.seen.traceID = trace.SpanContextFromContext(ctx).TraceID().String()
				return nil
			}),
		},
	}, struct{}{})
	grpccommon.RegisterHealth(srv)
	grpccommon.RegisterKnownMethods(srv)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(grpccommon.UnaryClientInterceptor(), grpccommon.UnaryClientIdentityInterceptor()),
		grpc.WithChainStreamInterceptor(grpccommon.StreamClientInterceptor(), grpccommon.StreamClientIdentityInterceptor()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h.conn = conn
	return h
}

func withIdentity(kv ...string) context.Context {
	base := []string{"x-user-id", "u1", "x-tenant-id", "t1", "x-tenant-roles", "user"}
	return metadata.AppendToOutgoingContext(context.Background(), append(base, kv...)...)
}

func TestGRPC_UnauthenticatedCallIsLoggedAtWarn(t *testing.T) {
	h := newGRPCHarness(t)
	err := h.conn.Invoke(context.Background(), "/test.Echo/Inspect", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
	if h.log.Msg != "grpc_request" || h.log.Level != "warn" || h.log.Fields["grpc_code"] != "Unauthenticated" {
		t.Fatalf("log = %s %s %v", h.log.Level, h.log.Msg, h.log.Fields)
	}
}

func TestGRPC_AmbiguousIdentityRejected(t *testing.T) {
	h := newGRPCHarness(t)
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"x-user-id", "u1", "x-user-id", "admin", "x-tenant-id", "t1")
	// Bypass the identity client interceptor's normalisation by using a raw connection call
	// without a RequestContext (the interceptor passes through when none is present).
	err := h.conn.Invoke(ctx, "/test.Echo/Inspect", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "ambiguous x-user-id metadata" {
		t.Fatalf("err = %v, want ambiguous x-user-id", err)
	}
}

func TestGRPC_RequestIDEchoedAndPropagatedDownstream(t *testing.T) {
	h := newGRPCHarness(t)
	var header metadata.MD
	err := h.conn.Invoke(withIdentity("x-request-id", "req-int-1"), "/test.Echo/Ping",
		&emptypb.Empty{}, &emptypb.Empty{}, grpc.Header(&header))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got := header.Get("x-request-id"); len(got) != 1 || got[0] != "req-int-1" {
		t.Fatalf("echoed x-request-id = %v", got)
	}
	h.seen.mu.Lock()
	defer h.seen.mu.Unlock()
	if h.seen.requestID != "req-int-1" {
		t.Fatalf("downstream request ID = %q", h.seen.requestID)
	}
	if h.seen.userID != "u1" {
		t.Fatalf("downstream user = %q, want forwarded u1", h.seen.userID)
	}
	if h.seen.traceID == "" || h.seen.traceID != h.upTrace {
		t.Fatalf("downstream trace = %q, upstream = %q", h.seen.traceID, h.upTrace)
	}
}

func TestGRPC_HealthBypassesAuth(t *testing.T) {
	h := newGRPCHarness(t)
	resp, err := healthpb.NewHealthClient(h.conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, %v; want SERVING", resp, err)
	}
}

func TestGRPC_UnknownMethodBoundedLabel(t *testing.T) {
	h := newGRPCHarness(t)
	before := testutil.ToFloat64(metrics.GrpcRequestsTotal.WithLabelValues(
		label.Unknown, "Unauthenticated", metrics.GRPCStatusClass(codes.Unauthenticated),
		metrics.GRPCRequestOutcome(codes.Unauthenticated)))
	// Well-formed but unregistered: only RegisterKnownMethods can bound it.
	err := h.conn.Invoke(context.Background(), "/attacker.Svc/Random123", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v", status.Code(err))
	}
	after := testutil.ToFloat64(metrics.GrpcRequestsTotal.WithLabelValues(
		label.Unknown, "Unauthenticated", metrics.GRPCStatusClass(codes.Unauthenticated),
		metrics.GRPCRequestOutcome(codes.Unauthenticated)))
	if after != before+1 {
		t.Fatalf("unknown-method counter delta = %v, want 1", after-before)
	}
	if h.log.Fields["method"] != label.Unknown {
		t.Fatalf("logged method = %v, want unknown", h.log.Fields["method"])
	}
}

// Paths under the health prefix that are not Check/Watch/List reach the
// UnknownServiceHandler; they must be authenticated like any other method.
func TestGRPC_HealthPrefixNearMissRequiresAuth(t *testing.T) {
	h := newGRPCHarness(t)
	for _, m := range []string{
		"/grpc.health.v1.Health/x/admin.Svc/Delete",
		"/grpc.health.v1.Health/CheckX",
		"/grpc.health.v1.HealthX/Check",
	} {
		err := h.conn.Invoke(context.Background(), m, &emptypb.Empty{}, &emptypb.Empty{})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("%s: code = %v, want Unauthenticated", m, status.Code(err))
		}
	}
}

// An idle client on a bidi stream whose handler loops on RecvMsg: the stream
// timeout must end the RPC and the handler must exit once grpc-go tears the
// stream down.
func TestGRPC_StreamTimeoutEndsIdleRecvLoop(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	handlerExited := make(chan struct{})
	srv := grpc.NewServer(grpc.ChainStreamInterceptor(grpccommon.TimeoutStreamInterceptor(100 * time.Millisecond)))
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Idle",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    "Chat",
			ClientStreams: true,
			ServerStreams: true,
			Handler: func(_ any, ss grpc.ServerStream) error {
				defer close(handlerExited)
				for {
					if err := ss.RecvMsg(new(emptypb.Empty)); err != nil {
						return err
					}
				}
			},
		}},
	}, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	cs, err := conn.NewStream(context.Background(),
		&grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, "/test.Idle/Chat")
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	start := time.Now()
	err = cs.RecvMsg(new(emptypb.Empty)) // client stays idle: never sends
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("client err = %v, want DeadlineExceeded", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("took %v, want ~100ms", el)
	}
	select {
	case <-handlerExited:
	case <-time.After(5 * time.Second):
		t.Fatal("handler goroutine still blocked in RecvMsg after the RPC ended")
	}
}

// With the stream timeout placed inside the observability chain (after
// ObservabilityStreamInterceptors), metrics record the DeadlineExceeded the client
// received even when the handler swallows the error and later returns nil.
func TestGRPC_StreamTimeoutInnermost_MetricsMatchClient(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	cfg := grpccommon.Config{ServiceName: "grpc-integration"}
	srv := grpc.NewServer(grpc.ChainStreamInterceptor(append(
		grpccommon.ObservabilityStreamInterceptors(cfg),
		grpccommon.TimeoutStreamInterceptor(100*time.Millisecond))...))
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Swallow",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    "Chat",
			ClientStreams: true,
			ServerStreams: true,
			Handler: func(_ any, ss grpc.ServerStream) error {
				<-ss.Context().Done()
				time.Sleep(50 * time.Millisecond)
				return nil // swallows the deadline
			},
		}},
	}, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	counter := func(c codes.Code) float64 {
		return testutil.ToFloat64(metrics.GrpcRequestsTotal.WithLabelValues(
			"/test.Swallow/Chat", c.String(), metrics.GRPCStatusClass(c), metrics.GRPCRequestOutcome(c)))
	}
	beforeDE, beforeOK := counter(codes.DeadlineExceeded), counter(codes.OK)

	cs, err := conn.NewStream(context.Background(),
		&grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, "/test.Swallow/Chat")
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if err := cs.RecvMsg(new(emptypb.Empty)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("client err = %v, want DeadlineExceeded", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for counter(codes.DeadlineExceeded) == beforeDE && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := counter(codes.DeadlineExceeded) - beforeDE; got != 1 {
		t.Fatalf("DeadlineExceeded count delta = %v, want 1", got)
	}
	time.Sleep(100 * time.Millisecond) // let the abandoned handler return nil
	if got := counter(codes.OK) - beforeOK; got != 0 {
		t.Fatalf("OK count delta = %v, want 0 (the swallowed nil must not be recorded)", got)
	}
}
