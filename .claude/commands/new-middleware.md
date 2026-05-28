Scaffold a new Gin HTTP middleware following this project's Clean Architecture conventions.

Given a middleware name (e.g. "rate-limiter"), do the following:

1. **Interface Adapter (inbound):** Create `internal/adapter/inbound/http/middleware/<name>.go`
   - Implement a `func <Name>Middleware(...) gin.HandlerFunc`
   - Depend only on `internal/core/domain`, `internal/core/port`, and `internal/adapter/outbound/metrics` — never on frameworks directly in the core
   - Follow the same signature and error-response patterns as existing middleware (e.g. `auth.go`, `logging.go`)

2. **Expose via public API:** Add the new middleware to `pkg/gincommon/gincommon.go` with a clear doc comment. Wire it into the appropriate stack (`DefaultMiddlewares`, `ObservabilityMiddlewares`, or `ProtectedMiddlewares`) only if it belongs there by default.

3. **Metrics (if applicable):** Register any new Prometheus metrics in `internal/adapter/outbound/metrics/metrics.go` using the existing `sync.Once` init pattern.

4. **Unit test:** Create `test/unit/middleware/<name>_test.go` mirroring the style of existing unit tests in that directory.

5. **Integration test (if applicable):** Add a test case to `test/integration/middleware_test.go` covering the middleware in the full stack.

Do not add the middleware to the stack without asking first if its placement is unclear.
