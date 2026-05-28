package middleware

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// UnaryRequestIDInterceptor reads x-request-id from gRPC metadata or generates a UUID.
func UnaryRequestIDInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = ContextWithRequestID(ctx, resolveRequestID(ctx))
		return handler(ctx, req)
	}
}

// StreamRequestIDInterceptor reads x-request-id from gRPC metadata or generates a UUID.
func StreamRequestIDInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ContextWithRequestID(ss.Context(), resolveRequestID(ss.Context()))
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

func resolveRequestID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MetaRequestID); len(vals) > 0 {
			if id, valid := domain.NormalizeRequestID(vals[0]); valid {
				return id
			}
		}
	}
	return uuid.New().String()
}
