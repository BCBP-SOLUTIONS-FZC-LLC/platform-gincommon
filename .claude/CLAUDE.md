# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repo Is

`platform-gincommon` is a **private Go shared library** (module: `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon`, Go 1.26.3) that provides reusable Gin HTTP and gRPC middleware for platform services. It lives in a **private GitHub repository** and is consumed as a Go module dependency by internal platform services — it is never deployed as a standalone server.

**Consuming services** must set `GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*` (or `GONOSUMDB`/`GOFLAGS` equivalents) to fetch private module versions. The `cmd/server/main.go` binary is a **reference/demo only** — it exists to exercise the middleware stack locally. The Docker image CI pushes the demo server container to GHCR; it is **not** a production artefact.

See [`ARCHITECTURE.md`](../ARCHITECTURE.md) for detailed flow diagrams and invariant tables.

## Common Commands

```bash
make setup        # Copy .env-example → .env (run once before anything else)
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
make race         # All tests with -race flag
make build        # Compile to bin/platform-gincommon
make run          # Run the example reference server
make cover        # Coverage HTML report (measures ./internal/... ./pkg/... ./cmd/...)
make cover-func   # Coverage summary by function (terminal)
make ci           # tidy + vet + lint + test-ci + build (full CI pipeline)
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
- `Config{Logger, ServiceName, BuildVersion, Tracing}` — configure the stack. `ServiceName` is **required** (panics if empty — prevents invalid Prometheus labels).
- `DefaultMiddlewares(Config)` — full stack: observability + auth + tenant context
- `ObservabilityMiddlewares(Config)` — panic recovery, request ID, tracing, metrics, logging
- `ProtectedMiddlewares(Config)` — auth validation + context building only
- `TimeoutMiddleware(duration)` — per-request context deadline; zero/negative is a no-op; insert before other groups
- `HealthHandler()` — returns 200 `{"status":"ok"}`; register before `ProtectedMiddlewares` (no auth)
- `ErrorResponse` — standard 4xx/5xx body struct with `Error`, `Status`, `TraceID`, `RequestID`
- `PropagateHeaders(c, req)` — injects trace context, request ID, and identity headers from inbound Gin context into outbound `*http.Request`
- `RequirePermission(action, resource, auth)` — per-route RBAC, no logging
- `RequirePermissionWithLogger(action, resource, auth, logger)` — per-route RBAC with audit log entries on denial
- `InitTracing()`, `EnsureTracing()`, `InitTracingFromEnv()` — OpenTelemetry OTLP/gRPC setup

**`pkg/grpccommon`** — for gRPC services:
- `Config{ServiceName, BuildVersion, Logger}` — `ServiceName` required; `Logger` enables logging and panic recovery
- `ObservabilityUnaryInterceptors(cfg)` / `ObservabilityStreamInterceptors(cfg)` — panic recovery, request ID, tracing, metrics, logging
- `ProtectedUnaryInterceptors(cfg)` / `ProtectedStreamInterceptors(cfg)` — `RequireAuth` + `ContextInterceptor`
- `FullUnaryInterceptors(cfg)` / `FullStreamInterceptors(cfg)` — full stack: observability + protected
- `DefaultUnaryInterceptors(cfg)`, `DefaultStreamInterceptors(cfg)` — aliases for `Observability*` (backward compatible)
- `RequirePermission(action, resource, auth)` — per-RPC RBAC unary interceptor (requires `ProtectedUnaryInterceptors`)
- `RequirePermissionWithLogger(action, resource, auth, logger)` — per-RPC RBAC with audit logging
- `RequestContextFromContext(ctx)` — retrieves `*domain.RequestContext` from gRPC handler context
- `RequestIDFromContext(ctx)` — retrieves correlation request ID from gRPC handler context

### Middleware Execution Order

When using `DefaultMiddlewares`, handlers run in this order:
0. *(Optional)* `TimeoutMiddleware(d)` — per-request context deadline; insert before `DefaultMiddlewares` if needed
1. `PanicRecoveryMiddleware` — recover panics, log (stderr fallback when logger is nil), record metrics
2. `RequestIDMiddleware` — generate/read `x-request-id`, echo in response
3. `TracingMiddleware(serviceName)` — OpenTelemetry HTTP span scoped to `serviceName`, W3C `traceparent` propagation
4. `CorrelationHeadersMiddleware` — propagate trace/request headers downstream
5. `MetricsMiddleware(logger)` — Prometheus HTTP metrics; logs a warn on every 429
6. `LoggingMiddleware` — structured Zap `http_request` log after response; nil logger is a complete no-op; logs Info for 2xx, Warn for 4xx, Error for 5xx; `duration_ms` field is float64 milliseconds
7. `RequireAuth` — validate `x-user-id` + `x-tenant-id` headers (401 if missing)
8. `ContextMiddleware` — build `RequestContext{TenantID,UserID,Roles,TraceID,ClientIP}`, store in Gin context
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

`internal/core/service/authz_service.go` is intentionally simple and default-deny: `"read"` action is always permitted for any role; `"delete"` action requires the `"admin"` role; **all other actions (`"write"`, `"create"`, `"update"`, etc.) are denied regardless of role**. This is a reference/demo implementation only — production services must implement the `Authorizer` interface in `internal/core/port/authorizer.go` and inject it via `RequirePermission`. Use `gincommon.NewDemoRBAC()` or `grpccommon.NewDemoRBAC()` in tests and demos.

### Metrics Initialization

`metrics.Init(serviceName, buildVersion string)` is idempotent via `sync.Once` — the first caller wins. For tests that need an isolated Prometheus registry use `metrics.InitWithRegisterer(serviceName, buildVersion, prometheus.NewRegistry())` which bypasses the Once guard and accepts a custom registerer.

### Key Configuration Defaults

`OtelExporterInsecure` defaults to **false** (TLS) unless `APP_ENV` is explicitly `"dev"`, `"development"`, or `"local"`. An unset `APP_ENV` is treated as secure. Set `OTEL_EXPORTER_OTLP_INSECURE=true` to override.

The demo router calls `r.SetTrustedProxies(nil)` — consuming services behind a real proxy should call `r.SetTrustedProxies([]string{"<proxy-cidr>"})` on their engine.

### Observability Stack (local dev)

`make docker-up` starts: OTel Collector (gRPC:4317, HTTP:4318) → Tempo → Prometheus (9090) → Loki + Promtail → Grafana (3000).

## Key Environment Variables

Defined in `.env-example` (copy to `.env` via `make setup`):

```
APP_NAME, APP_ENV, APP_PORT, BUILD_VERSION
OTEL_SERVICE_NAME, OTEL_EXPORTER_OTLP_ENDPOINT   # defaults to localhost:4317
OTEL_EXPORTER_OTLP_INSECURE                       # true/false; defaults false unless APP_ENV=dev
SMOKE_BASE_URL                                     # for smoke tests
TEST_SERVICE_NAME, E2E_SERVICE_NAME
```

## Test Layout

- `test/unit/` — isolated unit tests per package (metrics, middleware, logger, grpc, config, service, etc.)
- `test/integration/` — full middleware chain on an in-process HTTP server
- `test/e2e/` — full server startup
- `test/smoke/` — optional; requires a live server (`SMOKE_BASE_URL`)
- `test/fixtures/` — shared test helpers (MockLogger, HTTP helpers, error response decoder)
- `test/testenv/` — loads `.env-example` for tests

## Key Design Decisions

**Nil-safe logger and metrics** — every middleware that accepts a `port.Logger` checks for `nil` before calling it. `LoggingMiddleware(nil)` is a complete no-op (passes through without logging). `PanicRecoveryMiddleware(nil)` falls back to `log.Printf` on stderr. All gRPC logging and panic-recovery interceptors also check for nil logger. Prometheus collectors are only recorded if non-nil (i.e., `metrics.Init()` was called) — including the inner panic-recovery defer in `MetricsMiddleware`. Consumers that skip metrics or logging are unaffected — nothing panics. `Fatal` is excluded from `port.Logger` because it calls `os.Exit(1)` and belongs in `main()`, not middleware.

**Prometheus label cardinality** — `HTTPRoute` always returns the Gin route template (e.g. `/api/items/:id`) or the constant `"unknown"` for unregistered routes. The raw `c.Request.URL.Path` is never used as a Prometheus label — scanner probes and path-parameter abuse cannot explode cardinality.

**Production-safe defaults** — `AppEnv()` returns `""` when `APP_ENV` is unset so `OtelTraceSampleRatio` defaults to `0.1` (not `1.0`). Deployments that omit `APP_ENV` do not silently run at 100% trace sampling.

**gRPC span attributes** — `splitFullMethod` uses `strings.TrimLeft` + `strings.LastIndex` to split `info.FullMethod` into `rpc.service` and `rpc.method`. Bare method names (no slash) return `("", method)` so `rpc.method` is always correct. Empty or slash-only input returns `("", "unknown")` so `rpc.method` is never an empty Prometheus label. Double-slash proxy edge cases are also handled.

**gRPC span status on server errors** — `UnaryTracingInterceptor` and `StreamTracingInterceptor` call `span.SetStatus(otelcodes.Error, err.Error())` for all non-nil handler errors (not only panics). Per the OTel spec, `RecordError` alone does not change span status; the explicit `SetStatus` call ensures Tempo/Jaeger display non-OK gRPC responses as error spans, enabling trace-based 5xx alerting.

**gRPC logging fires on panic** — `UnaryLoggingInterceptor` uses named returns + a `panicked` sentinel so that a panicking handler still emits a `grpc_request` log entry (tagged as `codes.Internal`) before the panic propagates to the recovery interceptor.

**Panics counted in all HTTP metrics and logs** — both `MetricsMiddleware` and `LoggingMiddleware` use `panicked`-sentinel defers. `MetricsMiddleware` overrides code to 500 only when `panicked == true` so deliberate `c.Abort()` is recorded accurately. `LoggingMiddleware` emits an `http_request` log entry for panicking requests (status=500), matching the gRPC interceptor behaviour.

**gRPC observability parity on panic** — `UnaryMetricsInterceptor`, `StreamMetricsInterceptor`, `UnaryTracingInterceptor`, and `StreamTracingInterceptor` all use `panicked`-sentinel defers. Panicking handlers are counted in `grpc_requests_total`, observed in duration histograms, and have their OTel spans annotated with the **real panic value** (e.g. "nil pointer dereference") via an inner `defer recover()+re-panic` pattern — not a static `"panic"` string.

**Log injection prevention** — `PanicRecoveryMiddleware` applies `sanitizeLogValue` to `c.Request.URL.Path` before logging. `isSensitiveLogKey` uses `k == "key" || strings.HasSuffix(k, "_key")` to cover `signing_key`, `enc_key`, `hmac_key` without false-positive redaction of `monkey`, `hockey`, or `key_metrics`. gRPC `UnaryPanicRecoveryInterceptor`, `StreamPanicRecoveryInterceptor`, and `logGRPCRequest` all pass log field maps through `service.Sanitize` before emitting, matching the HTTP path's redaction guarantees.

**`duration_ms` is true float64 milliseconds** — both `LoggingMiddleware` and `logGRPCRequest` compute `duration_ms` as `float64(elapsed.Nanoseconds()) / 1e6` (not `float64(elapsed.Milliseconds())`). This preserves sub-millisecond precision: a 400 µs request logs `0.4` not `0`. `PanicRecoveryMiddleware` uses the same key (`"duration_ms"`) and computation so panic and normal request log entries have a consistent schema.

**gRPC `isClientError` matches metrics classification** — `isClientError` in `UnaryLoggingInterceptor`/`StreamLoggingInterceptor` includes `codes.DeadlineExceeded` (matching `GRPCStatusClass`/`GRPCRequestOutcome` which classify it as a client error). Timed-out RPCs log at `Warn` (not `Error`) to match how they are counted in Prometheus.

**gRPC `TraceID` in `RequestContext`** — `injectRequestContext` extracts the OTel trace ID from the active span via `trace.SpanFromContext(ctx).SpanContext().TraceID().String()` when `UnaryTracingInterceptor` ran first and the span context is valid. `rc.TraceID` is populated for all gRPC handlers that use the full or protected interceptor stack.

**`ProtoMessageSize` typed-nil safe** — `reflect.ValueOf(m).Kind() == reflect.Pointer && rv.IsNil()` guards against a `(*pb.Response)(nil)` typed nil that bypasses the plain `v == nil` check.

**OTel spans flushed on all demo-server exits** — `cmd/server/main.go` calls `shutdownTracing()` explicitly before every `os.Exit(1)` path rather than relying on `defer` which `os.Exit` bypasses.

**gRPC repeated-role metadata** — `injectRequestContext` uses `strings.Join(md.Get(MetaTenantRoles), ",")` before `domain.ParseRoles` so gRPC gateways that send roles as multiple repeated metadata entries are handled correctly and consistently with the HTTP path.

**gRPC role validation parity with HTTP** — `injectRequestContext` calls `domain.ParseRoles` (the same function used by HTTP `ContextMiddleware`) so gRPC paths apply the same `[a-zA-Z0-9_-]+` regex, `MaxRoleLen` cap, and deduplication.

**gRPC error message precision** — `checkRequiredMeta` distinguishes `len(vals)==0` ("missing") from `TrimSpace(vals[0])==""` ("blank") so engineers can distinguish gateway omission from gateway misconfiguration. `BuildContext` sentinel errors are mapped to fixed messages — internal error details are never forwarded to gRPC clients.

**`ZapLogger.Fatal` removed entirely** — both `port.Logger` interface and the `ZapLogger` concrete type no longer have a `Fatal` method. No code in the library or through the logger interface can call `os.Exit(1)`. Fatal-level decisions belong in `main()` using `log.Fatal` directly.

**gRPC feature parity** — `pkg/grpccommon` now provides the same layered stack as `pkg/gincommon`: observability (panic recovery, request ID, tracing, metrics, logging), authentication (`RequireAuth`), context building, and per-RPC RBAC. `FullUnaryInterceptors` / `FullStreamInterceptors` give the complete stack. `DefaultUnaryInterceptors` / `DefaultStreamInterceptors` remain as backward-compatible aliases for the observability-only chain.

**`ServiceName` is required (panics eagerly)** — `ObservabilityMiddlewares` panics immediately on an empty `ServiceName` before any middleware is registered. An empty service name produces invalid Prometheus const labels that corrupt cardinality for every metric. Failing at startup prevents subtle runtime surprises.

**Tracing idempotency via `EnsureInitTelemetry`** — `ObservabilityMiddlewares` calls `EnsureInitTelemetry` rather than `InitTelemetry`. This means calling `ObservabilityMiddlewares` multiple times (e.g. in tests) does not clobber an already-configured provider. `InitTracing` / `InitTracingFromEnv` are provided for services that need to initialise tracing before their HTTP server starts (background jobs, gRPC servers).

**OTLP export fallback** — if the OTLP gRPC dial fails at startup, `initTracing` silently falls back to an in-process-only `TracerProvider` so all spans still get valid trace IDs. Services do not crash when the collector is temporarily unavailable.

**Log injection prevention** — `LoggingMiddleware` pipes every raw header value through `LogSanitizer` (control characters stripped, truncated to 256 bytes) before emitting structured Zap fields. This prevents crafted request headers from injecting fake log lines.

**Auth order enforced by stack design** — `RequireAuth` runs before `ContextMiddleware`; `AuthZMiddleware` requires `ContextMiddleware` to have run. `DefaultMiddlewares` enforces this ordering. Placing `RequirePermission` before `ProtectedMiddlewares` (HTTP) returns `500 Internal Server Error`. For gRPC, placing `RequirePermission` before `ProtectedUnaryInterceptors` returns `codes.FailedPrecondition "request context missing — ensure ProtectedUnaryInterceptors ran before RequirePermission"` — a diagnosable misconfiguration error, not a silent pass or misleading Internal error.

**`DefaultUnaryInterceptors`/`DefaultStreamInterceptors` panic on empty `ServiceName`** — these aliases now route through `ObservabilityUnaryInterceptors` which panics on empty `ServiceName`, matching the HTTP stack's behaviour. Callers that previously passed an empty `ServiceName` (which the old metrics-only implementation silently allowed) will panic at server startup after upgrading — this is intentional and enforces the same cardinality contract as the HTTP stack.

**Gateway trust boundary** — `RequireAuth` validates header format (non-empty, ≤256 bytes) but does not cryptographically verify identity. The library assumes requests arrive from a trusted API gateway that has already authenticated the user. Consuming services are responsible for ensuring requests cannot bypass the gateway.

**Idempotent Prometheus registration** — `metrics.Init` is guarded by `sync.Once`; the first caller wins. For tests that need an isolated registry use `metrics.InitWithRegisterer(serviceName, buildVersion, prometheus.NewRegistry())`, which bypasses the Once guard.

## CI/CD

GitHub Actions runs three workflows. This is a **private module** — there is no production server deployment; CI validates library quality and publishes Go module versions via git tags.

**`.github/workflows/validate.yml`** (reusable, called by both ci.yml and release.yml):
1. `go mod download` + `go mod verify` — module integrity
2. `gofmt -l` — format check (fails if any files need formatting)
3. `go mod tidy` drift check — `go.mod`/`go.sum` must be tidy
4. `go vet` — static analysis
5. `golangci-lint` — linter suite
6. `govulncheck ./internal/... ./pkg/...` — vulnerability scan (library packages only; `cmd/` excluded as demo)
7. `make test-ci` — unit + integration + e2e with race detector

**`.github/workflows/ci.yml`** (push/PR to main):
1. **validate** — calls validate.yml
2. **coverage** — all tests with `-coverpkg` + ≥95% gate; uploads `coverage.out` artefact
3. **build** — `make build` (demo server binary); verifies the binary is executable
4. **docker** — (push events only) builds and pushes demo server image to GHCR with branch + SHA tags. This is the **reference/demo image**, not a production deployment.

**`.github/workflows/release.yml`** (push of `v*` tags):
1. **validate** — calls validate.yml at the tag ref
2. **build** — compile demo binary
3. **docker** — push demo image to GHCR with semver tags (`vMAJOR.MINOR.PATCH`, `vMAJOR.MINOR`, `vMAJOR`)
4. **publish** — create GitHub Release with notes extracted from `CHANGELOG.md`; this is the **Go module release** that consuming services pin to with `go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@vX.Y.Z`

## Extending the Library

- **New middleware:** implement in `internal/adapter/inbound/http/middleware/`, expose via `pkg/gincommon/gincommon.go`. Thread `port.Logger` through the constructor (nil-safe) following the `MetricsMiddleware` pattern.
- **New metrics:** register in `internal/adapter/outbound/metrics/metrics.go` inside `initMetricsWithRegisterer` — add to the `reg.MustRegister(...)` call at the bottom.
- **Replace logger:** implement `Logger` in `internal/core/port/logger.go`, place implementation in `internal/adapter/outbound/`, inject via `Config`.
- **Replace RBAC:** implement `Authorizer` in `internal/core/port/authorizer.go`, place implementation in `internal/adapter/outbound/`, inject via `RequirePermissionWithLogger`.
- **New use case:** add to `internal/core/service/`, depending only on `domain/` types and `port/` interfaces.
