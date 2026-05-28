package middleware

import (
	"context"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryLoggingInterceptor logs gRPC unary RPCs with method, status, duration, and identity.
// Uses a deferred log so panicking handlers are still logged (as codes.Internal) rather than
// silently dropping the grpc_request log entry. Nil logger is a no-op.
func UnaryLoggingInterceptor(logger port.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if logger == nil {
			return handler(ctx, req)
		}
		start := time.Now()
		panicked := true
		defer func() {
			logErr := err
			if panicked {
				logErr = status.Error(codes.Internal, "panic")
			}
			logGRPCRequest(ctx, logger, info.FullMethod, time.Since(start), logErr)
		}()
		resp, err = handler(ctx, req)
		panicked = false
		return
	}
}

// StreamLoggingInterceptor logs gRPC streaming RPCs.
// Uses a deferred log so panicking handlers are still logged (as codes.Internal).
func StreamLoggingInterceptor(logger port.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		if logger == nil {
			return handler(srv, ss)
		}
		start := time.Now()
		panicked := true
		defer func() {
			logErr := err
			if panicked {
				logErr = status.Error(codes.Internal, "panic")
			}
			logGRPCRequest(ss.Context(), logger, info.FullMethod, time.Since(start), logErr)
		}()
		err = handler(srv, ss)
		panicked = false
		return
	}
}

func logGRPCRequest(ctx context.Context, logger port.Logger, method string, elapsed time.Duration, err error) {
	st, _ := status.FromError(err)
	code := st.Code()

	raw := map[string]any{
		"method":      label.GRPCMethod(method),
		"grpc_code":   code.String(),
		"duration_ms": float64(elapsed.Nanoseconds()) / 1e6,
		"request_id":  RequestIDFromContext(ctx),
	}

	if rc, ok := domain.RequestContextFromGoContext(ctx); ok {
		raw["user_id"] = rc.UserID
		raw["tenant_id"] = rc.TenantID
	}

	fields := service.Sanitize(raw)
	switch {
	case code == codes.OK:
		logger.Info("grpc_request", fields)
	case isClientError(code):
		logger.Warn("grpc_request", fields)
	default:
		logger.Error("grpc_request", fields)
	}
}

func isClientError(c codes.Code) bool {
	switch c {
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange,
		codes.Canceled, codes.DeadlineExceeded:
		return true
	}
	return false
}
