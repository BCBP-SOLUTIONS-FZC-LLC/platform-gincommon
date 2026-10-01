#!/usr/bin/env bash
# Smoke tests for the CI-built demo-server image (keeps shell logic out of
# inline YAML). Proves the image runs the middleware stack end to end:
#   1. image size is within budget;
#   2. a misconfigured observability identity fails fast (non-zero exit),
#      exactly as ObservabilityMiddlewares documents;
#   3. a correctly configured container serves /health and exposes the
#      Enterprise Platform Observability Standard metrics on /metrics.
set -euo pipefail

IMAGE="${IMAGE:-platform-gincommon-ci-test}"
NAME="gincommon-smoke-$$"
PORT="${SMOKE_PORT:-18080}"
MAX_MB=60

cleanup() { docker rm -f "${NAME}" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "::group::Image size check (linux/amd64)"
size=$(docker image inspect "${IMAGE}" --format='{{.Size}}')
mb=$((size / 1024 / 1024))
echo "Image size: ${mb} MB (limit: ${MAX_MB} MB)"
if [ "${mb}" -gt "${MAX_MB}" ]; then
  echo "::error file=Dockerfile,title=Image size::Image is ${mb} MB, exceeds ${MAX_MB} MB — check COPY/ADD for accidental inclusions"
  exit 1
fi
echo "::endgroup::"

echo "::group::Startup gate — invalid identity must fail fast"
command -v timeout >/dev/null || { echo "::error::timeout(1) is required (GNU coreutils)"; exit 1; }
exit_code=0
gate_log=$(timeout 20s docker run --rm -e OBSERVABILITY_DOMAIN=not-a-domain "${IMAGE}" 2>&1) || exit_code=$?
echo "Container exit code: ${exit_code} (expected non-zero, not 124/timeout)"
# Assert the reason too, so an unrelated crash cannot pass the gate.
if [ "${exit_code}" -eq 0 ] || [ "${exit_code}" -eq 124 ] ||
  ! grep -q 'is not in the registry vocabulary' <<< "${gate_log}"; then
  echo "${gate_log}" | tail -5
  echo "::error file=pkg/gincommon/gincommon.go,title=Startup gate::An unknown OBSERVABILITY_DOMAIN must stop the server at startup with the registry error"
  exit 1
fi
echo "::endgroup::"

echo "::group::Serve — /health and platform metrics"
docker run -d --name "${NAME}" -p "127.0.0.1:${PORT}:8080" \
  -e OBSERVABILITY_DOMAIN=iam -e APP_ENV=prod -e BUILD_VERSION=smoke \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=127.0.0.1:1 \
  "${IMAGE}" >/dev/null
for _ in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1 && break
  sleep 1
done
if ! curl -fsS "http://127.0.0.1:${PORT}/health" | grep -q '"ok"'; then
  docker logs "${NAME}" || true
  echo "::error file=cmd/server/main.go,title=Health::/health did not return ok"
  exit 1
fi
metrics=$(curl -fsS "http://127.0.0.1:${PORT}/metrics")
for want in \
  'platform_build_info{domain="iam",environment="prod",service="platform-gincommon",version="smoke"} 1' \
  'platform_http_requests_total{domain="iam",environment="prod",error_class="success",method="GET",route="/health",service="platform-gincommon",status_class="2xx"}'; do
  if ! grep -qF "${want}" <<< "${metrics}"; then
    echo "::error title=Metrics::/metrics is missing: ${want}"
    exit 1
  fi
done
echo "::endgroup::"

{
  echo "### Smoke test results"
  echo "- ✅ Startup gate: invalid OBSERVABILITY_DOMAIN exits ${exit_code}"
  echo "- ✅ /health ok; /metrics exposes platform_build_info and platform_http_requests_total with domain/service/environment"
  echo "- 📦 Image size (linux/amd64): **${mb} MB** (limit: ${MAX_MB} MB)"
  echo "- 🔍 [Security (Trivy SARIF results)](https://github.com/${GITHUB_REPOSITORY:-}/security/code-scanning)"
} >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
