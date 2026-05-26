# platform-gincommon

Shared **Gin and gRPC** middleware library for platform services: authentication headers, tenant context propagation, request tracing, structured logging, Prometheus metrics (HTTP and gRPC), and optional RBAC.

**Repository:** [github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon)

This project is consumed as a **Go module** by other services. Those services keep their own `main`, routes, and business logic and only import `pkg/gincommon`. You do **not** need to deploy the example server in this repo in production.

---

## Features

| Capability | Description |
|------------|-------------|
| **Request ID** | Reads `x-request-id` or generates a UUID; echoes it on the response |
| **Tracing** | OpenTelemetry HTTP server spans; stores the OTel trace ID in Gin context (`trace_id`) |
| **Metrics** | Prometheus HTTP and gRPC metrics; `build_info` gauge (service, version); `rate_limited_requests_total` (HTTP 429); IAM-tuned histogram buckets |
| **Logging** | Structured `http_request` logs via Zap (dev/prod configs) |
| **Auth** | Requires `x-user-id` and `x-tenant-id`; otherwise `401` |
| **Tenant context** | Builds `requestContext` from headers (tenant, user, roles, trace) |
| **RBAC** | Optional per-route checks (`delete` requires `admin` role) |
| **OpenTelemetry** | Helper to init OTLP/gRPC trace export |

---

## Install (consumer services)

```bash
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon
```

**Go version:** 1.26+ (see `go.mod`).

Pin a release in production (do not use `@latest`):

```bash
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@v1.0.0
```

---

## Versioning & releases

This module follows **[Semantic Versioning](https://semver.org/)** (SemVer). Git tags (`v1.0.0`, …) are the source of truth for `go get`. Full policy: **[VERSIONING.md](./VERSIONING.md)**. History: **[CHANGELOG.md](./CHANGELOG.md)**.

### Release line

| Version | Status | `go get` | Go | Notes |
|---------|--------|----------|-----|--------|
| **v1.0.0** | Current | `@v1.0.0` | 1.26+ | First stable release; see [CHANGELOG](CHANGELOG.md#100---2026-05-26) |
| *Unreleased* | — | `@main` | 1.26+ | `main` branch; not for production pins |

### SemVer guarantees (public API: `pkg/gincommon`, `pkg/grpccommon`)

| Change type | Version bump | Consumer impact |
|-------------|--------------|-----------------|
| Breaking change in `pkg/*` (removed/rename, incompatible defaults) | **MAJOR** (`v2.0.0`) | Code or config updates required; migration notes in CHANGELOG |
| New feature, optional config, new middleware (backward compatible) | **MINOR** (`v1.1.0`) | Safe `go get` upgrade; opt in to new APIs |
| Bug fix, security fix, docs clarifying behavior | **PATCH** (`v1.0.1`) | Drop-in upgrade recommended |

**Not covered by SemVer:** `internal/*`, `cmd/server`, and test packages may change in any release.

### Tags and GitHub releases

| Artifact | Location |
|----------|----------|
| Git tags | `vMAJOR.MINOR.PATCH` on [tags](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/tags) |
| Release notes | [GitHub Releases](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/releases) (changelog excerpt per tag) |
| Changelog | [CHANGELOG.md](./CHANGELOG.md) |

Maintainers: see **[VERSIONING.md § Maintainer release process](./VERSIONING.md#maintainer-release-process)**.

---

## Using in your service

Import only **`pkg/gincommon`**, **`pkg/grpccommon`**, and **`pkg/logger`** from other repositories. Do not import `internal/*` (Go enforces this across modules).

### What you get

| Layer | Feature | Middleware / API | Response / output |
|-------|---------|------------------|-------------------|
| Transport | **Correlation ID** | `RequestIDMiddleware` | `x-request-id` echoed; invalid client values replaced with UUID |
| Transport | **Trace ID** | `TracingMiddleware` | `X-Trace-ID` on response; W3C `traceparent` supported |
| Middleware | **Tracing** | OpenTelemetry HTTP spans | OTLP export via `InitTracingFromEnv()` |
| Middleware | **Logging** | `LoggingMiddleware` | Structured `http_request` log per request (sanitized) |
| Middleware | **Metrics** | `MetricsMiddleware` | Prometheus `http_*` metrics + `build_info` |
| Middleware | **Recovery** | `PanicRecoveryMiddleware` | JSON 500 + `http_panic_total` |
| Middleware | **Auth** | `RequireAuth` | `401` without valid `x-user-id` / `x-tenant-id` |
| Middleware | **Context** | `ContextMiddleware` | `RequestContext` in Gin (`tenant`, `user`, `roles`, `trace_id`) |
| Middleware | **AuthZ** | `RequirePermission` | `403` when `port.Authorizer` denies |
| Domain | **RequestContext** | `gincommon.RequestContext(c)` | Use in handlers after protected middleware |

### Middleware order (recommended)

Apply **observability on the whole router**, then **auth + context only on protected groups**:

```text
Incoming request
  → PanicRecovery
  → RequestID          (correlation id)
  → Tracing            (OTel span + trace_id in context)
  → CorrelationHeaders (X-Trace-ID, X-Request-ID on response)
  → Metrics
  → Logging
  → [protected routes only]
  → RequireAuth
  → ContextMiddleware
  → [optional per-route] RequirePermission
  → your handler
```

### Complete HTTP example

`main.go` in **your** service (not this repo’s `cmd/server`):

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
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/logger"
)

func main() {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "dev"
	}
	if appEnv != "dev" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Logging — Zap adapter implementing port.Logger
	log, err := logger.NewLogger(appEnv)
	if err != nil {
		panic(err)
	}

	// Tracing — OTLP/gRPC (optional but recommended); call before router handles traffic
	shutdownTracing := gincommon.InitTracingFromEnv()
	defer shutdownTracing()

	cfg := gincommon.Config{
		Logger:       log,
		ServiceName:  "my-service",
		BuildVersion: os.Getenv("BUILD_VERSION"), // set via -ldflags or env in production
	}

	r := gin.New()

	// Observability: correlation id, tracing, metrics, logging, recovery (all routes)
	for _, mw := range gincommon.ObservabilityMiddlewares(cfg) {
		r.Use(mw)
	}

	// Public routes (no auth) — restrict /metrics in production (see Security)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Protected API — auth + tenant context
	api := r.Group("/api")
	for _, mw := range gincommon.ProtectedMiddlewares(cfg) {
		api.Use(mw)
	}

	// Reference RBAC (replace with your port.Authorizer in production)
	authz := gincommon.NewRBAC()

	api.GET("/items", listItems)
	api.DELETE("/items/:id",
		gincommon.RequirePermission("delete", "item", authz),
		deleteItem,
	)

	// Graceful shutdown: wait for SIGINT/SIGTERM, then drain in-flight requests.
	srv := &http.Server{Addr: ":8080", Handler: r}

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
}

func listItems(c *gin.Context) {
	reqCtx, ok := gincommon.RequestContext(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "missing request context"})
		return
	}
	// reqCtx.TenantID, reqCtx.UserID, reqCtx.Roles, reqCtx.TraceID available here
	c.JSON(http.StatusOK, gin.H{
		"tenant_id": reqCtx.TenantID,
		"user_id":   reqCtx.UserID,
		"roles":     reqCtx.Roles,
	})
}

