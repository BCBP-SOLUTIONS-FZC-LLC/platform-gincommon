#!/usr/bin/env bash
# Extracts the RELEASE_TAG section from CHANGELOG.md and appends the Go module
# and demo-image metadata.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${IMAGE_NAME:?IMAGE_NAME is required}"
: "${IMAGE_VERSION:?IMAGE_VERSION is required}"
: "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"

VERSION="${RELEASE_TAG#v}"
# index() rather than a regex so the dots in a semver are not wildcards.
awk -v ver="[${VERSION}]" '
  substr($0, 1, 3) == "## " && index($0, ver) > 0 { found=1; next }
  /^## \[/ { if (found) exit }
  found { print }
' CHANGELOG.md | sed -e 's/^[[:space:]]*$//' > release-notes.md

if [ ! -s release-notes.md ]; then
  echo "::error file=CHANGELOG.md::Missing entry for ${VERSION}"
  exit 1
fi

{
  echo ""
  echo "---"
  echo "**Go module:** \`go get github.com/${GITHUB_REPOSITORY:-BCBP-SOLUTIONS-FZC-LLC/platform-gincommon}@${RELEASE_TAG}\`"
  echo "**metricslint:** binaries attached below (\`sha256sum --check checksums.txt\`)"
  echo "**Demo image (reference only, not a production artefact):** \`${IMAGE_NAME}:${IMAGE_VERSION}\`"
  echo "**Digest:** \`${IMAGE_DIGEST}\`"
} >> release-notes.md
