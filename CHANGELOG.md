# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.1.0] - 2026-10-21

> **Versioning:** a MINOR release on the `/v2` module path — additive only (RFC 9457 extension members). Nothing changes for problems without extensions: their JSON is byte-for-byte identical to v2.0.0.

### Security

- **Toolchain `go1.26.9` → `go1.26.10`**, fixing GO-2026-6809 / CVE-2026-111943 in `crypto/x509` (HIGH), which failed CI's `govulncheck` step and the Trivy scan of the demo image's Go binary. The demo image builder is re-pinned to `golang:1.26.10-alpine@sha256:9ce231b2…f7ca` (`.docker-digests` updated). The `go 1.26.0` directive is unchanged, so consumers are not forced onto a newer Go.

### Added

- **RFC 9457 §3.2 extension members** on problems (additive; MINOR, planned v2.1.0). Services no longer need to hand-serialise a problem to add members such as `record_version`, which bypassed `AbortWithProblem`'s enrichment and logging.
  - `problem.Problem.Extensions map[string]any`, serialised as top-level members after the standard ones, sorted by name. A problem without extensions serialises byte-for-byte as before.
  - `(*problem.Error).WithExtension(name, value)` and `WithExtensions(map)`; `(*problem.Builder).Extension(name, value)` and `Extensions(map)`; `BuilderFor` copies an error's extensions. `WriteProblem` / `AbortWithProblem` / the global error handler carry them through with traceId, instance, verified tenantId and logging unchanged.
  - `problem.ValidExtensionName(name)`: RFC 9457's letter + `[A-Za-z0-9_]`, 3+ characters, and not a standard or platform member in any letter case. Other names are **dropped**, so an extension can never overwrite `status`, `traceId`, `tenantId` or any other standard member.
  - An `internal-error` drops every extension, as it drops its detail.
  - An extension value that cannot be marshalled is dropped with the rest of the extensions, the problem is still sent, and a warn-level `problem extensions dropped` entry is logged (never the values).
  - `Decode` / `FromResponse` keep every unrecognised top-level member in `Extensions` (numbers as `json.Number`); `(*problem.Problem).Extension(name, &dst)` decodes one into a typed value.
  - The shared `ProblemDetails` OpenAPI schema documents extension members (it already allowed additional properties).

## [2.0.0] - 2026-10-16

> **Versioning:** this release is **v2.0.0**, a proper MAJOR with the `/v2` module path (`github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2`), not a recorded exception. Follow *Upgrading to v2.0.0* in `README.md` and the migration table in `docs/problem-details/README.md`.

### Security

- **`platform_unclassified_errors_total{transport}`** (registry `approved`, LLD OQ-25): counts every unclassified error. That is an HTTP `problem.Error.Unclassified()` 500 or a non-status gRPC handler error sent as `Internal`, with `transport` = `http` | `grpc`. It is recorded with or without a logger, and both series are initialised to 0, so `increase()` sees the first error. New reference alert `PlatformUnclassifiedErrors` (ticket), with a promtool test and a runbook. The `unclassified error` log event is retained for diagnostics; the metric deliberately has no route or method label. New registry label `transport`.
- **gRPC handler errors no longer leak to clients.** A new error sanitizer is the innermost interceptor of `ObservabilityUnaryInterceptors` / `ObservabilityStreamInterceptors` (and so of `Full*` / `Default*`). Before it, grpc-go sent a plain handler error as `Unknown` with `err.Error()` as the message, and a status wrapped with `fmt.Errorf("…: %w", st)` carried the wrapper's text. Now:
  - a status error is kept; a wrapped one is sent as its inner status (code, message, details) without the wrapper's text;
  - `context.DeadlineExceeded` / `context.Canceled` (wrapped or not) become `DeadlineExceeded "deadline exceeded"` / `Canceled "canceled"`;
  - anything else, including a non-nil error claiming `OK`, becomes `Internal "internal server error"` and is logged at error level as `unclassified error` (`classified=false`, `trace_id`, `request_id`, `service`, `grpc_method`, `grpc_code`, `error_type`, `error_chain`), never `err.Error()`.
  - Metrics, the `grpc_request` log and spans record the sanitized status (they run outside it). Callers that matched on `Unknown` or on error text must use status codes.
- **Toolchain `go1.26.6` → `go1.26.9` and `golang.org/x/net` v0.59.0 → v0.60.0**, fixing the standard-library and x/net vulnerabilities `govulncheck` reports as reachable from library code:
  - GO-2026-6617 (HTTP/2 HPACK encoder race)
  - GO-2026-6613 and GO-2026-6605 (HTTP/1 CONNECT desynchronization)
  - GO-2026-6612, GO-2026-6611 and GO-2026-6610 (HTTP/2 flow control, window CPU and framing headers)
  - GO-2026-6608 (MIME header memory limit)
  - GO-2026-6607 (crypto/tls ECH)
  - GO-2026-6603 (HTTP/2 trailer memory)
  - The demo image builder is re-pinned to `golang:1.26.9-alpine`. The `go 1.26.0` directive is unchanged, so consumers are not forced onto a newer Go, but they should build with 1.26.9 or later for the same fixes.

### Changed (breaking)

- **Module path is `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2`.** Every import changes to `.../platform-gincommon/v2/pkg/...`, and `go run .../v2/cmd/metricslint@vX.Y.Z` replaces the v1 path.
- **Every HTTP error is an RFC 9457 problem** (`Content-Type: application/problem+json`). The body was `{error, status, trace_id, request_id}`; it is now `{type, title, status, detail, instance, traceId, errorCode, tenantId, timestamp, validationErrors}`. Status codes are unchanged. The request id stays in the `X-Request-ID` response header.
  - 401s (`RequireAuth`, `ContextMiddleware`): `platform/unauthorized`, with the previous messages as sentences in `detail` (e.g. `Missing or invalid authentication headers.`).
  - 400 from a `ContextBuilder` error: `platform/bad-request`, `errorCode` `INVALID_REQUEST_CONTEXT`.
  - 403 from `RequirePermission`: `platform/forbidden`.
  - 500s (panic, missing request context, nil or failing `Authorizer`): `platform/internal-error` with **no detail**. The v1 bodies exposed `request context missing` / `invalid request context` on 500s.
  - 503 timeout: `platform/service-unavailable`, `errorCode` `REQUEST_TIMEOUT`.
- **Removed `gincommon.ErrorResponse`.** Clients decode `problem.Problem` with `problem.FromResponse`.
- **Gin's plain-text 404 (and 405 with `HandleMethodNotAllowed`)** for unregistered routes is now a `platform/not-found` (`ROUTE_NOT_FOUND`) / `platform/method-not-allowed` problem, for every engine that `r.Use`s `ObservabilityMiddlewares`.

