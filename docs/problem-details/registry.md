# Error type registry

Every platform HTTP error carries a `type` URI from this registry:

```
https://errors.company.com/{domain}/{error}
```

- `{domain}` and `{error}` are lower-case kebab slugs (`[a-z][a-z0-9]*(-[a-z0-9]+)*`).
- A URI is **stable and version-independent**. It never contains an API or library version, and it never changes once it is published. To change a type's meaning, add a new type.
- The base (`https://errors.company.com`) is `problem.DefaultTypeBase`. platform-pgcommon-py uses the same default. A service may override it with `gincommon.Config.ProblemTypeBase`, but every service in an organisation must use the same base.
- `title` and `status` are fixed for each type. `errorCode` is the type's default: a service may refine it per occurrence (`Error.WithCode`), for example `TENDER_NOT_FOUND` on a `platform/not-found`.

## Platform types (`platform/*`)

These are defined in `pkg/problem` (`problem.PlatformTypes()`). `test/unit/problem` fails if this table drifts from the code.

| Type | Status | Default `errorCode` | Title | Use for | Go |
|---|---|---|---|---|---|
| `platform/bad-request` | 400 | `BAD_REQUEST` | Bad Request | A malformed request that is not a field validation failure; a bare 400 | `NewBadRequestError` |
| `platform/validation-failed` | 400 | `VALIDATION_FAILED` | Validation failed | Field validation failures, with `validationErrors` | `NewValidationError`, `gincommon.BindingError` |
| `platform/unauthorized` | 401 | `UNAUTHORIZED` | Unauthorized | Missing or invalid identity (authentication) | `NewUnauthorizedError` |
| `platform/forbidden` | 403 | `FORBIDDEN` | Forbidden | The identity may not perform the operation (authorization) | `NewForbiddenError` |
| `platform/not-found` | 404 | `NOT_FOUND` | Not Found | A resource that does not exist; an unregistered route (`ROUTE_NOT_FOUND`) | `NewNotFoundError` |
| `platform/method-not-allowed` | 405 | `METHOD_NOT_ALLOWED` | Method Not Allowed | A route without the requested method | (written by the error handler) |
| `platform/conflict` | 409 | `CONFLICT` | Conflict | A conflict with the resource's current state (duplicate, concurrent update, lock) | `NewConflictError` |
| `platform/payload-too-large` | 413 | `PAYLOAD_TOO_LARGE` | Payload Too Large | Request body over the limit (`*http.MaxBytesError`) | `FromError` |
| `platform/unsupported-media-type` | 415 | `UNSUPPORTED_MEDIA_TYPE` | Unsupported Media Type | A request body in a media type the endpoint does not accept | `NewError(TypeUnsupportedMediaType, …)` |
| `platform/business-rule-violation` | 422 | `BUSINESS_RULE_VIOLATION` | Business rule violation | A well-formed request that a business rule rejects | `NewBusinessRuleError` |
| `platform/rate-limit-exceeded` | 429 | `RATE_LIMIT_EXCEEDED` | Too Many Requests | Rate limit exceeded | `NewRateLimitError` |
| `platform/internal-error` | 500 | `INTERNAL_ERROR` | Internal Server Error | Any unhandled error. **Never has a detail** | `NewInternalError`, `FromError` |
| `platform/not-implemented` | 501 | `NOT_IMPLEMENTED` | Not Implemented | An unsupported operation (`errors.ErrUnsupported`) | `NewNotImplementedError` |
| `platform/service-unavailable` | 503 | `SERVICE_UNAVAILABLE` | Service Unavailable | Temporary inability to serve: a dependency down, overload, or the request deadline exceeded (`REQUEST_TIMEOUT`) | `NewServiceUnavailableError`, `FromError` |
| `platform/gateway-timeout` | 504 | `GATEWAY_TIMEOUT` | Gateway Timeout | A gateway's upstream did not answer in time | `NewError(TypeGatewayTimeout, …)` |

A status with no platform type (for example 418) is sent with `type: "about:blank"` and the HTTP status text as `title` (RFC 9457 §4.2.1).

### Refined error codes used by the platform libraries

| `errorCode` | Type | Emitted by |
|---|---|---|
| `ROUTE_NOT_FOUND` | `platform/not-found` | gincommon error handler (no route matches the path) |
| `INVALID_REQUEST_CONTEXT` | `platform/bad-request` | gincommon `ContextMiddleware` (a `ContextBuilder` rejected the identity) |
| `REQUEST_TIMEOUT` | `platform/service-unavailable` | gincommon `TimeoutMiddleware`; `problem.FromError(context.DeadlineExceeded)` |
| `DATABASE_UNAVAILABLE`, `SERVICE_OVERLOADED` | `platform/service-unavailable` | platform-pgcommon-py `healthz()` |
| `DATABASE_TIMEOUT` | `platform/service-unavailable` | recommended mapping in platform-pgcommon-py `docs/problem-details.md` |
| `RESOURCE_ALREADY_EXISTS`, `RESOURCE_IN_USE`, `CONCURRENT_UPDATE_CONFLICT`, `RESOURCE_LOCKED` | `platform/conflict` | recommended mapping in platform-pgcommon-py `docs/problem-details.md` |

## Domain types (`{domain}/*`)

Each domain owns the types under its domain slug. Define them once, as package-level variables, with `problem.MustType`. It rejects an invalid slug, a non-error status, a non-upper-snake code, and the reserved `platform` domain:

```go
var (
	TenderNotFound     = problem.MustType("tender", "not-found", "Tender not found", 404, "TENDER_NOT_FOUND")
	TenderInvalidStage = problem.MustType("tender", "invalid-stage", "Invalid tender stage", 422, "TENDER_INVALID_STAGE")
)
```

Register every domain type here in the same PR that introduces it:

| Type | Status | `errorCode` | Title | Owner |
|---|---|---|---|---|
| `tender/not-found` | 404 | `TENDER_NOT_FOUND` | Tender not found | *(example from the platform specification)* |
| `tender/invalid-stage` | 422 | `TENDER_INVALID_STAGE` | Invalid tender stage | *(example from the platform specification)* |
| `workflow/step-not-found` | 404 | `WORKFLOW_STEP_NOT_FOUND` | Workflow step not found | *(example from the platform specification)* |

Prefer a platform type with a refined `errorCode` over a new domain type when the platform type already says what the client must do. A domain type is worth adding when clients branch on the `type` itself.
