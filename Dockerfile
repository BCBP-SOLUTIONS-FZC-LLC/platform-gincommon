# syntax=docker/dockerfile:1

FROM golang:1.26.3-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG APP_NAME=platform-gincommon
RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/${APP_NAME} ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

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
