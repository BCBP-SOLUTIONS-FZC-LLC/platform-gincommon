package middleware

import (
	"context"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// UnaryClientPropagationInterceptor injects the W3C trace context of ctx (via the
// global OTel TextMapPropagator) and the inbound request ID into outgoing metadata,
// so downstream services join the same trace and correlation ID. It never forwards
// identity — see UnaryClientIdentityInterceptor.
func UnaryClientPropagationInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(withPropagation(ctx), method, req, reply, cc, opts...)
	}
}

// StreamClientPropagationInterceptor is the streaming counterpart of
// UnaryClientPropagationInterceptor.
func StreamClientPropagationInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(withPropagation(ctx), desc, cc, method, opts...)
	}
}

// UnaryClientIdentityInterceptor copies x-user-id, x-tenant-id and x-tenant-roles
// from the RequestContext in ctx into outgoing metadata, replacing any values
// already set. Install it only on connections to trusted internal services that
// accept gateway-style identity metadata. Calls without a RequestContext pass
// through unchanged.
func UnaryClientIdentityInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(withIdentity(ctx), method, req, reply, cc, opts...)
	}
}

// StreamClientIdentityInterceptor is the streaming counterpart of
// UnaryClientIdentityInterceptor.
func StreamClientIdentityInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(withIdentity(ctx), desc, cc, method, opts...)
	}
}

// withPropagation returns ctx with trace context and request ID in outgoing metadata.
// An x-request-id the caller already set explicitly is kept.
func withPropagation(ctx context.Context) context.Context {
	md := outgoingMD(ctx)
	otel.GetTextMapPropagator().Inject(ctx, metadataCarrier(md))
	if id := RequestIDFromContext(ctx); id != "" && len(md.Get(MetaRequestID)) == 0 {
		md.Set(MetaRequestID, id)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// withIdentity returns ctx with the RequestContext identity in outgoing metadata.
// Values are Set (not appended) so the downstream never sees repeated identity
// entries, which RequireAuth rejects as ambiguous.
func withIdentity(ctx context.Context) context.Context {
	rc, ok := domain.RequestContextFromGoContext(ctx)
	if !ok {
		return ctx
	}
	md := outgoingMD(ctx)
	md.Set(MetaUserID, rc.UserID)
	md.Set(MetaTenantID, rc.TenantID)
	if len(rc.Roles) > 0 {
		md.Set(MetaTenantRoles, strings.Join(rc.Roles, ","))
	} else {
		delete(md, MetaTenantRoles)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// outgoingMD returns a mutable copy of ctx's outgoing metadata (including pairs
// added with AppendToOutgoingContext), or an empty MD.
func outgoingMD(ctx context.Context) metadata.MD {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		return md // FromOutgoingContext returns a copy
	}
	return metadata.MD{}
}
