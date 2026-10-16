# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repo Is

`platform-gincommon` is a **private Go shared library** (module: `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2` since v2.0.0, `go 1.26.0` directive with `toolchain go1.26.9`) that provides reusable Gin HTTP and gRPC middleware for platform services. It lives in a **private GitHub repository** and is consumed as a Go module dependency by internal platform services — it is never deployed as a standalone server.

**Consuming services** must set `GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*` (or `GONOSUMDB`/`GOFLAGS` equivalents) to fetch private module versions. The `cmd/server/main.go` binary is a **reference/demo only** — it exists to exercise the middleware stack locally. The Docker image CI pushes the demo server container to GHCR; it is **not** a production artefact.

See [`ARCHITECTURE.md`](../ARCHITECTURE.md) for detailed flow diagrams and invariant tables.

## Common Commands

```bash
make setup        # Copy .env-example → .env and install git hooks (run once before anything else)
make install-hooks # Install .githooks/pre-commit (tidy, fmt-check, lint, observability registry + inventory drift)
make tidy         # go mod tidy
make fmt          # go fmt ./...
make vet          # go vet ./...
make lint         # golangci-lint
make test         # All tests (unit + integration + e2e), excludes smoke
make test-ci      # All tests with race detector (used in CI)
make test-unit    # Unit tests only
make test-int     # Integration tests only
make test-e2e     # E2E tests only
make test-smoke   # Smoke tests (requires live server at SMOKE_BASE_URL)
make race         # Alias for test-ci (all tests with -race)
make test-smoke-image # CI image smoke gate locally (docker + GNU timeout)
make vuln-check   # govulncheck internal/ + pkg/ (alias: vulncheck)
make mod-verify   # go mod verify
make godoc        # pkgsite at http://localhost:8080
make build        # Compile to bin/platform-gincommon
make run          # Run the example reference server
make cover        # Coverage HTML report (measures ./internal/... ./pkg/... ./cmd/...)
make cover-func   # Coverage summary by function (terminal)
make ci           # fmt-check + tidy + vet + lint + metrics-lint + cover-check (race + coverage) + build
make test-cover   # race + coverage run writing coverage.out (CI test gate input)
make pin-base-images # re-pin Dockerfile FROM digests + write .docker-digests (requires docker)
make metrics-lint # Observability standard: registry self-check, rule refs, promtool, inventory drift
make metrics-inventory # Regenerate docs/observability/metric-registry.md from the registry
make docker-up    # Start local observability stack (Prometheus, Grafana, Tempo, Loki)
make docker-down  # Stop observability stack
make clean        # Remove bin/ artefacts
```

To run a single test:
```bash
go test ./test/unit/metrics/... -run TestMetricsInit -v
go test ./test/integration/... -run TestMiddlewareChain -v
```

**Coverage note:** tests live under `test/` (a separate package tree from sources). Always use `-coverpkg=./internal/...,./pkg/...` to get meaningful numbers; running `go test ./...` without it shows 0% for source packages. `make cover` and `make cover-func` handle this correctly.

## Architecture

The library follows **Clean Architecture** — dependencies point inward; outer layers depend on inner layers, never the reverse.

```
pkg/            ← public API surface (consumers import these)
internal/
  core/
    domain/     ← Entities: RequestContext, domain errors (no external deps)
    port/       ← Interfaces: Logger, Authorizer (owned by the use-case layer)
    service/    ← Use Cases: ContextService, RBACService, LogSanitizer
  adapter/
    inbound/    ← HTTP middleware, gRPC interceptors
    outbound/   ← Zap logger, Prometheus metrics, OTel tracing
  config/       ← Environment variable loading
  server/       ← Reference server wiring (demo only)
```

**Dependency rule:** `domain` ← `port` ← `service` ← `adapter` ← `pkg`. The `core/` layers never import `adapter/` or framework packages. Interfaces in `core/port/` are implemented in `adapter/outbound/` and injected inward.

### Public API (`pkg/`)

