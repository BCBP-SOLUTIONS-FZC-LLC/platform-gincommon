package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryMetricsInterceptor records Prometheus metrics for unary RPCs.
// Uses a panicked-sentinel defer so that panicking handlers are still counted in
// grpc_requests_total (as codes.Internal), matching HTTP MetricsMiddleware behaviour.
func UnaryMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		if metrics.GrpcRequestsTotal == nil || metrics.GrpcInflight == nil {
			return handler(ctx, req)
		}
		method := label.GRPCMethod(info.FullMethod)
		metrics.GrpcInflight.WithLabelValues(method).Inc()
		start := time.Now()
		panicked := true

		defer func() {
			metrics.GrpcInflight.WithLabelValues(method).Dec()
			observeErr := err
			if panicked {
				metrics.RecordGRPCPanic(method)
				observeErr = status.Error(codes.Internal, "panic")
			}
			observeGRPC(method, time.Since(start), observeErr,
				metrics.ProtoMessageSize(req), metrics.ProtoMessageSize(resp))
		}()

		resp, err = handler(ctx, req)
		panicked = false
		return
	}
}

// StreamMetricsInterceptor records Prometheus metrics for streaming RPCs.
// Uses a panicked-sentinel defer so that panicking handlers are still counted in metrics.
func StreamMetricsInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		if metrics.GrpcRequestsTotal == nil || metrics.GrpcInflight == nil {
			return handler(srv, ss)
		}
		method := label.GRPCMethod(info.FullMethod)
		metrics.GrpcInflight.WithLabelValues(method).Inc()
		wrapped := &monitoredServerStream{ServerStream: ss}
		start := time.Now()
		panicked := true

		defer func() {
			metrics.GrpcInflight.WithLabelValues(method).Dec()
			observeErr := err
			if panicked {
				metrics.RecordGRPCPanic(method)
				observeErr = status.Error(codes.Internal, "panic")
			}
			rb, sb := wrapped.bytes()
			observeGRPC(method, time.Since(start), observeErr, rb, sb)
		}()

		err = handler(srv, wrapped)
		panicked = false
		return
	}
}

func observeGRPC(method string, elapsed time.Duration, err error, reqSize, respSize float64) {
	st, _ := status.FromError(err)
	code := st.Code()
	if code == codes.OK && err != nil {
		code = codes.Unknown
	}

	if code == codes.ResourceExhausted {
		metrics.RecordGRPCRateLimited(method)
	}

	errorClass := metrics.GRPCRequestOutcome(code)
	metrics.RecordGRPC(
		method,
		elapsed.Seconds(),
		code.String(),
		metrics.GRPCStatusClass(code),
		errorClass,
		reqSize,
		respSize,
	)
}

// monitoredServerStream wraps a gRPC ServerStream to track message byte counts.
// mu protects recvBytes and sentBytes because bidirectional streaming handlers
// may call RecvMsg and SendMsg concurrently from separate goroutines.
type monitoredServerStream struct {
	grpc.ServerStream
	mu        sync.Mutex
	recvBytes float64
	sentBytes float64
}

func (s *monitoredServerStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	s.mu.Lock()
	s.recvBytes += metrics.ProtoMessageSize(m)
	s.mu.Unlock()
	return nil
}

func (s *monitoredServerStream) SendMsg(m any) error {
	if err := s.ServerStream.SendMsg(m); err != nil {
		return err
	}
	s.mu.Lock()
	s.sentBytes += metrics.ProtoMessageSize(m)
	s.mu.Unlock()
	return nil
}

func (s *monitoredServerStream) bytes() (recv, sent float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recvBytes, s.sentBytes
}
