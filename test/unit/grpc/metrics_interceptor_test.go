package grpc_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// testServerStream is a minimal grpc.ServerStream for unit tests.
type testServerStream struct {
	ctx     context.Context
	recvErr error
	sendErr error
}

func (s *testServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *testServerStream) SendHeader(metadata.MD) error { return nil }
func (s *testServerStream) SetTrailer(metadata.MD)       {}
func (s *testServerStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
func (s *testServerStream) SendMsg(any) error { return s.sendErr }
func (s *testServerStream) RecvMsg(any) error { return s.recvErr }

// grpcOKError wraps a gRPC status with code OK but is itself non-nil,
// exercising the defensive `code == OK && err != nil` branch in observeGRPC.
type grpcOKError struct{}

func (grpcOKError) Error() string { return "artificial ok-status error" }
func (grpcOKError) GRPCStatus() *status.Status {
	return status.New(codes.OK, "artificial")
}

func TestUnaryMetricsInterceptor_RecordsSuccess(t *testing.T) {
	fixtures.InitMetrics("grpc-interceptor-test", "test")
	ic := grpcmw.UnaryMetricsInterceptor()

	req := &emptypb.Empty{}
	_, err := ic(
		context.Background(),
		req,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Ping"},
		func(ctx context.Context, in any) (any, error) {
			return &emptypb.Empty{}, nil
		},
	)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
}

func TestUnaryMetricsInterceptor_RecordsRateLimited(t *testing.T) {
	fixtures.InitMetrics("grpc-rl", "test")
	ic := grpcmw.UnaryMetricsInterceptor()

	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Limited"},
		func(ctx context.Context, in any) (any, error) {
			return nil, status.Error(codes.ResourceExhausted, "rate limited")
		},
	)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v", status.Code(err))
	}

	counter, err := metrics.GrpcRateLimitedRequestsTotal.GetMetricWithLabelValues("/test.Service/Limited")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("grpc_rate_limited_requests_total = %v, want 1", got)
	}
}

func TestUnaryMetricsInterceptor_RecordsError(t *testing.T) {
	fixtures.InitMetrics("grpc-interceptor-err", "test")
	ic := grpcmw.UnaryMetricsInterceptor()

	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Fail"},
		func(ctx context.Context, in any) (any, error) {
			return nil, status.Error(codes.InvalidArgument, "bad input")
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v", status.Code(err))
	}
}

func TestUnaryMetricsInterceptor_UnknownMethod(t *testing.T) {
	fixtures.InitMetrics("grpc-unknown-method", "test")
	ic := grpcmw.UnaryMetricsInterceptor()

	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: ""},
		func(ctx context.Context, in any) (any, error) {
			return &emptypb.Empty{}, nil
		},
	)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}

	counter, err := metrics.GrpcRequestsTotal.GetMetricWithLabelValues(
		"unknown", "OK", "2xx", "success",
	)
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("grpc_requests_total unknown method = %v, want 1", got)
	}
}

func TestDefaultUnaryInterceptors_NotEmpty(t *testing.T) {
	ics := grpccommon.DefaultUnaryInterceptors(grpccommon.Config{ServiceName: "pkg-grpc"})
	if len(ics) == 0 {
		t.Fatal("expected interceptors")
	}
}

func TestDefaultStreamInterceptors_NotEmpty(t *testing.T) {
	ics := grpccommon.DefaultStreamInterceptors(grpccommon.Config{ServiceName: "pkg-grpc-stream"})
	if len(ics) == 0 {
		t.Fatal("expected stream interceptors")
	}
}

