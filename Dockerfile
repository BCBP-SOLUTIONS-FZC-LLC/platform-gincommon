# syntax=docker/dockerfile:1

FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

WORKDIR /src

# --upgrade pulls patched libssl3/libcrypto3 from the 3.21 repo even when the
# pinned base digest predates the fix (Trivy gate: CVE-2026-75804, CVE-2026-84782,
# fixed in 3.3.7-r2). Re-pinning the base image supersedes this.
RUN apk add --no-cache --upgrade ca-certificates libssl3 libcrypto3 git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG APP_NAME=platform-gincommon
RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/${APP_NAME} ./cmd/server

# TODO: alpine 3.21 reaches end of support around Nov 2026 — move to a current
# alpine release and re-pin with `make pin-base-images` (Dependabot version
# updates are disabled, so this is manual).
FROM alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507

# --upgrade pulls patched libssl3/libcrypto3 from the 3.21 repo even when the
# pinned base digest predates the fix (Trivy gate: CVE-2026-75804, CVE-2026-84782,
# fixed in 3.3.7-r2). Re-pinning the base image supersedes this.
RUN apk add --no-cache --upgrade ca-certificates libssl3 libcrypto3

WORKDIR /app

ARG APP_NAME=platform-gincommon
ARG BUILD_VERSION=unknown
COPY --from=builder /out/${APP_NAME} ./server

ENV APP_NAME=${APP_NAME} \
    APP_ENV=prod \
    APP_PORT=8080 \
    BUILD_VERSION=${BUILD_VERSION}

EXPOSE 8080

# Verify the HTTP server is responding before declaring the container healthy.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://localhost:${APP_PORT}/health" || exit 1

USER nobody

ENTRYPOINT ["./server"]
