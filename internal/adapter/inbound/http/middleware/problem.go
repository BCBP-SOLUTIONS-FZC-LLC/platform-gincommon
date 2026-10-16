package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/trace"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/problem"
)

// ProblemSettings configures how problems are rendered and logged for a request.
type ProblemSettings struct {
	// TypeBase is the type URI base; empty means problem.DefaultTypeBase.
	TypeBase string
	// Logger receives the "problem response" entry; nil disables it.
	Logger port.Logger
	// Service is the service log field.
	Service string
}

// ctxProblemSettingsKey stores the request's ProblemSettings in the Gin context.
const ctxProblemSettingsKey = "gincommon.problem_settings"

// Fixed details of the problems gincommon writes itself.
const (
	DetailAmbiguousUserID   = "Ambiguous x-user-id header."
	DetailAmbiguousTenantID = "Ambiguous x-tenant-id header."
	DetailMissingIdentity   = "Missing or invalid authentication headers."
	DetailInvalidRoles      = "Invalid tenant roles."
	DetailMissingTenant     = "Missing tenant id."
	DetailMissingUser       = "Missing user id."
	DetailInvalidContext    = "Invalid request context."
	DetailForbidden         = "The identity is not allowed to perform this operation."
	DetailRouteNotFound     = "No route matches the request path."
	DetailMethodNotAllowed  = "The route does not support the request method."
)

// Error codes of the problems gincommon writes itself, beyond the types' defaults.
const (
	CodeInvalidRequestContext = "INVALID_REQUEST_CONTEXT"
	CodeRouteNotFound         = "ROUTE_NOT_FOUND"
)

// ProblemMiddleware is the global error handler. It stores s for every problem the
// request's middleware writes and, after the handlers return without writing a
// response, renders:
//
//   - the last error the handlers recorded with c.Error, classified by ClassifyError
//     (a context.Canceled request is recorded as 499 with no body: the client left);
//   - else an error status set without a body (c.Status(404), Gin's 404/405 for an
//     unregistered route or method) as the platform problem for that status.
//
// A response the handler already wrote is never touched. Place it as the innermost
// observability handler so metrics, logs and spans record the problem's status.
func ProblemMiddleware(s ProblemSettings) gin.HandlerFunc {
	registerValidationFieldNames()
	return func(c *gin.Context) {
		c.Set(ctxProblemSettingsKey, s)
		c.Next()
		if c.Writer.Written() {
			return
		}
		if last := c.Errors.Last(); last != nil {
			if errors.Is(last.Err, context.Canceled) {
				c.Status(StatusClientClosedRequest)
				c.Abort()
				return
			}
			WriteProblem(c, ClassifyError(last.Err, last.IsType(gin.ErrorTypeBind)))
			return
		}
		if status := c.Writer.Status(); status >= http.StatusBadRequest {
			WriteProblem(c, statusProblem(c, status))
		}
	}
}

// statusProblem is the problem for an error status written without a body.
func statusProblem(c *gin.Context, status int) *problem.Error {
	t := problem.TypeForStatus(status)
	if c.FullPath() == "" {
		switch status {
		case http.StatusNotFound:
			return &problem.Error{Type: t, Code: CodeRouteNotFound, Detail: DetailRouteNotFound}
		case http.StatusMethodNotAllowed:
			return &problem.Error{Type: t, Detail: DetailMethodNotAllowed}
		}
	}
	return &problem.Error{Type: t}
}

// ClassifyError maps a handler error to a problem error: problem.FromError, after the
// Gin-specific cases: validator.ValidationErrors (from ShouldBind*) and, when bind is
// true (gin.ErrorTypeBind, or BindingError), any binding error are 400
// validation-failed, and an Authorizer's domain.ErrForbidden is 403 forbidden.
func ClassifyError(err error, bind bool) *problem.Error {
	var pe *problem.Error
	if errors.As(err, &pe) && pe != nil {
		return pe
	}
	var ve validator.ValidationErrors
	if bind || errors.As(err, &ve) {
		return BindingError(err)
	}
	if errors.Is(err, domain.ErrForbidden) {
		return problem.NewForbiddenError(DetailForbidden).WithCause(err)
	}
	return problem.FromError(err)
}