func TestStreamMetricsInterceptor_RecordsSuccess(t *testing.T) {
	fixtures.InitMetrics("grpc-stream-success", "test")
	ic := grpcmw.StreamMetricsInterceptor()

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/BidiStream"}

	err := ic(
		nil,
		stream,
		info,
		func(srv any, ss grpc.ServerStream) error {
			_ = ss.RecvMsg(&emptypb.Empty{})
			_ = ss.SendMsg(&emptypb.Empty{})
			return nil
		},
	)
	if err != nil {
		t.Fatalf("stream interceptor err: %v", err)
	}

	counter, cerr := metrics.GrpcRequestsTotal.GetMetricWithLabelValues(
		"/test.Service/BidiStream", "OK", "2xx", "success",
	)
	if cerr != nil {
		t.Fatalf("get metric: %v", cerr)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("grpc_requests_total stream = %v, want 1", got)
	}
}

func TestStreamMetricsInterceptor_RecordsError(t *testing.T) {
	fixtures.InitMetrics("grpc-stream-err", "test")
	ic := grpcmw.StreamMetricsInterceptor()

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/ErrStream"}

	err := ic(
		nil,
		stream,
		info,
		func(srv any, ss grpc.ServerStream) error {
			return status.Error(codes.Internal, "stream failed")
		},
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}

func TestStreamMetricsInterceptor_NilSafe(t *testing.T) {
	saved := metrics.GrpcRequestsTotal
	metrics.GrpcRequestsTotal = nil
	defer func() { metrics.GrpcRequestsTotal = saved }()

	ic := grpcmw.StreamMetricsInterceptor()
	stream := &testServerStream{}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	if err := ic(nil, stream, info, func(srv any, ss grpc.ServerStream) error { return nil }); err != nil {
		t.Fatalf("nil guard err: %v", err)
	}
}

func TestUnaryMetricsInterceptor_NilSafe(t *testing.T) {
	saved := metrics.GrpcRequestsTotal
	metrics.GrpcRequestsTotal = nil
	defer func() { metrics.GrpcRequestsTotal = saved }()

	ic := grpcmw.UnaryMetricsInterceptor()
	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Ping"},
		func(ctx context.Context, in any) (any, error) { return &emptypb.Empty{}, nil },
	)
	if err != nil {
		t.Fatalf("nil guard err: %v", err)
	}
}

// TestUnaryMetricsInterceptor_NilInflight_Skips verifies that niling GrpcInflight
// (while GrpcRequestsTotal remains set) triggers the combined sentinel without panic.
func TestUnaryMetricsInterceptor_NilInflight_Skips(t *testing.T) {
	saved := metrics.GrpcInflight
	metrics.GrpcInflight = nil
	defer func() { metrics.GrpcInflight = saved }()

	ic := grpcmw.UnaryMetricsInterceptor()
	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Ping"},
		func(ctx context.Context, in any) (any, error) { return &emptypb.Empty{}, nil },
	)
	if err != nil {
		t.Fatalf("nil guard err: %v", err)
	}
}

// TestStreamMetricsInterceptor_NilInflight_Skips verifies that niling GrpcInflight
// (while GrpcRequestsTotal remains set) triggers the combined sentinel without panic.
func TestStreamMetricsInterceptor_NilInflight_Skips(t *testing.T) {
	saved := metrics.GrpcInflight
	metrics.GrpcInflight = nil
	defer func() { metrics.GrpcInflight = saved }()

	ic := grpcmw.StreamMetricsInterceptor()
	stream := &testServerStream{}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	if err := ic(nil, stream, info, func(srv any, ss grpc.ServerStream) error { return nil }); err != nil {
		t.Fatalf("nil guard err: %v", err)
	}
}

func TestStreamMetricsInterceptor_RecvMsgError(t *testing.T) {
	fixtures.InitMetrics("grpc-stream-recv-err", "test")
	ic := grpcmw.StreamMetricsInterceptor()

	recvErr := status.Error(codes.Unavailable, "recv failed")
	stream := &testServerStream{ctx: context.Background(), recvErr: recvErr}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/RecvErr"}

	handlerErr := ic(
		nil,
		stream,
		info,
		func(srv any, ss grpc.ServerStream) error {
			return ss.RecvMsg(&emptypb.Empty{})
		},
	)
	if status.Code(handlerErr) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", status.Code(handlerErr))
	}
}

