# platform-gincommon

Shared **Gin and gRPC** middleware library for platform services: authentication headers, tenant context propagation, request tracing, structured logging, Prometheus metrics (HTTP and gRPC), per-request timeouts, outbound header propagation, and optional RBAC.

**Repository:** [github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon)

This project is consumed as a **Go module** by other services. Those services keep their own `main`, routes, and business logic and only import from `pkg/`. You do **not** need to deploy the example server in this repo in production.

## Mental model

This library provides three layers your service sits on top of:

| Layer | What it does | Your responsibility |
|-------|-------------|---------------------|
| **Observability** | Request ID, tracing, Prometheus metrics, structured logging, panic recovery | Nothing — it is automatic on all routes |
| **Security** | Authentication (identity headers), authorization (RBAC) | Implement `port.Authorizer` for production IAM; add `RequirePermission` per protected route |
| **Context** | Builds `RequestContext{TenantID, UserID, Roles, TraceID, ClientIP}` and injects it | Read `RequestContext` in handlers; propagate headers outbound via `PropagateHeaders` |

Your service only implements **business logic** on top of these layers. Everything else — log format, span attributes, error response shape, header validation — is standardised here.

> **Tracing guarantee:** every incoming request creates a root OTel span, or continues an existing distributed trace if a `traceparent` header is provided. End-to-end traceability across services is automatic as long as handlers use `PropagateHeaders` for outbound calls. All outbound service calls must propagate both `traceparent` and `x-request-id` to maintain end-to-end observability and request correlation.

---

## Why this library exists

Every backend service in the platform needs the same cross-cutting infrastructure: request correlation, distributed tracing, structured logging, Prometheus metrics, identity extraction, and access control. Without a shared library each team re-implements these differently, leading to:

- Inconsistent log formats that break cross-service queries
- Missing trace context that breaks distributed traces
- Auth bugs from hand-rolled header validation
- Metrics with different label names that can't be aggregated

This library **standardises** these concerns once so every service gets:

- A consistent `http_request` / `grpc_request` log with the same fields
- OTel spans with the same attribute conventions
- A `RequestContext` with verified identity available in every protected handler
- The same error JSON shape (`trace_id` + `request_id`) across all services

---

## Features

| Capability | HTTP | gRPC |
|------------|------|------|
| **Panic recovery** | `PanicRecoveryMiddleware` — JSON 500 + metric | `UnaryPanicRecoveryInterceptor` — `codes.Internal` |
| **Request ID** | Valid client `x-request-id` values are preserved and echoed; missing or invalid values are replaced with a generated UUID. | Reads from metadata or generates UUID; stored in Go context |
| **Tracing** | OTel HTTP server spans; tracer scope = `ServiceName` | OTel gRPC server spans; W3C `traceparent` in metadata |
| **Metrics** | Prometheus HTTP `http_*` + `build_info` | Prometheus gRPC `grpc_server_*` |
| **Logging** | Structured `http_request` Zap log (Info/Warn/Error by status; `duration_ms` float64) | Structured `grpc_request` log (Info/Warn/Error by gRPC code) |
| **Auth** | `RequireAuth` — `401` without `x-user-id` / `x-tenant-id` | `UnaryRequireAuthInterceptor` — `codes.Unauthenticated` |
| **Tenant context** | `RequestContext{TenantID,UserID,Roles,TraceID,ClientIP}` in Gin context | Same struct in Go context via `domain.ContextWithRequestContext` |
| **RBAC** | `RequirePermission` / `RequirePermissionWithLogger` per route | `RequirePermission` / `RequirePermissionWithLogger` per RPC |
| **Timeout** | `TimeoutMiddleware(d)` — context deadline on request | — (set deadline on `context.Context` before dialling) |
| **Health** | `HealthHandler()` — 200 `{"status":"ok"}` | — |
| **Error type** | `ErrorResponse` struct — unmarshal 4xx/5xx bodies | — |
| **Outbound propagation** | `PropagateHeaders(c, req)` — injects trace + identity into outbound HTTP calls | — |
| **OpenTelemetry init** | `InitTracingFromEnv()` / `EnsureTracing()` | — |

---

## Install (consumer services)

This is a **private module**. Consuming services must set `GOPRIVATE` before running `go get`:

```bash
export GOPRIVATE=github.com/BCBP-SOLUTIONS-FZC-LLC/*
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@v1.0.0
```

**Go version:** 1.26+ (see `go.mod`). Pin a release in production — do not use `@latest`.

---

## Versioning & releases

