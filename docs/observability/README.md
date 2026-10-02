# Enterprise Platform Observability Standard — gincommon implementation

This library is where the standard is implemented for every Go service:

| Piece | Location |
|---|---|
| Platform Observability Registry (platform + domain metrics, label vocabulary, rules) | [`internal/obsregistry/registry.json`](../../internal/obsregistry/registry.json), rendered as [`metric-registry.md`](metric-registry.md) |
| Central label injection (`domain`, `service`, `environment`) | `gincommon.Config` / `grpccommon.Config` → `Domain`, `Environment`, `ServiceName`; `platform-events` / `platform-pgcommon` → their `Identity` |
| Shared metric constructors (registry-driven) | `gincommon.NewSharedCounterVec` / `NewSharedGaugeVec` / `NewSharedHistogramVec` |
| Service-specific metric constructors (Tier 3, validated) | `gincommon.NewCounterVec` / `NewCounter` / `NewGaugeVec` / `NewGauge` / `NewHistogramVec` |
| Compliance checks for services' CI | `pkg/obsregistry.Validate`, `cmd/metricslint` |
| Reference recording rules, SLO burn-rate alerts | [`docker/prometheus/rules/`](../../docker/prometheus/rules/) |
| Removed pre-standard names and their replacements | [`migration.md`](migration.md) |
| Alert runbooks | [`runbooks.md`](runbooks.md) |

## Platform libraries

The registry is the single central contract for every platform library. The libraries own the instrumentation; this registry owns the names, labels, values and buckets.

| Library | Emits | Status vs. this registry |
|---|---|---|
| platform-gincommon (Go) | `platform_http_*`, `platform_grpc_*`, `platform_build_info` | Validated in CI (`test/unit/platformmetrics`) |
| platform-events (Go) v1.6.0 | `platform_messages_*`, `platform_retry_total`, `platform_dlq_*`, `platform_outbox_*`, `platform_dependency_request_seconds`, ... | Its released registry is imported here verbatim; its emitted scrape validates with 0 errors |
| platform-pgcommon (Go) | `platform_db_*` | Its registry is imported here verbatim; its emitted scrape validates with 0 errors |
| platform-fastapicommon (Python port of gincommon) | Same `platform_http_*`/`platform_grpc_*` as gincommon (unary gRPC only) | Ships a vendored copy of this `registry.json` (`src/fastapicommon/metrics/registry.json`); re-copy it whenever this file changes. Scrape validates with 0 errors |
| platform-eventcommon (Python port of platform-events) | 18 of platform-events' 23 Tier 1 metrics (no inbox, queue-depth sampler, handler timeout or ordered outbox in Python); `platform_library_info{library="platform-eventcommon"}` | Scrape validates with 0 errors |
| platform-pgcommon-py (Python port of platform-pgcommon) | All `platform_db_*` except `platform_db_pool_connections{state="constructing"}` (asyncpg can't report it) | Scrape validates with 0 errors |

`python_*` runtime collectors are exempt like `go_*`, and prometheus_client's `<metric>_created` companion families are classified with their parent: a gauge or untyped `X_created` whose series carry exactly the label sets of the counter `X_total` or the histogram/summary `X`. Any other `*_created` family is validated as a metric of its own.

Where a library's own registry restricts a label more tightly than this one (for example platform-events' `dependency ∈ {sns, sqs, codec}`), the central entry keeps the platform-wide vocabulary for metrics any service may emit, and records the library's set in `governance_notes`. The library's own CI still enforces its narrower set.

## Tiers

| Tier | Namespace | Required labels | Who may add a metric |
|---|---|---|---|
| 1 — platform-shared | `platform_*` | `domain`, `service`, `environment` | Observability governance only (registry PR) |
| 2 — domain-shared | `<domain>_*` | `service`, `environment` | Observability governance, with the domain's owners (registry PR) |
| 3 — service-specific | `<domain>_<service>_*` | `service`, `environment` | The service team, no registry entry needed |

Decision tree: can services in **several domains** emit it with identical meaning → `platform_*`. Otherwise, can **several services in one domain** → `<domain>_*`. Otherwise → `<domain>_<service>_<metric>`.

The Tier 3 prefix is derived from `Config.Domain` and `Config.ServiceName`: `iam` + `event-consumer` (or `iam-event-consumer`) → `iam_event_consumer_`.

`domain` is carried on Tier 2/3 too, because the same const labels are injected on every collector. `version` is **not** a const label: like platform-events and platform-pgcommon, gincommon puts it only on `platform_build_info`, because a per-build label on every series would multiply series on each deploy.

## Enabling the standard in a service