func TestStreamMetricsInterceptor_SendMsgError(t *testing.T) {
	fixtures.InitMetrics("grpc-stream-send-err", "test")
	ic := grpcmw.StreamMetricsInterceptor()

	sendErr := status.Error(codes.ResourceExhausted, "send failed")
	stream := &testServerStream{ctx: context.Background(), sendErr: sendErr}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/SendErr"}

	handlerErr := ic(
		nil,
		stream,
		info,
		func(srv any, ss grpc.ServerStream) error {
			return ss.SendMsg(&emptypb.Empty{})
		},
	)
	if status.Code(handlerErr) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", status.Code(handlerErr))
	}
}

// TestUnaryMetricsInterceptor_PanicRecordedAs5xx verifies the key design invariant:
// a panicking unary handler is still counted in grpc_requests_total as Internal/5xx.
func TestUnaryMetricsInterceptor_PanicRecordedAs5xx(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-grpc-panic-5xx", "test", reg)

	recovery := grpcmw.UnaryPanicRecoveryInterceptor(nil)
	metricsIC := grpcmw.UnaryMetricsInterceptor()

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/PanicUnary"}

	// Chain: recovery → metrics → panicking handler (mirrors FullUnaryInterceptors order).
	_, err := recovery(
		context.Background(), nil, info,
		func(ctx context.Context, req any) (any, error) {
			return metricsIC(ctx, req, info,
				func(ctx context.Context, req any) (any, error) {
					panic("test panic for metrics")
				},
			)
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal after panic recovery", status.Code(err))
	}

	counter, cerr := metrics.GrpcRequestsTotal.GetMetricWithLabelValues(
		"/test.Service/PanicUnary", "Internal", "5xx", "server_error",
	)
	if cerr != nil {
		t.Fatalf("get metric: %v", cerr)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("grpc_requests_total[5xx/server_error] = %v, want 1 for panic", got)
	}
}

// TestStreamMetricsInterceptor_PanicRecordedAs5xx mirrors the unary test for streams.
func TestStreamMetricsInterceptor_PanicRecordedAs5xx(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-grpc-stream-panic-5xx", "test", reg)

	recovery := grpcmw.StreamPanicRecoveryInterceptor(nil)
	metricsIC := grpcmw.StreamMetricsInterceptor()

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/PanicStream"}
	stream := &testServerStream{ctx: context.Background()}

	err := recovery(
		nil, stream, info,
		func(srv any, ss grpc.ServerStream) error {
			return metricsIC(srv, ss, info,
				func(srv any, ss grpc.ServerStream) error {
					panic("stream test panic")
				},
			)
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal after stream panic recovery", status.Code(err))
	}

	counter, cerr := metrics.GrpcRequestsTotal.GetMetricWithLabelValues(
		"/test.Service/PanicStream", "Internal", "5xx", "server_error",
	)
	if cerr != nil {
		t.Fatalf("get metric: %v", cerr)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("grpc_requests_total[5xx/server_error] stream = %v, want 1 for panic", got)
	}
}

func TestUnaryMetricsInterceptor_OKStatusWithNonNilError(t *testing.T) {
	fixtures.InitMetrics("grpc-ok-err", "test")
	ic := grpcmw.UnaryMetricsInterceptor()

	// grpcOKError has GRPCStatus() code=OK but is a non-nil error,
	// exercising the `code == OK && err != nil → code = Unknown` path in observeGRPC.
	_, err := ic(
		context.Background(),
		&emptypb.Empty{},
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/OKErr"},
		func(ctx context.Context, in any) (any, error) {
			return nil, grpcOKError{}
		},
	)
	if err == nil {
		t.Fatal("expected non-nil error from grpcOKError handler")
	}
}
