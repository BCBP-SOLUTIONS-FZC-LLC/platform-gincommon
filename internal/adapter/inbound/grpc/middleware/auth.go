package middleware

import (
	"context"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryRequireAuthInterceptor rejects gRPC calls missing x-user-id or x-tenant-id metadata.
func UnaryRequireAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := requireAuthFromMetadata(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamRequireAuthInterceptor rejects streaming gRPC calls missing identity metadata.
func StreamRequireAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := requireAuthFromMetadata(ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func requireAuthFromMetadata(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	if err := checkRequiredMeta(md, MetaUserID, "x-user-id"); err != nil {
		return err
	}
	return checkRequiredMeta(md, MetaTenantID, "x-tenant-id")
}

// checkRequiredMeta distinguishes absent metadata ("missing") from blank values
// ("blank") so callers get an actionable error message when debugging auth failures.
// Enforces the same MaxHeaderValueLen limit as HTTP RequireAuth to maintain parity.
func checkRequiredMeta(md metadata.MD, key, name string) error {
	vals := md.Get(key)
	if len(vals) == 0 {
		return status.Error(codes.Unauthenticated, "missing "+name)
	}
	v := strings.TrimSpace(vals[0])
	if v == "" {
		return status.Error(codes.Unauthenticated, "blank "+name)
	}
	// NormalizeHeaderValue enforces MaxHeaderValueLen and rejects ASCII controls,
	// DEL, and Unicode line separators — matching the HTTP RequireAuth validation.
	if _, ok := domain.NormalizeHeaderValue(v); !ok {
		return status.Error(codes.Unauthenticated, "invalid "+name)
	}
	return nil
}

func getFirstMeta(md metadata.MD, key string) string {
	vals := md.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}