func deleteItem(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
```

### Environment variables (consumer service)

| Variable | Example | Default | Used for |
|----------|---------|---------|----------|
| `APP_ENV` | `dev` / `prod` | `dev` | Zap log format; OTLP insecure default; sampling default |
| `BUILD_VERSION` | `1.2.3` | `unknown` | Prometheus `build_info`, OTEL `service.version` |
| `OTEL_SERVICE_NAME` | `my-service` | value of `APP_NAME` | OTEL `service.name` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `collector:4317` | `localhost:4317` | OTLP/gRPC collector address |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | `true` when `APP_ENV=dev`, else `false` | Plaintext OTLP; set `false` in production |
| `OTEL_TRACES_SAMPLER_RATIO` | `0.1` | `1.0` in `dev`, **`0.1`** otherwise | Trace ID ratio sampling `0.0`–`1.0`; set `1.0` to sample every request |

### Calling the API (via gateway or local curl)

Your **API gateway** should set identity headers after login. For local testing:

```bash
# Public health (observability only — no auth headers required)
curl -i http://localhost:8080/health

# Protected list — note correlation + identity headers
curl -i http://localhost:8080/api/items \
  -H "x-user-id: user-1" \
  -H "x-tenant-id: tenant-1" \
  -H "x-tenant-roles: user" \
  -H "x-request-id: my-req-001"

# Delete — requires admin role (reference RBAC)
curl -i -X DELETE http://localhost:8080/api/items/42 \
  -H "x-user-id: user-1" \
  -H "x-tenant-id: tenant-1" \
  -H "x-tenant-roles: admin"

# Propagate upstream trace (optional)
curl -i http://localhost:8080/api/items \
  -H "x-user-id: user-1" \
  -H "x-tenant-id: tenant-1" \
  -H "traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
```

Successful responses include **`X-Trace-ID`** and **`X-Request-ID`**. Error JSON includes `trace_id` and `request_id` fields for support correlation.

### Per-route authorization (authz)

`RequirePermission` runs **after** `ProtectedMiddlewares` so `RequestContext` is available:

```go
authz := gincommon.NewRBAC() // demo only — implement port.Authorizer for production

api.DELETE("/users/:id",
	gincommon.RequirePermission("delete", "user", authz),
	deleteUserHandler,
)
```

Production services should provide a `port.Authorizer` backed by IAM/OPA; `NewRBAC()` only allows `read` and `delete` (admin role).

### gRPC metrics (same module)

`pkg/grpccommon` provides **Prometheus metrics only**. OpenTelemetry tracing, authentication, and authorization interceptors are **not included** — add them in your service before or after these interceptors as needed.

```go
import (
	"google.golang.org/grpc"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
)

cfg := grpccommon.Config{ServiceName: "my-service", BuildVersion: os.Getenv("BUILD_VERSION")}
s := grpc.NewServer(
	grpc.ChainUnaryInterceptor(
		// Add your auth/tracing interceptors here, then metrics last:
		grpccommon.DefaultUnaryInterceptors(cfg)...,
	),
	grpc.ChainStreamInterceptor(
		grpccommon.DefaultStreamInterceptors(cfg)...,
	),
)
// Register your gRPC services on s ...
```

Prometheus metric names: `grpc_server_handled_total`, `grpc_server_handling_seconds`, `grpc_server_request_size_bytes`, `grpc_server_response_size_bytes`, `grpc_server_inflight_requests`, `grpc_rate_limited_requests_total`.

> **Note:** `ProtectedMiddlewares(cfg)` accepts a `Config` parameter for forward compatibility. The `cfg.Logger` field is reserved for future use and is not currently wired into `ContextMiddleware`.

### Shortcut: full stack on every route

If **every** route requires auth (unusual for services with public `/health`), use one call:

```go
for _, mw := range gincommon.DefaultMiddlewares(cfg) {
	r.Use(mw)
}
```

Most services should prefer **ObservabilityMiddlewares** globally + **ProtectedMiddlewares** on `/api` only (see example above).

---

## Required HTTP headers

Downstream callers (API gateway, BFF, or tests) should send:

| Header | Required | Purpose |
|--------|----------|---------|
| `x-user-id` | Yes (for protected routes) | User identity |
| `x-tenant-id` | Yes (for protected routes) | Tenant identity |
| `x-tenant-roles` | Recommended | Comma-separated roles (e.g. `admin,user`) |
| `x-request-id` | Optional | Correlation ID (generated if missing) |
| `traceparent` | Optional | W3C trace context (OpenTelemetry propagator); `trace_id` in Gin context is the OTel trace ID |

Without valid `x-user-id` / `x-tenant-id` (non-empty, trimmed, max 256 chars, no control characters), `RequireAuth` responds with **401 Unauthorized**. Invalid `x-request-id` values are replaced with a generated UUID.

Header name constants: `gincommon.HeaderUserID`, `HeaderTenantID`, `HeaderTenantRoles`, `HeaderRequestID`, `HeaderTraceIDResponse`, `HeaderRequestIDResponse`.

---

## Security

| Topic | Guidance |
|--------|----------|
| **Trust boundary** | `x-user-id`, `x-tenant-id`, and `x-tenant-roles` must be set by a **trusted API gateway** after authentication. Block direct client access to services or strip/forbid these headers at the edge. |
| **Authorization** | `NewRBAC()` is a **reference** `port.Authorizer` for tests/demos. Production services should implement `port.Authorizer` with your IAM/OPA policy. |
| **Roles header** | Roles are validated (`[a-zA-Z0-9_-]+`) but not signed; the gateway must not forward client-supplied role headers. |
| **Metrics** | `/metrics` is often public for Prometheus; restrict via network policy, auth proxy, or a dedicated admin port. |
| **OTLP** | `OTEL_EXPORTER_OTLP_INSECURE` defaults to `true` only when `APP_ENV=dev`. Use TLS in production. |
| **Baggage** | W3C baggage is **off** by default (`OTEL_PROPAGATOR_BAGGAGE=true` to enable). |
| **Logging** | `service.Sanitize` redacts sensitive field names before logging. |
| **gRPC** | `pkg/grpccommon` provides metrics only; add authn/authz interceptors in your service. |

---

## Public API (`pkg/gincommon`)

| Package | Symbol | Purpose |
|---------|--------|---------|
| `pkg/gincommon` | `ObservabilityMiddlewares` | Correlation ID, tracing, metrics, logging, recovery |
| `pkg/gincommon` | `ProtectedMiddlewares` | Auth + `RequestContext` |
| `pkg/gincommon` | `DefaultMiddlewares` | Observability + protected (all routes) |
| `pkg/gincommon` | `RequirePermission` | Per-route authz |
| `pkg/gincommon` | `RequestContext` | `*domain.RequestContext` from Gin |
| `pkg/gincommon` | `InitTracingFromEnv` | OTLP tracing from env |
| `pkg/logger` | `NewLogger` | Zap `port.Logger` for middleware |
| `pkg/grpccommon` | `DefaultUnaryInterceptors` | gRPC Prometheus metrics |

See [Using in your service](#using-in-your-service) for a full example.

---

## Project layout

```
platform-gincommon/
├── pkg/gincommon/          # Public API — HTTP middleware, tracing, auth, authz
├── pkg/logger/             # Public Zap logger (port.Logger)
├── pkg/grpccommon/         # Public gRPC metrics interceptors
├── internal/
│   ├── adapter/inbound/http/middleware/
│   ├── adapter/outbound/{logger,metrics,otel}/
│   ├── core/{domain,port,service}/
│   ├── config/
│   └── server/             # Example router + Setup() (dev/demo only)
├── cmd/server/             # Optional example binary (not required for consumers)
├── test/
│   ├── unit/               # Unit tests (black-box)
│   ├── integration/        # HTTP / middleware integration tests
│   ├── e2e/                # End-to-end flows
│   └── smoke/              # Live or in-process smoke checks
├── docker/                 # Prometheus + OpenTelemetry Collector config
├── docker-compose.yml      # Local collector/Tempo/Loki/Prometheus/Grafana (optional)
└── .env-example            # Local dev environment template
```

---

## Local development (this repo)

### Setup

```bash
make setup    # copies .env-example → .env
make tidy
```

### Common commands

| Command | Description |
|---------|-------------|
| `make test` | All tests under `test/` (excludes optional live smoke) |
| `make test-unit` | `test/unit/...` only |
| `make test-int` | Integration tests |
| `make test-e2e` | E2E tests |
| `make test-smoke` | Smoke tests (in-process + optional live server) |
| `make lint` | golangci-lint (via `go tool`); enforces package/exported godoc via `revive` |
| `make cover` | Coverage report (`coverage.out` + HTML) |
| `make build` | Build example binary to `bin/platform-gincommon` |
| `make run` | Run **example** server (`cmd/server`) |
| `make docker-up` | Start local observability stack |
| `make ci` | `tidy` + `lint` + `test` + `build` |

### Example server (optional)

`cmd/server` is a **reference app** for local testing and Docker/smoke checks. It exposes public `GET /health` and `GET /metrics` with observability middleware only; `GET /panic` is registered only when `APP_ENV=dev`.

```bash
make run
# curl with auth headers:
# curl -H "x-user-id: user-1" -H "x-tenant-id: tenant-1" http://localhost:8080/health
```

### Observability stack (optional)

```bash
make docker-up
```

Uses `.env` for host ports. Trace flow: **app** → `OTEL_EXPORTER_OTLP_ENDPOINT` (gRPC `localhost:4317`) → **otel-collector** → **Tempo** (internal). Prometheus config is generated from `docker/prometheus/prometheus.yml.tpl`.

---

## Testing

- Tests live under **`test/`**, not beside production code in `internal/`.
- **`make lint`** requires godoc on exported symbols in production packages (`revive` `exported` / `package-comments` in `.golangci.yml`).
- **`make test`** does not require a running server.
- **`make test-smoke`** can hit a live URL (`SMOKE_BASE_URL` in `.env`) or uses an in-process server in tests.

---

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs on push/PR to `main`/`master`:

1. Format check (`gofmt`)
2. `make tidy` + `make lint` + `make test`
3. `make build`
4. Docker image build (example server)

---

## Docker (example server only)

```bash
make docker-build
docker run --rm -p 8080:8080 -e APP_ENV=prod platform-gincommon:local
```

The image runs the **demo** binary, not something consumer services must deploy.

---

## License / ownership

BCBP Solutions FZC LLC — internal platform shared library.

---

## See also

| Document | Description |
|----------|-------------|
| [VERSIONING.md](./VERSIONING.md) | SemVer rules, supported versions, release process |
| [CHANGELOG.md](./CHANGELOG.md) | Per-version changes |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Development setup, adding middleware, PR checklist |
| [SECURITY.md](./SECURITY.md) | Vulnerability reporting, trust model, supported versions |
