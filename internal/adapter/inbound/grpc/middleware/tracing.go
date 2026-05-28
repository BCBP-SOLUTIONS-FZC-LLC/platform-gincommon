package middleware

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryTracingInterceptor extracts W3C traceparent from gRPC metadata and starts an OTel server span.
func UnaryTracingInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	scope := serviceName
	if scope == "" {
		scope = "platform-gincommon/grpc"
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		ctx = extractTraceContext(ctx)
		tracer := otel.Tracer(scope)
		svc, method := splitFullMethod(info.FullMethod)
		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		span.SetAttributes(
			semconv.RPCSystemNameGRPC,
			attribute.String("rpc.service", svc),
			semconv.RPCMethodKey.String(method),
		)

		var panicValue any
		// Outer defer: finalise span attributes after handler and inner defer complete.
		defer func() {
			if panicValue != nil {
				// Record the actual panic value so Tempo/Jaeger show the real message.
				span.RecordError(fmt.Errorf("%v", panicValue))
				span.SetStatus(otelcodes.Error, "panic")
			} else if err != nil {
				span.RecordError(err)
				// RecordError alone does not change span status per OTel spec; set it
				// explicitly so Tempo/Jaeger mark non-OK gRPC responses as errors.
				span.SetStatus(otelcodes.Error, status.Convert(err).Message())
			}
			span.End()
		}()
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
	scope := serviceName
	if scope == "" {
		scope = "platform-gincommon/grpc"
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx := extractTraceContext(ss.Context())
		tracer := otel.Tracer(scope)
		svc, method := splitFullMethod(info.FullMethod)
		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		span.SetAttributes(
			semconv.RPCSystemNameGRPC,
			attribute.String("rpc.service", svc),
			semconv.RPCMethodKey.String(method),
		)

		var panicValue any
		defer func() {
			if panicValue != nil {
				span.RecordError(fmt.Errorf("%v", panicValue))
				span.SetStatus(otelcodes.Error, "panic")
			} else if err != nil {
				span.RecordError(err)
				span.SetStatus(otelcodes.Error, status.Convert(err).Message())
			}
			span.End()
		}()
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

// metadataCarrier adapts gRPC incoming metadata to OTel TextMapCarrier.
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
