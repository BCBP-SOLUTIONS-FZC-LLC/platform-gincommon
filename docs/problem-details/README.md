# RFC 9457 Problem Details

From v2.0.0, every HTTP error that gincommon writes, and every error a handler reports through it, is an [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem with the media type `application/problem+json`. This is the platform's single error contract. The same contract is implemented by platform-pgcommon-py (`docs/problem-details.md`), and Go and Python services produce the same bodies, trace ids and log lines.

- [`pkg/problem`](../../pkg/problem/problem.go): the framework-free model, type registry, builder, error types, trace-id resolution, client decoder and shared OpenAPI components (stdlib only; usable from gateways, BFFs and clients)
- [`pkg/gincommon`](../../pkg/gincommon/errors.go): the global error handler, `AbortWithProblem`, the binding and validation mapping
- [`registry.md`](registry.md): the error type registry
- [`openapi.yaml`](../../internal/problem/openapi.yaml): the shared `ProblemDetails` schema and standard responses (`problem.OpenAPIComponents()`)

## The contract

```json
{
  "type": "https://errors.company.com/tender/not-found",
  "title": "Tender not found",
  "status": 404,
  "detail": "Tender 123 was not found.",
  "instance": "/api/tenders/123",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "errorCode": "TENDER_NOT_FOUND",
  "tenantId": "tenant-001",
  "timestamp": "2026-10-15T10:45:22Z"
}
```

| Member | Always | Value |
|---|---|---|
| `type` | yes | `{base}/{domain}/{error}` from the [registry](registry.md), or `about:blank` for a status with no platform type |
| `title` | yes | The type's fixed title. It never varies per occurrence |
| `status` | yes | The response status |
| `detail` | no | A sentence the service wrote. It is absent on `internal-error`: the builder drops it even when one was set |
| `instance` | yes | The request path (`URL.Path`). The query string is never included, because it may carry tokens |
| `traceId` | yes | See [Trace correlation](#trace-correlation) |
| `errorCode` | yes | Upper snake case, stable. The type's default unless refined |
| `tenantId` | no | Only for a request with a **verified** tenant context (after `ContextMiddleware` built the `RequestContext`). It is never on a 401 |
| `timestamp` | yes | RFC 3339 UTC, second precision |
| `validationErrors` | no | `[{field, message}]` for `platform/validation-failed` |

## Reporting errors from handlers

`ObservabilityMiddlewares` ends with the **global error handler**. It is the single exception handler of a Gin service: register `ObservabilityMiddlewares` with `r.Use` and every error below becomes a problem.

```go
var TenderNotFound = problem.MustType("tender", "not-found", "Tender not found", 404, "TENDER_NOT_FOUND")

r.GET("/api/tenders/:id", func(c *gin.Context) {
	tender, err := repo.Get(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		_ = c.Error(problem.NewError(TenderNotFound, "Tender "+c.Param("id")+" was not found."))
		return
	case err != nil:
		_ = c.Error(err) // unclassified: 500 internal-error, no detail; err is logged by you, never sent
		return
	}
	c.JSON(http.StatusOK, tender)
})

r.POST("/api/tenders", func(c *gin.Context) {
	var req CreateTender
	if err := gincommon.ShouldBindJSON(c, &req); err != nil {
		gincommon.AbortWithProblem(c, err) // 400 validation-failed with validationErrors
		return
	}
	if !req.Deadline.After(time.Now()) {
		gincommon.AbortWithProblem(c, problem.NewValidationError(
			problem.FieldError{Field: "deadline", Message: "must be in the future"}))
		return
	}
	// ...
})
```

- **`c.Error(err)` and return** (deferred): the error handler renders the **last** recorded error after the handlers return, unless the handler already wrote a response.
- **`gincommon.AbortWithProblem(c, err)`** (immediate): renders now and aborts. Use it in middleware, or anywhere the chain must stop.
- **A bodiless error status** (`c.Status(409)`, Gin's 404 for an unregistered route, and its 405 when `r.HandleMethodNotAllowed = true`) is rendered as the platform problem for that status.
- **Not converted:** a response the handler already wrote (`c.JSON(404, gin.H{...})`) and a status committed without a body (`c.AbortWithStatus`, `c.AbortWithError`, `c.BindJSON`, `c.MustBindWith`). Gin flushes those headers immediately, so no body can follow. Use `c.Error` + `return`, `AbortWithProblem` or `ShouldBind*` instead. `make lint` does not detect this, so review handlers when migrating.

### Exception mapping

The specification's exception classes map to Go constructors on `*problem.Error`, which wraps an optional cause for `errors.Is`/`As`. The cause is never sent.

| Specification | Status | `type` | Go |
|---|---|---|---|
| ValidationException | 400 | `platform/validation-failed` | `problem.NewValidationError(fields...)`, `gincommon.BindingError(err)`, `gincommon.ShouldBindJSON` |
| AuthenticationException / UnauthorizedException | 401 | `platform/unauthorized` | `problem.NewUnauthorizedError(detail)`; `RequireAuth` / `ContextMiddleware` |
| AuthorizationException / ForbiddenException | 403 | `platform/forbidden` | `problem.NewForbiddenError(detail)`; an `Authorizer` returning `ErrForbidden` |
| ResourceNotFoundException | 404 | `platform/not-found` (or a domain type) | `problem.NewNotFoundError(detail)`, `problem.NewError(DomainType, detail)` |
| ConflictException | 409 | `platform/conflict` | `problem.NewConflictError(detail)` |
| BusinessRuleViolationException | 422 | `platform/business-rule-violation` (or a domain type) | `problem.NewBusinessRuleError(detail)` |
| RateLimitException | 429 | `platform/rate-limit-exceeded` | `problem.NewRateLimitError(detail)` |
| UnsupportedOperationException | 501 | `platform/not-implemented` | `problem.NewNotImplementedError(detail)`, `errors.ErrUnsupported` |
| UnhandledException | 500 | `platform/internal-error` | any other error, a panic, `problem.NewInternalError(cause)` |
| BusinessException (base) | the type's | any registered type | `problem.NewError(t, detail)` / `*problem.Error` |
| ProblemDetailsBuilder | — | — | `problem.NewBuilder(t)` / `problem.BuilderFor(err)` |

`gincommon.ClassifyError(err)` applies the same rules in this order:

1. a `*problem.Error` anywhere in the chain is sent unchanged;
2. `validator.ValidationErrors` become `validation-failed`;
3. `ErrForbidden` becomes 403;
4. then `problem.FromError`: `errors.ErrUnsupported` is 501, `context.DeadlineExceeded` is 503 `REQUEST_TIMEOUT`, `*http.MaxBytesError` is 413, and anything else is 500.

`context.Canceled` (the client went away) is recorded as 499 with no body.

### What gincommon itself emits

| Source | Status | `type` | `errorCode` | `detail` |
|---|---|---|---|---|
| `RequireAuth` / `ContextMiddleware`: missing or invalid identity | 401 | `platform/unauthorized` | `UNAUTHORIZED` | `Missing or invalid authentication headers.` (also `Ambiguous x-user-id header.`, `Ambiguous x-tenant-id header.`, `Invalid tenant roles.`, `Missing tenant id.`, `Missing user id.`) |
| `ContextMiddleware`: `ContextBuilder` error | 400 | `platform/bad-request` | `INVALID_REQUEST_CONTEXT` | `Invalid request context.` |
| `RequirePermission`: denied | 403 | `platform/forbidden` | `FORBIDDEN` | `The identity is not allowed to perform this operation.` |
| `RequirePermission`: misconfigured or `Authorizer` error | 500 | `platform/internal-error` | `INTERNAL_ERROR` | — |
| `PanicRecoveryMiddleware` | 500 | `platform/internal-error` | `INTERNAL_ERROR` | — |
| `TimeoutMiddleware` / `Config.RequestTimeout` | 503 | `platform/service-unavailable` | `REQUEST_TIMEOUT` | `The request timed out.` |
| Unregistered route / method | 404 / 405 | `platform/not-found` / `platform/method-not-allowed` | `ROUTE_NOT_FOUND` / `METHOD_NOT_ALLOWED` | `No route matches the request path.` / `The route does not support the request method.` |

The status codes are the same as in v1.x. Only the body and the content type changed.

## Validation errors

```json
{
  "type": "https://errors.company.com/platform/validation-failed",
  "title": "Validation failed",
  "status": 400,
  "instance": "/api/tenders",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "errorCode": "VALIDATION_FAILED",
  "timestamp": "2026-10-15T10:45:22Z",
  "validationErrors": [
    { "field": "deadline", "message": "must be in the future" },
    { "field": "title", "message": "is required" }
  ]
}
```

- **Field names** are the JSON names the client sent (`json` tag, else `form` tag, else the Go name), with nested paths like `items[0].sku`. On first use, the error handler registers a tag-name function on Gin's default validator (`binding.Validator`). A service that replaced `binding.Validator` keeps its own naming.
- **Messages** are fixed phrases derived from the validation tag and its parameter: `is required`, `must be at least 3 characters`, `must be one of: draft, open`, `must be a valid email address`, and so on. An unknown tag is `is invalid`. A message **never** echoes the rejected value, which may be PII.
- **Body errors** through `ShouldBindJSON` / `BindingError` / `c.Error(err).SetType(gin.ErrorTypeBind)`:
  - an empty body is `The request body is empty.`
  - malformed JSON is `The request body is not valid JSON.`
  - a JSON type mismatch is a `validationErrors` entry such as `{"field":"title","message":"must be a string"}`
  - any other binding failure is `The request could not be bound.`
  - an `http.MaxBytesReader` overflow is 413.
- A bare `*json.SyntaxError` passed to `c.Error` **without** the bind type is a 500: it may come from decoding an upstream response, which is not the client's fault.

## Trace correlation

`traceId` is the first usable value of:

1. the trace-id of a valid W3C `traceparent` (lower-case hex; not version `ff`, not an all-zero id, version `00` with exactly four fields);
2. the client's `x-request-id`, if it is 1 to 128 characters of `[A-Za-z0-9._:-]` (any other value is ignored, never truncated);
3. the server span's trace id (`TracingMiddleware`), which is also the response's `X-Trace-ID` header;
4. a new random 32-hex id.

This is platform-pgcommon-py's order (`problem.ResolveTraceID`, with the same test vectors). A valid `traceparent` gives the same id as the server span, so `traceId == X-Trace-ID`. When the client sends only `x-request-id`, `traceId` is that request id, which is also the `X-Request-ID` response header and the `request_id` field of the access log. The resolved `traceId` is always in the problem log line.

## Logging

Each problem is logged once, at **WARN**, as `problem response`, with exactly these fields:

- `traceId`
- `type`
- `status`
- `errorCode`
- `service` (`Config.ServiceName`)
- `tenantId` (only when verified)

These are the names and the level platform-pgcommon-py uses. The entry never contains the detail, the instance, headers, tokens or the identity beyond the verified tenant. The cause of a 500 is not logged by the error handler: log it yourself, with the trace id, where you have the context. `PanicRecoveryMiddleware` logs panics with their stack at ERROR, and `RequirePermissionWithLogger` logs `Authorizer` errors. The problem line is suppressed when `Config.Logger` is nil, and a logger that panics is ignored, because the response has already been sent.

## Security

- A 5xx `internal-error` has no `detail`: unclassified errors, panics, `Authorizer` errors and context misconfiguration all lose their text, and `Builder.Build` strips any detail set on an `internal-error`.
- `detail` and `validationErrors` are fixed or service-written text. Never put `err.Error()`, SQL, stack traces, secrets, tokens, document contents or tenant information in them.
- `instance` never includes the query string.
- `tenantId` comes only from the verified `RequestContext`, never from a raw header, so a rejected identity is not reflected.
- `x-request-id` is reflected only when it matches the bounded token pattern.

## Gateways, Envoy and BFFs

These rules are required by the platform specification. gincommon provides the Go building blocks; each gateway's configuration enforces them.

- **Pass `application/problem+json` through unchanged**: status, body and `Content-Type`. Never rewrite a problem body, and do not enable Envoy `local_reply_config` body mappers or NGINX `proxy_intercept_errors` for upstream problem responses.
- **Forward trace identifiers**: `traceparent`, `tracestate` and `x-request-id` upstream (`gincommon.PropagateHeaders`, `grpccommon.UnaryClientInterceptor`), and `X-Trace-ID` / `X-Request-ID` back to the client.
- **Generate a problem for gateway-generated errors**, with the same members:

  | Gateway error | Status | `type` |
  |---|---|---|
  | Route not found | 404 | `platform/not-found` (`ROUTE_NOT_FOUND`) |
  | Authentication failed | 401 | `platform/unauthorized` |
  | Request too large | 413 | `platform/payload-too-large` |
  | Rate limit exceeded | 429 | `platform/rate-limit-exceeded` |
  | Upstream timeout | 504 | `platform/gateway-timeout` |
  | Upstream unavailable | 503 | `platform/service-unavailable` |

  A Go gateway or BFF builds them with `problem.NewBuilder(t).Instance(path).TraceID(problem.ResolveTraceID(traceparent, requestID, "")).Build()`, written with `Content-Type: application/problem+json`. A Gin-based BFF gets them automatically from `ObservabilityMiddlewares`. For Envoy, use `local_reply_config` to give *locally generated* replies a JSON body in this shape.

## OpenAPI

Every endpoint declares 400, 401, 403, 404, 409, 422, 429 and 500 with the shared schema. Copy `problem.OpenAPIComponents()` (the [YAML](../../internal/problem/openapi.yaml)) into the service's OpenAPI document, or generate it from that function in the service's build, and reference it:

```yaml
responses:
  "404":
    $ref: "#/components/responses/NotFound"
  # or inline:
  "409":
    description: Conflict
    content:
      application/problem+json:
        schema:
          $ref: "#/components/schemas/ProblemDetails"
```

`problem.StandardResponses()` maps each of the eight statuses to its response component name, so a generator or a test can check that every operation declares all of them. `test/unit/problem` checks that the schema's properties match the `Problem` struct's JSON members.

## Clients and SDKs

```go
resp, err := http.DefaultClient.Do(req)
// ...
if resp.StatusCode >= 400 {
	p, err := problem.FromResponse(resp)
	if errors.Is(err, problem.ErrNotProblem) {
		// legacy or non-platform upstream: handle by status only
	}
	switch {
	case p.Type == TenderNotFound.URI("") || p.ErrorCode == "TENDER_NOT_FOUND":
		// ...
	}
	log.Printf("upstream %d %s %s traceId=%s", p.Status, p.Type, p.ErrorCode, p.TraceID)
}
```

Branch on `Status`, `Type` and `ErrorCode`, and surface `Title`, `Detail` and `TraceID` to the user or the log. Never parse `Title` or `Detail` text. `FromResponse` ignores unknown members, so newer extensions do not break older clients.

## Events and messaging

RFC 9457 applies to synchronous HTTP APIs only. Never publish a `Problem` to a topic or queue; event contracts (platform-events, platform-eventcommon) are unchanged. gRPC is unaffected: `pkg/grpccommon` keeps gRPC status codes.

## Migrating from v1.x

| v1.x | v2.0.0 |
|---|---|
| `Content-Type: application/json` | `Content-Type: application/problem+json` |
| `{"error": "...", "status": N, "trace_id": "...", "request_id": "..."}` | the problem above (`detail`, `status`, `traceId`); the request id stays in the `X-Request-ID` header |
| `gincommon.ErrorResponse` | `problem.Problem` (decode with `problem.FromResponse`) |
| 500 body `"internal server error"` / `"request context missing"` | `platform/internal-error` with no detail |
| 401 `"missing or invalid authentication headers"` | `platform/unauthorized`, `detail` `Missing or invalid authentication headers.` |
| 403 `"forbidden"` | `platform/forbidden` |
| 503 `"request timeout"` | `platform/service-unavailable`, `errorCode` `REQUEST_TIMEOUT` |
| Gin's plain-text `404 page not found` | `platform/not-found`, `errorCode` `ROUTE_NOT_FOUND` |

Steps for a consuming service:

1. Change the module path: `go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@v2.0.0`, then rewrite the imports to `.../platform-gincommon/v2/pkg/...`.
2. Replace every handler-written error body (`c.JSON(4xx/5xx, ...)`, `c.AbortWithStatusJSON`, `c.AbortWithError`, `c.BindJSON`) with `c.Error` + `return`, `gincommon.AbortWithProblem` or `gincommon.ShouldBindJSON`.
3. Define the service's domain types with `problem.MustType` and add them to the [registry](registry.md).
4. Update clients that decoded `error` / `trace_id` to `problem.FromResponse`.
5. Reference the shared `ProblemDetails` schema from the OpenAPI document for all eight standard statuses.
6. Set `r.HandleMethodNotAllowed = true` if 405s should be distinguished from 404s.
