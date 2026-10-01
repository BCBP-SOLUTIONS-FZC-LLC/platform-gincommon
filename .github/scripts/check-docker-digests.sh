#!/usr/bin/env bash
# Fails unless every Dockerfile FROM line is digest-pinned and matches the
# checked-in provenance record .docker-digests (written by
# `make pin-base-images`), so a hand-edited digest cannot slip through.
set -euo pipefail

if grep -E '^FROM [^ @]+:[^ @]+( AS .+)?$' Dockerfile; then
  echo "::error file=Dockerfile,title=Unpinned base image::Dockerfile FROM lines must use digest-pinned references (e.g. golang:1.26.6-alpine@sha256:...). Run 'make pin-base-images' to update."
  exit 1
fi

test -f .docker-digests || {
  echo "::error file=.docker-digests,title=Missing provenance record::.docker-digests is missing — run 'make pin-base-images' and commit it."
  exit 1
}

from=$(sed -nE 's/^FROM ([^ @]+)@(sha256:[0-9a-f]+).*$/\1 \2/p' Dockerfile | sort)
recorded=$(grep -v '^[[:space:]]*$' .docker-digests | sort)
if [ "${from}" != "${recorded}" ]; then
  echo "Dockerfile:";      echo "${from}"
  echo ".docker-digests:"; echo "${recorded}"
  echo "::error file=.docker-digests,title=Digest mismatch::Dockerfile FROM digests and .docker-digests differ — run 'make pin-base-images' and commit both."
  exit 1
fi
echo "Dockerfile base images are digest-pinned and match .docker-digests."
