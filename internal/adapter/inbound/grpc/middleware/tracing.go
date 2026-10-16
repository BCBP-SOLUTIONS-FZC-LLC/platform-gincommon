package middleware

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// rpcMethodOther is the semconv rpc.method value for methods the server does not recognise.
const rpcMethodOther = "_OTHER"

// legacyGRPCStatusCodeKey is the pre-1.40 semconv integer attribute (still emitted by
// otelgrpc and keyed on by many Tempo/Jaeger dashboards) kept alongside
// rpc.response.status_code.
const legacyGRPCStatusCodeKey = attribute.Key("rpc.grpc.status_code")

// UnaryTracingInterceptor extracts W3C traceparent from gRPC metadata and starts an OTel server span.
func UnaryTracingInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	scope := tracingScope(serviceName)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		ctx, span := startServerSpan(extractTraceContext(ctx), scope, info.FullMethod)

		var panicValue any
		// Outer defer: finalise span attributes after handler and inner defer complete.
		defer func() { finishServerSpan(span, panicValue, err) }()
		// Inner defer: capture panic value then re-panic so UnaryPanicRecoveryInterceptor
		// (outer in the chain) can still catch and convert it to codes.Internal.
		defer func() {
			if r := recover(); r != nil {
				panicValue = r
				panic(r)
			}
		}()

		resp, err = handler(ctx, req)
		return
	}
}

// StreamTracingInterceptor extracts W3C traceparent from gRPC metadata and starts an OTel server span.
func StreamTracingInterceptor(serviceName string) grpc.StreamServerInterceptor {
	scope := tracingScope(serviceName)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx, span := startServerSpan(extractTraceContext(ss.Context()), scope, info.FullMethod)

		var panicValue any
		defer func() { finishServerSpan(span, panicValue, err) }()
		defer func() {
			if r := recover(); r != nil {
				panicValue = r
				panic(r)
			}
		}()

		err = handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
		return
	}
}

func tracingScope(serviceName string) string {
	if serviceName == "" {
		return "platform-gincommon/grpc"
	}
	return serviceName
}

// startServerSpan starts a server span named after the bounded method label, so an
// attacker-chosen method (grpc.UnknownServiceHandler) cannot explode span-name
// cardinality. Unrecognised methods follow semconv: rpc.method="_OTHER" plus a
// sanitized, length-capped rpc.method_original.
func startServerSpan(ctx context.Context, scope, fullMethod string) (context.Context, trace.Span) {
	name := label.GRPCMethod(fullMethod)
	ctx, span := otel.Tracer(scope).Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer))
	if name == label.Unknown {
		span.SetAttributes(
			semconv.RPCSystemNameGRPC,
			semconv.RPCMethodKey.String(rpcMethodOther),
			semconv.RPCMethodOriginalKey.String(service.SanitizeString(fullMethod)),
		)
		return ctx, span
	}
	svc, method := splitFullMethod(name)
	span.SetAttributes(
		semconv.RPCSystemNameGRPC,
		attribute.String("rpc.service", svc),
		semconv.RPCMethodKey.String(method),
	)
	return ctx, span
}

// finishServerSpan records the wire status code (rpc.response.status_code and the
// legacy rpc.grpc.status_code) and error status, then ends the span.
func finishServerSpan(span trace.Span, panicValue any, err error) {
	defer span.End()
	if panicValue != nil {
		span.SetAttributes(
			semconv.RPCResponseStatusCodeKey.String(statusCodeName(codes.Internal)),
			legacyGRPCStatusCodeKey.Int(int(codes.Internal)),
		)
		// Record the actual panic value so Tempo/Jaeger show the real message.
		span.RecordError(errors.New(service.SanitizeString(fmt.Sprint(panicValue))))
		span.SetStatus(otelcodes.Error, "panic")
		return
	}
	st := grpcStatus(err)
	span.SetAttributes(
		semconv.RPCResponseStatusCodeKey.String(statusCodeName(st.Code())),
		legacyGRPCStatusCodeKey.Int(int(st.Code())),
	)
	if err != nil {
		span.RecordError(err)
		// RecordError alone does not change span status per OTel spec; set it
		// explicitly so Tempo/Jaeger mark non-OK gRPC responses as errors.
		span.SetStatus(otelcodes.Error, st.Message())
	}
}

// splitFullMethod splits "/pkg.Service/Method" into ("pkg.Service", "Method").
// For bare method names with no slash it returns ("", method). An empty or
// slash-only input returns ("", "unknown") so rpc.method is never an empty label.
func splitFullMethod(full string) (service, method string) {
	s := strings.TrimLeft(full, "/") // strip all leading slashes (handles "//" proxy edge case)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[:i], s[i+1:]
	}
	if s == "" {
		return "", "unknown"
	}
	return "", s // no slash — entire string is the method name
}

// metadataCarrier adapts gRPC metadata to OTel TextMapCarrier.
type metadataCarrier metadata.MD

func (mc metadataCarrier) Get(key string) string {
	vals := metadata.MD(mc).Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (mc metadataCarrier) Set(key, val string) {
	metadata.MD(mc).Set(key, val)
}

func (mc metadataCarrier) Keys() []string {
	out := make([]string, 0, len(mc))
	for k := range mc {
		out = append(out, k)
	}
	return out
}

func extractTraceContext(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		return otel.GetTextMapPropagator().Extract(ctx, metadataCarrier(md))
	}
	return ctx
}
