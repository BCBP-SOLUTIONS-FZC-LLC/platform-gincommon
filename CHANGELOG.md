# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- (none)

### Changed

- (none)

### Fixed

- (none)

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
