package middleware

import (
	"context"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// isHealthMethod reports whether fullMethod is exactly one of the standard
// grpc.health.v1.Health RPCs (Check, Watch, List). Health probes (kubelet, load
// balancers) carry no identity metadata, so the auth, context and authz
// interceptors let them through — the gRPC counterpart of registering the HTTP
// health handler before the protected middleware group.
//
// The match is exact, never a prefix: with grpc.UnknownServiceHandler (or any
// handler routing on the raw method string) a prefix match would let a caller
// reach "/grpc.health.v1.Health/x/admin.Svc/Delete" without authentication.
func isHealthMethod(fullMethod string) bool {
	switch fullMethod {
	case healthpb.Health_Check_FullMethodName,
		healthpb.Health_Watch_FullMethodName,
		healthpb.Health_List_FullMethodName:
		return true
	}
	return false
}

// UnaryRequireAuthInterceptor rejects gRPC calls missing x-user-id or x-tenant-id metadata.
// Calls to grpc.health.v1.Health are exempt (see isHealthMethod).
func UnaryRequireAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info != nil && isHealthMethod(info.FullMethod) {
			return handler(ctx, req)
		}
		if err := requireAuthFromMetadata(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamRequireAuthInterceptor rejects streaming gRPC calls missing identity metadata.
// Calls to grpc.health.v1.Health (e.g. Watch) are exempt.
func StreamRequireAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info != nil && isHealthMethod(info.FullMethod) {
			return handler(srv, ss)
		}
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
	if len(vals) > 1 {
		// Repeated identity metadata is ambiguous: a client could append its own value
		// next to the gateway's. Trusting vals[0] would make the outcome depend on
		// metadata order, so reject instead.
		return ambiguousMetaError(name)
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

func ambiguousMetaError(name string) error {
	return status.Error(codes.Unauthenticated, "ambiguous "+name+" metadata")
}

// checkSingleIdentityMeta rejects repeated x-user-id / x-tenant-id entries.
func checkSingleIdentityMeta(md metadata.MD) error {
	for _, key := range []string{MetaUserID, MetaTenantID} {
		if len(md.Get(key)) > 1 {
			return ambiguousMetaError(key)
		}
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
