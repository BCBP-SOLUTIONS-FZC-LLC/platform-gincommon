#!/usr/bin/env bash
# Ensures checked-out HEAD carries RELEASE_TAG (exact tag, not branch tip) and
# that the tagged commit is on the default branch, so a tag pushed to an
# unmerged branch cannot publish a module release.
# Requires a full-history checkout (actions/checkout fetch-depth: 0).
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
RELEASE_BRANCH="${RELEASE_BRANCH:-main}"

# HEAD may carry several tags (e.g. v1.4.0 and a moving alias), so check
# membership rather than relying on git describe picking one of them.
if ! git tag --points-at HEAD | grep -qxF -- "$RELEASE_TAG"; then
  echo "::error title=Tag mismatch::RELEASE_TAG=${RELEASE_TAG} is not among the tags at HEAD ($(git tag --points-at HEAD | tr '\n' ' '))"
  exit 1
fi
echo "  ✔  tag verified: ${RELEASE_TAG}"

if ! git rev-parse --verify --quiet "refs/remotes/origin/${RELEASE_BRANCH}" >/dev/null; then
  git fetch --no-tags --quiet origin "+refs/heads/${RELEASE_BRANCH}:refs/remotes/origin/${RELEASE_BRANCH}"
fi
if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  git fetch --no-tags --quiet --unshallow origin
fi

if ! git merge-base --is-ancestor HEAD "refs/remotes/origin/${RELEASE_BRANCH}"; then
  echo "::error title=Tag not on ${RELEASE_BRANCH}::${RELEASE_TAG} ($(git rev-parse --short HEAD)) is not reachable from origin/${RELEASE_BRANCH}"
  exit 1
fi
echo "  ✔  ${RELEASE_TAG} is reachable from origin/${RELEASE_BRANCH}"
