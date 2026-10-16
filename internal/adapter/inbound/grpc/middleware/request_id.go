package middleware

import (
	"context"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// UnaryRequestIDInterceptor reads x-request-id from gRPC metadata or generates a UUID,
// stores it in the context, and echoes it to the client as an x-request-id response
// header (matching the HTTP RequestIDMiddleware).
func UnaryRequestIDInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id := resolveRequestID(ctx)
		// SetHeader fails only when there is no server transport stream in ctx (e.g.
		// direct interceptor calls in tests) or headers were already sent; the
		// request must still be served, so the error is deliberately ignored.
		_ = grpc.SetHeader(ctx, metadata.Pairs(MetaRequestID, id))
		ctx = ContextWithRequestID(ctx, id)
		return handler(ctx, req)
	}
}

// StreamRequestIDInterceptor reads x-request-id from gRPC metadata or generates a UUID,
// and echoes it to the client as an x-request-id response header.
func StreamRequestIDInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		id := resolveRequestID(ss.Context())
		_ = ss.SetHeader(metadata.Pairs(MetaRequestID, id)) // see UnaryRequestIDInterceptor
		ctx := ContextWithRequestID(ss.Context(), id)
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
