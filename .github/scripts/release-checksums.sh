#!/usr/bin/env bash
# Publish job: re-verifies every per-binary .sha256 written by the binaries
# job (proves the artifact hand-off did not alter a binary), then writes the
# aggregate checksums.txt that release.yml signs with `cosign sign-blob` and
# create-github-release.sh attaches. Format matches
# `sha256sum --check checksums.txt`.
set -euo pipefail

shopt -s nullglob
sums=(metricslint_*.sha256)
if [ "${#sums[@]}" -eq 0 ]; then
  echo "::error title=Checksums::no metricslint_*.sha256 files — did the binary artifact download?"
  exit 1
fi

for f in "${sums[@]}"; do
  sha256sum --check --strict "$f"
done

BINARIES=()
for f in metricslint_*; do
  case "$f" in *.sha256) ;; *) BINARIES+=("$f") ;; esac
done
if [ "${#BINARIES[@]}" -ne "${#sums[@]}" ]; then
  echo "::error title=Checksums::${#BINARIES[@]} binaries but ${#sums[@]} .sha256 files"
  exit 1
fi

sha256sum "${BINARIES[@]}" > checksums.txt
echo "  ✔  checksums.txt covers ${#BINARIES[@]} binaries"
cat checksums.txt
