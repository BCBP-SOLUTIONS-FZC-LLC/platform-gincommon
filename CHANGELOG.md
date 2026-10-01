# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`pkg/gincommon` tracing API** so consuming services never import OpenTelemetry: `NewTracer(scope)`, `NoopTracer()`, `Tracer`/`Span` type aliases, `SpanTraceID(ctx)`, `RecordSpanError(ctx, err, description)`, `InjectTraceContext(ctx, header)` (the `context.Context` counterpart of `PropagateHeaders`), and `NewSpanTracer(scope)` (a `StartSpan(ctx, name)` hook for platform-pgcommon's `Config.Tracer`). All of them use the TracerProvider and propagator `InitTracing` installs.
- **`pkg/gincommon` metrics API** so consuming services never import the Prometheus client: `MetricsHandler()` (serves `MetricsRegisterer()`'s registry), `NewCounterVec`/`NewCounter`/`NewGaugeVec`/`NewGauge` (registered with `MetricsConstLabels()`; `nil` registerer means `MetricsRegisterer()`), plus `Registerer`/`CounterVec`/`Counter`/`GaugeVec`/`Gauge` type aliases.
- **Enterprise Platform Observability Standard.**
  - **Platform Observability Registry** (`internal/obsregistry/registry.json`, rendered to `docs/observability/metric-registry.md`): the platform-shared (`platform_*`) and domain-shared (`iam_*`) metrics with their semantic definitions, labels and aggregation expectations; the label vocabulary (allowed values or patterns, `max_values`, cardinality), with per-metric value sets (`label_values`); prohibited high-cardinality labels; histogram bucket presets; and the legacy names with their replacements. The five canonical messaging metrics are `approved`. The registry-proposed examples, gincommon's own HTTP/gRPC metrics and IAM's domain metrics are `proposed` until governance ratifies them.
  - **`Config.Domain` (required) / `Config.Environment`** on `gincommon.Config` and `grpccommon.Config`. Env fallbacks: `OBSERVABILITY_DOMAIN` for the domain; `APP_ENV`, then `ENVIRONMENT`, for the environment (trimmed and lowercased, no aliases), which is the precedence platform-events and platform-pgcommon use. With a domain, gincommon emits `platform_http_*`, `platform_grpc_*` and `platform_build_info{version}`, with `domain`, `service` and `environment` injected centrally on every collector. Like platform-events and platform-pgcommon, `version` is only on build info. The constructors panic at startup on a missing domain, or on a domain, service name or environment the registry rejects.
  - **Registry-driven shared metrics:** `NewSharedCounterVec`, `NewSharedGaugeVec` and `NewSharedHistogramVec` take name, help, labels and buckets from the registry entry. **Tier 3:** `NewHistogramVec`. `NewCounterVec`/`NewCounter`/`NewGaugeVec`/`NewGauge`/`NewHistogramVec` now reject registry and `platform_*` names, wrong suffixes (`_total`, `_seconds`), gauges ending in `_total`, prohibited labels and the centrally injected labels. With a domain, they also reject names outside `<domain>_<service>_`.
  - `LabeledRegisterer(reg)` hands the standard const labels to libraries that register their own collectors (platform-events, platform-pgcommon). `MetricsGatherer()`. `HistogramVec`/`Gatherer` type aliases.
  - **`pkg/obsregistry`** (`Validate`, `ValidateText`, `Default`, `Parse`, `ServicePrefix`) and **`cmd/metricslint`** (`check`, `refs`, `registry`, `inventory`). They enforce namespace classification, naming, counter/histogram suffixes, required labels, registry compliance, and label vocabulary and cardinality, on a service's metrics in its tests or against a live scrape. `refs` scans dashboards, alert/recording rules, SLOs and HPA manifests for legacy and unregistered names.
  - **CI:** `make metrics-lint` (registry self-check, reference-rule references, `promtool check rules` + `promtool test rules`, inventory drift) runs in `validate-quality.yml` and `make ci`. `test/unit/platformmetrics` validates gincommon's own emitted metrics.
  - **Central registry for every platform library:** it imports the released platform-events v1.6.0 and platform-pgcommon registries verbatim (48 + 23 metrics, including their legacy names and successors), and the scrapes of both libraries validate against it with 0 errors. The Python ports (platform-fastapicommon, platform-eventcommon, platform-pgcommon-py) validate against it too; `python_*` runtime collectors are exempt and prometheus_client `<metric>_created` families are classified with their parent. Registry fields `owner`, `label_values`, `cardinality`, `governance_notes`, multi-successor `replacements` and free-text `sunset`.
  - **Reference recording rules, SLO burn-rate alerts and promtool tests** in `docker/prometheus/rules/`, loaded by the local Prometheus. Also new: `docs/observability/` (the standard as implemented here, migration guide, runbooks).
- **`pkg/logger.Default()`**: a process-wide gincommon logger (built once for `APP_ENV`, then `ENVIRONMENT`, default production) for code paths that were never handed one.

### Fixed

- **1xx responses are no longer counted as server errors.** `HTTPStatusClass`/`RequestOutcome` mapped every status below 200, including `101 Switching Protocols` for WebSocket upgrades, to `5xx`/`server_error`, inflating error ratios. 1xx is now `2xx`/`success`, as the registry documents; `0` remains `5xx`. This affects legacy and platform series alike.
- `MetricsConstLabels`/`metrics.ConstLabels` docs no longer claim `version` is a platform const label.
- **The metrics `version` label now falls back to `BUILD_VERSION`**, as `gincommon.Config.BuildVersion` documents, instead of `unknown` when `Config.BuildVersion` is empty (`metrics.ApplyEnvDefaults`).
- **`LabeledRegisterer` resolves the identity labels when each collector registers,** so one created before `ObservabilityMiddlewares` is no longer silently label-less. Its docs now say not to wrap platform-events or platform-pgcommon, which inject the identity themselves.
- **CI/CD:**
  - The release Trivy gate now runs before any release tag is pushed.
  - Cosign verify accepts manual (`workflow_dispatch`) releases.
  - `pr-summary` also posts on failure, so a stale "ready to merge" comment is replaced.
  - The changelog check diffs from the merge base.
  - `metricslint check -url` caps the scrape at 64 MiB.
  - The demo router builds its `/metrics` handler once.

### Security

- **deps:** Bumped `google.golang.org/grpc` `v1.83.0` → `v1.83.1`, clearing govulncheck finding GO-2026-6348. Patch release; no `pkg/gincommon`/`pkg/grpccommon` API change.

### Removed (breaking)

- **Pre-standard metric names, with no compatibility period:** `build_info`, `http_requests_total`, `http_request_duration_seconds`, `http_request_size_bytes`, `http_response_size_bytes`, `http_active_requests`, `http_panic_total`, `rate_limited_requests_total`, `http_request_timeout_total`, and the `grpc_server_*`, `grpc_rate_limited_requests_total` and `grpc_panic_total` names. No platform library had been deployed to production or dev, so there were no dashboards, alerts or series to migrate. gincommon emits only `platform_*` (mapping: `docs/observability/migration.md`). The central registry keeps every removed name, including platform-events', platform-pgcommon's and the Python ports', with status `removed`, so `metricslint` reports emitting or querying one as an error naming its replacement.
- **The domain is now required.** `ObservabilityMiddlewares` and the `grpccommon` interceptor constructors panic without `Config.Domain` / `OBSERVABILITY_DOMAIN`. Consuming services must set it when they upgrade.
- **`gincommon.Config.DisableLegacyMetrics` and `grpccommon.Config.DisableLegacyMetrics`** (unreleased).
- **Internal:** `metrics.Init` / `metrics.InitWithRegisterer`. Use `metrics.InitWithOptions` / `InitWithRegistererOptions`, or the `test/fixtures` helpers in tests.
- **Constructors fail before initialization.** The metrics constructors (`NewCounterVec`, `NewShared*`, ...) now return an error until metrics are initialized, instead of registering collectors without identity labels.

### Changed

- The demo server's `/metrics` serves `gincommon.MetricsHandler()`.
- **CI/CD rebuilt on the IAM service pipeline model** (iam-org-membership):
  - `validate.yml` is replaced by reusable `validate-test.yml` (one `-race` + coverage run, ≥ 98% gate) and `validate-quality.yml` (adds the observability standard, an HTML-entity guard and a Dockerfile digest-pin check).
  - `ci.yml` runs them in parallel with a Hadolint-first cached image build, then Trivy (SARIF + SBOM), smoke tests that start the demo server, interop, a PR summary comment, and on `main` a Cosign-signed demo-image push.
  - `release.yml` adds tag and CHANGELOG gates, cross-platform **metricslint** binaries with checksums as release assets, an image CVE gate, SBOM, SLSA provenance and Cosign signing.
  - New `changelog-check.yml` requires a CHANGELOG entry on PRs that change code.
  - Scripts live in `.github/scripts/`.
  - Dockerfile base images are digest-pinned; new `make test-cover` and `make pin-base-images` targets.
  - **Makefile restructured on the IAM service layout:**
    - a grouped `help`;
    - `GOPRIVATE`/`GONOSUMDB` exports;
    - new `mod-verify`, `vuln-check` (`vulncheck` kept as an alias), `godoc` and `test-smoke-image` targets;
    - `build` also builds `bin/metricslint` and checks that the library packages compile;
    - `cover`/`cover-func`/`cover-check` share one `test-cover` run;
    - `ci` no longer runs the race suite twice;
    - `clean` removes coverage files;
    - `pin-base-images` writes `.docker-digests`, which CI checks against the Dockerfile (`check-docker-digests.sh`).
  - New `.githooks/pre-commit` (tidy, fmt-check, lint, observability registry self-check, reference-rule names and inventory drift), installed by `make install-hooks` and `make setup`.

---

## [1.3.0] - 2026-08-18

### Added

- **`pkg/gincommon`:** `MetricsRegisterer() prometheus.Registerer` and `MetricsConstLabels() prometheus.Labels` — expose the registerer and `{service, version}` const labels gincommon's own HTTP metrics are registered with, so consuming services can register custom application collectors consistently (instead of guessing `prometheus.DefaultRegisterer` and duplicating the labels themselves). Call after `ObservabilityMiddlewares`/`DefaultMiddlewares` has run.
- **`pkg/grpccommon`:** Same pair, `MetricsRegisterer()` and `MetricsConstLabels()`, for gRPC services. Metrics initialization is shared process-wide with `pkg/gincommon`.

### Fixed

- **Security:** Bumped `go.mod`'s `go` directive and the Docker builder base image to `1.26.6`, clearing 2 `govulncheck` findings in the standard library: GO-2026-6067 (`crypto/tls`) and GO-2026-5972 (`encoding/asn1`, recursion depth).

### Changed

- **deps:** Bumped `google.golang.org/grpc` `v1.82.1` → `v1.83.0` (transitively `go.opentelemetry.io/otel`/`otel/sdk`/`otel/trace`/`otel/metric`/`otlptracegrpc`/`otlptrace` `v1.43.0` → `v1.44.0` and `google.golang.org/genproto` to match), then `otel`/`otel/sdk`/`otel/trace`/`otel/metric`/`otlptracegrpc`/`otlptrace` `v1.44.0` → `v1.45.0` and `github.com/prometheus/client_golang` `v1.23.2` → `v1.24.1` (transitively `prometheus/common`, `prometheus/procfs`, `go-logr/logr`). Internal dependency only; no `pkg/gincommon`/`pkg/grpccommon` API change.

---

## [1.2.1] - 2026-07-28

### Fixed

- **Security:** Bumped `go.mod`'s `go` directive to `1.26.5` and `google.golang.org/grpc` to `v1.82.1`, clearing 4 `govulncheck` findings: GO-2026-6061 (gRPC HTTP/2 transport & xDS RBAC authorization engine), GO-2026-5856 (`crypto/tls` Encrypted Client Hello privacy leak), GO-2026-5039 (`net/textproto` unescaped error inputs), and GO-2026-5037 (`crypto/x509` inefficient hostname parsing) — the latter three fixed in the Go 1.26.4/1.26.5 standard library.
- **`Dockerfile`:** Builder base image bumped to `golang:1.26.5-alpine` (was `1.26.3-alpine`). The `go.mod` bump above raised the minimum required Go version, and `GOTOOLCHAIN=local` was rejecting `go mod download` under the older image.

### Changed

- **CI (`validate.yml`):** Split the single `validate` job into parallel `quality` (fmt/tidy/vet/lint/vulncheck) and `test` (race detector) jobs, shortening the gate's wall-clock time. `ci.yml`/`release.yml` gate on the reusable workflow as a whole and are unaffected.
- **CI (`ci.yml`):** Added an `interop` job (Cross-language compatibility) calling `platform-interop-tests`'s reusable workflow after `build`, wired to the existing `GO_PRIVATE_TOKEN` secret.
- **CI:** `actions/upload-artifact` bumped to `v5`.

---

## [1.2.0] - 2026-06-03

### Added

- **`pkg/gincommon`:** `Shutdown(logger port.Logger) error` — graceful telemetry cleanup for HTTP services. Stops the active OpenTelemetry `TracerProvider` (flushing buffered spans) and, when the logger implements `Sync() error`, flushes buffered log entries. Call from `main()` after the HTTP server stops accepting requests.
- **`pkg/grpccommon`:** `Shutdown(logger port.Logger) error` — same graceful cleanup for gRPC services; call after `grpc.Server.GracefulStop` returns.
- **`metrics`:** `http_request_timeout_total` counter (labels: `method`, `route`) and `RecordHTTPTimeout` helper — incremented by `TimeoutMiddleware` when the request deadline expires before the handler responds, so timeout-induced `503`s are distinguishable from other `5xx` responses.
- **`config`:** `OtelTraceSampleRatio` now honors the OpenTelemetry-spec `OTEL_TRACES_SAMPLER_ARG` (used with `OTEL_TRACES_SAMPLER=traceidratio`) as a fallback when the library-specific `OTEL_TRACES_SAMPLER_RATIO` is unset.

### Changed

- **`metrics` (internal):** `RequestOutcome` and `GRPCRequestOutcome` now return a single `errorClass` string (the previously unused `isError` return was removed). HTTP metric `Help` text now documents the label set and the 3xx-as-2xx status-class rule. These packages are under `internal/` and are not importable by consuming services — no external API impact.
- **`metrics` (internal):** Duration/payload bucket variables renamed `HTTPRequestDurationBuckets` → `RequestDurationBuckets` and `HTTPPayloadSizeBuckets` → `PayloadSizeBuckets` to reflect that both HTTP and gRPC histograms share them. Internal-only; no external impact.
- **`http/middleware/metrics.go` + `grpc/middleware/metrics.go`:** Nil-collector sentinel now checks the primary counter (`HttpRequestsTotal` / `GrpcRequestsTotal`) and the in-flight gauge together, preventing a panic if a gauge is niled independently.

### Removed

- **`metrics` (internal):** Deleted the unused `ErrorClass`, `HTTPIsError`, `GRPCErrorClass`, and `GRPCIsError` helpers (they surfaced an `is_error` label that no collector used). Internal-only; no external API impact.

---

## [1.1.0] - 2026-05-29

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

[1.2.0]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/releases/tag/v1.0.0
[Unreleased]: https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/compare/v1.2.0...HEAD