This module follows **[Semantic Versioning](https://semver.org/)**. Git tags are the source of truth for `go get`. Full policy: **[VERSIONING.md](./VERSIONING.md)**. History: **[CHANGELOG.md](./CHANGELOG.md)**.

### Release line

| Version | Status | `go get` | Go | Notes |
|---------|--------|----------|-----|--------|
| **v1.0.0** | Current | `@v1.0.0` | 1.26+ | First stable release |
| *Unreleased* | — | `@main` | 1.26+ | `main` branch; not for production pins |

### SemVer guarantees (public API: `pkg/gincommon`, `pkg/grpccommon`)

| Change type | Version bump |
|-------------|--------------|
| Breaking change in `pkg/*` (removed/rename, incompatible defaults) | **MAJOR** |
| New feature, optional config, new middleware (backward compatible) | **MINOR** |
| Bug fix, security fix | **PATCH** |

**Not covered by SemVer:** `internal/*`, `cmd/server`, and test packages may change in any release.

---

## HTTP — quick start

Import only `pkg/gincommon` and `pkg/logger` from your service.

### Middleware order

```text
Incoming request
  → [optional] TimeoutMiddleware      (context deadline — insert before everything)
  → PanicRecovery                     (catches panics from all below)
  → RequestID                         (read/generate x-request-id)
  → Tracing                           (OTel span, X-Trace-ID on response)
  → CorrelationHeaders                (X-Trace-ID, X-Request-ID on response)
  → Metrics                           (Prometheus http_* counters/histograms)
  → Logging                           (http_request log after response)
  ╠═ public routes (no auth) ════════════════════════════════╗
  ║   GET /health     → HealthHandler()                       ║
  ║   GET /metrics    → promhttp.Handler()                    ║
  ╚═══════════════════════════════════════════════════════════╝
  ╠═ protected routes (/api/...) ════════════════════════════╗
  ║   → RequireAuth        (401 without x-user-id/x-tenant-id) ║
  ║   → ContextMiddleware  (builds RequestContext)              ║
  ║   → [optional per route] RequirePermission                  ║
  ║   → your handler                                            ║
  ╚══════════════════════════════════════════════════════════╝
```

### Complete HTTP example

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

    // 1. Logger — Zap adapter implementing port.Logger
    log, err := logger.NewLogger(appEnv)
    if err != nil {
        panic(err)
    }

    // 2. Tracing — OTLP/gRPC; call before the router handles traffic
    shutdownTracing := gincommon.InitTracingFromEnv()
    defer shutdownTracing()

    cfg := gincommon.Config{
        Logger:       log,
        ServiceName:  "my-service",          // required — used as Prometheus label + OTel scope
        BuildVersion: os.Getenv("BUILD_VERSION"),
    }

    r := gin.New()

    // 3. Optional: enforce a 30 s deadline on every request.
    //    Place before ObservabilityMiddlewares so it wraps the full chain.
    r.Use(gincommon.TimeoutMiddleware(30 * time.Second))

    // 4. Observability on all routes: recovery, request ID, tracing, metrics, logging.
    r.Use(gincommon.ObservabilityMiddlewares(cfg)...)

    // 5. Public routes — no auth required.
    r.GET("/health", gincommon.HealthHandler())
    r.GET("/metrics", gin.WrapH(promhttp.Handler()))

    // 6. Protected API — auth + tenant context on /api/...
    api := r.Group("/api")
    api.Use(gincommon.ProtectedMiddlewares(cfg)...)

    // Reference RBAC (replace with your port.Authorizer in production)
    authz := gincommon.NewDemoRBAC()

    api.GET("/items", listItemsHandler)
    api.DELETE("/items/:id",
        gincommon.RequirePermissionWithLogger("delete", "items", authz, log),
        deleteItemHandler,
    )

    // 7. Graceful shutdown
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

    // Flush OpenTelemetry spans and buffered logs before exit.
    _ = gincommon.Shutdown(log)
}

