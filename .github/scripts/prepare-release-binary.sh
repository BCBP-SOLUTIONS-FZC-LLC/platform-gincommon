#!/usr/bin/env bash
# Cross-compiles the metricslint release binaries and writes per-binary
# checksums. platform-gincommon is a Go library (consumed with `go get`), so
# the only binary worth attaching to a release is metricslint: services'
# CI downloads it to enforce the Enterprise Platform Observability Standard
# without `go run`. cmd/server is a demo and ships only as the GHCR image.
#
# Produces for each platform, in the working directory:
#   metricslint_{tag}_{os}_{arch}[.exe]
#   metricslint_{tag}_{os}_{arch}[.exe].sha256
# release-checksums.sh (publish job) re-verifies the .sha256 files and writes
# the aggregate checksums.txt that release.yml Cosign-signs.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"

TARGETS=(
  "linux   amd64"
  "linux   arm64"
  "darwin  amd64"
  "darwin  arm64"
  "windows amd64"
)

echo "Building metricslint release binaries for tag ${RELEASE_TAG}"

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<< "$target"
  EXT=""
  [ "${GOOS}" = "windows" ] && EXT=".exe"
  ASSET_NAME="metricslint_${RELEASE_TAG}_${GOOS}_${GOARCH}${EXT}"

  echo "  → ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
    go build -trimpath -ldflags "-s -w" -o "${ASSET_NAME}" ./cmd/metricslint

  chmod +x "${ASSET_NAME}"
  sha256sum "${ASSET_NAME}" > "${ASSET_NAME}.sha256"
  echo "    ✔  ${ASSET_NAME}"
done

# The canonical (linux/amd64) asset, for steps that need one file reference.
echo "metricslint_${RELEASE_TAG}_linux_amd64" > release-asset.name
echo "All binaries prepared."