### Added

- **`pkg/problem`**, the platform's RFC 9457 contract for Go. It uses the standard library only, so gateways, BFFs and clients can use it without Gin.
  - `Problem`, `FieldError`.
  - The error type registry: 15 `platform/*` types, `MustType` for domain types, `TypeForStatus`, `TypeURI`, `DefaultTypeBase = https://errors.company.com`. It is documented in `docs/problem-details/registry.md`.
  - `*Error` with the `New*Error` constructors that map the specification's exception classes. A cause is kept for `errors.Is`/`As` and never sent.
  - `FromError` (the global exception mapping).
  - `Builder` (an `internal-error` never carries a detail).
  - `ResolveTraceID` (`traceparent` → sanitized `x-request-id` → span → generated). It follows platform-pgcommon-py's order and passes its test vectors.
  - `FromResponse` / `Decode` for clients.
  - `OpenAPIComponents()` (the shared `ProblemDetails` schema and the 400/401/403/404/409/422/429/500 responses) and `StandardResponses()`.
- **Global error handler**, the innermost `ObservabilityMiddlewares` handler. It renders the last `c.Error(err)` and any bodiless error status as a problem, after the handlers return. It never touches a written response. `context.Canceled` is recorded as 499 without a body.
- **`gincommon.AbortWithProblem(c, err)`, `ClassifyError(err)`, `BindingError(err)`, `ShouldBindJSON(c, obj)`, `ShouldBind(c, obj)`.**
  - Validator failures become `validationErrors` with JSON field paths (`items[0].sku`) and fixed messages (`is required`, `must be at least 3 characters`, ...). The rejected value is never echoed.
  - Empty and malformed bodies get fixed details; `*http.MaxBytesError` is 413.
  - Gin's default validator reports `json` / `form` tag names once the error handler is built.