func listItemsHandler(c *gin.Context) {
    // RequestContext is populated by ContextMiddleware on protected routes.
    rc, ok := gincommon.RequestContext(c)
    if !ok {
        c.JSON(http.StatusInternalServerError, gincommon.ErrorResponse{
            Error:  "missing request context",
            Status: http.StatusInternalServerError,
        })
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

### Outbound header propagation

When a handler calls another platform service, inject the trace context and identity headers so the receiving service can correlate the call:

```go
func listItemsHandler(c *gin.Context) {
    outReq, _ := http.NewRequestWithContext(c.Request.Context(),
        http.MethodGet, "http://inventory-svc/internal/stock", nil)

    // Injects: traceparent, x-request-id, x-user-id, x-tenant-id, x-tenant-roles
    gincommon.PropagateHeaders(c, outReq)

    resp, err := http.DefaultClient.Do(outReq)
    // ...
}
```

### Unmarshalling error responses

All 4xx/5xx responses from platform middleware use a consistent JSON shape. Unmarshal into `gincommon.ErrorResponse`:

```go
var errResp gincommon.ErrorResponse
if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
    log.Printf("error: %s  trace_id: %s  request_id: %s",
        errResp.Error, errResp.TraceID, errResp.RequestID)
}
```

### Using DefaultMiddlewares (shortcut)

If **every** route requires auth, use one call:

```go
r.Use(gincommon.DefaultMiddlewares(cfg)...)
// equivalent to ObservabilityMiddlewares + ProtectedMiddlewares
```

Most services should prefer `ObservabilityMiddlewares` globally + `ProtectedMiddlewares` on `/api` only.

---

## Registering custom application metrics

`ObservabilityMiddlewares`/`ObservabilityUnaryInterceptors` (or their `Default*`/`Full*` equivalents) register gincommon's own HTTP/gRPC metrics with `{service, version}` const labels. To add your own business metrics consistently — same registry, same labels — use `MetricsRegisterer()`/`MetricsConstLabels()` instead of assuming `prometheus.DefaultRegisterer` and duplicating the labels yourself:

```go
r.Use(gincommon.ObservabilityMiddlewares(cfg)...) // metrics are initialized here

itemsCreated := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name:        "items_created_total",
        Help:        "Total items created",
        ConstLabels: gincommon.MetricsConstLabels(), // {service, version} — matches gincommon's own metrics
    },
    []string{"tenant"},
)
gincommon.MetricsRegisterer().MustRegister(itemsCreated)
```

Call this **after** the middleware/interceptor constructor has run (metrics initialization is one-time and process-wide, shared between `pkg/gincommon` and `pkg/grpccommon`). `pkg/grpccommon` exposes the identical pair, `grpccommon.MetricsRegisterer()`/`grpccommon.MetricsConstLabels()`.

To avoid importing the Prometheus client at all, use the constructors instead. They register on `MetricsRegisterer()` (or the registerer you pass, e.g. an isolated registry in tests) with the `{service, version}` const labels. Then serve everything, gincommon's and yours, from one handler:

```go
itemsCreated, err := gincommon.NewCounterVec(nil, "items_created_total", "Total items created", "tenant")
// also gincommon.NewCounter, NewGaugeVec, NewGauge
itemsCreated.WithLabelValues(tenantID).Inc()

metricsMux.Handle("/metrics", gincommon.MetricsHandler())
```

## Tracing and logging outside handlers

`pkg/gincommon` covers what services need beyond the HTTP middleware, so they never import OpenTelemetry. Everything uses the TracerProvider and propagator that `InitTracing` installed:

| API | Use |
|---|---|
| `NewTracer(scope) Tracer` / `NoopTracer()` | Start child spans (`tracer.Start(ctx, name)` → `Span`) in services, repositories and consumers |
| `SpanTraceID(ctx)` | Trace ID from a `context.Context`, for log correlation. The `*gin.Context` form is `TraceIDFromContext` |
| `RecordSpanError(ctx, err, description)` | Record an error on the current span and mark it failed |
| `InjectTraceContext(ctx, header)` | W3C `traceparent` onto an outbound request without a `*gin.Context`. The `*gin.Context` form is `PropagateHeaders` |
| `NewSpanTracer(scope)` | A `StartSpan(ctx, name) (ctx, end)` hook, e.g. `platform-pgcommon`'s `Config.Tracer` |

`logger.Default()` (in `pkg/logger`) returns a process-wide gincommon logger for code paths that were never handed one, such as an early startup failure. It is built once, for `APP_ENV`, then `ENVIRONMENT`, defaulting to production.

---

## gRPC — quick start

Import only `pkg/grpccommon` from your service.

### Interceptor stacks

| Constructor | Interceptors included | Typical use |
|-------------|----------------------|-------------|
| `ObservabilityUnaryInterceptors(cfg)` | Panic recovery → Request ID → Tracing → Metrics → Logging | All servers — observability only |
| `ProtectedUnaryInterceptors(cfg)` | RequireAuth → ContextInterceptor | Add to protected servers after Observability |
| `FullUnaryInterceptors(cfg)` | All of the above combined | Servers where every RPC requires identity |
| `DefaultUnaryInterceptors(cfg)` | Alias for `ObservabilityUnaryInterceptors` | Backward compatibility |
| `RequirePermission(action, resource, auth)` | Single per-RPC RBAC interceptor | Per-method authorization |

Stream variants (`ObservabilityStreamInterceptors`, `ProtectedStreamInterceptors`, `FullStreamInterceptors`) follow the same pattern.

### Complete gRPC example

```go
package main

import (
    "context"
    "net"
    "os"

    "google.golang.org/grpc"

    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
    "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/logger"

    // your generated pb package:
    pb "github.com/your-org/your-service/gen/pb"
)

func main() {
    log, _ := logger.NewLogger(os.Getenv("APP_ENV"))

    // Optional: init tracing before the server starts (covers background jobs too).
    defer gincommon.InitTracingFromEnv()()

    cfg := grpccommon.Config{
        ServiceName:  "my-grpc-service",
        BuildVersion: os.Getenv("BUILD_VERSION"),
        Logger:       log, // enables structured logging + panic recovery logs
    }

    // Full stack: panic recovery, request ID, OTel tracing, Prometheus metrics,
    // structured logging, RequireAuth, and context building.
    s := grpc.NewServer(
        grpc.ChainUnaryInterceptor(grpccommon.FullUnaryInterceptors(cfg)...),
        grpc.ChainStreamInterceptor(grpccommon.FullStreamInterceptors(cfg)...),
    )

    pb.RegisterItemServiceServer(s, &itemServer{log: log})

    lis, _ := net.Listen("tcp", ":50051")
    if err := s.Serve(lis); err != nil {
        panic(err)
    }
}
```

### Observability-only (no auth on gRPC)

```go
s := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        grpccommon.ObservabilityUnaryInterceptors(cfg)...,
    ),
    grpc.ChainStreamInterceptor(
        grpccommon.ObservabilityStreamInterceptors(cfg)...,
    ),
)
```

### Reading context in a gRPC handler

After `FullUnaryInterceptors`, both the `RequestContext` and the request ID are available in the handler's `context.Context`:

```go
type itemServer struct{ log port.Logger }

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

### Per-RPC authorization

Add `RequirePermission` after `ProtectedUnaryInterceptors` for methods that need RBAC:

```go
authz := grpccommon.NewDemoRBAC() // demo; implement port.Authorizer for production

s := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        append(
            grpccommon.ObservabilityUnaryInterceptors(cfg),
            grpccommon.ProtectedUnaryInterceptors(cfg)...,
            // per-method: only admin can delete
            grpccommon.RequirePermissionWithLogger("delete", "items", authz, log),
        )...,
    ),
)
```

### Required gRPC metadata (protected calls)

Callers (API gateway, other services) must set incoming metadata:

| Metadata key | Required | Notes |
|-------------|----------|-------|
| `x-user-id` | Yes | Injected by trusted gateway |
| `x-tenant-id` | Yes | Injected by trusted gateway |
| `x-tenant-roles` | Recommended | Comma-separated (e.g. `admin,user`) |
| `x-request-id` | Optional | Generated if absent |
| `traceparent` | Optional | W3C trace context |

---

## RequestContext

Both HTTP and gRPC handlers receive the same struct after `ContextMiddleware` / `UnaryContextInterceptor`:

```go
type RequestContext struct {
    TenantID string   // from x-tenant-id
    UserID   string   // from x-user-id
    Roles    []string // from x-tenant-roles (validated, deduped)
    TraceID  string   // OTel trace ID for the request
    ClientIP string   // client IP from Gin c.ClientIP() or gRPC peer
}
```

**HTTP:** `rc, ok := gincommon.RequestContext(c)`  
**gRPC:** `rc, ok := grpccommon.RequestContextFromContext(ctx)`

---

## Required HTTP headers

| Header | Required | Purpose |
|--------|----------|---------|
| `x-user-id` | Yes (protected routes) | User identity — must be injected by a trusted API gateway; not cryptographically verified by this library |
| `x-tenant-id` | Yes (protected routes) | Tenant identity — must be injected by a trusted API gateway |
| `x-tenant-roles` | Recommended | Comma-separated roles (e.g. `admin,user`); validated as `[a-zA-Z0-9_-]+`, deduplicated, max 64 chars per token |
| `x-request-id` | Optional | Correlation ID; valid values are short alphanumeric tokens or UUIDs (`[A-Za-z0-9._-]`, max 128 chars) and are preserved and echoed; missing or invalid values are replaced with a generated UUID |
| `traceparent` | Optional | W3C trace context — propagates an upstream trace into OTel spans |

**Validation rules:**
- `x-user-id` / `x-tenant-id`: non-empty, ≤256 chars, no control characters. `RequireAuth` returns **401 Unauthorized** on failure.
- `x-request-id`: expected to be a UUID or a short alphanumeric token (`[A-Za-z0-9._-]`, max 128 chars). Internally, the system uses a validated request ID: if the client provides a valid `x-request-id` it is reused and echoed back; otherwise a new UUID is generated and used consistently for logging, tracing, and downstream propagation. Clients must not rely on an invalid value being echoed back.
- `x-tenant-roles`: invalid tokens are silently dropped; the handler receives only validated roles.

Header name constants: `gincommon.HeaderUserID`, `HeaderTenantID`, `HeaderTenantRoles`, `HeaderRequestID`, `HeaderTraceIDResponse`, `HeaderRequestIDResponse`.

---

## Correlation ID vs Trace ID

These two identifiers serve different purposes and are often confused:

| | `x-request-id` / `X-Request-ID` | `X-Trace-ID` / `traceparent` |
|--|----------------------------------|-------------------------------|
| **What it is** | Service-level correlation ID | OpenTelemetry distributed trace ID |
| **Who sets it** | Client (or generated by `RequestIDMiddleware`) | OTel SDK (or propagated from upstream via `traceparent`) |
| **Scope** | Single service call | Entire distributed trace across all services |
| **Format** | UUID or short alphanumeric token | 32-character hex string (128-bit trace ID) |
| **Use for** | Correlating a specific request in logs | Finding all spans across services for one trace in Tempo/Jaeger |
| **Header in** | `x-request-id` | `traceparent` |
| **Header out** | `X-Request-ID` | `X-Trace-ID` |
| **In `RequestContext`** | Via `grpccommon.RequestIDFromContext(ctx)` | `rc.TraceID` |

**Both are included in every error response** so a single identifier from a client report is enough to locate the full trace and the specific request log:

```json
{
  "error": "forbidden",
  "status": 403,
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "request_id": "b1a2c3d4-e5f6-7890-abcd-1234567890ef"
}
```

Use `trace_id` to search Tempo/Jaeger for the full distributed trace. Use `request_id` to search structured logs for this specific service call.

---

## Calling the API (local testing)

```bash
# Public health check — no auth headers
curl -i http://localhost:8080/health

