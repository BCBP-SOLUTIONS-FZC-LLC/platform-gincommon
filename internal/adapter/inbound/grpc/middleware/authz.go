package middleware

import (
	"context"
	"errors"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryAuthZInterceptor authorizes unary gRPC calls using the RequestContext in context.
// Requires UnaryContextInterceptor to have run first.
func UnaryAuthZInterceptor(auth port.Authorizer, action, resource string, logger port.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorizeGRPC(ctx, auth, action, resource, logger); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamAuthZInterceptor authorizes streaming gRPC calls.
func StreamAuthZInterceptor(auth port.Authorizer, action, resource string, logger port.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorizeGRPC(ss.Context(), auth, action, resource, logger); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func authorizeGRPC(ctx context.Context, auth port.Authorizer, action, resource string, logger port.Logger) error {
	rc, ok := domain.RequestContextFromGoContext(ctx)
	if !ok {
		// ProtectedUnaryInterceptors / UnaryContextInterceptor must run before RequirePermission.
		// Returning FailedPrecondition (not Internal) signals a misconfigured interceptor chain,
		// not a server fault — the request is rejected but the error is diagnosable.
		return status.Error(codes.FailedPrecondition, "request context missing — ensure ProtectedUnaryInterceptors ran before RequirePermission")
	}

	if auth == nil {
		return status.Error(codes.Internal, "internal server error")
	}

	if err := auth.Authorize(rc.UserID, rc.Roles, action, resource); err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			if logger != nil {
				logger.Warn("grpc authz: access denied", map[string]any{
					"user_id":  rc.UserID,
					"roles":    rc.Roles,
					"action":   action,
					"resource": resource,
				})
			}
			return status.Error(codes.PermissionDenied, "forbidden")
		}
		if logger != nil {
			logger.Error("grpc authz: internal error", map[string]any{
				"user_id":  rc.UserID,
				"action":   action,
				"resource": resource,
				"error":    service.SanitizeString(err.Error()),
			})
		}
		return status.Error(codes.Internal, "internal server error")
	}
	return nil
}
