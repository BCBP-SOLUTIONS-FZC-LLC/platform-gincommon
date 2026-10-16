package middleware

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// identityHolder is a mutable slot the logging interceptor places in the context
// before calling inner interceptors. The context interceptor (which runs later,
// deeper in the chain) fills it with the verified RequestContext, so logging can
// read identity after the handler returns even though it wraps authentication.
// That ordering is what lets rejected (Unauthenticated) calls be logged at all.
type identityHolder struct {
	rc atomic.Pointer[domain.RequestContext]
}

type identityHolderKey struct{}

func withIdentityHolder(ctx context.Context) (context.Context, *identityHolder) {
	h := &identityHolder{}
	return context.WithValue(ctx, identityHolderKey{}, h), h
}

// recordIdentity stores rc in the holder placed by an outer logging interceptor, if any.
func recordIdentity(ctx context.Context, rc *domain.RequestContext) {
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok {
		h.rc.Store(rc)
	}
}

// UnaryLoggingInterceptor logs gRPC unary RPCs with method, status, duration, and identity.
//
// Place it outside (before) the auth and context interceptors: it logs every call,
// including those rejected by RequireAuth, and still reports user_id/tenant_id for
// authenticated calls via the identity holder filled by UnaryContextInterceptor.
// Uses a deferred log so panicking handlers are still logged (as codes.Internal).
// Nil logger is a no-op.
func UnaryLoggingInterceptor(logger port.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if logger == nil {
			return handler(ctx, req)
		}
		ctx, holder := withIdentityHolder(ctx)
		start := time.Now()
		panicked := true
		defer func() {
			logErr := err
			if panicked {
				logErr = status.Error(codes.Internal, "panic")
			}
			logGRPCRequest(ctx, holder, logger, info.FullMethod, time.Since(start), logErr)
		}()
		resp, err = handler(ctx, req)
		panicked = false
		return
	}
}

// StreamLoggingInterceptor logs gRPC streaming RPCs. Same placement and identity
// semantics as UnaryLoggingInterceptor.
func StreamLoggingInterceptor(logger port.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		if logger == nil {
			return handler(srv, ss)
		}
		ctx, holder := withIdentityHolder(ss.Context())
		start := time.Now()
		panicked := true
		defer func() {
			logErr := err
			if panicked {
				logErr = status.Error(codes.Internal, "panic")
			}
			logGRPCRequest(ctx, holder, logger, info.FullMethod, time.Since(start), logErr)
		}()
		err = handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
		panicked = false
		return
	}
}

func logGRPCRequest(ctx context.Context, holder *identityHolder, logger port.Logger, method string, elapsed time.Duration, err error) {
	code := grpcCode(err)

	raw := map[string]any{
		"method":      label.GRPCMethod(method),
		"grpc_code":   code.String(),
		"duration_ms": float64(elapsed.Nanoseconds()) / 1e6,
		"request_id":  RequestIDFromContext(ctx),
	}

	// Verified identity only: the holder is filled by the context interceptor after
	// auth succeeded; a RequestContext already in ctx covers chains where logging
	// runs inside the context interceptor. Rejected calls carry no identity fields.
	rc := holder.rc.Load()
	if rc == nil {
		rc, _ = domain.RequestContextFromGoContext(ctx)
	}
	if rc != nil {
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