# Protected list
curl -i http://localhost:8080/api/items \
  -H "x-user-id: user-1" \
  -H "x-tenant-id: tenant-1" \
  -H "x-tenant-roles: user" \
  -H "x-request-id: my-req-001"

# Admin delete (reference RBAC requires admin role)
curl -i -X DELETE http://localhost:8080/api/items/42 \
  -H "x-user-id: user-1" \
  -H "x-tenant-id: tenant-1" \
  -H "x-tenant-roles: admin"

# Propagate an upstream trace
curl -i http://localhost:8080/api/items \
  -H "x-user-id: user-1" -H "x-tenant-id: tenant-1" \
  -H "traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
```

Successful responses include `X-Trace-ID` and `X-Request-ID`. Error JSON includes `trace_id` and `request_id` for support correlation.

---

## Security

| Topic | Guidance |
|--------|----------|
| **Trust boundary** | `x-user-id`, `x-tenant-id`, and `x-tenant-roles` must be set by a **trusted API gateway** after authentication. Block direct client access or strip these headers at the edge. |
| **Authorization** | `NewDemoRBAC()` is a **reference** `port.Authorizer` for tests/demos. Production services must implement `port.Authorizer` with your IAM/OPA policy. |
| **Roles header** | Roles are validated (`[a-zA-Z0-9_-]+`) and deduplicated but not signed; the gateway must not forward client-supplied role headers. |
| **HTTP gRPC parity** | `pkg/grpccommon` provides the same auth, context, and RBAC interceptors as `pkg/gincommon`. Use `FullUnaryInterceptors` / `FullStreamInterceptors` to enable them. |
| **Metrics** | `/metrics` is often public for Prometheus scraping; restrict via network policy, auth proxy, or a dedicated admin port. |
| **OTLP** | `OTEL_EXPORTER_OTLP_INSECURE` defaults to `true` only when `APP_ENV=dev`. Use TLS in production. |
| **Logging** | `service.Sanitize` redacts sensitive field names before logging. `LoggingMiddleware` sanitizes raw header values (control chars stripped, truncated to 256 bytes) to prevent log injection. |
| **Nil logger** | Passing `nil` as logger to any middleware or interceptor is safe — it degrades gracefully (no-op or stderr fallback), never panics. |

---

## Important rules for handler authors

These rules are enforced by the library's design. Violating them will produce incorrect behaviour or security gaps:

| Rule | Why |
|------|-----|
| **Never read identity headers directly** (`c.GetHeader("x-user-id")`) in a handler | Headers are validated and normalised by middleware. Raw header access bypasses validation and length limits. Always use `gincommon.RequestContext(c)` after `ProtectedMiddlewares`. |
| **Never write identity headers in a service response** | Identity headers flow inbound from the gateway only. Writing them outbound is a security leak. Use `PropagateHeaders(c, req)` for outbound service calls instead. |
| **Always put `RequirePermission` after `ProtectedMiddlewares`** | `AuthZMiddleware` requires a `RequestContext` in the Gin/Go context. Placing it before `ContextMiddleware` returns `500 Internal Server Error` rather than a meaningful error. |
| **Use `PropagateHeaders` for all service-to-service calls** | Calling another platform service without propagating `traceparent` and `x-request-id` breaks the distributed trace and makes it impossible to correlate requests across services. |
| **Use `ErrorResponse` to parse error bodies from other services** | All platform middleware emits a consistent `{error, status, trace_id, request_id}` JSON shape. Parsing raw strings will break when fields are added. |
| **Register `/health` before `ProtectedMiddlewares`** | `HealthHandler()` must not require auth — load balancers and k8s probes do not send identity headers. |

### Common mistakes

**Reading identity headers directly (incorrect):**

```go
// ❌ Wrong — bypasses validation, length limits, and normalisation
userID := c.GetHeader("x-user-id")
tenantID := c.GetHeader("x-tenant-id")
```

**Using RequestContext (correct):**

```go
// ✅ Correct — validated, trimmed, and ready to use after ProtectedMiddlewares
rc, ok := gincommon.RequestContext(c)
if !ok {
    c.AbortWithStatus(http.StatusInternalServerError)
    return
}
userID := rc.UserID
tenantID := rc.TenantID
```

**Making an outbound call without propagating headers (incorrect):**

```go
// ❌ Wrong — downstream service gets no trace context or identity; trace is broken
req, _ := http.NewRequest(http.MethodGet, "http://other-svc/data", nil)
http.DefaultClient.Do(req)
```

**Making an outbound call with propagation (correct):**

```go
// ✅ Correct — traceparent, x-request-id, x-user-id, x-tenant-id all forwarded
req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "http://other-svc/data", nil)
gincommon.PropagateHeaders(c, req)
http.DefaultClient.Do(req)
```

---

## Public API (`pkg/gincommon`)

| Symbol | Purpose |
|--------|---------|
| `Config{Logger, ServiceName, BuildVersion, Tracing}` | Middleware stack config. `ServiceName` is required (panics if empty). |
| `ObservabilityMiddlewares(cfg)` | `[PanicRecovery, RequestID, Tracing, CorrelationHeaders, Metrics, Logging]` |
| `ProtectedMiddlewares(cfg)` | `[RequireAuth, ContextMiddleware]` |
| `DefaultMiddlewares(cfg)` | Observability + Protected |
| `TimeoutMiddleware(d)` | Per-request context deadline; zero/negative is no-op |
| `HealthHandler()` | 200 `{"status":"ok"}` — register before ProtectedMiddlewares |
| `ErrorResponse` | Standard 4xx/5xx body: `Error`, `Status`, `TraceID`, `RequestID` |
| `PropagateHeaders(c, req)` | Inject trace + request ID + identity into outbound `*http.Request` |
| `RequirePermission(action, resource, auth)` | Per-route RBAC, no logging |
| `RequirePermissionWithLogger(action, resource, auth, logger)` | Per-route RBAC with audit log |
| `NewDemoRBAC()` | Reference `port.Authorizer` (demos/tests only); `NewRBAC()` is a deprecated alias |
| `RequestContext(c)` | `*domain.RequestContext` from Gin context |
| `TraceIDFromContext(c)` / `RequestIDFromContext(c)` | Correlation IDs from Gin context |
| `InitTracing(svc, ver, endpoint)` | Init OTLP export; returns shutdown func |
| `EnsureTracing(svc, ver, endpoint)` | Init only if no SDK provider registered yet |
| `InitTracingFromEnv()` | Init from `OTEL_*` env vars |
| `Shutdown(logger)` | Flush OTel spans + `logger.Sync()`; call from `main()` after server stops |
| `MetricsRegisterer()` | `prometheus.Registerer` gincommon's own metrics use — register custom collectors here |
| `MetricsConstLabels()` | Copy of the `{service, version}` const labels applied to gincommon's own metrics |

## Public API (`pkg/grpccommon`)

| Symbol | Purpose |
|--------|---------|
| `Config{ServiceName, BuildVersion, Logger, Tracing}` | `ServiceName` required. `Logger` enables logging and panic recovery logs. `Tracing *TracingOptions` overrides `OTEL_*` env. |
| `ObservabilityUnaryInterceptors(cfg)` | `[PanicRecovery, RequestID, Tracing, Metrics, Logging]` |
| `ObservabilityStreamInterceptors(cfg)` | Same for streaming RPCs |
| `ProtectedUnaryInterceptors(cfg)` | `[RequireAuth, ContextInterceptor]` |
| `ProtectedStreamInterceptors(cfg)` | Same for streaming RPCs |
| `FullUnaryInterceptors(cfg)` | Observability + Protected |
| `FullStreamInterceptors(cfg)` | Same for streaming RPCs |
| `DefaultUnaryInterceptors(cfg)` | Alias for `ObservabilityUnaryInterceptors` (backward compatible) |
| `DefaultStreamInterceptors(cfg)` | Alias for `ObservabilityStreamInterceptors` |
| `RequirePermission(action, resource, auth)` | Per-RPC RBAC unary interceptor |
| `RequirePermissionWithLogger(action, resource, auth, logger)` | Per-RPC RBAC with audit log |
| `RequireStreamPermission(action, resource, auth)` | Per-stream RBAC interceptor |
| `RequireStreamPermissionWithLogger(action, resource, auth, logger)` | Per-stream RBAC with audit log |
| `NewDemoRBAC()` | Reference `port.Authorizer` (demos/tests only) |
| `RequestContextFromContext(ctx)` | `*domain.RequestContext` from gRPC handler context |
| `RequestIDFromContext(ctx)` | Correlation request ID from gRPC handler context |
| `Shutdown(logger)` | Flush OTel spans + `logger.Sync()`; call after `GracefulStop` returns |
| `MetricsRegisterer()` | `prometheus.Registerer` gincommon's own metrics use — register custom collectors here |
| `MetricsConstLabels()` | Copy of the `{service, version}` const labels applied to gincommon's own metrics |

## Public API (`pkg/logger`)

| Symbol | Purpose |
|--------|---------|
| `NewLogger(env)` | Zap-backed `port.Logger`; dev format when `env=="dev"`, production otherwise |

---

## Environment variables (consumer service)

| Variable | Example | Default | Used for |
|----------|---------|---------|----------|
| `APP_ENV` | `dev` / `prod` | `dev` | Zap log format; OTLP insecure default |
| `BUILD_VERSION` | `1.2.3` | `unknown` | Prometheus `build_info`, OTel `service.version` |
| `OTEL_SERVICE_NAME` | `my-service` | value of `APP_NAME` | OTel `service.name` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `collector:4317` | `localhost:4317` | OTLP/gRPC collector |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | `true` when `APP_ENV=dev`, else `false` | Plaintext OTLP |
| `OTEL_TRACES_SAMPLER_RATIO` | `0.1` | `1.0` in dev, `0.1` otherwise | Sampling ratio `0.0`–`1.0` (takes precedence) |
| `OTEL_TRACES_SAMPLER_ARG` | `0.1` | — | OTel-spec fallback for the sampling ratio when `OTEL_TRACES_SAMPLER_RATIO` is unset |

---

## Project layout

```
platform-gincommon/
├── pkg/
│   ├── gincommon/          # Public HTTP middleware, tracing, auth, authz, timeout, health
│   ├── grpccommon/         # Public gRPC interceptors (observability, auth, context, RBAC)
│   └── logger/             # Public Zap logger (port.Logger)
├── internal/
│   ├── adapter/inbound/
│   │   ├── http/middleware/ # PanicRecovery, RequestID, Tracing, Metrics, Logging, Auth, Context, AuthZ, Timeout
│   │   └── grpc/middleware/ # gRPC equivalents of all HTTP middleware above
│   ├── adapter/outbound/
│   │   ├── logger/          # Zap adapter → port.Logger
│   │   ├── metrics/         # Prometheus HTTP + gRPC collectors
│   │   └── otel/            # OpenTelemetry setup
│   ├── core/
│   │   ├── domain/          # RequestContext{TenantID,UserID,Roles,TraceID,ClientIP}; transport constants; Go-context helpers
│   │   ├── port/            # Logger, Authorizer, ContextBuilder interfaces
│   │   └── service/         # ContextService, RBACService, LogSanitizer
│   ├── config/              # Env var loading
│   └── server/              # Example router (dev/demo only)
├── cmd/server/              # Optional example binary (not required for consumers)
├── test/
│   ├── unit/                # Unit tests per package (no Docker)
│   ├── integration/         # In-process HTTP server tests
│   ├── e2e/                 # Full server startup tests
│   └── smoke/               # Live or in-process smoke checks
├── docs/architecture/       # Architecture docs and Mermaid diagram sources
├── ARCHITECTURE.md          # Layer model, flow diagrams, invariants
├── docker-compose.yml       # Local OTel Collector / Tempo / Prometheus / Grafana / Loki
└── .env-example             # Local dev environment template
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
| `make test-unit` | `test/unit/...` only — no Docker |
| `make test-int` | Integration tests |
| `make test-e2e` | E2E tests |
| `make test-smoke` | Smoke tests (in-process + optional live server) |
| `make race` | All tests with `-race` |
| `make lint` | golangci-lint |
| `make cover` | Coverage report (`coverage.out` + HTML); ≥95% required |
| `make cover-func` | Per-function coverage summary |
| `make build` | Build example binary to `bin/platform-gincommon` |
| `make run` | Run example server (`cmd/server`) |
| `make docker-up` | Start local observability stack |
| `make ci` | `tidy` + `vet` + `lint` + `test-ci` + `build` |

