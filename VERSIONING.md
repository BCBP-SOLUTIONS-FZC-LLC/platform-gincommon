# Versioning and releases

This repository is a **Go module** consumed by platform services. Versions are published with **Git tags** and described in [CHANGELOG.md](./CHANGELOG.md).

## Semantic versioning (SemVer)

We use [SemVer 2.0.0](https://semver.org/): `MAJOR.MINOR.PATCH` (e.g. `v1.2.3`).

| Bump | When you change | Examples |
|------|------------------|----------|
| **MAJOR** | Breaking change in the **public** API (`pkg/gincommon`, `pkg/grpccommon`) | Removed export, renamed middleware signature, changed `RequestContext` JSON tags, incompatible default behavior |
| **MINOR** | New backward-compatible capability | New middleware option, new exported constant, new optional `Config` field |
| **PATCH** | Backward-compatible fix | Bug fix, performance, docs-only correction in published API behavior |

### What counts as public API

| In scope (SemVer applies) | Out of scope (may change without MAJOR) |
|---------------------------|----------------------------------------|
| `pkg/gincommon/*` | `internal/*` |
| `pkg/grpccommon/*` | `cmd/server` (example binary) |
| Documented env vars (`OTEL_*`, `BUILD_VERSION`) when behavior is guaranteed in README | Test-only packages under `test/` |

Import **`pkg/gincommon`** and **`pkg/grpccommon`** only. Do not import `internal/` from consumer services (Go enforces this for external modules).

### Guarantees

- **MAJOR `v1`:** We avoid breaking changes in `pkg/*` within `v1.x`. When breaking changes are required, we release **`v2.0.0`** and document migration in CHANGELOG.
- **MINOR:** Safe to upgrade with `go get` without code changes unless you opt into new features.
- **PATCH:** Drop-in replacement; upgrade recommended for security fixes.
- **Pre-1.0:** Not applicable after `v1.0.0`. Future `v0.y.z` tags will not be used once `v1.0.0` is published.

### Recorded exceptions

- **`v1.4.0` ships breaking changes as a MINOR release.** It requires `Config.Domain` (or `OBSERVABILITY_DOMAIN`) and panics at startup without it, removes the pre-standard metric names, changes the `Authorizer` interface to `Authorize(ctx, *RequestContext, action, resource)`, stops `PropagateHeaders` from forwarding identity headers, and reorders the gRPC `Full*` chains. These would normally require `v2.0.0` with a `/v2` module path. The platform team chose a MINOR release because no consumer of `v1.x` had been deployed to any environment, and every consuming service is upgraded to `v1.4.0` in lockstep. Do **not** let `go get -u` pull `v1.4.0` into a service without following the migration steps in `CHANGELOG.md` (`[1.4.0]`) and `docs/observability/migration.md`. Later `v1.x` releases follow the guarantees above again.

## Supported releases

| Version | Status | Go module | Supported until |
|---------|--------|-----------|-----------------|
| `v2.x` | **Current** (from `v2.0.0`) | `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2` | Active |
| `v1.4.x`–`v1.6.x` | Previous major | `github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@v1.6.0` | Security fixes only, for the period the platform team sets after `v2.0.0` |
| `v1.0.x`–`v1.3.x` | Superseded | `@v1.3.0` | No further releases; upgrade to `v2.x` |
| `< v1.0.0` | — | — | No tagged releases before `v1.0.0` |

`v2.0.0` is a proper MAJOR: the error contract became RFC 9457 Problem Details (`ErrorResponse` removed, every HTTP error body and its `Content-Type` changed), so the module path gained the `/v2` suffix Go requires. Import `.../platform-gincommon/v2/pkg/...`; see *Upgrading to v2.0.0* in the README.

When a new **MAJOR** line ships, the previous major receives **security fixes only** for a period defined by the platform team (typically 6 months after `v2.0.0`).

## Consume a release

Pin in your service `go.mod`:

```bash
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@v2.0.0
```

| Pin style | Use when |
|-----------|----------|
| `@v2.0.0` | Production; exact reproducibility |
| `@v2.0.3` | Production; latest patch on `2.0` |
| `@v2.2.0` | Accept new minors on `2.x` (still SemVer-safe) |
| `@latest` | Experiments only; not recommended for prod |

Go resolves versions from **Git tags** on the default branch (see [Go module versioning](https://go.dev/doc/modules/version-numbers)).

## Maintainer release process

The release workflow (`.github/workflows/release.yml`) automates validation, Docker image push, and GitHub Release creation. Your job is to prepare the commit and push the tag.

1. **Merge** all changes for the release to `main`.

2. **Update CHANGELOG.md:** move `[Unreleased]` entries into a new `## [X.Y.Z] - YYYY-MM-DD` section.
   The release workflow reads this section to populate the GitHub Release body — the tag push will fail with a clear error if the section is missing.

3. **Run `make ci` locally** to confirm everything is green before tagging:
   ```bash
   make ci   # tidy + vet + lint + test-ci (race) + build
   ```

4. **Create and push an annotated tag** — this triggers the release workflow automatically:
   ```bash
   git tag -a v1.2.3 -m "v1.2.3"
   git push origin v1.2.3
   ```

5. **The release workflow** (triggered by the tag) will:
   - Re-run `vet`, `lint`, and tests with the race detector at the exact tagged commit.
   - Build and push the Docker image to GHCR with version tags (`v1.2.3`, `v1.2`, `v1`, `latest`).
   - Create a **[GitHub Release](https://github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/releases)** with the CHANGELOG section as release notes.

6. **Notify consumers** (Slack/ADR) with upgrade notes if MINOR or MAJOR.

### Pre-release tags (optional)

| Tag pattern | Meaning |
|-------------|---------|
| `v1.1.0-rc.1` | Release candidate; not for production unless approved |
| `v1.1.0-beta.1` | Early integration testing |

## Compatibility matrix (library ↔ Go)

| platform-gincommon | Go (`go.mod`) |
|--------------------|---------------|
| `v1.x` | `1.26+` |
| `v2.x` | `1.26+` |

Consumer services must use the same or newer Go toolchain as stated in this module’s `go.mod`.

## Related files

| File | Purpose |
|------|---------|
| [CHANGELOG.md](./CHANGELOG.md) | User-facing history per version |
| [README.md](./README.md#versioning--releases) | Summary table and quick links |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Development guide and PR checklist |
| [.github/workflows/release.yml](../.github/workflows/release.yml) | Automated release pipeline |
| [go.mod](./go.mod) | Module path and minimum Go version |
