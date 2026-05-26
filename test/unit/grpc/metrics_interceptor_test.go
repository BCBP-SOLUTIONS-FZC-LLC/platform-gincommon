package grpc_test

import (
	"context"
	"testing"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestUnaryMetricsInterceptor_RecordsSuccess(t *testing.T) {
	metrics.Init("grpc-interceptor-test", "test")
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
	metrics.Init("grpc-rl", "test")
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
	metrics.Init("grpc-interceptor-err", "test")
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
	metrics.Init("grpc-unknown-method", "test")
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
