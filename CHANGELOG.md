# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`pkg/grpccommon`:** `Config.Tracing *TracingOptions` field — gRPC services can now override OTLP endpoint, insecure mode, sample ratio, and baggage propagation without importing `pkg/gincommon`.
- **`pkg/grpccommon`:** `NewDemoRBAC()` — reference RBAC for demos/tests (mirrors `gincommon.NewDemoRBAC()`).
- **`pkg/gincommon`:** `NewDemoRBAC()` — preferred name for the reference RBAC (`NewRBAC` kept as deprecated alias).
- **`pkg/gincommon`:** `TraceIDFromContext(c)` and `RequestIDFromContext(c)` — type-safe helpers to read correlation IDs from Gin handler context without knowing internal key strings.
- **`pkg/grpccommon`:** `RequireStreamPermission` and `RequireStreamPermissionWithLogger` — per-stream RBAC interceptors (previously only unary was exposed).
- **`config`:** `IsDevEnv(env string) bool` — canonical helper used consistently for dev/development/local detection across logger, gin mode, and OTel sampling.
- **`metrics`:** `GrpcPanicTotal` counter (`grpc_panic_total`) — mirrors `http_panic_total`; incremented by `UnaryMetricsInterceptor` and `StreamMetricsInterceptor` on panic.
- **`ZapLogger`:** `Sync() error` method — allows callers to flush buffered log entries before process exit.

### Changed

- **`pkg/grpccommon`:** `ObservabilityUnaryInterceptors`, `ObservabilityStreamInterceptors`, `FullUnaryInterceptors`, `FullStreamInterceptors` now call `EnsureInitTelemetry` so gRPC services get valid OTel trace IDs without separately calling `gincommon.InitTracingFromEnv`.
- **`internal/server`:** Shutdown function now also calls `ZapLogger.Sync()` to flush buffered log entries on graceful exit.
- **`SetGinMode`:** Recognises `"development"` and `"local"` as dev environments (previously only `"dev"`), consistent with `OtelExporterInsecure`.
- **`OtelTraceSampleRatio`:** Uses `IsDevEnv` for dev detection — `"development"` and `"local"` now return 1.0 (was only `"dev"`).
- **`NewLogger`:** Uses `IsDevEnv` — `"development"` and `"local"` now return development (console) config.
- **`TimeoutMiddleware`:** Writes a `503 Service Unavailable` JSON response when the context deadline expires and the handler wrote nothing, preventing a silent `200 OK` on timed-out requests.
- **`writeJSONError`:** Uses a typed `errorResponseBody` struct instead of `gin.H` map — compile-time enforcement of JSON field consistency.
- **`http/middleware/context.go`:** `ErrMissingTenant` and `ErrMissingUser` now return `401 Unauthorized` (was `400 Bad Request`), matching gRPC `codes.Unauthenticated` semantics.
- **CI:** Coverage step now includes `internal/adapter/inbound/grpc/middleware` in test packages and `./cmd/...` in `-coverpkg`, matching `make cover`.
- **Release workflow:** Added coverage gate (`≥95%`) before build; scoped job permissions (`contents: write` only for `publish`, `packages: write` only for `docker`); fixed `workflow_dispatch` checkout ref in `docker` and `publish` jobs; suppressed automatic `latest` tag on demo image; fixed semver awk regex to use fixed-string matching; added `set -euo pipefail` to release-notes extraction.
- **`Dockerfile`:** Builder pinned to `golang:1.26.3-alpine` (exact patch, matching `go.mod`).
- **`.dockerignore`:** `docs/` excluded from build context.
- **`Makefile`:** `ci` target now also runs `fmt-check` and `cover-check`; added `vulncheck`, `fmt-check`, `cover-check` targets.
- **`govulncheck`:** Pinned to `@v1.1.4` (was `@latest`).

### Fixed