**`pkg/gincommon`** — what consuming services import:
- `Config{Logger, ServiceName, BuildVersion, Domain, Environment, Tracing, RequestTimeout, ProblemTypeBase}` — configure the stack. `ProblemTypeBase` is the RFC 9457 type URI base (default `https://errors.company.com`). `RequestTimeout > 0` appends `TimeoutMiddleware` as the innermost observability handler, so metrics/logs/spans record the same 503 the client gets. `ServiceName` is **required** (panics if empty — prevents invalid Prometheus labels).
- `DefaultMiddlewares(Config)` — full stack: observability + auth + tenant context
- `ObservabilityMiddlewares(Config)` — panic recovery, request ID, tracing, metrics, logging
- `ProtectedMiddlewares(Config)` — auth validation + context building only
- `TimeoutMiddleware(duration)` — per-request context deadline; zero/negative is a no-op; only `DeadlineExceeded` (not client cancel) writes 503; does not interrupt the handler goroutine. Prefer `Config.RequestTimeout`; if used directly, register it **after** `ObservabilityMiddlewares`
- `HealthHandler()` — returns 200 `{"status":"ok"}`; register before `ProtectedMiddlewares` (no auth)
- `AbortWithProblem(c, err)`, `ClassifyError(err)`, `BindingError(err)`, `ShouldBindJSON(c, obj)`, `ShouldBind(c, obj)` — RFC 9457 errors. Handlers `c.Error(err)` + `return` (rendered by the global error handler) or `AbortWithProblem`. `ErrorResponse` was removed in v2.0.0
- `PropagateHeaders(c, req)` — injects `traceparent`/`tracestate` and `x-request-id` only; safe for any destination
- `PropagateIdentityHeaders(c, req)` — additionally sends `x-user-id`/`x-tenant-id`/`x-tenant-roles` and baggage; trusted internal services only
- `RequirePermission(action, resource, auth)` — per-route RBAC, no logging
- `RequirePermissionWithLogger(action, resource, auth, logger)` — per-route RBAC with audit log entries on denial
- `InitTracingWithConfig(TracingConfig{ServiceName, BuildVersion, Domain, Environment, Logger, Tracing})` (`ErrServiceNameRequired`), `InitTracing()`, `EnsureTracing()`, `InitTracingFromEnv()` — OpenTelemetry OTLP/gRPC setup. No library default service name: without `OTEL_SERVICE_NAME`/`APP_NAME` it warns and exports `unknown_service` provisionally, replaced by the first stack built with a `ServiceName`
- `NewTracer(scope)`, `NoopTracer()`, `Tracer`/`Span` aliases, `SpanTraceID(ctx)`, `RecordSpanError(ctx, err, desc)`, `InjectTraceContext(ctx, header)`, `NewSpanTracer(scope)` (a `StartSpan` hook, e.g. pgcommon's `Config.Tracer`) — tracing outside handlers, with no OpenTelemetry import in consumers
- `Config.Domain` (required; else `OBSERVABILITY_DOMAIN`) / `Environment` (else `APP_ENV` → `ENVIRONMENT`, no aliases) — Enterprise Platform Observability Standard identity: `platform_*` metrics only, with `{domain, service, environment}` const labels (`version` only on `platform_build_info`); missing or invalid identity panics at startup. The pre-standard `http_*`/`grpc_*`/`build_info` names were removed (no compatibility period: nothing was deployed)
- `MetricsHandler()`, `MetricsGatherer()`, `NewCounterVec`/`NewCounter`/`NewGaugeVec`/`NewGauge`/`NewHistogramVec` (Tier 3, validated: `<domain>_<service>_` prefix, suffixes, prohibited labels), `NewSharedCounterVec`/`NewSharedGaugeVec`/`NewSharedHistogramVec(reg, name)` (Tier 1/2 from the registry), `LabeledRegisterer(reg)`, plus type aliases — no Prometheus import in consumers
- `pkg/logger.Default()` — process-wide gincommon logger for code paths without an injected one

**`pkg/problem`** — RFC 9457 Problem Details, stdlib only (implementation in `internal/problem`, facade here): `Problem`/`FieldError` (`type,title,status,detail,instance` + `traceId,errorCode,tenantId,timestamp,validationErrors`), the type registry (`Type`, 15 `Type*` platform types, `MustType(domain,name,title,status,code)` for domain types, `TypeForStatus`; documented in `docs/problem-details/registry.md`, which `test/unit/problem` checks for drift), `*Error` + `New*Error` constructors (cause never sent), `FromError` (ErrUnsupported 501, DeadlineExceeded 503 `REQUEST_TIMEOUT`, MaxBytesError 413, else 500 without detail), `Builder` (strips detail from `internal-error`), `ResolveTraceID` (traceparent → sanitized x-request-id → span → generated: platform-pgcommon-py's order and vectors), `FromResponse`/`Decode` for clients, `OpenAPIComponents()`/`StandardResponses()`. Contract: `docs/problem-details/README.md`.

**`pkg/obsregistry`** — the Platform Observability Registry (`internal/obsregistry/registry.json`, embedded) and compliance checks: `Validate(gatherer, opts)`, `ValidateText`, `Default`, `Parse`, `ServicePrefix`. `cmd/metricslint` (`check`, `refs`, `registry`, `inventory`) is the CLI over the same code. See `docs/observability/`.

**`pkg/grpccommon`** — for gRPC services:
- `Config{ServiceName, BuildVersion, Logger, Domain, Environment}` — `ServiceName` and `Domain` required; `Logger` enables logging and panic recovery; observability-standard fields as in `gincommon.Config`
- `ObservabilityUnaryInterceptors(cfg)` / `ObservabilityStreamInterceptors(cfg)` — panic recovery, request ID, tracing, metrics, logging
- `ProtectedUnaryInterceptors(cfg)` / `ProtectedStreamInterceptors(cfg)` — `RequireAuth` + `ContextInterceptor`
- `FullUnaryInterceptors(cfg)` / `FullStreamInterceptors(cfg)` — full stack: observability + protected
- `DefaultUnaryInterceptors(cfg)`, `DefaultStreamInterceptors(cfg)` — aliases for `Observability*` (backward compatible)
- `RequirePermission(action, resource, auth)` — per-RPC RBAC unary interceptor (requires `ProtectedUnaryInterceptors`)
- `RequirePermissionWithLogger(action, resource, auth, logger)` — per-RPC RBAC with audit logging
- `RequestContextFromContext(ctx)` — retrieves `*domain.RequestContext` from gRPC handler context
- `RequestIDFromContext(ctx)` — retrieves correlation request ID from gRPC handler context
- `RequireStreamPermission*` — stream counterparts of `RequirePermission*`. All four (and auth/context) let exactly `/grpc.health.v1.Health/Check`, `/Watch` and `/List` through without identity — an exact match, not a prefix, so `UnknownServiceHandler` paths under that name can't bypass auth
- `RegisterKnownMethods(srv)` — call after registering services, before `Serve`; bounds `grpc_method`/span names to registered methods (anything else → `unknown`). Without it, names not matching `^/[A-Za-z0-9_.]+/[A-Za-z0-9_]+$` or > 256 bytes are already `unknown`
- `RegisterHealth(srv)`, `TimeoutUnaryInterceptor(d)`/`TimeoutStreamInterceptor(d)` (cap deadline, keep earlier client deadline; place **innermost**, after `Full*`, so observability records the client's `DeadlineExceeded` — the stream variant runs the handler in a goroutine and abandons it at the deadline)
- `UnaryClientInterceptor()`/`StreamClientInterceptor()` — outbound trace context + `x-request-id`; `UnaryClientIdentityInterceptor()`/`StreamClientIdentityInterceptor()` — opt-in identity forwarding from the `RequestContext`

### Middleware Execution Order

When using `DefaultMiddlewares`, handlers run in this order:
1. `PanicRecoveryMiddleware` — recover panics, log (stderr fallback when logger is nil), record metrics
2. `RequestIDMiddleware` — generate/read `x-request-id`, echo in response
3. `TracingMiddleware(serviceName)` — OpenTelemetry HTTP span scoped to `serviceName`, W3C `traceparent` propagation
4. `CorrelationHeadersMiddleware` — sets `X-Trace-ID`/`X-Request-ID` response headers **before** `c.Next()` (headers set after the body is written never reach the client; tests assert on `w.Result().Header`)
5. `MetricsMiddleware(logger)` — Prometheus HTTP metrics; the 429 warn log is sampled to one per route per `RateLimitLogInterval` (1s) with a `suppressed` count
6. `LoggingMiddleware` — structured Zap `http_request` log after response; nil logger is a complete no-op; logs Info for 2xx, Warn for 4xx, Error for 5xx; `duration_ms` field is float64 milliseconds; `tenant_id`/`user_id` only for verified identity, unverified headers as `claimed_tenant_id`/`claimed_user_id`
6a. *(Optional)* `TimeoutMiddleware` via `Config.RequestTimeout` — writes a 503 `service-unavailable` problem
6b. `ProblemMiddleware` (internal; the global error handler) — innermost observability handler: stores the problem settings, then renders the last `c.Error` (`ClassifyError`; `context.Canceled` → 499 without body) or a bodiless error status (Gin's 404 `ROUTE_NOT_FOUND`/405 included) as a problem; never touches a written response. `middleware.WriteProblem` is the only HTTP error writer; it logs one warn `problem response` {traceId,type,status,errorCode,service,tenantId}
7. `RequireAuth(logger)` — validate `x-user-id` + `x-tenant-id` headers (401 if missing); logs warn `auth: request rejected`
8. `ContextMiddleware(svc, logger)` — build `RequestContext{TenantID,UserID,Roles,TraceID,ClientIP}`, store in Gin context; `x-tenant-roles` over 4096 bytes or 64 roles → 401
9. *(Optional)* `AuthZMiddleware(auth, action, resource, logger)` — per-route RBAC; logs warn on 403, error on 500

### Required HTTP Headers (Protected Routes)

| Header | Required | Notes |
|--------|----------|-------|
| `x-user-id` | Yes | Injected by trusted API gateway — not cryptographically verified |
| `x-tenant-id` | Yes | Injected by trusted API gateway |
| `x-tenant-roles` | Recommended | Comma-separated roles (e.g., `admin,user`) |
| `x-request-id` | Optional | Correlation ID — generated if absent |
| `traceparent` | Optional | W3C trace context |

Identity headers are trusted entirely at the network layer. The consuming service is responsible for ensuring requests cannot bypass the gateway.

### RBAC Logic

`internal/core/service/authz_service.go` is intentionally simple and default-deny: `"read"` action is always permitted for any role; `"delete"` action requires the `"admin"` role; **all other actions (`"write"`, `"create"`, `"update"`, etc.) are denied regardless of role**. This is a reference/demo implementation only — production services must implement the `Authorizer` interface in `internal/core/port/authorizer.go` — `Authorize(ctx context.Context, rc *domain.RequestContext, action, resource string) error` (`nil` allows, `domain.ErrForbidden` denies, other errors are internal). Consumers name these through the `pkg/` aliases in `types.go`: `gincommon.Authorizer`/`RequestContextData`/`ErrForbidden`/`Logger`/`DemoRBAC` and `grpccommon.Authorizer`/`RequestContextData`/`ErrForbidden`/`Logger`/`DemoRBAC` (named `RequestContextData` in both because `gincommon.RequestContext` is the accessor func); `test/unit/gincommon/consumer_api_test.go` pins that a consumer needs no `internal/` import — and inject it via `RequirePermission`. Use `gincommon.NewDemoRBAC()` or `grpccommon.NewDemoRBAC()` in tests and demos.

### Metrics Initialization

`metrics.InitWithOptions(metrics.Options{ServiceName, BuildVersion, Domain, Environment})` is transactional and retryable (mutex + atomically published collector set; on a registration failure nothing is published). The same identity again is a no-op; a different domain/service/environment/version returns `metrics.ErrIdentityMismatch`; a missing domain is `metrics.ErrDomainRequired`; an empty/`unknown` version on a later call inherits the established one; an invalid build version is sanitised by `metrics.SanitizeVersion` (warn once; fails only if empty). `Registry.CheckVersion` remains for `metricslint`. `pkg/gincommon`/`pkg/grpccommon` call `metrics.MustInitFromConfig`, which applies `ApplyEnvDefaults` (`BUILD_VERSION`, `OBSERVABILITY_DOMAIN`, `APP_ENV` → `ENVIRONMENT`) and panics on a missing or rejected identity. For tests that need an isolated registry use `metrics.InitWithRegistererOptions(opts, prometheus.NewRegistry())`, or the `test/fixtures` helpers `InitMetrics` / `InitMetricsWithRegisterer` (test identity `iam`/`test`). `test/testenv` defaults `OBSERVABILITY_DOMAIN=iam` and `ENVIRONMENT=test` for packages that build the full stack, and calls `metrics.AllowIdentityChangeForTesting(true)` so one test process can build stacks with different service names (the mismatch check itself is tested without testenv in `test/unit/uninit` and, through the public constructors, `test/unit/identity`).

Middleware records through `metrics.RecordHTTP`/`RecordHTTPPanic`/`HTTPActiveInc`/`RecordGRPC`/`GRPCInflightInc`/... (`record.go`, each nil-safe). The exported collector vars (`metrics.HttpRequestsTotal`, ...) are the `platform_*` collectors; their names, help, labels and buckets come from the registry (`registryBuilder` / `initCollectors` in `metrics.go`). They are a read-only view for tests — recording reads the atomic collector set. There are no legacy collectors. `MetricsHandler` uses `promhttp.ContinueOnError`.

### Key Configuration Defaults

`OtelExporterInsecure` defaults to **false** (TLS) unless `APP_ENV` is explicitly `"dev"` or `"local"` (`IsDevEnv`; `development` is not a dev env and is rejected by the registry). An unset `APP_ENV` is treated as secure. Set `OTEL_EXPORTER_OTLP_INSECURE=true` to override.

The sampler is always `ParentBased(TraceIDRatioBased)`. `OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED=true` (or `TracingOptions.IgnoreRemoteParentSampled`) re-samples remote-sampled parents at the ratio so external callers can't force 100% sampling — enable on edge services. Trace resources carry `service.namespace` (domain) and `deployment.environment.name`. Zap sampling is off by default (`LOG_SAMPLING=true` enables); `LOG_LEVEL` sets the level.

The demo router calls `r.SetTrustedProxies(nil)` — consuming services behind a real proxy should call `r.SetTrustedProxies([]string{"<proxy-cidr>"})` on their engine.

### Observability Stack (local dev)

`make docker-up` starts: OTel Collector (gRPC:4317, HTTP:4318) → Tempo → Prometheus (9090) → Loki + Promtail → Grafana (3000).

## Key Environment Variables

`.env-example` (copied to `.env` by `make setup`) sets only values that differ from a built-in default: `OBSERVABILITY_DOMAIN=iam`, `APP_ENV=dev`, `LOG_FILE=logs/app.log`. Everything else is documented there as commented overrides. `BUILD_VERSION` is deliberately unset so the Makefile stamps `git describe` (the Makefile `-include`s `.env` before its `?=` defaults, so a value in `.env` wins).

Read by the library (consumer-facing):

```
OBSERVABILITY_DOMAIN, APP_ENV → ENVIRONMENT, BUILD_VERSION
OTEL_SERVICE_NAME → APP_NAME (no library default), OTEL_EXPORTER_OTLP_ENDPOINT (localhost:4317; none/off/disabled or OTEL_TRACES_EXPORTER=none disables export)
OTEL_EXPORTER_OTLP_INSECURE (default true only when APP_ENV=dev/local)
OTEL_TRACES_SAMPLER_RATIO → OTEL_TRACES_SAMPLER_ARG, OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED, OTEL_PROPAGATOR_BAGGAGE
LOG_LEVEL (debug|info|warn|error), LOG_SAMPLING (default false), LOG_FILE
```

Demo / tests / tooling only (defaults in `internal/config` or the Makefile): `APP_PORT`, `SMOKE_BASE_URL`, `TEST_SERVICE_NAME`, `E2E_SERVICE_NAME`, `GO`.

Local docker-compose stack only — **never read by the library**, defaults in `docker-compose.yml`/Makefile: `OTEL_COLLECTOR_GRPC_PORT`, `OTEL_COLLECTOR_HTTP_PORT`, `LOKI_HTTP_PORT`, `PROMETHEUS_HTTP_PORT`, `GRAFANA_HTTP_PORT`, `PROMETHEUS_SCRAPE_TARGET` (substituted into `docker/prometheus/prometheus.yml.tpl` by the Makefile).

## Test Layout

- `test/unit/` — isolated unit tests per package (metrics, middleware, logger, grpc, config, service, etc.)
- `test/integration/` — full middleware chain on an in-process HTTP server
- `test/e2e/` — full server startup
- `test/smoke/` — optional; requires a live server (`SMOKE_BASE_URL`)
- `test/fixtures/` — shared test helpers (MockLogger, HTTP helpers, error response decoder)
- `test/testenv/` — loads `.env` (if present) for tests and defaults `OBSERVABILITY_DOMAIN=iam`, `ENVIRONMENT=test`

## Key Design Decisions

**Nil-safe logger and metrics** — every middleware that accepts a `port.Logger` checks for `nil` before calling it. `LoggingMiddleware(nil)` is a complete no-op (passes through without logging). `PanicRecoveryMiddleware(nil)` falls back to `log.Printf` on stderr. All gRPC logging and panic-recovery interceptors also check for nil logger. Prometheus collectors are only recorded once metrics are initialized (`metrics.InitWithOptions` / `MustInitFromConfig`) — including the inner panic-recovery defer in `MetricsMiddleware`. Consumers that skip metrics or logging are unaffected — nothing panics. `Fatal` is excluded from `port.Logger` because it calls `os.Exit(1)` and belongs in `main()`, not middleware.

**Prometheus label cardinality** — `HTTPRoute` always returns the Gin route template (e.g. `/api/items/:id`) or the constant `"unknown"` for unregistered routes. The raw `c.Request.URL.Path` is never used as a Prometheus label — scanner probes and path-parameter abuse cannot explode cardinality.

**Production-safe defaults** — `AppEnv()` returns `""` when `APP_ENV` is unset so `OtelTraceSampleRatio` defaults to `0.1` (not `1.0`). Deployments that omit `APP_ENV` do not silently run at 100% trace sampling.

**gRPC span attributes** — `splitFullMethod` uses `strings.TrimLeft` + `strings.LastIndex` to split `info.FullMethod` into `rpc.service` and `rpc.method`. Bare method names (no slash) return `("", method)` so `rpc.method` is always correct. Empty or slash-only input returns `("", "unknown")` so `rpc.method` is never an empty Prometheus label. Double-slash proxy edge cases are also handled.

**gRPC span status on server errors** — `UnaryTracingInterceptor` and `StreamTracingInterceptor` call `span.SetStatus(otelcodes.Error, err.Error())` for all non-nil handler errors (not only panics). Per the OTel spec, `RecordError` alone does not change span status; the explicit `SetStatus` call ensures Tempo/Jaeger display non-OK gRPC responses as error spans, enabling trace-based 5xx alerting.

**gRPC logging fires on panic** — `UnaryLoggingInterceptor` uses named returns + a `panicked` sentinel so that a panicking handler still emits a `grpc_request` log entry (tagged as `codes.Internal`) before the panic propagates to the recovery interceptor.

**Panics counted in all HTTP metrics and logs** — both `MetricsMiddleware` and `LoggingMiddleware` use `panicked`-sentinel defers. `MetricsMiddleware` overrides code to 500 only when `panicked == true` so deliberate `c.Abort()` is recorded accurately. `LoggingMiddleware` emits an `http_request` log entry for panicking requests (status=500), matching the gRPC interceptor behaviour.

**gRPC observability parity on panic** — `UnaryMetricsInterceptor`, `StreamMetricsInterceptor`, `UnaryTracingInterceptor`, and `StreamTracingInterceptor` all use `panicked`-sentinel defers. Panicking handlers are counted in `platform_grpc_server_handled_total`, observed in duration histograms, and have their OTel spans annotated with the **real panic value** (e.g. "nil pointer dereference") via an inner `defer recover()+re-panic` pattern — not a static `"panic"` string.

**Log injection prevention** — `PanicRecoveryMiddleware` applies `sanitizeLogValue` to `c.Request.URL.Path` before logging. `isSensitiveLogKey` uses `k == "key" || strings.HasSuffix(k, "_key")` to cover `signing_key`, `enc_key`, `hmac_key` without false-positive redaction of `monkey`, `hockey`, or `key_metrics`. gRPC `UnaryPanicRecoveryInterceptor`, `StreamPanicRecoveryInterceptor`, and `logGRPCRequest` all pass log field maps through `service.Sanitize` before emitting, matching the HTTP path's redaction guarantees.

**`duration_ms` is true float64 milliseconds** — both `LoggingMiddleware` and `logGRPCRequest` compute `duration_ms` as `float64(elapsed.Nanoseconds()) / 1e6` (not `float64(elapsed.Milliseconds())`). This preserves sub-millisecond precision: a 400 µs request logs `0.4` not `0`. `PanicRecoveryMiddleware` uses the same key (`"duration_ms"`) and computation so panic and normal request log entries have a consistent schema.

**gRPC `isClientError` matches metrics classification** — `isClientError` in `UnaryLoggingInterceptor`/`StreamLoggingInterceptor` includes `codes.DeadlineExceeded` (matching `GRPCStatusClass`/`GRPCRequestOutcome` which classify it as a client error). Timed-out RPCs log at `Warn` (not `Error`) to match how they are counted in Prometheus.

**gRPC `TraceID` in `RequestContext`** — `injectRequestContext` extracts the OTel trace ID from the active span via `trace.SpanFromContext(ctx).SpanContext().TraceID().String()` when `UnaryTracingInterceptor` ran first and the span context is valid. `rc.TraceID` is populated for all gRPC handlers that use the full or protected interceptor stack.

**`ProtoMessageSize` typed-nil safe** — `reflect.ValueOf(m).Kind() == reflect.Pointer && rv.IsNil()` guards against a `(*pb.Response)(nil)` typed nil that bypasses the plain `v == nil` check.

**OTel spans flushed on all demo-server exits** — `cmd/server/main.go` calls `shutdownTracing()` explicitly before every `os.Exit(1)` path rather than relying on `defer` which `os.Exit` bypasses.

**gRPC repeated-role metadata** — `injectRequestContext` uses `strings.Join(md.Get(MetaTenantRoles), ",")` before `domain.ParseRoles` so gRPC gateways that send roles as multiple repeated metadata entries are handled correctly and consistently with the HTTP path. Repeated `x-user-id` / `x-tenant-id` are the opposite: rejected with `Unauthenticated "ambiguous <key> metadata"` (a gateway that appends instead of replacing must not let a client value win).

**gRPC role validation parity with HTTP** — `injectRequestContext` calls `domain.ParseRoles` (the same function used by HTTP `ContextMiddleware`) so gRPC paths apply the same `[a-zA-Z0-9_-]+` regex, `MaxRoleLen` cap, and deduplication.

**gRPC error message precision** — `checkRequiredMeta` distinguishes `len(vals)==0` ("missing") from `TrimSpace(vals[0])==""` ("blank") so engineers can distinguish gateway omission from gateway misconfiguration. `BuildContext` sentinel errors are mapped to fixed messages — internal error details are never forwarded to gRPC clients.

**`ZapLogger.Fatal` removed entirely** — both `port.Logger` interface and the `ZapLogger` concrete type no longer have a `Fatal` method. No code in the library or through the logger interface can call `os.Exit(1)`. Fatal-level decisions belong in `main()` using `log.Fatal` directly.

**gRPC feature parity** — `pkg/grpccommon` now provides the same layered stack as `pkg/gincommon`: observability (panic recovery, request ID, tracing, metrics, logging), authentication (`RequireAuth`), context building, and per-RPC RBAC. `FullUnaryInterceptors` / `FullStreamInterceptors` give the complete stack, ordered recovery → request ID → tracing → metrics → **logging → auth → context** (= `Observability*` + `Protected*`), so auth rejections are logged; the context interceptor hands the verified identity back to the logging interceptor through a ctx holder. Context errors (`context.DeadlineExceeded`/`Canceled`) are classified via `status.FromContextError` in metrics, logs and spans (`grpcCode` in `status.go`). The request ID is echoed as a response header. `DefaultUnaryInterceptors` / `DefaultStreamInterceptors` remain as backward-compatible aliases for the observability-only chain.

**`ServiceName` is required (panics eagerly)** — `ObservabilityMiddlewares` panics immediately on an empty `ServiceName` before any middleware is registered. An empty service name produces invalid Prometheus const labels that corrupt cardinality for every metric. Failing at startup prevents subtle runtime surprises.

**Tracing idempotency via `ensureInitTelemetry`** — `ObservabilityMiddlewares` calls the internal `otel.EnsureInitTelemetry` (unexported from `pkg/gincommon`) rather than `InitTelemetry`; a provisional `unknown_service` provider is replaced. This means calling `ObservabilityMiddlewares` multiple times (e.g. in tests) does not clobber an already-configured provider. `InitTracing` / `InitTracingFromEnv` are provided for services that need to initialise tracing before their HTTP server starts (background jobs, gRPC servers).

**OTLP export fallback** — if the OTLP gRPC dial fails at startup, `initTracing` silently falls back to an in-process-only `TracerProvider` so all spans still get valid trace IDs. Services do not crash when the collector is temporarily unavailable.

**Log injection prevention** — `LoggingMiddleware` pipes every raw header value through `LogSanitizer` (control characters stripped, truncated to 256 bytes) before emitting structured Zap fields. This prevents crafted request headers from injecting fake log lines.

**Auth order enforced by stack design** — `RequireAuth` runs before `ContextMiddleware`; `AuthZMiddleware` requires `ContextMiddleware` to have run. `DefaultMiddlewares` enforces this ordering. Placing `RequirePermission` before `ProtectedMiddlewares` (HTTP) returns `500 Internal Server Error`. For gRPC, placing `RequirePermission` before `ProtectedUnaryInterceptors` returns `codes.FailedPrecondition "request context missing — ensure ProtectedUnaryInterceptors ran before RequirePermission"` — a diagnosable misconfiguration error, not a silent pass or misleading Internal error.

**`DefaultUnaryInterceptors`/`DefaultStreamInterceptors` panic on empty `ServiceName`** — these aliases now route through `ObservabilityUnaryInterceptors` which panics on empty `ServiceName`, matching the HTTP stack's behaviour. Callers that previously passed an empty `ServiceName` (which the old metrics-only implementation silently allowed) will panic at server startup after upgrading — this is intentional and enforces the same cardinality contract as the HTTP stack.

**Gateway trust boundary** — `RequireAuth` validates header format (non-empty, ≤256 bytes) but does not cryptographically verify identity. The library assumes requests arrive from a trusted API gateway that has already authenticated the user. Consuming services are responsible for ensuring requests cannot bypass the gateway.

**One metrics identity per process** — see *Metrics Initialization*: re-init with the same identity is a no-op, a different one is `ErrIdentityMismatch` (startup panic). For isolated test registries use `metrics.InitWithRegistererOptions(opts, prometheus.NewRegistry())`.

**Enterprise Platform Observability Standard** — `internal/obsregistry/registry.json` is the central registry for all platform libraries: it imports platform-events' and platform-pgcommon's released registries verbatim (their contracts win on conflict; library-specific enums on cross-emitter metrics go to `governance_notes`). Identity rules match those libraries: same env precedence, same `[a-z][a-z0-9_-]{0,62}` value rule, no `version` on platform series. The registry is the contract: shared metrics (Tier 1 `platform_*`, Tier 2 `<domain>_*`) exist only as registry entries; Tier 3 `<domain>_<service>_*` metrics are free but validated (suffixes, prohibited labels, namespace). Required labels (`domain`, `service`, `environment`) are const labels injected centrally, never declared by instrumentation. Platform HTTP `method` is bounded (non-standard methods → `OTHER`); platform gRPC uses `grpc_method` (not `method`) so each label name has one meaning. Pre-standard names (gincommon's, platform-events', platform-pgcommon's) are `removed` legacy registry entries with replacements: `metricslint` reports emitting or querying them as errors. Byte-size histograms use `_bytes` via an explicit `unit: bytes` registry exception to rule 5 (pending governance ratification).

## CI/CD

GitHub Actions, modelled on the IAM service pipelines (iam-org-membership). This is a **private module** — there is no production deployment; CI validates library quality, and a tag publishes the Go module version. Shell logic lives in `.github/scripts/` (shellcheck-clean), not inline YAML. Actions are pinned to full commit SHAs (`# vX.Y.Z` comments). Dependabot version updates are disabled: the config lives in `.github/dependabot.yml.disabled` (re-enable with `git mv` back to `dependabot.yml`; docker bumps then need `make pin-base-images`); alerts/security updates are repo settings; the interop reusable workflow is pinned to a SHA. Dockerfile base images are digest-pinned (`make pin-base-images`). `.golangci.yml` is v2 syntax and `make lint` runs `config verify`. A release tag must point at HEAD and be reachable from `origin/main`.

**`validate-test.yml`** (reusable; output `pct`): `go mod download/verify` → `make test-cover` (unit + integration + e2e under `-race`, one run that also writes `coverage.out` with `-coverpkg`) → `coverage-gate.sh` (≥ 98%, `COVERAGE_THRESHOLD`) → upload coverage.

**`validate-quality.yml`** (reusable): mod verify → reject HTML-escaped operators in workflows/scripts → `gofmt` → tidy drift → `go vet` → golangci-lint → `make metrics-lint` (observability standard) → `govulncheck` (library packages only) → Dockerfile digest-pin check against `.docker-digests` (`check-docker-digests.sh`).

**`ci.yml`** (push/PR to main; skips Markdown/mermaid-only commits):
- `validate-test` ∥ `validate-quality` ∥ `build-image` (Hadolint, ignoring DL3018 because the bases are digest-pinned; `.dockerignore` check; amd64 build into GHA + registry cache)
- `trivy` (CRITICAL/HIGH/UNKNOWN fails; SARIF upload; CycloneDX SBOM) and `smoke` (`smoke-tests.sh`: image ≤ 60 MB; an invalid `OBSERVABILITY_DOMAIN` must exit non-zero with the registry error; `/health` ok and `/metrics` exposes `platform_build_info` / `platform_http_requests_total` with identity labels), both after validate-test + build-image
- `interop` (platform-interop-tests) after validate-test
- `pr-summary` (PRs: one updated status comment) and `push` (main: demo image → GHCR with branch + `sha-` tags, provenance + SBOM, Cosign keyless sign + verify). The image is the **reference/demo image**, not a production artefact.

**`release.yml`** (`v*` tags or manual) — mirrors platform-pgcommon/platform-events: `verify` (verify-release-tag.sh: dispatch ref must be `main` or the tag, HEAD == tag, tag reachable from `origin/main`; verify-changelog-entry.sh via changelog-section.sh — an rc may use its base version's section; `SOURCE_DATE_EPOCH`; release-image-tags.sh picks forward-only `X.Y`/`X`) → the same gates as `ci.yml` at the tag (validate-test ∥ validate-quality ∥ build-image → trivy, smoke, interop) + `binaries` (metricslint ×5 with `.sha256`) → `push` (image-fingerprint.sh: the pre-push local build and the pulled-back pushed digest must match the fingerprints trivy and smoke recorded; Cosign sign + verify; BuildKit SLSA provenance via export-image-provenance.sh) → `publish` (release-checksums.sh re-verifies and writes `checksums.txt`, Cosign `sign-blob` → `checksums.txt.sigstore.json`, create-github-release.sh). The demo image never gets `latest`. Signer identity `release.yml@refs/tags/vX.Y.Z` (or `@refs/heads/main` for a dispatch from main). This is the **Go module release** consuming services pin to with `go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@vX.Y.Z`. There is no deploy gate (nothing is deployed).

**`changelog-check.yml`** (PRs touching `internal/`, `pkg/`, `cmd/` or `go.mod`): fails unless `CHANGELOG.md` is updated.

## Extending the Library

- **New middleware:** implement in `internal/adapter/inbound/http/middleware/`, expose via `pkg/gincommon/gincommon.go`. Thread `port.Logger` through the constructor (nil-safe) following the `MetricsMiddleware` pattern.
- **New metrics:** a new `platform_*` or `<domain>_*` metric is a registry change first: add the entry (and any new label) to `internal/obsregistry/registry.json` with `"status": "proposed"` (governance approval flips it to `approved`), run `make metrics-inventory`, then build the collector from the registry in `initCollectors` (`internal/adapter/outbound/metrics/metrics.go`). Never add new un-namespaced legacy collectors. `make metrics-lint` and `test/unit/platformmetrics` must pass.
- **Replace logger:** implement `Logger` in `internal/core/port/logger.go`, place implementation in `internal/adapter/outbound/`, inject via `Config`.
- **Replace RBAC:** implement `Authorizer` in `internal/core/port/authorizer.go`, place implementation in `internal/adapter/outbound/`, inject via `RequirePermissionWithLogger`.
- **New use case:** add to `internal/core/service/`, depending only on `domain/` types and `port/` interfaces.
