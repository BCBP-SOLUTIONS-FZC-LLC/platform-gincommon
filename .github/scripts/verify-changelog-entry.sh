#!/usr/bin/env bash
# Fails the release if CHANGELOG.md has no section for RELEASE_TAG.
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"

VERSION="${RELEASE_TAG#v}"
# Fixed-string match: the dots in VERSION must not act as regex wildcards
# (otherwise "1.4.0" would also match a "## [1x4y0]" heading).
if ! grep -qF -- "## [${VERSION}]" CHANGELOG.md 2>/dev/null; then
  echo "::error file=CHANGELOG.md::Missing entry for ${VERSION}"
  exit 1
fi
echo "  ✔  changelog entry found for ${VERSION}"
