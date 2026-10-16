package middleware

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
)

// MsgInternal is the fixed message of every Internal status the library produces.
const MsgInternal = "internal server error"

// sanitizeError maps a handler error to the status the client may see, so no error
// text, SQL or internal detail reaches it (the gRPC counterpart of the HTTP
// error handler):
//
//   - a status error is kept; a status wrapped by other errors keeps only that
//     status (code, message, details), never the wrappers' text;
//   - context.DeadlineExceeded / context.Canceled (or an error wrapping one) become
//     DeadlineExceeded / Canceled with a fixed message;
//   - anything else, and a non-nil error claiming OK, becomes Internal
//     "internal server error" and is reported as unclassified.
//
// grpc-go would otherwise send a plain error as Unknown with err.Error() as the message.
func sanitizeError(err error) (out error, unclassified bool) {
	if err == nil {
		return nil, false
	}
	var gs interface{ GRPCStatus() *status.Status }
	if errors.As(err, &gs) {
		if st := gs.GRPCStatus(); st != nil && st.Code() != codes.OK {
			return st.Err(), false
		}
		return status.Error(codes.Internal, MsgInternal), true
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded"), false
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "canceled"), false
	}
	return status.Error(codes.Internal, MsgInternal), true
}

// UnaryErrorSanitizerInterceptor applies sanitizeError to the handler's error and
// logs an unclassified one as an error-level "unclassified error" entry (types,
// never text). It is the innermost observability interceptor, so metrics, the
// access log and the span record the status the client receives.
func UnaryErrorSanitizerInterceptor(logger port.Logger, serviceName string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		out, unclassified := sanitizeError(err)
		if unclassified {
			metrics.RecordUnclassifiedError(metrics.TransportGRPC)
			logUnclassifiedGRPC(ctx, logger, serviceName, info.FullMethod, err)
		}
		return resp, out
	}
}

// StreamErrorSanitizerInterceptor is the streaming counterpart of
// UnaryErrorSanitizerInterceptor.
func StreamErrorSanitizerInterceptor(logger port.Logger, serviceName string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		out, unclassified := sanitizeError(err)
		if unclassified {
			metrics.RecordUnclassifiedError(metrics.TransportGRPC)
			logUnclassifiedGRPC(ss.Context(), logger, serviceName, info.FullMethod, err)
		}
		return out
	}
}

// logUnclassifiedGRPC emits the same "unclassified error" entry as the HTTP error
// handler: where it happened and the error-chain types, never err.Error().
func logUnclassifiedGRPC(ctx context.Context, logger port.Logger, serviceName, fullMethod string, cause error) {
	if logger == nil {
		return
	}
	defer func() { _ = recover() }() // a failing logger must not change the RPC result
	traceID := ""
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		traceID = sc.TraceID().String()
	}
	fields := map[string]any{
		"classified":  false,
		"trace_id":    traceID,
		"request_id":  RequestIDFromContext(ctx),
		"grpc_method": label.GRPCMethod(fullMethod),
		"grpc_code":   statusCodeName(codes.Internal),
		"error_type":  "<nil>",
		"error_chain": []string{},
	}
	if serviceName != "" {
		fields["service"] = serviceName
	}
	if chain := problem.ErrorChainTypes(cause, problem.MaxErrorChain); len(chain) > 0 {
		fields["error_type"] = chain[0]
		fields["error_chain"] = chain
	}
	logger.Error("unclassified error", fields)
}