// WriteProblem writes perr as an application/problem+json response, aborts the
// chain and logs it. traceId, instance and (for a verified tenant context) tenantId
// come from the request. A nil perr is a 500 internal-error.
func WriteProblem(c *gin.Context, perr *problem.Error) {
	if perr == nil {
		perr = problem.NewInternalError(nil)
	}
	s := problemSettings(c)
	b := problem.BuilderFor(perr).
		TypeBase(s.TypeBase).
		Instance(c.Request.URL.Path).
		TraceID(problemTraceID(c))
	if rc, ok := verifiedRequestContext(c); ok && rc.TenantID != "" {
		b.TenantID(rc.TenantID)
	}
	p := b.Build()

	attachCorrelationHeaders(c, traceIDFromContext(c), requestIDFromContext(c))
	c.Abort()
	body, _ := json.Marshal(p) // cannot fail: Problem has only strings, ints and string slices
	c.Data(p.Status, problem.ContentType, body)
	logProblem(s, p)
	if perr.Unclassified() {
		metrics.RecordUnclassifiedError(metrics.TransportHTTP) // recorded even without a logger
		logUnclassified(c, s, p, perr.Err)
	}
}

func problemSettings(c *gin.Context) ProblemSettings {
	if v, ok := c.Get(ctxProblemSettingsKey); ok {
		if s, ok := v.(ProblemSettings); ok {
			return s
		}
	}
	return ProblemSettings{}
}

// problemTraceID resolves the problem's traceId: a valid traceparent's trace-id, a
// sanitized client x-request-id, the server span's trace id, else a new id
// (problem.ResolveTraceID).
func problemTraceID(c *gin.Context) string {
	span := traceIDFromContext(c)
	if span == "" {
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
			span = sc.TraceID().String()
		}
	}
	return problem.ResolveTraceID(c.GetHeader("traceparent"), c.GetHeader(domain.HeaderRequestID), span)
}

func verifiedRequestContext(c *gin.Context) (*domain.RequestContext, bool) {
	v, ok := c.Get(CtxRequestContextKey)
	if !ok {
		return nil, false
	}
	rc, ok := v.(*domain.RequestContext)
	return rc, ok && rc != nil
}

// logProblem emits one warn-level "problem response" entry with the platform's
// problem fields (the same names and level as platform-pgcommon-py). It never logs
// the detail, the instance, headers or identity beyond the verified tenant. The
// response is already written; a panicking logger is ignored so it cannot turn the
// problem into an aborted connection.
func logProblem(s ProblemSettings, p problem.Problem) {
	if s.Logger == nil {
		return
	}
	defer func() { _ = recover() }()
	fields := map[string]any{
		"traceId":   p.TraceID,
		"type":      p.Type,
		"status":    p.Status,
		"errorCode": p.ErrorCode,
	}
	if s.Service != "" {
		fields["service"] = s.Service
	}
	if p.TenantID != "" {
		fields["tenantId"] = p.TenantID
	}
	s.Logger.Warn("problem response", service.Sanitize(fields))
}

// logUnclassified emits the error-level "unclassified error" entry for a 500 that
// reached the error handler unmapped (problem.Error.Unclassified): a programming
// defect to alert on. It carries where it happened and the Go types along the error
// chain, never the error's text, which may hold SQL, PII, tokens or secrets.
// Services classify expected failures (a *problem.Error) or log the cause
// themselves, sanitized, where they have the context.
func logUnclassified(c *gin.Context, s ProblemSettings, p problem.Problem, cause error) {
	if s.Logger == nil {
		return
	}
	defer func() { _ = recover() }() // the response is already written
	fields := map[string]any{
		"classified":  false,
		"traceId":     p.TraceID,
		"trace_id":    traceIDFromContext(c),
		"request_id":  requestIDFromContext(c),
		"method":      sanitizeLogValue(c.Request.Method),
		"route":       label.HTTPRoute(c),
		"operation":   handlerOperation(c.HandlerName()),
		"status":      p.Status,
		"errorCode":   p.ErrorCode,
		"error_type":  "<nil>",
		"error_chain": []string{},
	}
	if s.Service != "" {
		fields["service"] = s.Service
	}
	if chain := problem.ErrorChainTypes(cause, problem.MaxErrorChain); len(chain) > 0 {
		fields["error_type"] = chain[0]
		fields["error_chain"] = chain
	}
	s.Logger.Error("unclassified error", fields)
}

// handlerOperation trims a Gin handler name to package.Func (the import path and
// the method-value "-fm" suffix dropped), e.g. "tenders.(*API).Create".
func handlerOperation(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return sanitizeLogValue(strings.TrimSuffix(name, "-fm"))
}
