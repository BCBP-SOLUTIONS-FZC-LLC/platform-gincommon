#!/usr/bin/env bash
# Creates the GitHub release (the Go module version consumers pin to) with the
# metricslint binaries, checksums, SBOM and SLSA provenance attached.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"

test -f release-asset.name || {
  echo "::error::release-asset.name missing — run prepare-release-binary.sh first"
  exit 1
}

ASSET_NAME=$(cat release-asset.name)
for f in release-notes.md "$ASSET_NAME" "${ASSET_NAME}.sha256" provenance.slsa.json sbom.cyclonedx.json; do
  test -f "$f" || {
    echo "::error::Release asset missing: ${f}"
    exit 1
  }
done

# One verifiable file for every binary: sha256sum --check checksums.txt
sha256sum metricslint_* | grep -v '\.sha256$' > checksums.txt
mapfile -t BINARIES < <(find . -maxdepth 1 -name 'metricslint_*' ! -name '*.sha256' -printf '%f\n' | sort)

PRERELEASE_FLAG=()
if echo "$RELEASE_TAG" | grep -q -- '-'; then
  PRERELEASE_FLAG=(--prerelease)
fi

DIGEST_SHORT="${IMAGE_DIGEST#sha256:}"
DIGEST_SHORT="${DIGEST_SHORT:0:12}"

gh release create "$RELEASE_TAG" \
  --title "${RELEASE_TAG} · demo image sha256:${DIGEST_SHORT}" \
  --notes-file release-notes.md \
  "${BINARIES[@]}" \
  checksums.txt \
  provenance.slsa.json \
  sbom.cyclonedx.json \
  "${PRERELEASE_FLAG[@]}"