```go
cfg := gincommon.Config{
    Logger:       log,
    ServiceName:  "event-consumer",   // service label (kebab-case)
    BuildVersion: version,
    Domain:       "iam",              // or OBSERVABILITY_DOMAIN
    Environment:  "",                 // APP_ENV → ENVIRONMENT (same precedence as platform-events / platform-pgcommon)
}
r.Use(gincommon.ObservabilityMiddlewares(cfg)...)
```

- **The domain is required.** Set `Config.Domain` or `OBSERVABILITY_DOMAIN`; gincommon emits only `platform_http_*`, `platform_grpc_*` and `platform_build_info{version}`. The pre-standard names (`http_requests_total`, ...) were removed with no compatibility period, because no platform library had been deployed.
- `ObservabilityMiddlewares` (and the `grpccommon` interceptor constructors) **panic at startup** on a missing domain, on a domain that is not in the registry, on a service name that is not kebab-case, or on an environment outside `local, dev, test, staging, prod` (after trimming and lowercasing). There are no aliases: `APP_ENV=production` is rejected, so set `prod`. platform-events and platform-pgcommon read the same variables in the same order, so every library in the process gets the same label. This fails at startup on purpose, like the empty-`ServiceName` check: a bad identity label would corrupt cross-service aggregation for every metric.
- The build version (`Config.BuildVersion`, else `BUILD_VERSION`, else `unknown`) is trimmed and **sanitised** to the registry's `version` pattern `^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$` instead of failing startup: a leading `refs/tags/` or `refs/heads/` is removed, any other character becomes `_`, leading non-alphanumerics are dropped, and the result is cut to 64 characters (`refs/tags/v1.4.0` → `v1.4.0`, `feat/observability-api` → `feat_observability-api`). A rewritten value is reported once on stderr with the original and the sanitised value. Only a version with nothing usable left (e.g. `///`) panics.
- **One identity per process.** The first successful initialization fixes `domain`, `service`, `environment` and `version`. Building the stack again with the same identity (several routers, HTTP plus gRPC) is fine. A later initialization with no version (`unknown`, e.g. a `grpccommon.Config{}` without `BUILD_VERSION` next to `gincommon.Config{BuildVersion: "1.4.0"}`) inherits the established version. The reverse is still a mismatch: if the first stack established `unknown`, a later one naming a real version panics, so build the versioned stack first or set `BUILD_VERSION`. A different one panics with `metrics: already initialized with a different identity: <field> ...`, because otherwise it would be silently ignored and the second service's metrics would be reported under the first one's labels. A failed initialization (bad identity, or a registration conflict) registers nothing and can be retried.
- The metrics constructors (`NewCounterVec`, `NewShared*`, ...) return an error until `ObservabilityMiddlewares` or the gRPC interceptors have initialized metrics, because the identity labels are not known yet.

### Shared metrics (Tier 1 and 2)

```go
processed, err := gincommon.NewSharedCounterVec(nil, "platform_messages_processed_total")
// labels come from the registry, in registry order: queue, event_type
processed.WithLabelValues("iam.events", "user.created").Inc()

auth, err := gincommon.NewSharedCounterVec(nil, "iam_auth_events_total") // only in domain iam
dep, err := gincommon.NewSharedHistogramVec(nil, "platform_dependency_request_seconds") // registry buckets
```

Name, help text, label names and buckets all come from the registry entry, so instrumentation code cannot drift from the contract. A domain metric can only be created by a service in that domain. Label **values** must stay inside the vocabulary (for example `outcome` ∈ `success, failure, error`). CI checks this (see below).

### Service-specific metrics (Tier 3)

```go
hmac, err := gincommon.NewCounterVec(nil, "iam_event_consumer_hmac_failures_total", "HMAC verification failures", "reason")
lookup, err := gincommon.NewHistogramVec(nil, "iam_event_consumer_tenant_lookup_seconds", "Tenant lookup latency", nil, "cache")
```

The constructors reject:

- registry names and anything under `platform_*`
- counters without `_total`, histograms without `_seconds`, and gauges ending in `_total`
- prohibited labels: `user_id`, `email`, `tenant_id`, `request_id`, `event_id`, `session_id`, `trace_id`, ..., and any label ending in `_id`, `_email`, `_ip`, `_uuid` or `_token`
- declaring a centrally injected label
- with a `Domain` configured, any name outside the service's `<domain>_<service>_` prefix

### Libraries that register their own collectors

platform-events and platform-pgcommon inject the identity labels themselves from their own `Identity`; pass them the same domain and service as `gincommon.Config` (they read the environment from `APP_ENV`/`ENVIRONMENT` exactly as gincommon does). Give them `gincommon.MetricsRegisterer()` so one `/metrics` serves everything. For other third-party collectors, `gincommon.LabeledRegisterer(nil)` adds `domain`, `service` and `environment`.

