# Contributing

This is an internal shared library for BCBP Solutions platform services. This guide covers how to set up a development environment, add new middleware, write tests, and cut a release.

## Prerequisites

- Go 1.26+ (matches `go.mod`)
- Docker (for the local observability stack)
- `golangci-lint` is managed as a Go tool — no separate install needed

## Development setup

```bash
git clone https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon
cd platform-gincommon
make setup   # copies .env-example → .env
make tidy    # go mod tidy
make lint    # verify linter passes
make test    # run all tests
```

## Project layout

```
pkg/            ← Public API (consumers import these — SemVer applies)
internal/
  core/domain/  ← Entities: no external deps allowed
  core/port/    ← Interfaces owned by the use-case layer
  core/service/ ← Use cases: depend only on domain + port
  adapter/inbound/   ← HTTP middleware, gRPC interceptors
  adapter/outbound/  ← Zap logger, Prometheus metrics, OTel
  config/       ← Env var loading
  server/       ← Example server wiring (demo only)
cmd/server/     ← Example binary (not for consumers)
test/           ← All tests live here, not beside production code
```

**Dependency rule:** `domain ← port ← service ← adapter ← pkg`. Inner layers must never import outer layers.

## Adding a new HTTP middleware

1. Implement in `internal/adapter/inbound/http/middleware/your_middleware.go`.
2. Expose via a public function in `pkg/gincommon/gincommon.go` (or a new file under `pkg/gincommon/`).
3. Add unit tests in `test/unit/middleware/`.
4. If the middleware adds new metrics, register them in `internal/adapter/outbound/metrics/metrics.go` inside `initMetrics`.
5. If the middleware changes the execution order, update the middleware order diagram in `README.md` and `CLAUDE.md`.

## Adding a new metric

Register the collector variable in `internal/adapter/outbound/metrics/metrics.go`:
- Declare the package-level `var` with the collector type.
- Initialize it inside `initMetrics` (called by `Init` via `sync.Once`).
- Add a nil guard in any helper function that accesses the variable directly (follow the pattern in `RecordHTTPRateLimited`).

## Replacing logger or RBAC

- **Logger:** implement `port.Logger` (`internal/core/port/logger.go`), place the implementation in `internal/adapter/outbound/`, inject via `gincommon.Config.Logger`.
- **RBAC:** implement `port.Authorizer` (`internal/core/port/authorizer.go`), pass to `gincommon.RequirePermission`.

## Testing requirements

| Layer | Location | Notes |
|-------|----------|-------|
| Unit | `test/unit/` | Black-box, no network, no filesystem; use `httptest` for HTTP |
| Integration | `test/integration/` | Full middleware chain on an in-process `httptest.Server` |
| E2E | `test/e2e/` | Full server startup; keeps the binary honest |
| Smoke | `test/smoke/` | Optional; targets a live URL (`SMOKE_BASE_URL`) |

All tests under `test/unit/` and `test/integration/` must pass without a running server or Docker stack.

Run tests with the race detector before submitting a PR:

```bash
make race
```

## Linting

```bash
make lint
```

The lint config (`.golangci.yml`) enforces `revive` rules including godoc on every exported symbol in `pkg/` and `internal/`. Add a package doc comment to new packages.

## Pull request checklist

- [ ] `make ci` passes locally (`tidy` + `lint` + `test` + `build`)
- [ ] `make race` passes (no data races)
- [ ] New exported symbols have godoc comments
- [ ] New behavior is covered by tests in `test/unit/` or `test/integration/`
- [ ] `CHANGELOG.md` `[Unreleased]` section updated
- [ ] If a public API in `pkg/` changed: README and `VERSIONING.md` impact assessed

## Commit style

Use a short imperative subject line (≤ 72 chars). Reference the area:

```
fix(metrics): clamp response size to 0 for bodyless responses
feat(tracing): always set span attributes on panic path
docs: update sampling ratio default in README
```

## Cutting a release

See **[VERSIONING.md § Maintainer release process](./VERSIONING.md#maintainer-release-process)** for the full steps. Short version:

1. Move `[Unreleased]` items to a new `## [X.Y.Z] - YYYY-MM-DD` section in `CHANGELOG.md`.
2. Run `make ci` locally.
3. Create and push an annotated tag: `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`.
4. Create a GitHub Release from the tag; paste the CHANGELOG section as release notes.
5. Notify consuming teams via Slack with upgrade notes if MINOR or MAJOR.