- **`Config.ProblemTypeBase`**: the type URI base (default `https://errors.company.com`).
- **Problem logging**: each problem is logged once at WARN as `problem response`, with `traceId`, `type`, `status`, `errorCode`, `service`, `tenantId`. These are the names and the level platform-pgcommon-py uses. The entry never contains the detail, the instance, headers or the identity.
- **Unclassified errors are logged as a defect, without their text.** A handler error the error handler cannot classify (a 500 from `problem.FromError`'s fallback; `problem.Error.Unclassified()`) also emits an error-level `unclassified error` entry: `classified=false`, `traceId`/`trace_id`/`request_id`, `service`, `method`, `route`, `operation` (the Gin handler), `status`, `errorCode`, and `error_type` / `error_chain` (the Go types along the `Unwrap` chain, at most 8). It never contains `err.Error()`, which may hold SQL, PII, tokens or secrets. Alert on it. Deliberate `problem.NewInternalError` and panics (already logged with their stack) are not unclassified.
- `tenantId` is set only from the verified `RequestContext` (never on a 401), and `instance` never includes the query string.
- **Docs**: `docs/problem-details/` (the contract, exception mapping, validation, trace correlation, logging, security, gateway/Envoy/BFF rules, OpenAPI, clients, migration) and the error type registry.
- **CI smoke gate**: the demo image must answer an unknown route with a `platform/not-found` problem.
- Tests:
  - `test/unit/problem`: registry invariants and doc drift, builder, mapping, trace vectors, client decoding, OpenAPI schema vs struct.
  - `test/unit/gincommon/errors_test.go`: the full stack, covering status mapping, validation, no leaks, tenant and trace.
  - `test/integration/problem_details_test.go`: gateway passthrough through a reverse proxy, a gateway-generated problem, and client parsing.

## [1.6.0] - 2026-10-03

### Added

- **Trace export can be turned off from the environment.** `OTEL_EXPORTER_OTLP_ENDPOINT=none` (also `off` / `disabled`, case-insensitive) or the OpenTelemetry spec's `OTEL_TRACES_EXPORTER=none` now disables OTLP export for `InitTracingWithConfig`, `InitTracingFromEnv` and the middleware stacks: the in-process TracerProvider is still installed (trace and request IDs keep working) and initialisation logs `exporter=disabled`.
  - Previously there was no way to do this with `InitTracingWithConfig`. An empty endpoint is treated as unset and falls back to `localhost:4317`, and `OTEL_TRACES_EXPORTER` was not read. A local run without a collector therefore logged a failed export on every flush.
  - Behaviour is unchanged for every other value: an empty or unset endpoint still means `localhost:4317`, and an `OTEL_TRACES_EXPORTER` other than `none` is ignored. New helper: `config.OtelTraceExportDisabled`.

## [1.5.0] - 2026-10-03

### Added

- **Registry: four more Tier 2 `iam_*` metrics** (`proposed`, no owner yet). IAM services already emit them, and until now `metricslint check` reported them as namespace errors:
  - `iam_rls_violations_total{violation_type}`: emitted by iam-org-membership, iam-audit-log and iam-user-profile. The new `violation_type` label is a fixed set: `cross_tenant_access`, `missing_or_invalid_guc`, `missing_primary_email`, `other`.
  - `iam_auth_session_revoke_failed_total{reason}`, with `reason` ∈ `transport`, `http_4xx`, `http_5xx`, `http_other`. iam-org-membership currently puts the raw HTTP status code in `reason` (e.g. `"503"`), which the `reason` vocabulary rejects, so it has to map codes to these classes.
  - `iam_lifecycle_event_skipped_total{event_type}` and `iam_lifecycle_event_lag_seconds{event_type}`. The new `lifecycle_event_lag` bucket preset is iam-org-membership's existing bucket set. The governance note on the lag histogram records its overlap with `platform_event_propagation_seconds`.

### Fixed

- **`release.yml`: release builds use their own GHA cache scope** (`release-<tag>`). `build-image` exports to it, and `trivy`, `smoke` and `push` read only from it.
  - On the first v1.4.0 run, `push` resolved the Go build from main's shared `ci.yml` cache, a record with identical inputs but a different binary, since the build is not byte-reproducible across builders.
  - The image-fingerprint gate rejected it before anything was pushed or signed.

## [1.4.0] - 2026-10-02

> **Versioning:** this release is tagged **v1.4.0** although it contains breaking changes — a recorded exception to SemVer (see `VERSIONING.md` → *Recorded exceptions*). No consumer had been deployed, and every consuming service is upgraded in lockstep. Follow the *Upgrading to v1.4.0* checklist in `README.md`.

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

- **Production hardening — HTTP:**
  - `gincommon.PropagateIdentityHeaders(c, req)`: everything `PropagateHeaders` sends, plus the identity headers from the `RequestContext` and the full configured OTel propagator output (including baggage). Use it only for trusted internal services.
  - `gincommon.Config.RequestTimeout`: when positive, `TimeoutMiddleware` is appended as the innermost `ObservabilityMiddlewares` handler, so metrics, the access log and the server span record the same 503 the client receives.
  - `domain.ParseRolesStrict(header) ([]string, error)`, `domain.ErrInvalidRoles`, `domain.MaxRolesHeaderLen` (4096) and `domain.MaxRoles` (64).
  - `ProtectedMiddlewares` now passes `Config.Logger` down, so every 401 emits a warn-level `auth: request rejected` audit entry. The entry has `reason`, `method`, `route`, `status`, `client_ip`, `trace_id`, `request_id`, and the sanitized, 256-byte-truncated `claimed_user_id` / `claimed_tenant_id`.
  - Authz denial and error log entries now include `tenant_id`.

- **Production hardening — gRPC:**
  - `grpccommon.UnaryClientInterceptor()` / `StreamClientInterceptor()`: outbound client interceptors that inject the caller's W3C trace context (global OTel propagator) and `x-request-id` into outgoing metadata. A request ID the caller sets explicitly is kept. They never forward identity.
  - `grpccommon.UnaryClientIdentityInterceptor()` / `StreamClientIdentityInterceptor()`: opt-in forwarding of the verified `x-user-id`, `x-tenant-id` and `x-tenant-roles` from the `RequestContext`, for trusted internal calls only. Values are set rather than appended, so downstream services never see repeated identity metadata. Calls without a `RequestContext` pass through unchanged.
  - `grpccommon.RegisterKnownMethods(srv ServiceInfoProvider)` (and the `ServiceInfoProvider` interface, which `*grpc.Server` implements): limits the `grpc_method` label and the server span name to the methods the server registered. Call it after all services are registered and before `Serve`. Any other method, for example a path that reaches `grpc.UnknownServiceHandler`, is recorded as `unknown`. The allow-list is process-wide and additive.
  - `grpccommon.TimeoutUnaryInterceptor(d)` / `TimeoutStreamInterceptor(d)`: the gRPC counterpart of `gincommon.TimeoutMiddleware`. They cap the handler deadline at `d` and keep an earlier client deadline. Zero or negative `d` does nothing.
  - `grpccommon.RegisterHealth(srv) *health.Server`: registers the standard `grpc.health.v1.Health` service.
  - Server spans carry `rpc.response.status_code` (semconv v1.40, e.g. `DEADLINE_EXCEEDED`) and the legacy integer `rpc.grpc.status_code`. Spans for unrecognised methods use `rpc.method="_OTHER"` plus a sanitized, length-capped `rpc.method_original`.
  - The request-ID interceptors send `x-request-id` back to the client as a response header (`grpc.SetHeader` / `ServerStream.SetHeader`), matching HTTP.

- **Production hardening — Metrics & metricslint:**
  - `metrics.ErrIdentityMismatch`: initializing metrics again with a different `domain`, `service`, `environment` or build `version` now returns this error (wrapped, naming the field). `ObservabilityMiddlewares`, `DefaultMiddlewares` and the `grpccommon` interceptor constructors panic on it at startup. The same identity again is still a no-op.
  - `metrics.AllowIdentityChangeForTesting(bool) (restore func())`: restores "first identity wins" for test harnesses (`test/testenv` enables it). The package is internal, so consumers cannot reach it.
  - `obsregistry.Registry.CheckVersion`. The build version (`Config.BuildVersion` / `BUILD_VERSION`, trimmed, default `unknown`) is now checked at startup against the registry's `version` pattern `^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`. An invalid value fails fast like an invalid domain. `unknown`, `dev`, semver, `git describe` output and short SHAs stay valid.
  - `metricslint check -allow-proposed-owner list` / `obsregistry.Options.AllowProposedOwners`: under `-strict`, proposed registry entries owned by the listed libraries (e.g. `platform-gincommon`) stay warnings. Ownerless proposed entries are still errors.
  - `metricslint check` accepts OpenMetrics text (detected by `# EOF`). Counter, info, stateset, unknown and gaugehistogram families are mapped to the text format, and timestamps, exemplars, `# UNIT` and `_created` samples are ignored. `prometheus/common` has no OpenMetrics parser, so the input is normalized first.
  - New validation rules `label-name` (service-owned Tier 3 label names must be snake_case on the scrape, as `CheckCustom` already required at construction) and `gather` (a collector failed during `Gather`).
  - `pkg/obsregistry` re-exports `Severity`, `SeverityError`/`SeverityWarning` and the `Rule*` constants.

- **Production hardening — Tracing, logging & CI:**
  - `gincommon.InitTracingWithConfig(gincommon.TracingConfig) (func(), error)`: starts tracing before `ObservabilityMiddlewares` with the same identity (`ServiceName` required, `BuildVersion`, `Domain`, `Environment`, `Logger`, `Tracing *TracingOptions`). Returns `gincommon.ErrServiceNameRequired` when `ServiceName` is empty.
  - Trace resources now carry `service.namespace` (the observability domain) and `deployment.environment.name` (semconv v1.40.0) when they are known. They come from `Config.Domain`/`Environment`, falling back to `OBSERVABILITY_DOMAIN` and then `APP_ENV` → `ENVIRONMENT`, for both HTTP and gRPC stacks.
  - `TracingOptions.IgnoreRemoteParentSampled *bool` (in both `gincommon` and `grpccommon`) and the env var `OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED` (default `false`). When enabled, spans whose remote parent is marked sampled are re-sampled at the configured ratio, so an external caller cannot force 100% sampling by sending a sampled `traceparent`. Turn it on for edge services, or have the gateway re-issue `traceparent`.
  - `LOG_LEVEL` (`debug`|`info`|`warn`|`error`; default `debug` in `dev`/`local`, `info` otherwise). An invalid value keeps the default and prints a warning to stderr once.
  - `LOG_SAMPLING` (default `false`) turns Zap sampling on (100 entries/s per message+level, then 1 in 100).
  - Tracing init writes the effective configuration once (endpoint, insecure, exporter state, ratio, remote-parent mode, identity) as `otel_tracing_initialized`. OpenTelemetry SDK errors go through `otel.SetErrorHandler` to `Config.Logger` (`otel_error`), or to stderr when there is no logger.
  - `.github/dependabot.yml`: weekly `docker` ecosystem updates, alongside the existing `gomod` and `github-actions` ones.
  - Dependabot version updates are disabled, as in platform-events. The config moves to `.github/dependabot.yml.disabled`, so GitHub stops opening update PRs, but the configuration is kept. Re-enable with `git mv .github/dependabot.yml.disabled .github/dependabot.yml`. Dependabot alerts and security updates are repository settings and are unaffected.

- **Production hardening, review 2 — HTTP:**
  - `pkg/gincommon` and `pkg/grpccommon` export public aliases for the core types in their signatures, so a consuming module can implement and name them without importing `internal/` (which Go forbids): `Authorizer` (= `port.Authorizer`), `Logger` (= `port.Logger`), `DemoRBAC` (= `service.RBACService`, the `NewDemoRBAC` return type) and `ErrForbidden` (= `domain.ErrForbidden`) in both; the request identity type as `RequestContextData` in both packages (`gincommon.RequestContext` is already the accessor function), the same type, so a single `Authorizer` works for both stacks.
  - HTTP `ContextMiddleware` writes a warn-level `context: request rejected` audit entry (reason, status 400, correlation IDs, sanitized `claimed_*` identity) when the `ContextBuilder` fails with a non-sentinel error. Before, that 400 path wrote no audit entry. The builder's error text is logged sanitized and is never sent to the client.
  - The demo router sets `Config.RequestTimeout` to `server.DemoRequestTimeout` (30s), which the `cmd/server` comment already described.

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

- **Production hardening — HTTP:**
  - `CorrelationHeadersMiddleware` set `X-Trace-ID` / `X-Request-ID` after `c.Next()`, which is after the response had already been flushed, so clients never saw `X-Trace-ID` on handler-written responses. The headers are now set before the handler runs. Tests now check `w.Result().Header`, which reflects what was actually sent, instead of the live recorder map, which hid the bug.
  - `TimeoutMiddleware` counted a client disconnect (`context.Canceled`) as a timeout and answered 503. Only `context.DeadlineExceeded` triggers the timeout now. The docs now place the middleware inside the observability stack (or use `Config.RequestTimeout`), so the recorded status matches the status the client gets. They also state that the middleware does not interrupt the handler goroutine, so handlers must honour `c.Request.Context()`.
  - `PanicRecoveryMiddleware` appended a JSON 500 to a response the handler had already started writing. It now only aborts in that case. It also writes the 500 when the handler called `c.Abort()` before panicking, where the client previously got an empty 200 while metrics recorded 500.
  - `PanicRecoveryMiddleware` now re-panics `http.ErrAbortHandler` after metrics are recorded, so `net/http` aborts the connection silently. It is logged at Debug level without a stack.
  - `PanicRecoveryMiddleware` no longer annotates the server span, which had already ended by then and made the call a no-op. `TracingMiddleware` records the panic on the span.
  - `PropagateHeaders` doc claimed that a nil request panics; it is a no-op. A nil `*gin.Context` or a nil `req.Header` is now handled too.

- **Production hardening — gRPC:**
  - A handler that returns a plain `context.DeadlineExceeded` / `context.Canceled` (or an error wrapping one) is now classified as `DeadlineExceeded` / `Canceled` in metrics, logs and spans, which is what grpc-go sends to the client. Before, it was recorded as `Unknown`, counted as a server error and logged at Error. Metrics, logging and tracing now share one status helper.

- **Production hardening — Metrics & metricslint:**
  - `metricslint refs` no longer fails with `bufio.Scanner: token too long` on lines over 4 MiB (minified dashboards). Lines are now read without a length limit.

- **Production hardening — Tracing, logging & CI:**
  - When `otlptracegrpc.New` fails, the library now logs a warning (`otel_exporter_init_failed`) before falling back to an in-process provider. It used to fail silently.
  - An invalid or out-of-range `OTEL_TRACES_SAMPLER_RATIO`/`OTEL_TRACES_SAMPLER_ARG` now warns once on stderr. It used to fall back silently.
  - `verify-release-tag.sh` accepts a HEAD that carries several tags (`git tag --points-at HEAD`) and fails unless the tagged commit is reachable from `origin/main`.
  - `verify-changelog-entry.sh` matches `## [X.Y.Z]` as a fixed string, so the dots in the version are no longer regex wildcards.
  - The pre-commit hook fails when `make tidy` changed `go.mod`/`go.sum`. Previously it modified them silently and let the commit through.

- **Production hardening, review 2 — HTTP:**
  - The access log no longer puts identity under the verified `tenant_id` / `user_id` keys when `RequireAuth` accepted the headers but `ContextMiddleware` then rejected the request (for example an oversized `x-tenant-roles` returns 401). In that case the identity is logged as `claimed_tenant_id` / `claimed_user_id`.
  - Documented that the per-route 429 log limiter reports the suppressed count of a final burst only on the route's next logged 429 (there is no timer flush). Use `platform_http_rate_limited_requests_total` for totals.

- **Production hardening, review 2 — gRPC:**
  - **`TimeoutStreamInterceptor` now ends idle streams.** Before, the capped deadline existed only on the handler's stream context. `RecvMsg`/`SendMsg` block on the transport stream, so a handler looping on `RecvMsg` against an idle client never returned. The interceptor now runs the handler in a goroutine and returns `codes.DeadlineExceeded` when the cap fires. grpc-go then tears the stream down, which unblocks the pending call. After that, every stream call the handler makes returns `DeadlineExceeded`, so the goroutine exits.
    - A handler panic is re-raised in the interceptor goroutine with its original value, so `PanicRecovery`, metrics, logging and tracing still see it.
    - A panic after the deadline has fired is logged to stderr and dropped.
    - A client deadline earlier than the cap still runs the handler inline.
  - **`rpc.response.status_code` span attribute uses the canonical `CANCELLED`.** It was `CANCELED`. All 17 names now come from a fixed table; out-of-range codes are the decimal value.
  - **gRPC `x-tenant-roles` parsing matches HTTP.** The context interceptor uses `domain.ParseRolesStrict`: input over `MaxRolesHeaderLen` or with more than `MaxRoles` roles now returns `codes.Unauthenticated "invalid tenant roles"`. Before, every role was silently dropped. Repeated `x-tenant-roles` entries are still joined before parsing, and the joined total counts against the limits.

- **Production hardening, review 2 — Metrics & metricslint:**
  - `docs/observability/README.md` no longer says that `platform_queue_depth` has no owner (its owner is platform-events). It now says the only ownerless proposed entries are the four Tier 2 `iam_*` metrics. It also recommends `-allow-proposed-owner platform-gincommon,platform-events,platform-pgcommon` and documents the version sanitising and the "unknown inherits" rule.

- **Production hardening, review 2 — Tracing & config:**
  - Tracing: tracers obtained before a TracerProvider replacement no longer drop their spans. Examples are `gincommon.NewTracer` / `NewSpanTracer` (e.g. platform-pgcommon's `Config.Tracer` created after `InitTracingFromEnv()`) and package-level `otel.Tracer` values. A replacement happens when `ObservabilityMiddlewares` swaps out the provisional `unknown_service` provider, or on a second `InitTracing`. The package now installs one delegating global TracerProvider. Every span start uses the current SDK provider, which is swapped atomically. The replaced provider is force-flushed and shut down after the swap, within a 5s limit and without holding the init lock. Spans already in flight at swap time end on the old provider; any that end after its shutdown are dropped.
  - Tracing: OpenTelemetry SDK errors (`otel_error`) now reach `Config.Logger` when tracing was first started by `InitTracing` / `EnsureTracing` / `InitTracingFromEnv` (no logger) and a later `ObservabilityMiddlewares(Config{Logger})` keeps that provider. A logger already configured is not replaced.
  - Config: `APP_ENV` is trimmed and lowercased before dev detection, as the metrics identity already does. With `APP_ENV=Dev`, OTLP export now defaults to plaintext and sampling to 1.0, matching the `dev` metrics environment. Before, it got TLS and a 0.1 ratio. `config.AppEnv()` returns the normalised value, `config.IsDevEnv` and the Zap logger's dev detection compare case-insensitively, and `config.NormalizeEnv` is new.

- **Production hardening, review 3:**
  - `PanicRecoveryMiddleware`: a handler that panics after writing part of the response now aborts the connection (re-panics `http.ErrAbortHandler`), so the client sees a truncated response instead of a complete 200 while metrics, logs and the span record 500.
  - A wrapped `http.ErrAbortHandler` is re-panicked as the bare sentinel, because net/http compares by identity; previously the server still logged it with a stack trace.
  - Metrics identity: a real build version now upgrades an established `unknown` one (`platform_build_info` is relabelled), so a gRPC stack built before an HTTP stack with a version no longer panics at startup. Two different real versions are still `ErrIdentityMismatch`.
  - Tracing `Shutdown` keeps the delegating global provider installed (recording nothing until the next init) instead of swapping in a plain no-op provider, so a provider captured during a restart is live again after re-init, and a consumer's own provider is never replaced.
  - README quick starts compile and start as written from an external module: the gRPC example uses `grpccommon.Logger` (not the internal `port.Logger`), and the HTTP example passes `Environment: appEnv` so it starts with `APP_ENV` unset.

- **LLD-vs-code verification fixes:**
  - gRPC authz audit entries (`grpc authz: access denied` / `grpc authz: internal error`) now include `tenant_id`, matching HTTP.
  - `PropagateIdentityHeaders` removes an `x-tenant-roles` header already on the outbound request when the caller has no verified roles, so an unverified value is never forwarded (matches the gRPC identity client interceptor).
  - The gRPC tracing interceptor sanitizes the panic value recorded on the span (control characters stripped, 256-byte cap), matching HTTP.
  - Every library-produced HTTP 500 body is `internal server error`. `PanicRecoveryMiddleware` previously wrote `Internal Server Error` while `AuthZMiddleware` wrote the lowercase form.
  - `RegisterKnownMethods` on a server with no registered services is a no-op. Previously it switched to allow-list mode with an empty list and mapped every method to `unknown`.

### Security

- **deps:** Bumped `google.golang.org/grpc` `v1.83.0` → `v1.83.2`. `v1.83.1` cleared govulncheck finding GO-2026-6348; `v1.83.2` clears GHSA-2v4p-qf9q-27wj (xDS server DoS on missing `:authority`/`Host`, high; the library does not use xDS). Indirect `golang.org/x/{crypto,net,text,tools,mod}` patch bumps. No `pkg/gincommon`/`pkg/grpccommon` API change.

- **Production hardening — HTTP:**
  - `x-tenant-roles` is now capped at 4096 bytes and 64 distinct valid roles. `ContextMiddleware` rejects over-limit headers with 401 `invalid tenant roles`, the same way as other invalid identity. `domain.ParseRoles`, which the gRPC context interceptor uses, fails closed and returns no roles on over-limit input.
  - Panic values are sanitized (control characters stripped, truncated to 256 bytes) in the stderr fallback log and in the span exception event, which prevents log and trace injection.
  - Gateway identity headers are no longer leaked to arbitrary outbound destinations by `PropagateHeaders` (see Removed).
  - Unverified, client-claimed identity is no longer logged under the verified `tenant_id` / `user_id` fields.

- **Production hardening, review 2 — HTTP:**
  - HTTP `RequireAuth` and `ContextMiddleware` reject a repeated `x-user-id` or `x-tenant-id` header with 401 `ambiguous <header> header`, logged in the `auth: request rejected` audit entry. Before, the first value was trusted, so a client could append its own value next to the gateway's and the outcome depended on header order. This matches the gRPC `ambiguous ... metadata` rejection.

- **Production hardening, review 2 — gRPC:**
  - **gRPC health exemption is now an exact match.** The auth, context and authz interceptors (unary and stream) exempt only `/grpc.health.v1.Health/Check`, `/Watch` and `/List` (the `grpc_health_v1` `*_FullMethodName` constants). Before, any path starting with `/grpc.health.v1.Health/` was exempt, so with `grpc.UnknownServiceHandler` a caller could reach e.g. `/grpc.health.v1.Health/x/admin.Svc/Delete` and skip `RequireAuth`, `ContextInterceptor` and `RequirePermission`. Near-miss paths (`/Health/CheckX`, `/HealthX/Check`, `/Health/x/a.B/C`, a trailing slash) are now authenticated like any other method.
- **Dependencies upgraded to their latest releases:**
  - OpenTelemetry `v1.45.0` → `v1.46.0` (otel, sdk, trace, otlptracegrpc)
  - `google.golang.org/grpc` `v1.83.2` → `v1.84.0`
  - `prometheus/common` `v0.70.1` → `v0.72.0`, `prometheus/client_model` `v0.6.2` → `v0.6.3`
  - `google.golang.org/protobuf` `v1.36.11` → `v1.36.12`
  - indirect `golang.org/x/*`, genproto and testify patch releases

  govulncheck reports no vulnerabilities. The `go 1.26.0` directive is unchanged.
- **Demo image:** the runtime stage upgrades `libssl3`/`libcrypto3` to `3.3.7-r2` (CVE-2026-75804, CVE-2026-84782, HIGH), which the pinned `alpine:3.21` digest predates. Found by the first CI Trivy run.

### Removed (breaking)

- **Pre-standard metric names, with no compatibility period:** `build_info`, `http_requests_total`, `http_request_duration_seconds`, `http_request_size_bytes`, `http_response_size_bytes`, `http_active_requests`, `http_panic_total`, `rate_limited_requests_total`, `http_request_timeout_total`, and the `grpc_server_*`, `grpc_rate_limited_requests_total` and `grpc_panic_total` names. No platform library had been deployed to production or dev, so there were no dashboards, alerts or series to migrate. gincommon emits only `platform_*` (mapping: `docs/observability/migration.md`). The central registry keeps every removed name, including platform-events', platform-pgcommon's and the Python ports', with status `removed`, so `metricslint` reports emitting or querying one as an error naming its replacement.
- **The domain is now required.** `ObservabilityMiddlewares` and the `grpccommon` interceptor constructors panic without `Config.Domain` / `OBSERVABILITY_DOMAIN`. Consuming services must set it when they upgrade.
- **`gincommon.Config.DisableLegacyMetrics` and `grpccommon.Config.DisableLegacyMetrics`** (unreleased).
- **Internal:** `metrics.Init` / `metrics.InitWithRegisterer`. Use `metrics.InitWithOptions` / `InitWithRegistererOptions`, or the `test/fixtures` helpers in tests.
- **Constructors fail before initialization.** The metrics constructors (`NewCounterVec`, `NewShared*`, ...) now return an error until metrics are initialized, instead of registering collectors without identity labels.

- **Production hardening — HTTP:**
  - `port.Authorizer.Authorize(userID string, roles []string, action, resource string) error` is replaced by `Authorize(ctx context.Context, rc *domain.RequestContext, action, resource string) error`. Authorizers now get the request context (deadline, cancellation, trace span) and the full `RequestContext`, including `TenantID`, so policies can be tenant-scoped. The return contract is unchanged: `nil` allows, an error wrapping `domain.ErrForbidden` denies, and any other error is an internal failure. The HTTP `AuthZMiddleware`, the gRPC authz interceptors and `NewDemoRBAC` (`service.RBACService`, which denies a nil `rc`) are updated.
  - `gincommon.PropagateHeaders` no longer sends `x-user-id`, `x-tenant-id`, `x-tenant-roles` or W3C baggage. It now sends only `traceparent`/`tracestate` and `x-request-id`, so it is safe for any destination. Call `PropagateIdentityHeaders` for trusted internal calls that need the caller's identity.

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

- **Production hardening — HTTP:**
  - Internal middleware constructors take a nil-safe logger: `middleware.RequireAuth(logger)` and `middleware.ContextMiddleware(ctxSvc, logger)`.
  - The `http_request` access log now logs `tenant_id` / `user_id` only for verified identity: the `RequestContext` built by `ContextMiddleware`, or else the values `RequireAuth` validated. On public or rejected routes, the client-supplied headers are logged sanitized under `claimed_tenant_id` / `claimed_user_id`, and are left out when absent. Dashboards and queries that read `user_id` on unauthenticated routes must switch to `claimed_user_id`.
  - The `method` field in the access log and panic log is sanitized.
  - `TracingMiddleware` bounds the HTTP method. A non-standard method is recorded as `http.request.method="_OTHER"` with a span named `HTTP <route>`, and the original method is kept (truncated to 32 bytes) in `http.request.method_original`, per the OTel HTTP semantic conventions. `server.address` (the Host header) is truncated to 256 bytes.
  - `MetricsMiddleware` still counts every 429, but the warn-level `rate limited` log is now sampled to at most one entry per route per second (`middleware.RateLimitLogInterval`). A new `suppressed` field counts the 429s on that route that were not logged since the previous entry.
  - `ObservabilityMiddlewares` doc: metrics identity is initialized once per process, so a later call with a different `ServiceName` / `Domain` / `Environment` is a configuration error. Calling it again does not reconfigure it.
  - The `cmd/server` reference `http.Server` now sets `ReadHeaderTimeout` (5s), `ReadTimeout` (30s), `IdleTimeout` (120s) and `MaxHeaderBytes` (1 MiB).

- **Production hardening — gRPC:**
  - **Breaking:** the order of `FullUnaryInterceptors` / `FullStreamInterceptors` is now recovery → request ID → tracing → metrics → **logging → auth → context**, the same as `Observability*` followed by `Protected*`. Calls rejected by `RequireAuth` or the context interceptor now produce a `grpc_request` log at Warn with `grpc_code=Unauthenticated`. Authenticated calls still log `user_id`/`tenant_id`: the context interceptor passes the verified `RequestContext` back to the logging interceptor. The `Observability*` + `Protected*` composition gets the same identity fields.
  - **Breaking:** `grpc_method` metric labels, the `method` log field and server span names are bounded. A name that is empty, longer than 256 bytes or doesn't match `^/[A-Za-z0-9_.]+/[A-Za-z0-9_]+$` maps to `unknown` (a subset of the registry `grpc_method` pattern). After `RegisterKnownMethods`, unregistered names map to `unknown` as well.
  - **Breaking:** `RequireAuth` and the context interceptors reject repeated `x-user-id` / `x-tenant-id` metadata with `codes.Unauthenticated` "ambiguous <key> metadata". Previously the first value was trusted.
  - The auth and context interceptors let `/grpc.health.v1.Health/*` through without identity, so health probes work behind the `Protected*`/`Full*` chains. This is the gRPC counterpart of registering `HealthHandler` before the protected group. Observability still applies to these calls.
  - `grpccommon.RequirePermission*` / `RequireStreamPermission*` also let `/grpc.health.v1.Health/*` through, so a server-wide permission interceptor does not reject health probes with `FailedPrecondition`.

- **Production hardening — Metrics & metricslint:**
  - **Breaking:** metrics initialization is now transactional. Every collector is registered with `Register` (not `MustRegister`). On a failure, the ones already registered are unregistered and the error is returned. Nothing is published, and the call can be retried (`sync.Once` was replaced by a mutex plus the published set). `InitWithRegistererOptions` returns registration errors instead of panicking.
  - Recording helpers (`RecordHTTP`, `RecordGRPC`, `HTTPActiveInc`, ...) and `Registerer`/`ConstLabels`/`Identity`/`Initialized` now read an atomically published collector set, so recording no longer races with re-initialization. The exported collector vars (`metrics.HttpRequestsTotal`, ...) are kept as a read-only view for tests. Assigning them no longer affects recording.
  - `MetricsHandler` uses `promhttp.ContinueOnError` and logs gather errors to stderr. A failing custom collector no longer blanks the whole `/metrics` page.
  - `pkg/obsregistry.Validate` validates the families that were gathered even when `Gather` returns an error. It returns the report together with the error, and the report carries a `gather` error finding.
  - The `payload_size` bucket preset (gincommon-owned, used only by `platform_*_size_bytes`) now adds 4 MiB (the gRPC default max message size) and 16 MiB upper bounds. `metrics.PayloadSizeBuckets`/`RequestDurationBuckets` are now copied from the registry. Python ports that vendor `registry.json` must re-copy it.
  - `platform_http_request_size_bytes` no longer observes requests of unknown length (`Content-Length: -1`, chunked) as 0 bytes: `metrics.RequestBodySize(-1)` returns the new `metrics.UnknownSize` (-1), and `RecordHTTP`/`RecordGRPC` skip negative sizes. Registry help text updated.
  - `metricslint check -url URL file...` is now a usage error (exit 2) instead of silently ignoring the files.
  - `metricslint registry -registry missing.json` exits 2 (I/O error) per the documented convention. An invalid document still exits 1. Exit codes are now listed in the usage text.
  - `metricslint refs` skips `.git`, `node_modules` and `vendor` while walking (an explicitly named root is still scanned). An entry it cannot read inside a walked directory is reported as an error finding (`path:0: error: cannot read: ...`) and the walk continues, instead of aborting with exit 2.
  - `CheckCustom` on a legacy (removed/deprecated) name now points to the replacement instead of `NewShared*` (which rejects legacy names).
  - The `_created` companion exemption is now precise. Only a gauge or untyped `X_created` whose label sets equal those of the counter `X_total` (or the histogram/summary `X`) is exempt. Any other `*_created` family is validated.

- **Production hardening — Observability docs:**
  - `docs/observability/README.md`: notes that `-strict` / `Options{Strict: true}` currently fails on every gincommon service, because all gincommon `platform_*` metrics are `proposed` pending governance, and explains the `-allow-proposed-owner` workaround. Also documents one identity per process, version validation, `metricslint` exit codes, OpenMetrics input, the refs walk behaviour, the new rules, and the payload preset change.

- **Production hardening — Tracing, logging & CI:**
  - **Breaking:** Zap sampling is now off by default. Previously the production config dropped about 99% of identical `http_request` entries (and repeated warn/error entries) above 100 rps. The level and sampling now apply the same way to stderr and `LOG_FILE` output. The `LOG_FILE` sink used to log at debug in every environment; it now follows the resolved level.
  - **Breaking:** `IsDevEnv` and the logger's dev detection accept only `dev` and `local`, matching the registry. `APP_ENV=development` no longer gets dev OTel or logging defaults; it was always rejected by metrics identity.
  - **Breaking:** the sampler is always `ParentBased`. With a ratio ≥ 1 or ≤ 0 it used to ignore the parent's decision.
  - **Breaking:** `InitTracingFromEnv` has no library default service name any more. It reads `OTEL_SERVICE_NAME`, then `APP_NAME`. When neither is set it warns on stderr and exports as `unknown_service`, and the provider is provisional: the first `ObservabilityMiddlewares`/gRPC interceptor stack (or `EnsureTracing` with a name) replaces it with the configured `ServiceName`. `InitTracing`/`EnsureTracing` with an empty service behave the same way.
  - **Breaking:** `gincommon.EnsureInitTelemetry` was removed (now unexported). Its parameter type was internal, so consumers could not call it. Use `EnsureTracing`, `InitTracingWithConfig` or `ObservabilityMiddlewares`.
  - `go.mod` now declares `go 1.26.0` and `toolchain go1.26.9`, so the module no longer forces consumers onto Go 1.26.9. CI still builds with 1.26.9 because `setup-go` uses `go-version-file`, which prefers `toolchain`.
  - CI: every GitHub Action is pinned to a full commit SHA with a `# vX.Y.Z` comment. The `platform-interop-tests` reusable workflow is pinned to a `main` commit SHA (that repo has no tags yet). The pin moved to `c7e62e6`: the interop Go probe was updated for v1.4.0 (it sets an observability identity and forwards identity explicitly to its peer), which fixes the four pair-2 interop failures on PR #30.
  - `.golangci.yml` moves from the v1 `issues.exclude-rules` to the v2 `linters.exclusions.rules` (`golangci-lint config verify` now passes), and `make lint` runs `config verify` before linting.
  - Boolean `OTEL_*` env vars are parsed case-insensitively. An unrecognised value warns once on stderr and falls back to the default.

- **Production hardening, review 2 — HTTP:**
  - `http.ErrAbortHandler` (or an error wrapping it) is no longer treated as a server panic by `MetricsMiddleware`, `LoggingMiddleware` and `TracingMiddleware`. The request is recorded with the status already written, else 499 (`middleware.StatusClientClosedRequest`, `status_class=4xx`, `error_class=client_error`), and `platform_http_panics_total` is not incremented. The access log entry is at Debug with `"aborted": true`. The server span has no Error status: it gets an `http.aborted` attribute and event, and is ended before the re-panic so the SDK does not add an `exception` event. The same value is still re-panicked so net/http aborts the connection silently.
  - HTTP `x-tenant-roles`: all repeated header values are joined with `,` before `ParseRolesStrict`, the same way gRPC handles repeated metadata. Before, only the first value was read.
  - `TimeoutMiddleware` keeps a response whose status the handler set without a body (for example `c.Status(http.StatusNoContent)` after the deadline) and no longer replaces it with a 503. Gin cannot tell an explicit `c.Status(200)`, or headers set without a status, from "nothing set", so those cases still get the 503.

- **Production hardening, review 2 — gRPC:**
  - `UnaryTimeoutInterceptor` / `TimeoutUnaryInterceptor`: the unary cap works only when the handler honours `ctx`, because a unary handler cannot be interrupted safely.
  - `RegisterKnownMethods` (and `label.RegisterGRPCMethods`): the method allow-list is process-wide. Call it for every server in the process, after all services (including `RegisterHealth`) are registered. Any server or service left out is recorded as `unknown`.

- **Production hardening, review 2 — Metrics & metricslint:**
  - **Build version is sanitised, not fatal.** `Config.BuildVersion` / `BUILD_VERSION` values outside the registry `version` pattern no longer panic at startup. A leading `refs/tags/` or `refs/heads/` is removed, any other disallowed character becomes `_`, leading non-alphanumerics are dropped, and the result is cut to 64 characters (`refs/tags/v1.4.0` → `v1.4.0`, `feat/observability-api` → `feat_observability-api`). Each rewritten value is reported once on stderr with the original and the sanitised value. Only a version with nothing usable left (e.g. `///`) still panics. `obsregistry.CheckVersion` is unchanged, and metricslint still uses it.
  - **A later "unknown" version inherits the established one.** For example, `gincommon.Config{BuildVersion: "1.4.0"}` together with `grpccommon.Config{}` (with `BUILD_VERSION` unset) in one process no longer panics with `ErrIdentityMismatch`. The reverse still panics: if the first stack set `unknown`, a later real version is a mismatch. Domain, service and environment still have to match exactly.
  - **`metricslint refs` exits 2 on I/O errors.** An unreadable path, whether inside a walked directory, named as a file or missing, now becomes an error finding. The scan carries on and prints every finding, then exits **2** instead of 1, as the documented exit codes say.
  - **`metricslint refs` / `ScanReferences` reads at most 64 MiB per file.** A larger file is reported as an I/O error finding (`ErrReferenceFileTooLarge`) instead of being loaded into memory in full. References found before the cap are still reported.
  - **`MetricsHandler` rate-limits its error log.** Gather and encoding errors now go through `pkg/logger.Default()`, falling back to stderr. Each distinct error is logged when first seen, then at most once every 5 minutes, and the set of remembered errors has a fixed size limit. `MetricsHandler` also registers `promhttp_metric_handler_errors_total{cause}` on `MetricsRegisterer`, so every failed scrape is counted on `/metrics`. `promhttp_*` is an exempt prefix in the registry.
  - New `test/unit/identity` package, which does not use testenv. Through the public constructors, it checks that a different `ServiceName` (or a different real version) panics with `ErrIdentityMismatch`. It also checks that the same identity, or the same identity with no version, does not panic.
  - New `test/unit/metricshandler` package: the error counter is exposed and counts every failed scrape, and the scrape still passes `obsregistry.Validate`.
  - New tests for `SanitizeVersion` (each example), the one-time and bounded stderr warning, the rate limiter, the refs size cap and refs exit code 2.

- **Production hardening, review 2 — Tracing & config:**
  - `otel.GetTracerProvider()` now returns the package's delegating provider while gincommon tracing is active, not a `*sdktrace.TracerProvider`. Code that type-asserts the global to `*sdktrace.TracerProvider` must use `otel.SDKProvider()` / `otel.IsGlobalProvider()` (internal) instead. A consumer that registers its own `*sdktrace.TracerProvider` is still kept by `EnsureTracing` / `ObservabilityMiddlewares`.
  - README: the gRPC per-RPC authorization example is now valid Go. It no longer uses `append(a, b..., x)`.

- **Production hardening, review 3:**
  - **Breaking (unreleased API):** the gRPC request-identity alias is `grpccommon.RequestContextData`, the same name as `gincommon.RequestContextData`.
  - `TimeoutUnaryInterceptor` / `TimeoutStreamInterceptor` are documented to go **innermost** (after `Full*` / `Observability*`), so metrics, logs and spans record the `DeadlineExceeded` the client receives rather than an abandoned stream handler's later result. The README example is updated and an integration test pins it.

- `.env-example` sets only values that differ from a built-in default (`OBSERVABILITY_DOMAIN`, `APP_ENV`, `LOG_FILE`); the rest are commented overrides. It no longer sets `BUILD_VERSION=dev`, which overrode the Makefile's `git describe` version. The local docker-compose port variables are labelled as stack-only and not read by the library. Removed the unused internal `config.PrometheusScrapeTarget()`.

- Makefile aligned further with iam-org-membership: explicit test package groups (`TEST_UNIT_PKGS`, `TEST_INT_PKGS`, `TEST_E2E_PKGS`, `TEST_SMOKE_PKGS`, `TEST_INTERNAL_PKGS`), and `test-unit` now also runs the white-box gRPC middleware tests. `make run` frees `APP_PORT`, exports `.env` and passes the git `BUILD_VERSION`. `docker-up`/`docker-down` work without a `.env`. Variables whose defaults already live in `internal/config` or `docker-compose.yml` are no longer redeclared or exported. Every target name CI and the docs use is unchanged.

- **`release.yml` mirrors platform-pgcommon's pipeline:**
  - **`verify` job first:** dispatch ref, tag on `main`, CHANGELOG section (an rc may use its base version's section), `SOURCE_DATE_EPOCH`, forward-only floating tags.
  - **Same gates as `ci.yml` at the tag:** validate-test, validate-quality, build-image (Hadolint), trivy (CRITICAL/HIGH/UNKNOWN), smoke and interop, plus `binaries`.
  - **`push` only when the image matches what was tested:** the image's content fingerprint must equal the one trivy and smoke recorded, checked both before and after the push.
  - **`publish` signs `checksums.txt`:** Cosign `sign-blob`, shipped as `checksums.txt.sigstore.json`.
  - **SLSA provenance** is read from the BuildKit image index instead of `cosign download attestation`.
  - **Image tags:** `X.Y`/`X` only move forward; the demo image still never gets `latest`.
  - New scripts `image-fingerprint.sh`, `release-image-tags.sh`, `release-checksums.sh` and `changelog-section.sh`; `verify-release-tag.sh`, `verify-changelog-entry.sh`, `extract-release-notes.sh`, `create-github-release.sh` and `export-image-provenance.sh` are aligned with platform-pgcommon.

## [1.3.0] - 2026-08-18

### Added

- **`pkg/gincommon`:** `MetricsRegisterer() prometheus.Registerer` and `MetricsConstLabels() prometheus.Labels` — expose the registerer and `{service, version}` const labels gincommon's own HTTP metrics are registered with, so consuming services can register custom application collectors consistently (instead of guessing `prometheus.DefaultRegisterer` and duplicating the labels themselves). Call after `ObservabilityMiddlewares`/`DefaultMiddlewares` has run.
- **`pkg/grpccommon`:** Same pair, `MetricsRegisterer()` and `MetricsConstLabels()`, for gRPC services. Metrics initialization is shared process-wide with `pkg/gincommon`.

### Fixed

- **Security:** Bumped `go.mod`'s `go` directive and the Docker builder base image to `1.26.9`, clearing 2 `govulncheck` findings in the standard library: GO-2026-6067 (`crypto/tls`) and GO-2026-5972 (`encoding/asn1`, recursion depth).

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
