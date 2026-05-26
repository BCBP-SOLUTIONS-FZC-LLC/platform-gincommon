package middleware

import (
	"context"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryMetricsInterceptor records Prometheus metrics for unary RPCs.
func UnaryMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if metrics.GrpcInflight == nil {
			return handler(ctx, req)
		}
		method := label.GRPCMethod(info.FullMethod)
		metrics.GrpcInflight.WithLabelValues(method).Inc()
		defer metrics.GrpcInflight.WithLabelValues(method).Dec()

		start := time.Now()
		resp, err := handler(ctx, req)
		observeGRPC(method, time.Since(start), err, metrics.ProtoMessageSize(req), metrics.ProtoMessageSize(resp))
		return resp, err
	}
}

// StreamMetricsInterceptor records Prometheus metrics for streaming RPCs.
func StreamMetricsInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if metrics.GrpcInflight == nil {
			return handler(srv, ss)
		}
		method := label.GRPCMethod(info.FullMethod)
		metrics.GrpcInflight.WithLabelValues(method).Inc()
		defer metrics.GrpcInflight.WithLabelValues(method).Dec()

		wrapped := &monitoredServerStream{ServerStream: ss}
		start := time.Now()
		err := handler(srv, wrapped)
		observeGRPC(method, time.Since(start), err, wrapped.recvBytes, wrapped.sentBytes)
		return err
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

	errorClass, _ := metrics.GRPCRequestOutcome(code)
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

type monitoredServerStream struct {
	grpc.ServerStream
	recvBytes float64
	sentBytes float64
}

func (s *monitoredServerStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	s.recvBytes += metrics.ProtoMessageSize(m)
	return nil
}

func (s *monitoredServerStream) SendMsg(m any) error {
	if err := s.ServerStream.SendMsg(m); err != nil {
		return err
	}
	s.sentBytes += metrics.ProtoMessageSize(m)
	return nil
}
