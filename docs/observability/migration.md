# Removed pre-standard metric names

gincommon's pre-standard metric names (`http_requests_total`, `grpc_server_handled_total`, ...) broke the Enterprise Platform Observability Standard in three ways:

- They had no namespace.
- They lacked the `domain` and `environment` labels.
- One label name had two meanings: `method` was the HTTP verb on `http_*` but the RPC on `grpc_*`.

The standard's backward-compatibility process (emit both, then migrate, deprecate and remove) exists to protect dashboards and alerts that already query the old names. No platform library (gincommon, fastapicommon, platform-events, platform-pgcommon or their Python ports) had been deployed to production or dev, so there were no such series to protect. The old names were therefore **removed outright, with no compatibility period**. The libraries emit only `platform_*`.

## Name mapping

| Removed name | Replacement | Label changes |
|---|---|---|
| `build_info` | `platform_build_info` | + `domain`, `environment`; `version` stays (only here) |
| `http_requests_total` | `platform_http_requests_total` | + `domain`, `environment`, − `version`; non-standard `method` values become `OTHER` |
| `http_request_duration_seconds` | `platform_http_request_duration_seconds` | same as above; identical buckets |
| `http_request_size_bytes` | `platform_http_request_size_bytes` | same as above |
| `http_response_size_bytes` | `platform_http_response_size_bytes` | same as above |
| `http_active_requests` | `platform_http_active_requests` | + `domain`, `environment` |
| `http_panic_total` | `platform_http_panics_total` | + `domain`, `environment`; `method` bounded |
| `rate_limited_requests_total` | `platform_http_rate_limited_requests_total` | + `domain`, `environment`; `method` bounded |
| `http_request_timeout_total` | `platform_http_request_timeouts_total` | + `domain`, `environment`; `method` bounded |
| `grpc_server_handled_total` | `platform_grpc_server_handled_total` | + `domain`, `environment`; `method` → `grpc_method` |
| `grpc_server_handling_seconds` | `platform_grpc_server_handling_seconds` | same as above |
| `grpc_server_request_size_bytes` | `platform_grpc_server_request_size_bytes` | same as above |
| `grpc_server_response_size_bytes` | `platform_grpc_server_response_size_bytes` | same as above |
| `grpc_server_inflight_requests` | `platform_grpc_server_inflight_requests` | same as above |
| `grpc_rate_limited_requests_total` | `platform_grpc_rate_limited_requests_total` | same as above |
| `grpc_panic_total` | `platform_grpc_panics_total` | same as above |

All other labels (`route`, `status_class`, `error_class`, `grpc_code`, `service`) and all bucket boundaries are unchanged. `version` is dropped from every series except `platform_build_info`; to break a query down by build, join on `platform_build_info`. Every removed series has an equivalent platform series recorded on the same code path.

platform-events' (`events_*`, `outbox_*`, `sqs_*`) and platform-pgcommon's (`pgcommon_*`) pre-standard names were removed the same way; each repo's `docs/observability/` describes its own replacements. All removed names stay in the central [registry](metric-registry.md#legacy-metrics-removed-or-deprecated) with status `removed`, so `metricslint` turns any remaining use into an error naming the replacement.

## Upgrading a consuming service

There is no parallel-emission phase, so do this in the same change that bumps gincommon:

1. **Set the identity.** Set `Config.Domain` (or `OBSERVABILITY_DOMAIN`, e.g. in the Helm values), and make sure the environment resolves (`Config.Environment`, else `APP_ENV`, else `ENVIRONMENT`) to one of `local, dev, test, staging, prod`. `production` is rejected. The service will not start without a valid domain.
2. **Find every reference.** Run `metricslint refs` over everything that queries metrics: dashboards, alert rules, recording rules, SLO definitions, HPA / prometheus-adapter config, release health gates, runbooks. Every hit is an error naming the replacement:
   ```bash
   go run github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/cmd/metricslint@vX.Y.Z refs deploy/ .github/ dashboards/
   # deploy/hpa.yaml:14: error: legacy metric http_active_requests was removed and is no longer emitted; use platform_http_active_requests
   ```
3. **Rewrite the queries** with the table above. When a query groups by `method` on a gRPC metric, change it to `grpc_method`. Remove `version` from selectors and groupings except on `platform_build_info`. Remove `{service="..."}`-only assumptions where the series now also carry `domain` and `environment` (selectors still match; groupings may need them).
4. **Lock it in.** Add `metricslint refs <paths>` (it fails on removed names) and a test with `obsregistry.Validate` to the service's CI.

## PromQL examples

```promql
# before
sum by (service) (rate(http_requests_total{status_class="5xx"}[5m]))
# after (aggregates across services and domains)
sum by (domain, service, environment) (rate(platform_http_requests_total{status_class="5xx"}[5m]))

# before
histogram_quantile(0.99, sum by (le, method) (rate(grpc_server_handling_seconds_bucket[5m])))
# after
histogram_quantile(0.99, sum by (le, grpc_method) (rate(platform_grpc_server_handling_seconds_bucket[5m])))
```

## HPA

prometheus-adapter rule, before and after:

```yaml
# before
- seriesQuery: 'http_active_requests{namespace!="",pod!=""}'
  name: { as: "http_active_requests" }
  metricsQuery: 'sum(<<.Series>>{<<.LabelMatchers>>}) by (<<.GroupBy>>)'
# after
- seriesQuery: 'platform_http_active_requests{namespace!="",pod!=""}'
  name: { as: "platform_http_active_requests" }
  metricsQuery: 'sum(<<.Series>>{<<.LabelMatchers>>}) by (<<.GroupBy>>)'
```

Update the `HorizontalPodAutoscaler`'s `metric.name` to the new name in the same change, since the adapter exposes the renamed metric.