### Example server (optional)

`cmd/server` is a **reference app** for local testing. It exposes `/health`, `/metrics`, and a `GET /panic` route (dev only).

```bash
make run
# Protected request:
curl -H "x-user-id: user-1" -H "x-tenant-id: tenant-1" http://localhost:8080/health
```

### Observability stack (optional)

```bash
make docker-up
```

Trace flow: **app** → OTLP gRPC `localhost:4317` → **otel-collector** → **Tempo**. Prometheus scrapes the app on `:8080/metrics`. Grafana is at `localhost:3000`.

**Logs:** the example server writes structured JSON to `LOG_FILE` (default `logs/app.log` in `.env-example`). **Promtail** ships that file into **Loki**. Set `LOG_FILE` in `.env`, run `make docker-up`, then `make run`, and generate traffic so `http_request` lines are emitted. In Grafana → **Explore** → **Loki**, try `{job="platform-gincommon"}` or `{service="platform-gincommon"}`.

---

## Testing

- Tests live under **`test/`**, separate from production packages.
- **Coverage gate:** CI requires ≥95% statement coverage on `./internal/...` and `./pkg/...`.
- **`make test`** runs all tests locally without a running server.
- **`make test-smoke`** can hit a live URL (`SMOKE_BASE_URL` in `.env`) or uses an in-process server.
- Use `test/fixtures.MockLogger` to capture and assert structured log output in unit tests.

