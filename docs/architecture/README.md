# Architecture diagrams

Mermaid source files for the diagrams embedded in [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md).

Each `.mmd` file is a standalone Mermaid diagram. The corresponding section in `ARCHITECTURE.md` embeds the content as a fenced code block and carries a `> Source:` link back here.

| File | Diagram |
|------|---------|
| [`layer-model.mmd`](mermaid/layer-model.mmd) | Clean Architecture layer model — pkg → adapter → core |
| [`package-dependencies.mmd`](mermaid/package-dependencies.mmd) | Go import graph (module-internal only) |
| [`middleware-stack-composition.mmd`](mermaid/middleware-stack-composition.mmd) | How `DefaultMiddlewares`, `ObservabilityMiddlewares`, and `ProtectedMiddlewares` are assembled |
| [`request-flow.mmd`](mermaid/request-flow.mmd) | Full HTTP request sequence through all eight middleware layers |
| [`panic-recovery-flow.mmd`](mermaid/panic-recovery-flow.mmd) | `PanicRecoveryMiddleware` defer/recover sequence |
| [`rbac-middleware-flow.mmd`](mermaid/rbac-middleware-flow.mmd) | `AuthZMiddleware` authorization decision flow |
| [`tracing-initialization.mmd`](mermaid/tracing-initialization.mmd) | OTel provider setup: `InitTracing`, `EnsureTracing`, OTLP dial, fallback |
| [`metrics-initialization.mmd`](mermaid/metrics-initialization.mmd) | Prometheus collector registration and `sync.Once` guard |
| [`grpc-interceptors.mmd`](mermaid/grpc-interceptors.mmd) | Unary and stream metrics interceptor sequence |
| [`observability-stack.mmd`](mermaid/observability-stack.mmd) | Prometheus metrics and OTel spans side by side |
| [`consuming-service-wiring.mmd`](mermaid/consuming-service-wiring.mmd) | How a consuming service wires the library |
| [`documentation-assets.mmd`](mermaid/documentation-assets.mmd) | Relationship between doc sources and rendered outputs |
