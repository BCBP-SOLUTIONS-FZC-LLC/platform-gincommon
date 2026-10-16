#!/usr/bin/env bash
# Extracts RELEASE_TAG section from CHANGELOG.md and appends the Go module
# install line, the metricslint binaries and the demo image metadata.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${IMAGE_NAME:?IMAGE_NAME is required}"
: "${IMAGE_VERSION:?IMAGE_VERSION is required}"
: "${IMAGE_DIGEST:?IMAGE_DIGEST is required}"

# shellcheck source=changelog-section.sh
source "$(dirname "$0")/changelog-section.sh"

VERSION="${RELEASE_TAG#v}"
if ! SECTION=$(changelog_section_version "$RELEASE_TAG"); then
  echo "::error file=CHANGELOG.md::Missing entry for ${VERSION}"
  exit 1
fi
# Plain redirects, not `| tee file | true`: under pipefail, `true` exits
# without reading, tee can die of SIGPIPE and fail the whole release.
changelog_section_body "$SECTION" > release-notes.md

if [ ! -s release-notes.md ]; then
  echo "::error file=CHANGELOG.md::Section [${SECTION}] for ${VERSION} is empty"
  exit 1
fi
if [ "$SECTION" != "$VERSION" ]; then
  release_notes_prefix="> Release candidate ${RELEASE_TAG} of ${SECTION}."
fi

{
  if [ -n "${release_notes_prefix:-}" ]; then
    echo "${release_notes_prefix}"
    echo ""
  fi
  cat release-notes.md
  echo ""
  echo "---"
  echo "**Go module:** \`go get github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2@${RELEASE_TAG}\`"
  echo "**metricslint:** binaries attached below (\`sha256sum --check checksums.txt\`)"
  echo "**Demo image (reference only, not a production artefact):** \`${IMAGE_NAME}:${IMAGE_VERSION}\`"
  echo "**Digest:** \`${IMAGE_DIGEST}\`"
  echo "**Verify:** the image and \`checksums.txt\` are Cosign keyless-signed (bundle: \`checksums.txt.sigstore.json\`); see the README's *Releases* section for the commands."
} > release-notes.md.tmp
mv release-notes.md.tmp release-notes.md
cat release-notes.md
