# platform-gincommon

The platform's shared **Gin HTTP and gRPC middleware library**: the transport-layer system of record for how every Go service in the platform recovers panics, correlates requests, traces, logs, emits metrics, trusts gateway identity, builds tenant context and authorizes calls. It also owns the **Enterprise Platform Observability Standard** implementation for Go — the central metric registry (`internal/obsregistry/registry.json`), the `platform_*` collectors built from it, and the `metricslint` tool every service runs in CI. Every IAM service imports it on every request path.

**Repository:** `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon`
**Module:** `go 1.26.0` directive · `toolchain go1.26.9` · private module · **never deployed** — consumers import `pkg/`; `cmd/server` is a reference/demo binary and the CI image is a demo image, not a production artefact
**Design:** Clean Architecture (`domain ← port ← service ← adapter ← pkg`); the observability contract is [`docs/observability/`](docs/observability/README.md) and the registry rendered at [`docs/observability/metric-registry.md`](docs/observability/metric-registry.md). The registry imports platform-events' and platform-pgcommon's released registries verbatim — where they conflict, their released contract wins. Every HTTP error is an [RFC 9457](docs/problem-details/README.md) problem (`application/problem+json`). Release `v2.0.0` moves the module to `.../platform-gincommon/v2` for that breaking change; `v1.4.0` shipped earlier breaking changes as a MINOR — a [recorded exception](VERSIONING.md#recorded-exceptions).

---

## Mental model

| This library owns | It does NOT own |
|---|---|
| The middleware/interceptor **order** and what each layer records — `PanicRecovery → RequestID → Tracing → CorrelationHeaders → Metrics → Logging → [Timeout] → RequireAuth → Context → [RequirePermission]` | Authentication itself — JWT/OAuth/API keys are validated by the API gateway before headers reach a service |
| Gateway-identity **validation**: `x-user-id`/`x-tenant-id` (non-empty, ≤256 bytes, no control chars, exactly one value), `x-tenant-roles` (`[a-zA-Z0-9_-]+`, deduped, ≤4096 bytes, ≤64 roles) | Cryptographic identity verification — headers are trusted at the network layer |
| `RequestContext{TenantID, UserID, Roles, TraceID, ClientIP}` in Gin and Go contexts | Business authorization policy — production services implement `Authorizer`; `NewDemoRBAC()` is reference-only |
| The standard error envelope `{error, status, trace_id, request_id}` and `X-Trace-ID`/`X-Request-ID` response headers | Request-body binding, business validation, domain error mapping |
| `platform_http_*`, `platform_grpc_*`, `platform_build_info` collectors with `{domain, service, environment}` injected centrally | Service-specific (Tier 3) metric *definitions* — services declare them, gincommon only validates them |
| The central Platform Observability Registry (115 entries across six platform libraries) and its enforcement (`pkg/obsregistry`, `cmd/metricslint`) | Messaging metrics *emission* (platform-events) and DB-pool/query metrics (platform-pgcommon) — they emit, the registry governs |
| OpenTelemetry setup: one delegating global `TracerProvider`, `ParentBased` sampling, OTLP/gRPC export, W3C propagation | The collector/Tempo/Prometheus/Loki infrastructure (local stack in `docker-compose.yml` is dev-only) |
| Outbound propagation: `PropagateHeaders` (trace + request ID, any destination) and opt-in `PropagateIdentityHeaders` / gRPC client interceptors | Retries, circuit breaking, rate limiting (gateway / mesh) |
| Structured Zap logging with sanitization and redaction | Database access, pooling, RLS (platform-pgcommon); outbox/SNS/SQS (platform-events) |

Consuming services keep their own `main`, routes and business logic and import only `pkg/` — never `internal/` (Go forbids it, and every type a consumer needs has a `pkg/` alias: `Authorizer`, `RequestContextData`, `Logger`, `DemoRBAC`, `ErrForbidden`).

**Tracing guarantee:** every incoming request starts a server span, or continues the caller's trace when a valid `traceparent` is present. End-to-end traces hold as long as outbound calls use `PropagateHeaders` (HTTP) or `UnaryClientInterceptor` (gRPC).

---

## Why this library exists

Every backend service needs the same cross-cutting transport concerns — panic recovery, request correlation, distributed tracing, structured logs, Prometheus metrics, gateway-identity extraction and access control. Centralizing them in one library means:

- **One log schema.** Every service emits the same `http_request` / `grpc_request` entry (same fields, `duration_ms` as float64 milliseconds, Info/Warn/Error by outcome), so cross-service log queries work without per-service translation.
- **One metric vocabulary.** `platform_http_requests_total{method, route, status_class, error_class}` means the same thing in every service, so dashboards and SLO burn-rate alerts aggregate across the whole platform. Label cardinality is bounded once (route templates, `OTHER` methods, registered gRPC methods) instead of per team.
- **One trust boundary.** Header validation, repeated-header rejection, role caps and the `claimed_*` vs verified-identity split in logs are implemented and tested once, not hand-rolled per service.
- **One governance gate.** The registry is the contract: a service cannot emit an unregistered shared metric, a prohibited label (`tenant_id`, `user_id`, ...) or a legacy name without `metricslint` failing its CI.

This is enforced structurally: the `core/` layers never import `adapter/` or framework packages; the metric collectors are *built from* registry entries (name, help, labels, buckets), so code and contract cannot drift; and `make metrics-lint` + a ≥98% coverage gate run on every PR.

---

## API overview

Five public packages and one CLI. Source of truth: `go doc ./pkg/<name>`; the tables below are the stable surface covered by SemVer ([VERSIONING.md](VERSIONING.md)). `internal/*`, `cmd/server` and `test/*` are not.

**Default HTTP chain:** `gincommon.DefaultMiddlewares` = `ObservabilityMiddlewares` (`PanicRecovery → RequestID → Tracing → CorrelationHeaders → Metrics → Logging → [Timeout when Config.RequestTimeout > 0] → error handler`) + `ProtectedMiddlewares` (`RequireAuth → ContextMiddleware`). **Default gRPC chain:** `grpccommon.FullUnaryInterceptors` = `recovery → request ID → tracing → metrics → logging → error sanitizer → auth → context` (logging wraps auth, so rejected calls are logged; the sanitizer turns a plain handler error into `Internal "internal server error"` instead of grpc-go sending `Unknown` with the error text).

### `pkg/gincommon` — HTTP services

| Group | Symbols | Purpose |
|---|---|---|
| Configuration | `Config{Logger, ServiceName, BuildVersion, Domain, Environment, Tracing, RequestTimeout, ProblemTypeBase}`, `TracingOptions{Endpoint, Insecure, SampleRatio, EnableBaggage, IgnoreRemoteParentSampled}` | `ServiceName` and `Domain` (or `OBSERVABILITY_DOMAIN`) required — missing or invalid identity panics at startup |
| Middleware stacks | `ObservabilityMiddlewares`, `ProtectedMiddlewares`, `DefaultMiddlewares`, `TimeoutMiddleware(d)` | The chains above; 401s are audit-logged to `cfg.Logger` |
| Health | `HealthHandler()` | `200 {"status":"ok"}`; register before `ProtectedMiddlewares` |
| Authorization | `RequirePermission(action, resource, auth)`, `RequirePermissionWithLogger(...)`, `NewDemoRBAC()`, `Authorizer`, `DemoRBAC`, `ErrForbidden` | Per-route RBAC; `Authorize(ctx, rc, action, resource) error` — `nil` allows, `ErrForbidden` denies (403), other errors → 500 |
| Request context | `RequestContext(c)`, `RequestContextData`, `RequestIDFromContext(c)`, `TraceIDFromContext(c)`, `Header*` constants | Verified identity and correlation IDs in handlers |
| Outbound | `PropagateHeaders(c, req)`, `PropagateIdentityHeaders(c, req)`, `InjectTraceContext(ctx, header)` | Trace + request ID to any destination; identity only to trusted internal services |
| Errors | `AbortWithProblem(c, err)`, `ClassifyError(err)`, `BindingError(err)`, `ShouldBindJSON(c, obj)`, `ShouldBind(c, obj)` | RFC 9457 problems; `c.Error(err)` + `return` is rendered by the global error handler in `ObservabilityMiddlewares` |
| Tracing | `InitTracingWithConfig(TracingConfig)`, `InitTracing`, `EnsureTracing`, `InitTracingFromEnv`, `Shutdown(logger)`, `NewTracer`, `NoopTracer`, `NewSpanTracer`, `SpanTraceID`, `RecordSpanError`, `Tracer`/`Span` | Tracing outside handlers with no OpenTelemetry import; `NewSpanTracer` is pgcommon's `Config.Tracer` hook |
| Metrics | `MetricsHandler()`, `MetricsGatherer()`, `MetricsRegisterer()`, `MetricsConstLabels()`, `LabeledRegisterer(reg)`, `NewShared{Counter,Gauge,Histogram}Vec(reg, name)`, `New{CounterVec,Counter,GaugeVec,Gauge,HistogramVec}` + type aliases | Tier 1/2 from the registry; Tier 3 validated; no Prometheus import |
| Logging | `Logger` | The structured-logging port `Config.Logger` takes |

### `pkg/problem` — RFC 9457 Problem Details (stdlib only)

| Group | Symbols | Purpose |
|---|---|---|
| Model | `Problem`, `FieldError`, `ContentType`, `FormatTimestamp` | The platform error body: `type`, `title`, `status`, `detail`, `instance` + `traceId`, `errorCode`, `tenantId`, `timestamp`, `validationErrors` |
| Registry | `Type`, `Type*` (15 platform types), `PlatformTypes()`, `NewType`/`MustType`, `TypeForStatus`, `TypeURI`, `DefaultTypeBase` | `{base}/{domain}/{error}` URIs — see [registry.md](docs/problem-details/registry.md) |
| Errors | `Error`, `NewError`, `NewValidationError`, `NewBadRequestError`, `NewUnauthorizedError`, `NewForbiddenError`, `NewNotFoundError`, `NewConflictError`, `NewBusinessRuleError`, `NewRateLimitError`, `NewNotImplementedError`, `NewServiceUnavailableError`, `NewInternalError`, `FromError` | What handlers return; a cause is kept for `errors.Is`/`As` and never sent |
| Builder | `Builder`, `NewBuilder(t)`, `BuilderFor(err)` | Gateways/BFFs building their own problems; strips any detail from `internal-error` |
| Correlation | `ResolveTraceID`, `TraceIDFromTraceparent`, `SanitizeRequestID`, `NewTraceID` | `traceparent` → `x-request-id` → span → generated (platform-pgcommon-py's order) |
| Clients | `FromResponse(resp)`, `Decode(r)`, `IsProblemContentType`, `ErrNotProblem` | Parse problems; branch on `Status`/`Type`/`ErrorCode`, never on text |
| OpenAPI | `OpenAPIComponents()`, `StandardResponses()` | Shared `ProblemDetails` schema and the 400/401/403/404/409/422/429/500 responses |

### `pkg/grpccommon` — gRPC services

| Group | Symbols | Purpose |
|---|---|---|
| Configuration | `Config{ServiceName, BuildVersion, Logger, Domain, Environment, Tracing}` | Same identity rules as `gincommon.Config`; `Logger` enables logging and panic-recovery logs |
| Server stacks | `Observability{Unary,Stream}Interceptors`, `Protected{Unary,Stream}Interceptors`, `Full{Unary,Stream}Interceptors`, `Default*` (aliases of `Observability*`) | Panic recovery outermost in every chain |
| Timeouts | `TimeoutUnaryInterceptor(d)`, `TimeoutStreamInterceptor(d)` | Cap the handler deadline; place **innermost** (after `Full*`) so observability records the client's `DeadlineExceeded` |
| Authorization | `RequirePermission*`, `RequireStreamPermission*`, `NewDemoRBAC()`, `Authorizer`, `DemoRBAC`, `ErrForbidden` | Per-RPC RBAC; `PermissionDenied` / `Internal` / `FailedPrecondition` (misordered chain) |
| Health | `RegisterHealth(srv) *health.Server` | `grpc.health.v1.Health`; exactly `Check`/`Watch`/`List` bypass auth/context/RBAC |
| Cardinality | `RegisterKnownMethods(srv)` | After registering every service, before `Serve`: unregistered methods → `unknown` (process-wide allow-list) |
| Client | `UnaryClientInterceptor()`, `StreamClientInterceptor()`, opt-in `UnaryClientIdentityInterceptor()`, `StreamClientIdentityInterceptor()` | Outbound trace context + `x-request-id`; identity only to trusted internal services |
| Context | `RequestContextFromContext(ctx)`, `RequestContextData`, `RequestIDFromContext(ctx)` | Same `RequestContext` type as HTTP — one `Authorizer` serves both stacks |
| Lifecycle | `Shutdown(logger)`, `MetricsRegisterer()`, `MetricsConstLabels()`, `Logger` | Flush spans + logs after `GracefulStop` |

### `pkg/obsregistry` and `cmd/metricslint` — observability governance

| Symbol / command | Purpose |
|---|---|
| `Default()`, `Parse(data)`, `ServicePrefix(domain, service)` | The embedded registry; a registry document; the `<domain>_<service>_` Tier 3 prefix |
| `Validate(gatherer, opts)`, `ValidateText(r, opts)` | Check emitted metrics: namespace, naming, suffixes, required labels, registry compliance, label vocabulary, cardinality (text and OpenMetrics input) |
| `metricslint check [-url U \| files] [-strict] [-allow-proposed-owner list]` | Validate a scrape or exposition files |
| `metricslint refs <paths>` | Find legacy/unregistered metric names in dashboards, rules, SLOs, HPA manifests |
| `metricslint registry` / `inventory` | Self-check a registry document / render `metric-registry.md` |

Exit codes: `0` clean, `1` findings, `2` usage or I/O error. Release builds attach `metricslint` binaries for linux/darwin amd64+arm64 and windows amd64.

### `pkg/logger`

| Symbol | Purpose |
|---|---|
| `NewLogger(env)` | Zap-backed `Logger`; dev format for `dev`/`local`; `LOG_LEVEL`, `LOG_SAMPLING`, `LOG_FILE` apply |
| `Default()` | Process-wide logger for code paths never handed one (e.g. an early startup failure) |

---

## Input validation

Every rejection the library produces is an RFC 9457 problem (`application/problem+json`), with `X-Trace-ID` / `X-Request-ID` response headers set. Internal error detail is never sent to the client: a 5xx `internal-error` has no `detail`, and the cause goes to the sanitized log entry only. Full contract: [docs/problem-details](docs/problem-details/README.md).

```json
{
  "type": "https://errors.company.com/platform/forbidden",
  "title": "Forbidden",
  "status": 403,
  "detail": "The identity is not allowed to perform this operation.",
  "instance": "/api/items/42",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "errorCode": "FORBIDDEN",
  "tenantId": "tenant-001",
  "timestamp": "2026-10-15T10:45:22Z"
}
```

### Notable validation rules

| Rule | Enforcement |
|---|---|
| `x-user-id` / `x-tenant-id` required on protected routes | Non-empty after trim, ≤256 bytes, no control characters → else `401` / `codes.Unauthenticated` with a warn `auth: request rejected` audit entry |
| Repeated identity headers are ambiguous | More than one `x-user-id` or `x-tenant-id` value → `401 ambiguous ...` (HTTP) / `Unauthenticated "ambiguous <key> metadata"` (gRPC) — a gateway that appends rather than replaces can never let a client value win |
| Roles are bounded and validated | Repeated `x-tenant-roles` values are joined; tokens must match `[a-zA-Z0-9_-]+` (≤64 chars, invalid tokens dropped), deduplicated; a header over 4096 bytes or with more than 64 roles → `401` (`Invalid tenant roles.`) |
| `x-request-id` is reused only when valid | `[A-Za-z0-9._-]`, ≤128 chars, else a new UUID is generated and used consistently for logs, spans and propagation |
| Unverified identity is never logged as verified | `tenant_id`/`user_id` in `http_request` come only from the built `RequestContext`; client-supplied values on public or rejected routes appear as `claimed_tenant_id`/`claimed_user_id` |
| Metric labels cannot explode | HTTP `route` is the Gin template or `unknown` (never the raw path); non-standard methods → `OTHER`; gRPC method names outside `^/[A-Za-z0-9_.]+/[A-Za-z0-9_]+$`, over 256 bytes, or (after `RegisterKnownMethods`) unregistered → `unknown` |
| Observability identity fails fast | Missing `Domain`, or a domain/service/environment the registry rejects → startup panic; a different identity on a second stack in one process → `ErrIdentityMismatch`; an invalid `BUILD_VERSION` is sanitised with a warning |
| Tier 3 metric names are validated at construction | `<domain>_<service>_` prefix, `_total` counters, `_seconds` histograms (or registry `unit: bytes`), no prohibited or centrally injected labels |

---

## Architecture

Clean Architecture — dependencies point inward. Full flow diagrams and invariant tables are in **[ARCHITECTURE.md](ARCHITECTURE.md)**; Mermaid sources live in **[`docs/architecture/`](docs/architecture/README.md)**.

```
platform-gincommon/
├── pkg/                                  # Public API — the only packages consumers import
│   ├── gincommon/                        # HTTP stacks, tracing, metrics API, propagation, type aliases (types.go)
│   ├── grpccommon/                       # gRPC stacks, client interceptors, health, known-method allow-list, aliases
│   ├── problem/                          # RFC 9457 Problem Details (stdlib only): model, type registry, Builder, errors, client decoder, OpenAPI
│   ├── obsregistry/                      # Public face of the registry: Validate, ValidateText, Default, Parse
│   └── logger/                           # NewLogger, Default
├── internal/
│   ├── core/
│   │   ├── domain/                       # RequestContext, header/metadata constants, ParseRoles(Strict), sentinel errors — no external deps
│   │   ├── port/                         # Logger, Authorizer, ContextBuilder interfaces
│   │   └── service/                      # ContextService, RBACService (demo), LogSanitizer
│   ├── adapter/
│   │   ├── inbound/
│   │   │   ├── http/middleware/          # Recovery, RequestID, Tracing, Correlation, Metrics, Logging, Timeout, Auth, Context, AuthZ, 429 log limiter,
│   │   │   │                             #   ProblemMiddleware (global error handler), WriteProblem, BindingError
│   │   │   ├── grpc/middleware/          # gRPC equivalents + client interceptors, stream timeout, status helper
│   │   │   └── label/                    # Bounded HTTPRoute / GRPCMethod labels, known-method allow-list
│   │   └── outbound/
│   │       ├── metrics/                  # platform_* collectors built from the registry; transactional init; atomic collector set
│   │       ├── otel/                     # Delegating TracerProvider, OTLP exporter, sampler, resource, error handler
│   │       └── logger/                   # Zap adapter (LOG_LEVEL, LOG_SAMPLING, LOG_FILE tee)
│   ├── problem/                          # RFC 9457 implementation behind pkg/problem; openapi.yaml (embedded)
│   ├── obsregistry/                      # registry.json (embedded), validation, refs scanner, OpenMetrics normaliser, CLI
│   ├── config/                           # Env var loading and normalisation
│   └── server/                           # Demo router (reference only)
├── cmd/
│   ├── server/                           # Demo binary — /health, /metrics, /panic (dev only)
│   └── metricslint/                      # Observability-standard CLI
├── docs/
│   ├── observability/                    # The standard as implemented, metric-registry.md (generated), migration, runbooks
│   ├── problem-details/                  # RFC 9457 contract, error type registry (registry.md)
│   ├── lld/                              # Low-level design
│   └── architecture/                     # Architecture docs + Mermaid sources
├── docker/                               # Local otel-collector, Tempo, Prometheus (+ rules, promtool tests), Loki, Promtail, Grafana config
├── .github/                              # Workflows, shellcheck-clean scripts, dependabot config (disabled)
├── .githooks/pre-commit                  # tidy (fails on drift) + fmt-check + lint + metrics-lint registry/inventory drift
└── test/                                 # unit/, integration/, e2e/, smoke/, fixtures/, testenv/
```

### Dependency rules

| Component | May depend on |
|---|---|
| `core/domain` | Nothing internal |
| `core/port` | `domain` |
| `core/service` | `domain` (`RBACService` satisfies `port.Authorizer` structurally) |
| `adapter/inbound/{http,grpc}/middleware` | `label`, `adapter/outbound/metrics`, `service`, `port`, `domain`; HTTP also `problem` |
| `adapter/outbound/metrics` | `obsregistry`, `config` |
| `adapter/outbound/otel` | `config`, `port` |
| `adapter/outbound/logger`, `label`, `obsregistry`, `problem`, `config` | Nothing internal |
| `pkg/*` | The composition layer; `pkg/gincommon` and `pkg/grpccommon` never import each other; `pkg/problem` imports only `internal/problem` |

Generated from `go list`; the full graph is in [ARCHITECTURE.md](ARCHITECTURE.md#package-dependency-graph).

The core never imports Gin, gRPC, Prometheus or OpenTelemetry; interfaces in `core/port` are implemented in `adapter/outbound` and injected inward.

### Runtime components

| Concern | Technology | Notes |
|---|---|---|
| **HTTP framework** | Gin `v1.12.0` | `SetTrustedProxies(nil)` in the demo — consumers behind a proxy set their CIDRs |
| **gRPC** | `google.golang.org/grpc v1.84.0` | Custom interceptors (no otelgrpc dependency); `grpc/health` for `RegisterHealth` |
| **Metrics** | Prometheus `client_golang v1.24.1` | Registered on `prometheus.DefaultRegisterer` unless an isolated registry is passed; `/metrics` uses `ContinueOnError` |
| **Tracing** | OpenTelemetry `v1.46.0`, OTLP/gRPC exporter | One delegating global provider (its inner SDK provider is swapped atomically); in-process fallback when the exporter cannot be created |
| **Logging** | Zap `v1.28.0` | JSON in production, console in dev/local; sampling off unless `LOG_SAMPLING=true` |
| **Registry** | Embedded `registry.json` | 115 entries: 6 `approved`, 52 `proposed`, 57 `removed` (legacy names kept so `metricslint` can name their replacement) |

### Sibling platform libraries

| Library | Relationship |
|---|---|
| `platform-events` | Its released registry is imported verbatim (23 entries it owns); it injects identity labels itself — do not wrap it in `LabeledRegisterer` |
| `platform-pgcommon` | Registry imported verbatim (10 entries it owns); `gincommon.NewSpanTracer(scope)` is its `Config.Tracer` hook |
| `platform-fastapicommon` (Python) | Vendors a copy of `registry.json` — re-copy it whenever the registry changes |
| `platform-interop-tests` | Cross-library interop suite, run by CI (`interop` job, pinned to a commit SHA) |

---

## Integrating into a consuming service

### 1. Prerequisites

This is a **private module**:

```bash
export GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@v2.0.0
```

Go 1.26+ (the `go 1.26.0` directive does not force consumers onto 1.26.9). Pin a tag — never `@latest`. Requests must arrive through the trusted API gateway, which authenticates the caller and injects `x-user-id`/`x-tenant-id`/`x-tenant-roles`; the service must not be reachable around the gateway.

### 2. HTTP service

```text
Incoming request
  → PanicRecovery                     (catches panics from all below)
  → RequestID                         (read/generate x-request-id)
  → Tracing                           (OTel span, X-Trace-ID on response)
  → CorrelationHeaders                (sets X-Trace-ID, X-Request-ID before the handler runs)
  → Metrics                           (Prometheus platform_http_* counters/histograms)
  → Logging                           (http_request log after response)
  → [Config.RequestTimeout > 0] Timeout (innermost, so metrics/logs/span record its 503)
  ╠═ public routes (no auth) ════════════════════════════════╗
  ║   GET /health     → HealthHandler()                       ║
  ║   GET /metrics    → MetricsHandler()                      ║
  ╚═══════════════════════════════════════════════════════════╝
  ╠═ protected routes (/api/...) ════════════════════════════╗
  ║   → RequireAuth        (401 + audit log without ids)       ║
  ║   → ContextMiddleware  (builds RequestContext)              ║
  ║   → [optional per route] RequirePermission                  ║
  ║   → your handler                                            ║
  ╚══════════════════════════════════════════════════════════╝
```

```go
package main

import (
    "context"
    "errors"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gin-gonic/gin"

    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/logger"
)

func main() {
    appEnv := os.Getenv("APP_ENV")
    if appEnv == "" {
        appEnv = "dev"
    }
    if appEnv != "dev" {
        gin.SetMode(gin.ReleaseMode)
    }

    // 1. Logger — Zap adapter implementing gincommon.Logger (LOG_LEVEL / LOG_SAMPLING apply)
    log, err := logger.NewLogger(appEnv)
    if err != nil {
        panic(err)
    }

    cfg := gincommon.Config{
        Logger:       log,
        ServiceName:  "my-service",  // required — Prometheus `service` label + OTel service.name
        Domain:       "iam",         // required (or OBSERVABILITY_DOMAIN) — a registry domain
        // Empty would use APP_ENV, then ENVIRONMENT; must be local|dev|test|staging|prod.
        // Pass appEnv so the "dev" fallback above also applies to metrics identity.
        Environment:  appEnv,
        BuildVersion: os.Getenv("BUILD_VERSION"), // empty → BUILD_VERSION → "unknown"
        // 30 s deadline per request; appended as the innermost observability handler.
        RequestTimeout: 30 * time.Second,
    }

    // 2. Tracing — ObservabilityMiddlewares installs a TracerProvider for cfg itself.
    //    Start it earlier only if startup work or background jobs need spans, and
    //    use the same identity so service.name is right from the first span:
    shutdownTracing, err := gincommon.InitTracingWithConfig(gincommon.TracingConfig{
        ServiceName: cfg.ServiceName, BuildVersion: cfg.BuildVersion,
        Domain: cfg.Domain, Logger: log,
    })
    if err != nil {
        panic(err)
    }
    defer shutdownTracing()

    r := gin.New()

    // 3. Observability on all routes: recovery, request ID, tracing, metrics, logging,
    //    and the RequestTimeout deadline. Panics on a missing/invalid identity.
    r.Use(gincommon.ObservabilityMiddlewares(cfg)...)

    // 4. Public routes — no auth required.
    r.GET("/health", gincommon.HealthHandler())
    r.GET("/metrics", gin.WrapH(gincommon.MetricsHandler()))

    // 5. Protected API — auth + tenant context on /api/... (401s are audit-logged)
    api := r.Group("/api")
    api.Use(gincommon.ProtectedMiddlewares(cfg)...)

    // Reference RBAC (replace with your gincommon.Authorizer in production)
    authz := gincommon.NewDemoRBAC()

    api.GET("/items", listItemsHandler)
    api.DELETE("/items/:id",
        gincommon.RequirePermissionWithLogger("delete", "items", authz, log),
        deleteItemHandler,
    )

    // 6. Graceful shutdown
    srv := &http.Server{Addr: ":8080", Handler: r, ReadHeaderTimeout: 5 * time.Second}
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            panic(err)
        }
    }()
    <-quit

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        panic(err)
    }

    // Flush OpenTelemetry spans and buffered logs before exit.
    _ = gincommon.Shutdown(log)
}

func listItemsHandler(c *gin.Context) {
    // RequestContext is populated by ContextMiddleware on protected routes.
    rc, ok := gincommon.RequestContext(c)
    if !ok {
        // Rendered by the global error handler as a 500 internal-error problem.
        _ = c.Error(errors.New("request context missing"))
        return
    }
    // rc.TenantID, rc.UserID, rc.Roles, rc.TraceID, rc.ClientIP all available.
    c.JSON(http.StatusOK, gin.H{
        "tenant_id": rc.TenantID,
        "user_id":   rc.UserID,
        "roles":     rc.Roles,
        "client_ip": rc.ClientIP,
    })
}

func deleteItemHandler(c *gin.Context) {
    c.Status(http.StatusNoContent)
}
```

If **every** route requires auth, `r.Use(gincommon.DefaultMiddlewares(cfg)...)` is the one-call equivalent; most services prefer `ObservabilityMiddlewares` globally plus `ProtectedMiddlewares` on `/api` only.

### 3. gRPC service

```go
package main

import (
    "net"
    "os"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/logger"

    // your generated pb package:
    pb "github.com/your-org/your-service/gen/pb"
)

func main() {
    log, _ := logger.NewLogger(os.Getenv("APP_ENV"))

    cfg := grpccommon.Config{
        ServiceName:  "my-grpc-service", // required
        Domain:       "iam",             // required (or OBSERVABILITY_DOMAIN)
        // Environment: empty uses APP_ENV, then ENVIRONMENT (local|dev|test|staging|prod).
        BuildVersion: os.Getenv("BUILD_VERSION"),
        Logger:       log, // enables structured logging + panic recovery logs
    }

    // Full stack: panic recovery, request ID, OTel tracing (installed for cfg),
    // Prometheus metrics, structured logging, RequireAuth, and context building.
    // The timeout interceptors go last (innermost), so observability records the
    // DeadlineExceeded the client receives.
    s := grpc.NewServer(
        grpc.ChainUnaryInterceptor(append(grpccommon.FullUnaryInterceptors(cfg),
            grpccommon.TimeoutUnaryInterceptor(30*time.Second))...),
        grpc.ChainStreamInterceptor(append(grpccommon.FullStreamInterceptors(cfg),
            grpccommon.TimeoutStreamInterceptor(5*time.Minute))...),
    )

    pb.RegisterItemServiceServer(s, &itemServer{log: log, inventory: dialInventory()})
    health := grpccommon.RegisterHealth(s) // grpc.health.v1.Health — no identity required
    defer health.Shutdown()

    // After every service is registered, before Serve: bounds grpc_method labels
    // and span names to these methods (anything else → "unknown").
    grpccommon.RegisterKnownMethods(s)

    lis, _ := net.Listen("tcp", ":50051")
    if err := s.Serve(lis); err != nil {
        panic(err)
    }
}

// Outbound calls: propagate traceparent + x-request-id on every connection; add the
// identity interceptor only for trusted internal services.
func dialInventory() *grpc.ClientConn {
    conn, err := grpc.NewClient("inventory-svc:50051",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithChainUnaryInterceptor(
            grpccommon.UnaryClientInterceptor(),
            grpccommon.UnaryClientIdentityInterceptor(), // opt-in: forwards x-user-id, x-tenant-id, x-tenant-roles
        ),
    )
    if err != nil {
        panic(err)
    }
    return conn
}
```

Reading identity in a handler, and per-RPC RBAC:

```go
type itemServer struct {
    log       grpccommon.Logger
    inventory *grpc.ClientConn
}

func (s *itemServer) GetItem(ctx context.Context, req *pb.GetItemRequest) (*pb.GetItemResponse, error) {
    // Identity and tenant info — populated by ContextInterceptor.
    rc, ok := grpccommon.RequestContextFromContext(ctx)
    if !ok {
        return nil, status.Error(codes.Internal, "missing request context")
    }

    // Correlation ID — populated by RequestIDInterceptor.
    requestID := grpccommon.RequestIDFromContext(ctx)

    s.log.Info("GetItem", map[string]any{
        "user_id":    rc.UserID,
        "tenant_id":  rc.TenantID,
        "roles":      rc.Roles,
        "client_ip":  rc.ClientIP,
        "request_id": requestID,
    })

    return &pb.GetItemResponse{Id: req.GetId()}, nil
}
```

```go
authz := grpccommon.NewDemoRBAC() // demo; implement grpccommon.Authorizer for production

interceptors := append(
    grpccommon.ObservabilityUnaryInterceptors(cfg),
    grpccommon.ProtectedUnaryInterceptors(cfg)...,
)
// RBAC runs after the protected stack: only admin can delete items
interceptors = append(interceptors,
    grpccommon.RequirePermissionWithLogger("delete", "items", authz, log),
)

s := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptors...))
```

`RequirePermission` placed before `ProtectedUnaryInterceptors` returns `codes.FailedPrecondition` — a diagnosable misconfiguration, never a silent pass.

### 4. Implementing a production `Authorizer`

One type serves both stacks — `gincommon.RequestContextData` and `grpccommon.RequestContextData` are the same type:

```go
type opaAuthorizer struct{ /* client */ }

func (a opaAuthorizer) Authorize(ctx context.Context, rc *gincommon.RequestContextData, action, resource string) error {
    allowed, err := a.decide(ctx, rc.TenantID, rc.UserID, rc.Roles, action, resource)
    if err != nil {
        return err // → 500 / codes.Internal (logged, never sent to the client)
    }
    if !allowed {
        return gincommon.ErrForbidden // → 403 / codes.PermissionDenied
    }
    return nil
}
```

### 5. Outbound calls

| Destination | HTTP | gRPC | Sends |
|---|---|---|---|
| Anything, including third parties | `PropagateHeaders(c, req)` | `UnaryClientInterceptor()` / `StreamClientInterceptor()` | `traceparent`, `tracestate`, `x-request-id` |
| Trusted internal platform service that needs the caller's identity | `PropagateIdentityHeaders(c, req)` | add `UnaryClientIdentityInterceptor()` / `StreamClientIdentityInterceptor()` | the above + `x-user-id`, `x-tenant-id`, `x-tenant-roles` (+ baggage on HTTP) |
| No `*gin.Context` (jobs, consumers) | `InjectTraceContext(ctx, req.Header)` | — | `traceparent` (+ baggage when enabled) |

Identity values are **set, not appended**, so downstream services never see repeated identity headers. Never use the identity variants for calls that leave the trust boundary.

### 6. Custom metrics

```go
cfg := gincommon.Config{ServiceName: "event-consumer", BuildVersion: version, Domain: "iam", Logger: log}
r.Use(gincommon.ObservabilityMiddlewares(cfg)...) // metrics are initialized here

// Tier 1/2: shared metrics come from the registry (name, help, labels, buckets).
processed, err := gincommon.NewSharedCounterVec(nil, "platform_messages_processed_total")
processed.WithLabelValues("iam.events", "user.created").Inc()

// Tier 3: service-specific metrics must be named <domain>_<service>_*; suffixes,
// prohibited labels (tenant_id, user_id, ...) and the namespace are checked.
hmac, err := gincommon.NewCounterVec(nil, "iam_event_consumer_hmac_failures_total", "HMAC failures", "reason")
// also NewCounter, NewGaugeVec, NewGauge, NewHistogramVec, NewSharedGaugeVec, NewSharedHistogramVec

metricsMux.Handle("/metrics", gincommon.MetricsHandler())
```

Call constructors **after** the middleware/interceptor constructor ran (before it they return an error rather than registering label-less collectors). A new shared (`platform_*` or `<domain>_*`) metric is a **registry change first** — add the entry with `"status": "proposed"` here, then build it with `NewShared*`.

### 7. Tracing and logging outside handlers

| API | Use |
|---|---|
| `NewTracer(scope)` / `NoopTracer()` | Child spans in services, repositories and consumers — tracers obtained before init follow the active provider |
| `NewSpanTracer(scope)` | A `StartSpan(ctx, name) (ctx, end)` hook — platform-pgcommon's `Config.Tracer` |
| `SpanTraceID(ctx)` | Trace ID from a `context.Context` for log correlation |
| `RecordSpanError(ctx, err, description)` | Record an error on the current span and mark it failed |
| `logger.Default()` | A process-wide logger for paths never handed one |

### 8. Returning and handling errors

Handlers report errors with `c.Error(err)` + `return`, or `gincommon.AbortWithProblem(c, err)` to stop the chain now; the global error handler in `ObservabilityMiddlewares` renders them as problems. Bind with `gincommon.ShouldBindJSON` so validation failures become `validationErrors`:

```go
var TenderNotFound = problem.MustType("tender", "not-found", "Tender not found", 404, "TENDER_NOT_FOUND")

func getTender(c *gin.Context) {
    t, err := repo.Get(c.Request.Context(), c.Param("id"))
    if errors.Is(err, store.ErrNotFound) {
        _ = c.Error(problem.NewError(TenderNotFound, "Tender "+c.Param("id")+" was not found."))
        return
    }
    if err != nil {
        _ = c.Error(err) // 500 internal-error, no detail
        return
    }
    c.JSON(http.StatusOK, t)
}
```

Every platform 4xx/5xx from another service is a problem:

```go
if p, err := problem.FromResponse(resp); err == nil {
    log.Printf("upstream %d %s %s traceId=%s", p.Status, p.Type, p.ErrorCode, p.TraceID)
}
```

Branch on `Status`, `Type` and `ErrorCode`, never on `Title`/`Detail` text. Use `traceId` to find the trace in Tempo and the `problem response` log line. Mapping table, validation, gateway behaviour and OpenAPI: [docs/problem-details](docs/problem-details/README.md).

### 9. Rate limits

The library implements no request rate limiter — throttling belongs to the gateway/mesh. `MetricsMiddleware` *observes* `429` responses (`platform_http_rate_limited_requests_total`) and logs at most one warn per route per second, with a `suppressed` count.

### 10. Upgrading to v2.0.0

v2.0.0 replaces the error contract and moves the module path (full detail: `[2.0.0]` in [CHANGELOG.md](CHANGELOG.md) and the [migration table](docs/problem-details/README.md#migrating-from-v1x)):

- [ ] **Change the module path.** `go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@v2.0.0` and rewrite every import to `.../platform-gincommon/v2/pkg/...`.
- [ ] **Stop writing error bodies by hand.** Replace `c.JSON(4xx/5xx, ...)`, `c.AbortWithStatusJSON`, `c.AbortWithError`, `c.BindJSON` and `gincommon.ErrorResponse` with `c.Error` + `return`, `gincommon.AbortWithProblem` or `gincommon.ShouldBindJSON`.
- [ ] **Define domain error types** with `problem.MustType` and register them in [registry.md](docs/problem-details/registry.md).
- [ ] **Update clients** that decoded `error` / `trace_id` / `request_id` to `problem.FromResponse` (the request id stays in the `X-Request-ID` header).
- [ ] **Update OpenAPI documents** to reference the shared `ProblemDetails` schema for 400, 401, 403, 404, 409, 422, 429 and 500 with `application/problem+json`.
- [ ] **Check gateways** pass `application/problem+json` through unchanged.

### 11. Upgrading to v1.4.0

v1.4.0 breaks the v1.x API as a [recorded exception](VERSIONING.md#recorded-exceptions). Do not let `go get -u` pull it in without these steps (full detail: `[1.4.0]` in [CHANGELOG.md](CHANGELOG.md) and the [metric name mapping](docs/observability/migration.md)):

- [ ] **Set the observability identity.** `Config.Domain` (or `OBSERVABILITY_DOMAIN`) is required — a registry domain such as `iam`.
- [ ] **Use a registry environment.** `Config.Environment`, else `APP_ENV`, else `ENVIRONMENT`, must be one of `local`, `dev`, `test`, `staging`, `prod` (trimmed, lowercased). `development`/`production` panic and no longer get dev defaults.
- [ ] **Check `BUILD_VERSION`.** Values outside `^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$` (e.g. `feat/x`, `refs/tags/v1.4.0`) are sanitised with a warning. An `unknown` version on one stack and a real one on another are compatible in either order.
- [ ] **Update dashboards and alerts to `platform_*`.** The `http_*`, `grpc_*` and `build_info` series are gone; run `metricslint refs` over dashboards and rules.
- [ ] **Change your `Authorizer`** to `Authorize(ctx context.Context, rc *gincommon.RequestContextData, action, resource string) error`, returning `gincommon.ErrForbidden` to deny.
- [ ] **Switch identity-forwarding calls to `PropagateIdentityHeaders`** — `PropagateHeaders` now sends only trace context and `x-request-id`. On gRPC clients add `UnaryClientInterceptor` (and the identity variant only for trusted services).
- [ ] **Update log queries.** On public or rejected routes the access log carries `claimed_user_id` / `claimed_tenant_id`.
- [ ] **gRPC:** `Full*` runs logging before auth; repeated `x-user-id`/`x-tenant-id` metadata is rejected; call `RegisterKnownMethods(srv)`; register health with `RegisterHealth`; place timeout interceptors innermost.
- [ ] **One metrics identity per process.** A different `ServiceName`/`Domain`/`Environment` on a second stack panics (`ErrIdentityMismatch`).
- [ ] **Move the HTTP timeout inside observability** — replace the outermost `r.Use(gincommon.TimeoutMiddleware(...))` with `Config.RequestTimeout`. Every IAM service registers it before `ObservabilityMiddlewares` today, which v1.4.0 records as 200 while the client gets 503.
- [ ] **Review logging volume** — Zap sampling is off by default (`LOG_SAMPLING=true` restores it); set `LOG_LEVEL` as needed.
- [ ] **Tracing service name** — `InitTracingFromEnv` has no default name; set `OTEL_SERVICE_NAME`, use `InitTracingWithConfig`, or let `ObservabilityMiddlewares` install tracing.

---

## Local development

### Prerequisites

- Go 1.26+ (the `toolchain go1.26.9` line is fetched automatically)
- Docker — only for the optional observability stack (`make docker-up`) and the image smoke gate
- `GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*` and access to the BCBP org
- GNU `timeout` for `make test-smoke-image` (`brew install coreutils` on macOS)

### Setup

```bash
git clone https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon
cd platform-gincommon
make setup       # copies .env-example → .env and installs .githooks/pre-commit (run once)
make tidy        # go mod tidy
make test        # unit + integration + e2e — no Docker needed
make run         # demo server on :8080 (/health, /metrics, /panic in dev)
```

### Common commands

| Command | Description |
|---|---|
| `make setup` / `make install-hooks` | `.env` from `.env-example`; install the pre-commit hook |
| `make tidy` / `make fmt` / `make fmt-check` / `make vet` | Go basics; `fmt-check` mirrors CI and modifies nothing |
| `make lint` | `golangci-lint config verify` + `golangci-lint` (v2 config) |
| `make metrics-lint` | Observability standard: registry self-check, rule references, `promtool check/test rules`, inventory drift |
| `make metrics-inventory` | Regenerate `docs/observability/metric-registry.md` from the registry |
| `make test` | All tests under `test/` (unit + integration + e2e), excluding live smoke |
| `make test-unit` / `make test-int` / `make test-e2e` | One suite |
| `make test-ci` / `make race` | All tests with `-race` |
| `make test-cover` | One `-race` run writing `coverage.out` (`-coverpkg=./internal/...,./pkg/...,./cmd/...`) |
| `make cover` / `make cover-func` / `make cover-check` | HTML report / per-function summary / ≥98% gate |
| `make test-smoke` | Smoke tests (in-process, or a live server at `SMOKE_BASE_URL`) |
| `make test-smoke-image` | CI's image smoke gate locally (docker + GNU timeout) |
| `make vuln-check` | `govulncheck` on `internal/` + `pkg/` |
| `make mod-verify` | `go mod verify` |
| `make build` | `bin/platform-gincommon` (demo) + `bin/metricslint`; verifies library packages compile |
| `make run` | Run the demo server |
| `make docker-up` / `make docker-down` | Start/stop the local observability stack |
| `make pin-base-images` | Re-pin Dockerfile `FROM` digests and write `.docker-digests` (docker required) |
| `make godoc` | pkgsite at http://localhost:8080 |
| `make ci` | `fmt-check` + `tidy` + `vet` + `lint` + `metrics-lint` + `cover-check` + `build` |
| `make clean` / `make help` | Remove `bin/`; list every target |

### Running a single test

Tests live under `test/`, a separate package tree, so coverage needs `-coverpkg` (the Makefile handles it):

```bash
go test ./test/unit/middleware/... -run TestPanicRecovery_PartialBodyNotAppendedTo -v
go test ./test/integration/... -run TestGRPC_StreamTimeoutInnermost_MetricsMatchClient -race -v
```

### Calling the demo server locally

The demo server (`make run`) mounts only the public routes:

```bash
curl -i http://localhost:8080/health      # 200 {"status":"ok"}, X-Request-ID and X-Trace-ID headers
curl -s http://localhost:8080/metrics | grep '^platform_build_info'
curl -i http://localhost:8080/panic       # dev only: recovered JSON 500, platform_http_panics_total +1

# Continue an upstream trace — X-Trace-ID echoes 4bf92f35...
curl -i http://localhost:8080/health \
  -H "traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
```

Against a service built from the HTTP example above, protected routes need gateway headers:

```bash
curl -i http://localhost:8080/api/items \
  -H "x-user-id: user-1" -H "x-tenant-id: tenant-1" -H "x-tenant-roles: user" \
  -H "x-request-id: my-req-001"

# Reference RBAC: delete requires the admin role (403 otherwise)
curl -i -X DELETE http://localhost:8080/api/items/42 \
  -H "x-user-id: user-1" -H "x-tenant-id: tenant-1" -H "x-tenant-roles: admin"

# 401 + "auth: request rejected" audit log; repeated identity → 401 ambiguous
curl -i http://localhost:8080/api/items -H "x-tenant-id: tenant-1"
```

### Developer tools

```bash
go run ./cmd/metricslint check -url http://localhost:8080/metrics    # validate a live scrape
go run ./cmd/metricslint check -strict -allow-proposed-owner platform-gincommon,platform-events,platform-pgcommon -url http://localhost:8080/metrics
go run ./cmd/metricslint refs docker/prometheus/rules                # legacy/unregistered names in rules
make godoc                                                           # browse the public API
```

---

## Testing observability locally

Every request through the stack produces a span, a metric observation and a log line; the local stack lets you see all three.

### How the pipeline works

```
demo server / your service (make run)
    │
    ├── spans ──OTLP/gRPC :4317──▶ otel-collector ──▶ Tempo (:3200)
    │
    ├── /metrics ◀──scrape── Prometheus (:9090)  ── recording rules + SLO burn-rate alerts (docker/prometheus/rules)
    │
    └── JSON logs ──LOG_FILE──▶ Promtail ──▶ Loki (:3100)
                                                     │
                                   Grafana (:3000) ◀─┴─ Tempo · Prometheus · Loki datasources
```

### Step 1 — Start the stack

```bash
make docker-up
```

| Container | Image | Host port(s) | Purpose |
|---|---|---|---|
| `otel-collector` | `otel/opentelemetry-collector-contrib` | `4317`, `4318` | OTLP gRPC/HTTP receiver → Tempo |
| `tempo` | `grafana/tempo:2.7.2` | `3200` | Trace store |
| `prometheus` | `prom/prometheus` | `9090` | Scrapes the app; loads the reference recording rules and alerts |
| `loki` | `grafana/loki` | `3100` | Log store |
| `promtail` | `grafana/promtail` | — | Ships `LOG_FILE` into Loki |
| `grafana` | `grafana/grafana` | `3000` | Dashboards / Explore |

Ports are overridable (`OTEL_COLLECTOR_GRPC_PORT`, `TEMPO_HTTP_PORT`, `PROMETHEUS_HTTP_PORT`, `LOKI_HTTP_PORT`, `GRAFANA_HTTP_PORT`).

### Step 2 — Run the app and generate traffic

```bash
# .env: APP_ENV=dev, OBSERVABILITY_DOMAIN=iam, LOG_FILE=logs/app.log
make run
for i in $(seq 1 20); do curl -s localhost:8080/health >/dev/null; done
curl -s localhost:8080/panic >/dev/null
```

`APP_ENV=dev` gives plaintext OTLP and 100% sampling, so every request is exported.

### Step 3 — Inspect each signal

| Signal | Where | Query |
|---|---|---|
| Metrics | http://localhost:9090 | `sum by (route, status_class) (rate(platform_http_requests_total[1m]))`, `platform_http_panics_total` |
| Traces | Grafana → Explore → Tempo | Search by service name, or paste an `X-Trace-ID` from a response |
| Logs | Grafana → Explore → Loki | `{job="platform-gincommon"} \|= "http_request"` — `trace_id`/`request_id` link a log line to its trace |
| Rules | `make metrics-lint` | `promtool check rules` + `promtool test rules` over `docker/prometheus/rules` |

### Troubleshooting observability

| Symptom | Likely cause | Fix |
|---|---|---|
| Startup panic `metrics: a domain is required` | No `Config.Domain` and no `OBSERVABILITY_DOMAIN` | Set one — a registry domain such as `iam` |
| Startup panic `environment "..." is not in the registry vocabulary` | `APP_ENV`/`ENVIRONMENT` unset or not `local\|dev\|test\|staging\|prod` | Set `Config.Environment` or `APP_ENV` to a registry value |
| Startup panic `ErrIdentityMismatch` | Two stacks in one process with different `ServiceName`/`Domain`/`Environment` | Give HTTP and gRPC stacks the same identity |
| Stderr `otel_exporter_init_failed` / no traces in Tempo | Collector unreachable, or TLS against a plaintext collector | `make docker-up`; outside dev set `OTEL_EXPORTER_OTLP_INSECURE=true` only for plaintext collectors |
| Traces appear as `service.name=unknown_service` | `InitTracingFromEnv` ran with no `OTEL_SERVICE_NAME`/`APP_NAME` and no stack with a `ServiceName` was built | Set `OTEL_SERVICE_NAME`, or use `InitTracingWithConfig` |
| Only ~10% of requests traced | Non-dev default ratio `0.1` | `OTEL_TRACES_SAMPLER_RATIO=1.0` locally |
| `grpc_method="unknown"` for real methods | `RegisterKnownMethods` called before a service (or `RegisterHealth`) was registered, or not called for a second server | Call it after every service is registered, for every server |
| `metricslint check -strict` fails on `proposed` | gincommon's `platform_*` entries await governance ratification | Add `-allow-proposed-owner platform-gincommon,platform-events,platform-pgcommon` |
| No logs in Loki | `LOG_FILE` unset | Set `LOG_FILE=logs/app.log` in `.env`, restart `make run` |

---

## Testing

### Canonical tests (do not break)

- **`test/unit/platformmetrics`** — gincommon's own emitted metrics validate against the registry with 0 errors; version sanitisation and identity rules.
- **`test/unit/identity`** — the one-identity-per-process contract through the public constructors, deliberately without `test/testenv` (which relaxes it for every other package).
- **`test/unit/gincommon/consumer_api_test.go`** — a consumer can implement `Authorizer` for both stacks using only `pkg/` imports.
- **`test/integration/grpc_server_test.go`** — a real bufconn gRPC server: health-path near-miss auth bypass, ambiguous identity, request-ID echo, unknown-method labels, stream timeout ending an idle `RecvMsg` loop, and metrics matching the client's `DeadlineExceeded`.
- **`test/unit/middleware/abort_identity_test.go`** + **`test/unit/gincommon/abort_chain_test.go`** — `http.ErrAbortHandler` through the full chain is a 499 client abort (never a panic or 5xx); a real panic is counted exactly once.
- **`test/unit/otel`** — the delegating provider: tracers captured before replacement or after `Shutdown` export to the active provider; concurrent swaps under `-race`.
- **`test/unit/problem`** + **`test/unit/gincommon/errors_test.go`** — the RFC 9457 contract: registry invariants and `docs/problem-details/registry.md` drift, `traceId` vectors shared with pgcommon-py, OpenAPI schema vs struct, and through the full stack: content type, required members, status mapping, validation paths/messages, no 5xx leaks, tenant and trace.
- **`test/integration/problem_details_test.go`** — problems pass through a reverse-proxy gateway unchanged; a gateway-generated problem; client parsing with `problem.FromResponse`.
- **`test/unit/customvalidator`** — a service-replaced `binding.Validator` is left alone (own process, because the field-name registration runs once).
- **`test/unit/uninit`** — metrics before initialisation: constructors fail, middleware and interceptors pass through. Valid only with `-count=1` (it checks process state).
- **`test/unit/middleware/authz_logging_test.go`** — a failing `Authorizer` is logged sanitized and never reaches the client.

### Coverage

`make cover-check` (CI: `coverage-gate.sh`, `COVERAGE_THRESHOLD`) fails below **98%** statement coverage measured with `-coverpkg=./internal/...,./pkg/...,./cmd/...` — tests live in `test/`, so plain `go test ./...` shows 0% for source packages. The current total is **99.5%**. Use `test/fixtures.MockLogger` to assert structured log output, and `metrics.InitWithRegistererOptions(opts, prometheus.NewRegistry())` (or the `test/fixtures` helpers) for isolated registries.

---

## Environment variables

Read by the library at startup in a consuming service (`.env-example` documents the demo's set):

| Variable | Default | Purpose |
|---|---|---|
| `OBSERVABILITY_DOMAIN` | — (**required** unless `Config.Domain`) | `domain` label; OTel `service.namespace` |
| `APP_ENV` / `ENVIRONMENT` | — | `environment` label (`local\|dev\|test\|staging\|prod`, trimmed + lowercased; `APP_ENV` first). `dev`/`local` select dev log format, debug level, plaintext OTLP, 100% sampling |
| `BUILD_VERSION` | `unknown` | `platform_build_info{version}`, OTel `service.version`; sanitised with a warning when invalid |
| `APP_NAME` / `OTEL_SERVICE_NAME` | — (**no library default**) | OTel `service.name` for `InitTracingFromEnv` (`OTEL_SERVICE_NAME` first); unset → warning + provisional `unknown_service` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC collector. An empty value counts as unset; `none`, `off` or `disabled` turns trace export off (spans stay in-process) |
| `OTEL_TRACES_EXPORTER` | — | Only `none` is honoured: it turns trace export off. The library always exports OTLP/gRPC otherwise |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` in `dev`/`local`, else `false` | Plaintext OTLP |
| `OTEL_TRACES_SAMPLER_RATIO` / `OTEL_TRACES_SAMPLER_ARG` | `1.0` dev/local, `0.1` otherwise | Sampling ratio `0.0`–`1.0` (`_RATIO` takes precedence; invalid → default + warning) |
| `OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED` | `false` | Re-sample remote-sampled parents at the ratio — enable on edge services |
| `OTEL_PROPAGATOR_BAGGAGE` | `false` | W3C baggage propagation |
| `LOG_LEVEL` | `debug` dev/local, else `info` | `debug\|info\|warn\|error`; invalid → default + one warning |
| `LOG_SAMPLING` | `false` | Zap sampling (100/s per message+level, then 1 in 100) |
| `LOG_FILE` | — | Also write JSON logs to this file (same level and sampling) |
| `APP_PORT` | `8080` | Demo server only |
| `SMOKE_BASE_URL` / `TEST_SERVICE_NAME` / `E2E_SERVICE_NAME` | — | Tests only |

Boolean `OTEL_*` values are case-insensitive (`true`/`1`/`yes`, `false`/`0`/`no`); anything else warns once and uses the default.

---

## Security

| Topic | Guidance |
|---|---|
| **Gateway trust boundary** | `x-user-id`, `x-tenant-id`, `x-tenant-roles` are trusted, not verified. The gateway must authenticate, strip client-supplied identity headers, and **replace** (not append) them; the service must be unreachable around the gateway |
| **Ambiguous identity** | Repeated `x-user-id`/`x-tenant-id` are rejected on both HTTP and gRPC; roles are joined then capped (4096 bytes, 64 roles) |
| **Authorization** | `NewDemoRBAC()` is reference-only (read allowed, delete needs `admin`, everything else denied). Production services implement `Authorizer` with tenant-scoped policy |
| **Outbound identity** | `PropagateHeaders` and `UnaryClientInterceptor` never forward identity; the identity variants are for trusted internal services only |
| **Health exemption** | gRPC auth/context/RBAC skip exactly `/grpc.health.v1.Health/{Check,Watch,List}` — an exact match, so `UnknownServiceHandler` paths under that prefix cannot bypass auth |
| **Log injection and redaction** | Header values are sanitized (control chars stripped, 256-byte cap); sensitive keys (`*_key`, `password`, `token`, ...) are redacted via `service.Sanitize`; panic values are sanitized in logs and spans |
| **Error leakage** | Clients get fixed messages; internal errors, panic values and authorizer errors go to logs only |
| **Trace sampling abuse** | Sampling is `ParentBased`; on edge services set `OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED=true` so a caller cannot force 100% sampling |
| **OTLP** | TLS unless `APP_ENV` is `dev`/`local` |
| **`/metrics`** | Often scraped unauthenticated — restrict via network policy, auth proxy or a dedicated admin port |
| **Supply chain** | Actions pinned to commit SHAs, Dockerfile bases digest-pinned, Trivy gate, Cosign keyless signing, SBOM + SLSA provenance. Vulnerability reporting: [SECURITY.md](SECURITY.md) |

---

## Observability

**SLOs.** The library defines no SLO of its own; `docker/prometheus/rules/` ships reference recording rules and multi-window burn-rate alerts over `platform_http_*` / `platform_grpc_*` (with `promtool` tests) that consuming services adopt.

### Metrics

Per the [Enterprise Platform Observability Standard](docs/observability/README.md)'s three-tier hierarchy, with `domain`/`service`/`environment` injected centrally as const labels — never declared at a call site, and `version` only on `platform_build_info`:

- **Tier 1 — `platform_*`** (emitted by this library): `platform_http_requests_total`, `platform_http_request_duration_seconds`, `platform_http_request_size_bytes`, `platform_http_response_size_bytes`, `platform_http_active_requests`, `platform_http_panics_total`, `platform_http_rate_limited_requests_total`, `platform_http_request_timeouts_total`; `platform_grpc_server_handled_total`, `platform_grpc_server_handling_seconds`, `platform_grpc_server_request_size_bytes`, `platform_grpc_server_response_size_bytes`, `platform_grpc_server_inflight_requests`, `platform_grpc_panics_total`, `platform_grpc_rate_limited_requests_total`; `platform_unclassified_errors_total{transport}` (both transports); `platform_build_info`. Messaging (`platform_messages_*`, `platform_dlq_*`) and DB metrics come from platform-events / platform-pgcommon under the same registry.
- **Tier 2 — `<domain>_*`** (shared within a domain — today `iam_auth_events_total`, `iam_tenant_resolution_total`, `iam_identity_sync_total`, `iam_realm_operations_total`, `iam_rls_violations_total`, `iam_auth_session_revoke_failed_total`, `iam_lifecycle_event_skipped_total`, `iam_lifecycle_event_lag_seconds`, all `proposed`): registry entries, built with `NewShared*`.
- **Tier 3 — `<domain>_<service>_*`** (one service only): free to add, validated at construction and by `metricslint`.

HTTP `method` is bounded (non-standard → `OTHER`); gRPC uses `grpc_method` so each label name has one meaning. `promhttp_metric_handler_errors_total` is exposed (exempt prefix) to alert on a failing custom collector. Full inventory: [metric-registry.md](docs/observability/metric-registry.md); runbooks: [runbooks.md](docs/observability/runbooks.md).

### Tracing and logs

Server spans per request/RPC with W3C propagation, `rpc.response.status_code` on gRPC spans, bounded span names, and trace resources carrying `service.name`, `service.version`, `service.namespace` (domain) and `deployment.environment.name`. Structured Zap logs — `http_request` / `grpc_request` (Info 2xx/OK, Warn 4xx/client codes incl. `DeadlineExceeded`, Error 5xx/server codes), `panic recovered`, `auth: request rejected`, `otel_tracing_initialized` — never carry credentials; identity fields are verified-only, with `claimed_*` for unverified values.

---

## Releases

### Nothing is deployed

This is a Go module. A release is a **git tag** that consumers pin with `go get ...@vX.Y.Z`; there is no deployment or deploy gate. The demo image exists to smoke-test the stack in CI and is never run in production.

| Artefact | Where | Purpose |
|---|---|---|
| Go module version | git tag `vX.Y.Z` | What consuming services pin |
| `metricslint` binaries + `.sha256`, `checksums.txt` + `checksums.txt.sigstore.json` (Cosign-signed) | GitHub Release assets | Run the observability checks in non-Go CI |
| Demo image (`X.Y.Z`, forward-only `X.Y` / `X`, no `latest`) | GHCR, Cosign-signed, SBOM + SLSA provenance | CI smoke target only |

### Versioning

SemVer over `pkg/*` ([VERSIONING.md](VERSIONING.md)); history in [CHANGELOG.md](CHANGELOG.md).

| Version | Status | `go get` | Notes |
|---|---|---|---|
| **v2.0.0** | Current | `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@v2.0.0` | RFC 9457 error contract, `/v2` module path; follow [Upgrading to v2.0.0](#10-upgrading-to-v200) |
| v1.4.x–v1.6.x | Previous major | `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@v1.6.0` | Security fixes only; v1.4.0 was a [recorded exception](VERSIONING.md#recorded-exceptions) ([Upgrading to v1.4.0](#11-upgrading-to-v140)) |
| v1.0.x–v1.3.x | Superseded | `@v1.3.0` | No further releases |

### Cutting a release

1. Rename `## [Unreleased]` to `## [X.Y.Z] - <date>` in `CHANGELOG.md` (`verify-changelog-entry.sh` fails the tag otherwise).
2. Merge to `main` — `verify-release-tag.sh` requires the tag to point at HEAD and be reachable from `origin/main`.
3. Tag `vX.Y.Z` (or `vX.Y.Z-rc.N`, which uses the base version's CHANGELOG section) and push; `release.yml` does the rest.

`release.yml` mirrors platform-pgcommon's and platform-events'. It runs in this order:

1. **Verify.** Checks the tag and the CHANGELOG first. The tag must be reachable from `origin/main`, and a manual `workflow_dispatch` must be started from `main` or from the tag itself.
2. **Gates.** Runs the same gates as `ci.yml` at the tag: tests, quality, Hadolint, Trivy, smoke and interop.
3. **Binaries.** Cross-compiles `metricslint` for five platforms, with checksums.
4. **Push.** Only after every gate passes does it push the demo image. It first proves, by content fingerprint, that this is the image Trivy scanned and smoke tested. It then signs the image (with SBOM and SLSA provenance) and signs `checksums.txt`.
5. **Publish.** Creates the GitHub Release, with notes extracted from `CHANGELOG.md`.

Image tags have no `v`. `X.Y.Z` is always set. `X.Y` and `X` are set only when the release is the newest stable version of that line, so a hotfix such as `v1.3.5` after `v1.4.0` does not move `1`. A pre-release gets only its own tag, and the demo image never gets `latest`.

**Verifying a release** (needs [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) ≥ 2.4). A tag-triggered release is signed by `release.yml@refs/tags/<tag>`. A re-run dispatched from `main` is signed by `release.yml@refs/heads/main`, which the regular expression below also accepts.

```bash
TAG=v1.4.1   # the release to verify
REPO=BCBP-SOLUTIONS-FZC-LLC/platform-gincommon
IDENTITY="^https://github\.com/${REPO}/\.github/workflows/release\.yml@refs/(tags/${TAG}|heads/main)$"

# metricslint: verify the signed checksums, then the binary against them
gh release download "$TAG" -R "$REPO" -p checksums.txt -p checksums.txt.sigstore.json \
  -p "metricslint_${TAG}_linux_amd64"
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt   # macOS: shasum -a 256 -c --ignore-missing

# Demo image (tag without the v)
cosign verify --certificate-identity-regexp "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "ghcr.io/bcbp-solutions-fzc-llc/platform-gincommon:${TAG#v}"
```

Each GitHub Release carries:
- the five `metricslint` binaries: `metricslint_<tag>_{linux,darwin}_{amd64,arm64}` and `metricslint_<tag>_windows_amd64.exe`
- `checksums.txt` and its Cosign bundle `checksums.txt.sigstore.json`
- the demo image's SLSA provenance (`provenance.slsa.json`)
- the demo image's CycloneDX SBOM (`sbom.cyclonedx.json`)

The workflow itself verifies with the looser `refs/(tags/v.+|heads/main)`; pinning the exact tag, as above, is stricter.

---

## CI

Five workflow files; shell logic lives in `.github/scripts/` (shellcheck-clean); every action is pinned to a full commit SHA; Dependabot version updates are **disabled** (`.github/dependabot.yml.disabled`, re-enable with `git mv .github/dependabot.yml.disabled .github/dependabot.yml`); Dependabot alerts and security updates are repository settings and stay on.

- **`ci.yml`** — orchestrator (push/PR to `main`, skipping Markdown/Mermaid-only commits): `validate-test` ∥ `validate-quality` ∥ `build-image` (Hadolint, `.dockerignore` check, cached amd64 build) → `trivy` (CRITICAL/HIGH/UNKNOWN fails, SARIF, CycloneDX SBOM) and `smoke` (image ≤ 60 MB; an invalid `OBSERVABILITY_DOMAIN` must exit non-zero with the registry error; `/health` ok and `/metrics` exposes `platform_build_info` / `platform_http_requests_total` with identity labels) → `interop` (platform-interop-tests, pinned SHA) → `pr-summary` (one updated PR comment, also on failure) → `push` (main only: demo image to GHCR, Cosign sign + verify).
- **`validate-test.yml`** (reusable) — `go mod download/verify` → `make test-cover` (unit + integration + e2e, `-race`, `-coverpkg`) → `coverage-gate.sh` (**≥ 98%**) → upload coverage.
- **`validate-quality.yml`** (reusable) — `go mod verify` → HTML-escaped-operator guard → `gofmt` → tidy drift → `go vet` → `golangci-lint config verify` + lint → `make metrics-lint` → `govulncheck` (library packages) → Dockerfile digest pins vs `.docker-digests`.
- **`changelog-check.yml`** — PRs touching `internal/`, `pkg/`, `cmd/` or `go.mod` must update `CHANGELOG.md` (diffed from the merge base).
- **`release.yml`** (`v*` tags or manual) — same job graph as platform-pgcommon's: `verify` (dispatch ref, tag on `main`, CHANGELOG section, `SOURCE_DATE_EPOCH`, floating-tag selection) → the `ci.yml` gates at the tag (`validate-test` ∥ `validate-quality` ∥ `build-image` → `trivy`, `smoke`, `interop`) + `binaries` (metricslint ×5) → `push` (pre- and post-push image fingerprint must match the image trivy and smoke judged; Cosign sign + verify; SLSA provenance) → `publish` (re-verified checksums, Cosign `sign-blob` of `checksums.txt`, GitHub Release).

**Required repository secret: `GO_PRIVATE_TOKEN`** — the interop job fetches sibling private modules with it.

---

## Docker

The only image is the **demo server** (`cmd/server`).

### Building the demo image

```bash
make docker-build
docker run --rm -p 8080:8080 -e APP_ENV=prod -e OBSERVABILITY_DOMAIN=iam platform-gincommon:local
```

Two-stage `Dockerfile`: digest-pinned `golang:1.26.9-alpine` builder, digest-pinned `alpine:3.21` runtime (`USER nobody`), `EXPOSE 8080`, `HEALTHCHECK` on `/health`. `alpine:3.21` reaches end of support around November 2026 — bump it and run `make pin-base-images`.

### Health

| Endpoint | Returns | Checks |
|---|---|---|
| `GET /health` (HTTP, `HealthHandler()`) | `200 {"status":"ok"}` | Pure liveness — never inspects a dependency; no auth |
| `grpc.health.v1.Health/Check` (`RegisterHealth`) | `SERVING` / per-service status | Set by the service through the returned `*health.Server`; no identity required |

### Minimum required environment variables

```bash
OBSERVABILITY_DOMAIN=iam          # or Config.Domain
APP_ENV=prod                      # local|dev|test|staging|prod
BUILD_VERSION=v2.0.0
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector.observability:4317
```

---

## Platform dependencies

### 1. Consuming services (services → this library)

All 11 IAM Go services import `pkg/gincommon`, and `iam-authz-enrichment` also uses `pkg/grpccommon` (`Config`, `ObservabilityUnaryInterceptors`, `Shutdown`). As of 2026-10-16 ten pin `v1.6.0` and `iam-org-membership` pins `v1.5.0`; none is on `/v2` yet (see *v2.0.0 migration status* below). Every service uses `Config`, `ObservabilityMiddlewares`, `TimeoutMiddleware`, `InitTracingFromEnv`, `Shutdown` and `MetricsRegisterer`; beyond that (non-test code, comments excluded):

| Service | Also uses |
|---|---|
| `iam-org-membership` | `ProtectedMiddlewares`, `NewSharedCounterVec`, `NewSharedHistogramVec` |
| `iam-audit-log` | `ProtectedMiddlewares`, `NewSharedCounterVec`, `NewSharedGaugeVec`, `NewSharedHistogramVec` |
| `iam-realm-provisioner` | `ProtectedMiddlewares`, `NewSharedHistogramVec` |
| `iam-delegation` | `ProtectedMiddlewares`, `NewSharedCounterVec`, `NewSharedHistogramVec`, **`PropagateHeaders` (4 outbound clients)** |
| `iam-group-mapping` | `ProtectedMiddlewares`, `NewCounterVec`, `NewGaugeVec`, `MetricsHandler`, `InitTracing`, `NewTracer`, `NoopTracer`, `NewSpanTracer`, `InjectTraceContext`, `RecordSpanError`, `SpanTraceID` |
| `iam-tender-acl` | `ProtectedMiddlewares`, `NewCounterVec`, `MetricsHandler` |
| `iam-catalog-admin`, `iam-token-service`, `iam-user-profile` | `ProtectedMiddlewares` |
| `iam-authz-enrichment` | `grpccommon.ObservabilityUnaryInterceptors` (no `ProtectedMiddlewares`) |
| `iam-event-consumer` | observability only (no `ProtectedMiddlewares`) |

No service calls `LabeledRegisterer` outside tests. Only `iam-delegation` calls `PropagateHeaders` to reach other IAM services; calls that rely on identity forwarding must use `PropagateIdentityHeaders`.

**v2.0.0 migration status** (non-test code in the services' working trees, 2026-10-16). Each service needs the `/v2` import path and the RFC 9457 handler changes in [Upgrading to v2.0.0](#10-upgrading-to-v200):

| Service | `gincommon.ErrorResponse` uses | Hand-written `c.JSON`/`c.AbortWithStatusJSON` error bodies | Timeout |
|---|---|---|---|
| `iam-group-mapping` | 86 | 2 | `Config.RequestTimeout` |
| `iam-delegation` | 65 | 0 | `TimeoutMiddleware` |
| `iam-user-profile` | 0 | 62 | `TimeoutMiddleware` |
| `iam-org-membership` | 2 | 11 | `Config.RequestTimeout` |
| `iam-catalog-admin` | 0 | 10 | `TimeoutMiddleware` |
| `iam-realm-provisioner` | 0 | 10 | `Config.RequestTimeout` |
| `iam-token-service` | 0 | 4 | `Config.RequestTimeout` |
| `iam-audit-log` | 1 | 3 | `TimeoutMiddleware` |
| `iam-event-consumer` | 2 | 2 | `Config.RequestTimeout` |
| `iam-authz-enrichment` | 0 | 1 | `TimeoutMiddleware` |
| `iam-tender-acl` | 1 | 0 | `Config.RequestTimeout` |

The five services still calling `TimeoutMiddleware` directly must register it after `ObservabilityMiddlewares` (or switch to `Config.RequestTimeout`), otherwise metrics and logs record a 200 while the client gets 503 ([LLD §16 OQ-18](docs/lld/platform-gincommon-lld.md#16-open-questions-and-sign-off-register)).

### 2. Sibling libraries

| Library | Direction | Contract |
|---|---|---|
| `platform-events` | registry ← | Its released registry is imported verbatim; emits `platform_messages_*` / `platform_dlq_*` itself |
| `platform-pgcommon` | registry ←, tracer → | Registry imported verbatim; consumes `gincommon.NewSpanTracer` as `Config.Tracer` |
| `platform-fastapicommon` | registry → | Vendors `registry.json` — must be re-copied on every registry change |
| `platform-interop-tests` | CI → | Cross-library interop suite run by the `interop` job |

### 3. Infrastructure dependencies

| System | Role | On failure |
|---|---|---|
| **OTel collector** (OTLP/gRPC) | Span export | Exporter creation failure → warning + in-process provider (spans keep valid IDs, nothing exported); runtime export errors go to `Config.Logger` as `otel_error` |
| **Prometheus** | Scrapes `/metrics` | No effect on request handling; a failing collector leaves the rest of the scrape intact (`ContinueOnError`) |
| **Log pipeline** | Ships stdout/`LOG_FILE` | No effect on request handling |
| **API gateway** | Authenticates and injects identity headers | Without it, protected routes return `401` — never trust a request that bypassed it |

---

## Out of scope

| Concern | Where it lives |
|---|---|
| JWT validation, OAuth, API keys, MFA | API gateway / Keycloak |
| Business authorization policy | Each service's `Authorizer` (IAM / OPA) |
| Database access, pooling, RLS, migrations | `platform-pgcommon` |
| Outbox, SNS/SQS, schema registry | `platform-events`, `platform-schemagov` |
| Rate limiting, circuit breaking, retries | API gateway / service mesh |
| Request body binding and business validation | Your service handlers |
| Collector, Tempo, Prometheus, Loki, Grafana in production | Platform observability infrastructure |
| Metric governance decisions (ratifying `proposed` entries) | Observability governance — this repo records them in `registry.json` |

Keeping scope to **transport, observability and identity context** keeps the library safe to upgrade across every service without business-logic risk.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, extending the library (new middleware, new metrics, replacing the logger or authorizer) and the PR checklist.

| Document | Description |
|---|---|
| [`.claude/CLAUDE.md`](.claude/CLAUDE.md) | Guidance for Claude Code in this repo — design decisions and invariants |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Layer model, HTTP/gRPC flow diagrams, invariant tables |
| [`docs/lld/platform-gincommon-lld.md`](docs/lld/platform-gincommon-lld.md) | Low-level design — API contract, limits, algorithms, error taxonomy, open questions |
| [`docs/architecture/`](docs/architecture/README.md) | Architecture docs and Mermaid sources |
| [`docs/observability/README.md`](docs/observability/README.md) | The observability standard as implemented, CI enforcement, strict mode |
| [`docs/observability/metric-registry.md`](docs/observability/metric-registry.md) | Generated registry inventory (`make metrics-inventory`) |
| [`docs/observability/migration.md`](docs/observability/migration.md) | Legacy → `platform_*` name mapping |
| [`docs/observability/runbooks.md`](docs/observability/runbooks.md) | Alert runbooks |
| [VERSIONING.md](VERSIONING.md) | SemVer rules, recorded exceptions, supported releases |
| [CHANGELOG.md](CHANGELOG.md) | Per-version changes and migration steps |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting and trust model |

---

## License / ownership

BCBP Solutions FZC LLC — internal platform shared library. Not for external distribution.
