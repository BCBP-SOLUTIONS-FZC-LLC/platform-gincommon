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

## Supported releases

| Version | Status | Go module | Supported until |
|---------|--------|-----------|-----------------|
| `v1.0.x` | **Current** | `@v1.0.0` | Active; patch releases as needed |
| `< v1.0.0` | — | — | No tagged releases before `v1.0.0` |

When a new **MAJOR** line ships (e.g. `v2`), the previous major receives **security fixes only** for a period defined by the platform team (typically 6 months after `v2.0.0`).

## Consume a release

Pin in your service `go.mod`:

```bash
go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon@v1.0.0
```

| Pin style | Use when |
|-----------|----------|
| `@v1.0.0` | Production; exact reproducibility |
| `@v1.0.3` | Production; latest patch on `1.0` |
| `@v1.2.0` | Accept new minors on `1.x` (still SemVer-safe) |
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
| `v1.0.x` | `1.26+` |

Consumer services must use the same or newer Go toolchain as stated in this module’s `go.mod`.

## Related files

| File | Purpose |
|------|---------|
| [CHANGELOG.md](./CHANGELOG.md) | User-facing history per version |
| [README.md](./README.md#versioning--releases) | Summary table and quick links |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Development guide and PR checklist |
| [.github/workflows/release.yml](../.github/workflows/release.yml) | Automated release pipeline |
| [go.mod](./go.mod) | Module path and minimum Go version |