---

## CI

GitHub Actions runs on push/PR to `main`:

1. **validate** — `gofmt`, `go mod tidy` drift check, `go vet`, `golangci-lint`, `govulncheck`
2. **coverage** — all tests with race detector + ≥95% coverage gate
3. **build** — compile demo binary
4. **docker** — build and push demo image to GHCR (push events only)

Release (`v*` tags): validate → build → docker (semver tags) → GitHub Release (changelog excerpt).

---

## Docker (example server only)

```bash
make docker-build
docker run --rm -p 8080:8080 -e APP_ENV=prod platform-gincommon:local
```

The image runs the **demo** binary. Consumer services do not deploy this image.

---

## Out of scope

This library does **not** handle:

| Concern | Where it lives |
|---------|---------------|
| JWT validation, OAuth, API key auth | API gateway — identity is trusted and pre-validated before headers reach this library |
| Database access, connection pooling, retry logic | [`platform-pgcommon`](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon) |
| Business validation logic | Your service handlers |
| Rate limiting, circuit breaking | Infrastructure layer (API gateway, service mesh) |
| Request body parsing and binding | Your service (Gin's `c.ShouldBindJSON`, etc.) |

This library only standardises **transport, observability, and identity context propagation**. Keeping this scope narrow ensures it stays safe to upgrade across all services without risk of business logic changes.

---

## License / ownership

BCBP Solutions FZC LLC — internal platform shared library.

---

## See also

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Layer model, request/gRPC flow diagrams, key invariants |
| [VERSIONING.md](./VERSIONING.md) | SemVer rules, supported versions, release process |
| [CHANGELOG.md](./CHANGELOG.md) | Per-version changes |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Development setup, adding middleware, PR checklist |
| [SECURITY.md](./SECURITY.md) | Vulnerability reporting, trust model, supported versions |
