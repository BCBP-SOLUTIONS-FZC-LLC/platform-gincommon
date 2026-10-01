# Architecture diagrams

Standalone Mermaid source files. [`ARCHITECTURE.md`](../../ARCHITECTURE.md) embeds every one of these `.mmd` files verbatim as a fenced code block, each under a `> Source:` link back to the file here, so a diagram only needs to be correct in one place. Keep both in sync when either changes.

| File | Diagram | Related section |
|------|---------|-----------------|
| [`layer-model.mmd`](mermaid/layer-model.mmd) | Clean Architecture layers: `cmd` → `pkg` → adapters → core, plus `obsregistry` and `config` leaves | ARCHITECTURE.md § Layer model |
| [`package-dependencies.mmd`](mermaid/package-dependencies.mmd) | Module-internal Go import graph, generated from `go list` | § Package dependency graph |
| [`middleware-stack-composition.mmd`](mermaid/middleware-stack-composition.mmd) | What each HTTP/gRPC stack constructor validates, initialises and returns | § Middleware stack composition |
| [`request-flow.mmd`](mermaid/request-flow.mmd) | Full HTTP request through observability, timeout, auth and context | § Request flow |
| [`grpc-interceptors.mmd`](mermaid/grpc-interceptors.mmd) | `Full*` interceptor chain: health bypass, auth, context, innermost timeout, status classification | § gRPC interceptor flow |
| [`auth-context-flow.mmd`](mermaid/auth-context-flow.mmd) | Identity validation: repeated headers, caps, roles, `RequestContext`, audit logging with `claimed_*` | § Identity, authentication and request context |
| [`rbac-middleware-flow.mmd`](mermaid/rbac-middleware-flow.mmd) | `Authorizer` decision on HTTP and gRPC: misordered chain, deny, internal error, allow | § Authorization (RBAC) flow |
| [`panic-recovery-flow.mmd`](mermaid/panic-recovery-flow.mmd) | Panic vs `http.ErrAbortHandler` through the four recording layers; panic after a partial write | § Panic and abort recovery |
| [`timeout-flow.mmd`](mermaid/timeout-flow.mmd) | HTTP `RequestTimeout`, gRPC unary cap, gRPC stream handler goroutine | § Timeouts |
| [`tracing-initialization.mmd`](mermaid/tracing-initialization.mmd) | Entry points, provisional provider, sampler, exporter fallback, delegating global provider, `Shutdown` | § Tracing initialisation |
| [`metrics-initialization.mmd`](mermaid/metrics-initialization.mmd) | Identity resolution, transactional registration, mismatch, version upgrade, atomic recording | § Metrics initialisation and process identity |
| [`registry-governance.mmd`](mermaid/registry-governance.mmd) | Registry sources → runtime collectors → CI enforcement (`Validate`, `metricslint`) → mirrors | § Observability registry and governance |
| [`observability-stack.mmd`](mermaid/observability-stack.mmd) | Per-request layers → Prometheus, OpenTelemetry, structured logs, reference rules | § Observability stack |
| [`consuming-service-wiring.mmd`](mermaid/consuming-service-wiring.mmd) | How a consuming service wires `pkg/` for HTTP, gRPC and outbound calls | § Consuming service wiring |
| [`documentation-assets.mmd`](mermaid/documentation-assets.mmd) | Documentation sources and where they are rendered | § Documentation assets |

To render locally, open any `.mmd` file in a Mermaid-aware IDE (VS Code + Mermaid Preview, IntelliJ + Mermaid plugin) or paste it into [mermaid.live](https://mermaid.live).

`ARCHITECTURE.md` is assembled by inlining these files at `<<MMD:name>>` markers. When you change a diagram, update the matching fenced block in `ARCHITECTURE.md` to the file's exact contents.