## CI enforcement

**This repository** (`make metrics-lint`, in `validate-quality.yml`):

- `metricslint registry`: the registry is self-consistent. Namespaces match tiers, counters end in `_total` and histograms in `_seconds` (`_bytes` only for registry entries declaring `unit: bytes`). Labels exist in the vocabulary and are not prohibited. Shared metrics have help and aggregation expectations. Legacy entries have a replacement and a sunset release.
- `metricslint refs -deny-deprecated docker/prometheus/rules`: the reference rules use only registered `platform_*` names.
- `promtool check rules` + `promtool test rules`: the rules are valid and the burn-rate alerts fire as intended.
- Inventory drift: `metric-registry.md` matches the registry.
- `test/unit/platformmetrics`: gincommon's own emitted metrics pass the full compliance check.

**Consuming services** should add both of these:

```go
// metrics_compliance_test.go: build the real router/server, drive one request, then:
rep, err := obsregistry.Validate(gincommon.MetricsGatherer(), obsregistry.Options{})
if err != nil { t.Fatal(err) } // a collector failed; rep still holds the families that were gathered
for _, f := range rep.Errors() { t.Error(f) }
```

> **Strict mode currently fails on every gincommon service.** All metrics gincommon emits (`platform_http_*`, `platform_grpc_*`, `platform_build_info`) are `status: proposed` until observability governance ratifies them (see [Items awaiting ratification](#items-awaiting-ratification)). `metricslint check -strict` and `Options{Strict: true}` turn proposed entries into errors, so they fail on gincommon's own metrics regardless of what the service does. The same holds for the platform-events (`platform_queue_depth`, `platform_messages_*`, `platform_outbox_*`, ...) and platform-pgcommon (`platform_db_*`) entries, which are also proposed and owned by those libraries. Until ratification, either run without `-strict` (proposed entries are warnings), or keep strict mode for everything the service emits itself and exempt the platform libraries' metrics; `-allow-proposed-owner` takes a comma-separated list: `metricslint check -strict -allow-proposed-owner platform-gincommon,platform-events,platform-pgcommon` / `Options{Strict: true, AllowProposedOwners: []string{"platform-gincommon", "platform-events", "platform-pgcommon"}}`. The only proposed entries without an owner are the eight Tier 2 `iam_*` metrics (`iam_auth_events_total`, `iam_tenant_resolution_total`, `iam_identity_sync_total`, `iam_realm_operations_total`, `iam_rls_violations_total`, `iam_auth_session_revoke_failed_total`, `iam_lifecycle_event_skipped_total`, `iam_lifecycle_event_lag_seconds`), which IAM services emit themselves through `NewSharedCounterVec` / `NewSharedHistogramVec`; they stay errors under `-strict` whatever owners are allowed.

Each gincommon release attaches prebuilt `metricslint` binaries (linux/darwin amd64+arm64, windows amd64) with a `checksums.txt`, so CI can download one instead of `go run`.

```bash
# Dashboards, alerts, recording rules, SLOs, HPA manifests, runbooks:
go run github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/cmd/metricslint@vX.Y.Z refs deploy/ dashboards/
# Live scrape, e.g. in an e2e job:
go run github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/cmd/metricslint@vX.Y.Z check -url http://localhost:8080/metrics
```

`metricslint` exit codes: **0** compliant, **1** findings (for `registry`: the document is readable but violates the standard), **2** usage or I/O error (bad flags, `-url` together with files, a missing or unreadable file, a failed scrape). `check` accepts the Prometheus text format and OpenMetrics text (recognized by its `# EOF` line; timestamps, exemplars, `# UNIT` and `_created` samples are ignored, `unknown` families are treated as untyped). `refs` walks directories recursively, skipping `.git`, `node_modules` and `vendor` (unless named explicitly). A path it cannot read, inside a walked directory or named on the command line, and any file over 64 MiB, is reported as an error finding and the scan continues; once every finding is printed the exit code is **2** (I/O error) rather than 1.

What `Validate` / `metricslint check` enforce on every emitted metric:

| Rule | Check |
|---|---|
| `namespace` | Classified as platform / domain / service. Unregistered `platform_*` and `<domain>_*` names fail. A Tier 3 name must match its own service and domain. |
| `name-format`, `suffix` | snake_case; `_total` for counters, `_seconds` for histograms, no `_total` on gauges |
| `type` | Matches the registry type; summaries and untyped metrics are not allowed |
| `required-label` | `domain`, `service` and `environment` present as the tier requires |
| `label-vocabulary` | Shared metrics carry exactly the registry labels plus const labels |
| `prohibited-label` | No high-cardinality labels on any tier |
| `label-name` | Service-owned labels on Tier 3 metrics are snake_case `[a-z0-9_]` |
| `label-value` | Values inside the enum/pattern of every vocabulary label |
| `cardinality` | Distinct values per label within the registry's `max_values` |
| `status` | `proposed` entries warn (error with `-strict` / `Options{Strict: true}`, except owners listed in `-allow-proposed-owner` / `AllowProposedOwners`) |
| `gather` | (`Validate` only) a collector failed during `Gather`; the families that were gathered are still validated |
| `removed` | A pre-standard name that was removed: emitting or querying it is an **error** naming the replacement |
| `deprecated` | (Future migrations only) a legacy name still emitted during a compatibility period: a warning |

`go_*`, `process_*` and `promhttp_*` runtime collectors are exempt.

## Changing the registry (governance)

Rules 10 to 12 of the standard apply: services do not invent shared metrics. A registry change is a PR to `internal/obsregistry/registry.json` that must include:

1. The entry: `name`, `tier` (and `domain` for Tier 2), `type`, `unit`/`buckets` for histograms, `help` (the semantic definition), `labels`, and `aggregation` (how it may be summed or quantiled across services).
2. Any new label in `labels`, with its meaning, allowed values (`values` or `pattern`), `max_values` and cardinality expectation.
3. `"status": "proposed"` until observability governance approves; the approving PR flips it to `approved`.
4. `make metrics-inventory` to regenerate `metric-registry.md`.

The registry ships with each gincommon release. Services pick up approved changes by bumping the module.

### Items awaiting ratification

Everything with `status: proposed` in [`metric-registry.md`](metric-registry.md) needs a governance decision:

- **Registry-proposed examples from the standard:** `platform_duplicate_messages_total`, `platform_queue_depth`, `platform_dlq_depth`, `platform_dependency_request_seconds`, `platform_event_propagation_seconds`.
- **The HTTP and gRPC server metrics gincommon emits:** `platform_http_*`, `platform_grpc_*`, `platform_build_info`.
- **IAM's domain metrics:** `iam_auth_events_total`, `iam_tenant_resolution_total`, `iam_identity_sync_total`, `iam_realm_operations_total`, `iam_rls_violations_total`, `iam_auth_session_revoke_failed_total`, `iam_lifecycle_event_skipped_total`, `iam_lifecycle_event_lag_seconds`. Their definitions must be confirmed by IAM's owners.
- **Byte-size histograms** (`platform_*_size_bytes`). Rule 5 says histograms end in `_seconds`. The registry allows `_bytes` only for entries with `unit: bytes` and flags this as a deliberate exception. Governance should confirm it, or decide to drop payload histograms. The gincommon-owned `payload_size` preset runs from 128 B to 4 MiB (the gRPC default maximum message size) with a 16 MiB tail; HTTP requests of unknown length (chunked uploads) are not observed in `platform_http_request_size_bytes`, so its `_count` can be lower than `platform_http_requests_total`.
- **The vocabularies:** the domain list (in particular the `tender_management` spelling), the environment values, and the `outcome` enum.

The five canonical messaging metrics (`platform_messages_received_total`, `platform_messages_processed_total`, `platform_messages_failed_total`, `platform_retry_total`, `platform_dlq_messages_total`) are registered as `approved`, as given in the standard.

## SLOs

The reference rules implement the following SLIs and SLOs. Copy them into a service's rule set and adjust the targets per service.

| SLO | SLI (recording rule) | Target | Alerts |
|---|---|---|---|
| HTTP availability | `service:platform_http_error_ratio:rate{5m,30m,1h,6h}`: share of `status_class="5xx"` in `platform_http_requests_total` | 99.9% / 30d | `PlatformHTTPErrorBudgetFastBurn` (14.4×, 1h+5m, page), `PlatformHTTPErrorBudgetSlowBurn` (6×, 6h+30m, ticket) |
| gRPC availability | `service:platform_grpc_error_ratio:rate*` over `platform_grpc_server_handled_total` | 99.9% / 30d | `PlatformGRPCErrorBudget{Fast,Slow}Burn` |
| HTTP latency | `service_route:platform_http_request_duration_seconds:p99_5m` | p99 < 1s | `PlatformHTTPLatencyHigh` |
| Message processing | `service_queue:platform_messages_failed_ratio:rate5m` | < 5% failures | `PlatformMessagesFailureRatioHigh`, `PlatformMessagesDeadLettered` |

All SLIs keep `domain`, `service` and `environment`, so `sum by (domain, environment)` gives a domain-level view and `sum by (environment)` a platform-level one.

## HPA

Scale on the platform names through prometheus-adapter (see [migration](migration.md#hpa)). The recording rules `service:platform_http_active_requests:sum` and `service:platform_grpc_server_inflight_requests:sum` are meant as HPA inputs.
