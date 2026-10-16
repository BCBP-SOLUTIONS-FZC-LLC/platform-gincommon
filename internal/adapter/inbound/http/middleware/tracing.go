package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

// maxSpanHostLen caps server.address: the Host header is client-controlled.
const maxSpanHostLen = 256

// spanAttrAborted is the span attribute and event set on a request aborted with
// http.ErrAbortHandler; such spans are not marked as errors.
const spanAttrAborted = "http.aborted"

// maxSpanMethodLen caps http.request.method_original for non-standard methods.
const maxSpanMethodLen = 32

// TracingMiddleware creates an OpenTelemetry server span and stores trace_id in Gin context.
// serviceName is used as the OTel tracer scope; defaults to "platform-gincommon/http" when empty.
// A panic with http.ErrAbortHandler does not set the span status to Error: the span gets
// an "http.aborted" attribute and event and the written status, else 499.
func TracingMiddleware(serviceName string) gin.HandlerFunc {
	scope := serviceName
	if scope == "" {
		scope = "platform-gincommon/http"
	}
	return func(c *gin.Context) {
		tracer := otel.Tracer(scope)
		// Bounded method: arbitrary client method tokens must not explode span-name
		// cardinality in the trace backend. Per OTel HTTP semconv a non-standard
		// method is reported as http.request.method="_OTHER" (the truncated original
		// goes in http.request.method_original) and the span is named "HTTP".
		rawMethod := c.Request.Method
		method, spanMethod := rawMethod, rawMethod
		if metrics.HTTPMethodLabel(rawMethod) != rawMethod {
			method, spanMethod = "_OTHER", "HTTP"
		}

		ctx := otel.GetTextMapPropagator().Extract(
			c.Request.Context(),
			propagation.HeaderCarrier(c.Request.Header),
		)

		ctx, span := tracer.Start(
			ctx,
			spanMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		traceID := trace.SpanContextFromContext(ctx).TraceID().String()
		c.Set(CtxTraceIDKey, traceID)

		c.Request = c.Request.WithContext(ctx)

		var (
			finalCode  int
			panicValue any
			aborted    bool // panicked with http.ErrAbortHandler
		)

		// annotate sets the span name, attributes and status. It runs from the second
		// defer below (after the panic-capturing one), so even on panic, except for an
		// abort, where the capturing defer runs it itself.
		annotated := false
		annotate := func() {
			if annotated {
				return
			}
			annotated = true
			route := label.HTTPRoute(c)
			span.SetName(spanMethod + " " + route)
			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(method),
				semconv.HTTPRoute(route),
				semconv.URLPath(truncateSpanAttr(c.Request.URL.Path, 512)),
				semconv.UserAgentOriginal(truncateSpanAttr(c.Request.UserAgent(), 512)),
				semconv.ServerAddress(truncateSpanAttr(c.Request.Host, maxSpanHostLen)),
				semconv.ClientAddress(c.ClientIP()),
			)
			if method != rawMethod {
				span.SetAttributes(semconv.HTTPRequestMethodOriginal(truncateSpanAttr(rawMethod, maxSpanMethodLen)))
			}
			code := finalCode
			// finalCode is 0 when a panic interrupted c.Next() before the status was
			// written (PanicRecoveryMiddleware writes the 500 later, after this defer).
			// This guard is load-bearing: without it every panicking request produces
			// a span with http.response_status_code=0, which violates OTel semconv and
			// breaks trace-based 5xx alerting. Do not remove without updating the panic path.
			if aborted {
				code = abortedStatus(c)
			} else if code == 0 {
				code = http.StatusInternalServerError
			}
			span.SetAttributes(semconv.HTTPResponseStatusCode(code))
			if aborted {
				// A deliberate abort (typically a client disconnect) is not a server
				// error: annotate it instead of setting the span status to Error.
				span.SetAttributes(attribute.Bool(spanAttrAborted, true))
				span.AddEvent(spanAttrAborted)
			} else if panicValue != nil {
				span.SetStatus(codes.Error, "panic")
			} else if code >= 500 {
				span.SetStatus(codes.Error, http.StatusText(code))
			}
		}
		defer annotate()

		// Runs first on panic: captures the value so the attribute defer can detect it.
		defer func() {
			if r := recover(); r != nil {
				panicValue = r
				if isAbortPanic(r) {
					// End the span while no panic is in flight: the SDK's End would
					// otherwise record the abort as an "exception" event. The deferred
					// annotate / span.End calls are then no-ops.
					aborted = true
					annotate()
					span.End()
					panic(r)
				}
				// Sanitized: the panic value can embed request data (control characters,
				// unbounded length).
				span.RecordError(errors.New(service.SanitizeString(fmt.Sprint(r))))
				panic(r)
			}
		}()

		c.Next()

		finalCode = c.Writer.Status()
	}
}
