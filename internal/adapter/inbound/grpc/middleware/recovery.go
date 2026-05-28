package middleware

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryPanicRecoveryInterceptor recovers panics in unary gRPC handlers, logs them,
// and returns codes.Internal rather than crashing the server.
func UnaryPanicRecoveryInterceptor(logger port.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				if logger != nil {
					logger.Error("grpc panic recovered", service.Sanitize(map[string]any{
						"method": info.FullMethod,
						"panic":  service.SanitizeString(fmt.Sprint(r)),
						"stack":  string(stack),
					}))
				}
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// StreamPanicRecoveryInterceptor recovers panics in streaming gRPC handlers.
func StreamPanicRecoveryInterceptor(logger port.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				if logger != nil {
					logger.Error("grpc panic recovered", service.Sanitize(map[string]any{
						"method": info.FullMethod,
						"panic":  service.SanitizeString(fmt.Sprint(r)),
						"stack":  string(stack),
					}))
				}
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}