- **`grpc/middleware/authz.go`:** Nil `auth` parameter caused a nil-pointer panic; now returns `codes.Internal` without panicking.
- **`grpc/middleware/request_id.go`:** Client-supplied `x-request-id` metadata was used verbatim (log injection risk); now validated via `domain.NormalizeRequestID`, falling back to UUID on failure.
- **`http/middleware/authz.go`:** Typed-nil `*RequestContext` bypassed the `!ok` guard; guard is now `!ok || ctx == nil`.
- **`grpc/middleware/auth.go`:** `checkRequiredMeta` accepted control characters and Unicode line separators in identity headers; now calls `domain.NormalizeHeaderValue` for parity with HTTP `RequireAuth`.
- **`http/middleware/tracing.go`:** Raw `url.path` and `user_agent.original` OTel span attributes were unbounded; now truncated to 512 bytes at a UTF-8 rune boundary.
- **`http/middleware/authz.go` + `grpc/middleware/authz.go`:** Custom `Authorizer` error strings logged verbatim (potential sensitive-data leak); now sanitized via `service.SanitizeString`.
- **`grpc/middleware/recovery.go` + `http/middleware/recovery.go`:** Panic value (`fmt.Sprint(r)`) could contain attacker-controlled control characters; now sanitized before logging.
- **`service/log_sanitizer.go`:** Added `"jwt"`, `"bearer"`, `"session"`, `"session_id"`, `"cert"` to sensitive key set; added `SanitizeString` for sanitizing untrusted string values.
- **`metrics/metrics.go`:** `Init` now takes `initMu` inside `initOnce.Do` — eliminates a theoretical race-detector flag when readers start before `Init` returns.
- **`CLAUDE.md`:** Corrected RBAC description — "all other combinations are permitted" was wrong; all non-read, non-delete actions are denied by the reference `RBACService`.

---

## [1.0.0] - 2026-05-26

First stable release of the shared Gin/gRPC middleware library.

### Added

- **Transport:** `x-user-id`, `x-tenant-id`, `x-tenant-roles`, `x-request-id`; response `X-Trace-ID`, `X-Request-ID` (exported from `pkg/gincommon`)
- **HTTP middleware:** panic recovery, request ID, OpenTelemetry tracing, correlation headers, Prometheus metrics, structured logging, `RequireAuth`, `ContextMiddleware`, `AuthZMiddleware`
- **Domain:** `RequestContext`, `port.Authorizer`, `port.Logger`, `port.ContextBuilder`; reference `RBACService`
- **Observability:** Zap logger adapter, Prometheus HTTP/gRPC metrics, OTLP/gRPC tracing with env-based TLS/sampling/baggage
- **Public API:** `pkg/gincommon` (`DefaultMiddlewares`, `ObservabilityMiddlewares`, `ProtectedMiddlewares`, `RequestContext`, tracing helpers)
- **gRPC:** `pkg/grpccommon` unary/stream metrics interceptors
- **Security:** header validation, log field redaction (`Sanitize`), RBAC default-deny for unknown actions
- **CI:** lint (golangci-lint + revive), tests with race detector, build, Docker image, automated release workflow

### Fixed

- **`MetricsMiddleware`:** `gin.ResponseWriter.Size()` returns `-1` when no body is written (e.g. `204 No Content`). Response size observation is now clamped to `math.Max(0, size)` to prevent undefined Prometheus histogram behaviour.
- **`TracingMiddleware`:** Span attributes (`http.route`, `url.path`, `http.response.status_code`, `server.address`, `client.address`) were never recorded when a handler panicked. All attributes are now set via deferred functions and are present on panicked spans.
- **`RecordGRPC`:** The `isError` parameter was accepted by the function signature but silently unused. The parameter has been removed.
- **`MetricsMiddleware` / gRPC interceptors:** A nil-pointer panic occurred when middleware was used before `metrics.Init()`. Nil guards added; uninitialized middleware now passes through cleanly.
- **`OtelTraceSampleRatio`:** Both branches of the default-value logic returned `1.0` (dead code). Non-`dev` environments now correctly default to `0.1` (10% sampling). The same env-aware fallback is also applied when `OTEL_TRACES_SAMPLER_RATIO` contains an invalid value. Override with `OTEL_TRACES_SAMPLER_RATIO`.
- **Reference server (`cmd/server`):** `router.Run()` blocked indefinitely, preventing `defer shutdownTracing()` from running on `SIGTERM`. The server now uses `net/http.Server.Shutdown` with a 30-second drain and handles `SIGINT`/`SIGTERM`.
- **`PanicRecoveryMiddleware`:** `trace.SpanFromContext` never returns nil; removed always-true nil guard around span recording.

### Changed

- **`ContextMiddleware`:** Accepts `port.ContextBuilder` (interface) instead of `*service.ContextService` (concrete type). `internal` package only; public API and all existing call sites are unaffected.

[1.0.0]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/releases/tag/v1.0.0
[Unreleased]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/compare/v1.0.0...HEAD
