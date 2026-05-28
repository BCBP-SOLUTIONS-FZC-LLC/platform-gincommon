package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// UnaryContextInterceptor builds a domain.RequestContext from gRPC metadata and stores it
// in the context via domain.ContextWithRequestContext. Requires UnaryRequireAuthInterceptor first.
func UnaryContextInterceptor(ctxSvc port.ContextBuilder) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := injectRequestContext(ctx, ctxSvc)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamContextInterceptor builds a domain.RequestContext and stores it in the stream context.
func StreamContextInterceptor(ctxSvc port.ContextBuilder) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := injectRequestContext(ss.Context(), ctxSvc)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

func injectRequestContext(ctx context.Context, ctxSvc port.ContextBuilder) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// Missing metadata is a client-side misconfiguration, not a server fault.
		return ctx, status.Error(codes.Unauthenticated, "missing metadata")
	}

	userID := getFirstMeta(md, MetaUserID)
	tenantID := getFirstMeta(md, MetaTenantID)
	// Join all repeated metadata entries before parsing — gRPC gateways may send
	// roles as multiple entries ("x-tenant-roles: admin", "x-tenant-roles: editor")
	// rather than a single comma-separated value. strings.Join produces the same
	// comma-separated string that domain.ParseRoles expects.
	roles := domain.ParseRoles(strings.Join(md.Get(MetaTenantRoles), ","))
	// Extract trace ID from the active OTel span — UnaryTracingInterceptor (if present)
	// has already called tracer.Start, so SpanFromContext returns the active span.
	traceID := ""
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		traceID = sc.TraceID().String()
	}
	clientIP := clientIPFromContext(ctx)

	rc, err := ctxSvc.BuildContext(tenantID, userID, roles, traceID, clientIP)
	if err != nil {
		// Map known sentinel errors to fixed messages — never forward internal
		// error details (e.g. DB connection strings) to the gRPC client.
		switch {
		case errors.Is(err, domain.ErrMissingTenant):
			return ctx, status.Error(codes.Unauthenticated, "missing tenant id")
		case errors.Is(err, domain.ErrMissingUser):
			return ctx, status.Error(codes.Unauthenticated, "missing user id")
		default:
			return ctx, status.Error(codes.InvalidArgument, "invalid request context")
		}
	}

	return domain.ContextWithRequestContext(ctx, rc), nil
}

func clientIPFromContext(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}
