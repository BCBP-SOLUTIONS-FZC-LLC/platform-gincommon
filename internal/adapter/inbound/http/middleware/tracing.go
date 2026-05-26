package middleware

import (
	"fmt"
	"net/http"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates an OpenTelemetry server span and stores trace_id in Gin context.
func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tracer := otel.Tracer("http-server")
		method := c.Request.Method

		ctx := otel.GetTextMapPropagator().Extract(
			c.Request.Context(),
			propagation.HeaderCarrier(c.Request.Header),
		)

		ctx, span := tracer.Start(
			ctx,
			method,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		traceID := trace.SpanContextFromContext(ctx).TraceID().String()
		c.Set(CtxTraceIDKey, traceID)

		c.Request = c.Request.WithContext(ctx)

		var (
			finalCode  int
			panicValue any
		)

		// Runs second (after panic recovery): always sets span attributes even on panic.
		defer func() {
			route := label.HTTPRoute(c)
			span.SetName(method + " " + route)
			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(method),
				semconv.HTTPRoute(route),
				semconv.URLPath(c.Request.URL.Path),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
				semconv.ServerAddress(c.Request.Host),
				semconv.ClientAddress(c.ClientIP()),
			)
			code := finalCode
			if code == 0 {
				code = http.StatusInternalServerError
			}
			span.SetAttributes(semconv.HTTPResponseStatusCode(code))
			if panicValue != nil {
				span.SetStatus(codes.Error, "panic")
			} else if code >= 500 {
				span.SetStatus(codes.Error, http.StatusText(code))
			}
		}()

		// Runs first on panic: captures the value so the attribute defer can detect it.
		defer func() {
			if r := recover(); r != nil {
				panicValue = r
				span.RecordError(fmt.Errorf("%v", r))
				panic(r)
			}
		}()

		c.Next()

		finalCode = c.Writer.Status()
	}
}
