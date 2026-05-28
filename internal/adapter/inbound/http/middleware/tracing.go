package middleware

import (
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// truncateSpanAttr truncates s to at most max bytes at a UTF-8 rune boundary.
// Prevents unbounded OTel span attribute values from attacker-controlled inputs.
func truncateSpanAttr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

// TracingMiddleware creates an OpenTelemetry server span and stores trace_id in Gin context.
// serviceName is used as the OTel tracer scope; defaults to "platform-gincommon/http" when empty.
func TracingMiddleware(serviceName string) gin.HandlerFunc {
	scope := serviceName
	if scope == "" {
		scope = "platform-gincommon/http"
	}
	return func(c *gin.Context) {
		tracer := otel.Tracer(scope)
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
				semconv.URLPath(truncateSpanAttr(c.Request.URL.Path, 512)),
				semconv.UserAgentOriginal(truncateSpanAttr(c.Request.UserAgent(), 512)),
				semconv.ServerAddress(c.Request.Host),
				semconv.ClientAddress(c.ClientIP()),
			)
			code := finalCode
			// finalCode is 0 when a panic interrupted c.Next() before the status was
			// written (PanicRecoveryMiddleware writes the 500 later, after this defer).
			// This guard is load-bearing: without it every panicking request produces
			// a span with http.response_status_code=0, which violates OTel semconv and
			// breaks trace-based 5xx alerting. Do not remove without updating the panic path.
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
