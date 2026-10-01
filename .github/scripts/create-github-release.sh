#!/usr/bin/env bash
# Creates the GitHub release (the Go module version consumers pin to) with the
# metricslint binaries, checksums (plus their Cosign Sigstore bundle), SLSA
# provenance and the demo image SBOM attached.
# checksums.txt comes from release-checksums.sh and is signed by release.yml
# before this runs.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"

test -f release-asset.name || {
  echo "::error::release-asset.name missing — run prepare-release-binary.sh first"
  exit 1
}

ASSET_NAME=$(cat release-asset.name)
CHECKSUM_FILE="${ASSET_NAME}.sha256"

for f in release-notes.md "$ASSET_NAME" "$CHECKSUM_FILE" checksums.txt checksums.txt.sigstore.json provenance.slsa.json sbom.cyclonedx.json; do
  test -f "$f" || {
    echo "::error::Release asset missing: ${f}"
    exit 1
  }
done

PRERELEASE_FLAG=""
if echo "$RELEASE_TAG" | grep -q -- '-'; then
  PRERELEASE_FLAG="--prerelease"
fi

DIGEST_SHORT="${IMAGE_DIGEST#sha256:}"
DIGEST_SHORT="${DIGEST_SHORT:0:12}"
RELEASE_TITLE="${RELEASE_TAG} · demo image sha256:${DIGEST_SHORT}"

# Attach every platform binary of metricslint (not just linux/amd64).
BINARIES=()
for f in metricslint_*; do
  case "$f" in *.sha256) ;; *) BINARIES+=("$f") ;; esac
done

gh release create "$RELEASE_TAG" \
  --title "$RELEASE_TITLE" \
  --notes-file release-notes.md \
  "${BINARIES[@]}" \
  checksums.txt \
  checksums.txt.sigstore.json \
  provenance.slsa.json \
  sbom.cyclonedx.json \
  $PRERELEASE_FLAG
