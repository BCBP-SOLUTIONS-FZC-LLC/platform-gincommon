# -----------------------------
# CONFIG
# -----------------------------
-include .env

APP_NAME      ?= platform-gincommon
APP_ENV       ?= dev
APP_PORT      ?= 8080
GO            ?= go
BUILD_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
SMOKE_BASE_URL ?= http://localhost:8080
OTEL_SERVICE_NAME ?= platform-gincommon
OTEL_EXPORTER_OTLP_ENDPOINT ?= localhost:4317
OTEL_COLLECTOR_GRPC_PORT ?= 4317
OTEL_COLLECTOR_HTTP_PORT ?= 4318
LOKI_HTTP_PORT ?= 3100
PROMETHEUS_HTTP_PORT ?= 9090
GRAFANA_HTTP_PORT ?= 3000
PROMETHEUS_SCRAPE_TARGET ?= host.docker.internal:8080
TEST_SERVICE_NAME ?= test-service
E2E_SERVICE_NAME ?= e2e-test

# Private modules — resolved via SSH (git@github.com:) using the global URL
# rewrite, same as the IAM services. platform-gincommon itself has no private
# dependencies today; the exports keep `go get` of sibling platform modules
# (e.g. in a go.work) working without extra setup.
export GOPRIVATE  ?= github.com/BCBP-SOLUTIONS-FZC-LLC/*
export GONOSUMDB  ?= github.com/BCBP-SOLUTIONS-FZC-LLC/*

export APP_NAME APP_ENV APP_PORT GO SMOKE_BASE_URL BUILD_VERSION
export OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT
export OTEL_COLLECTOR_GRPC_PORT OTEL_COLLECTOR_HTTP_PORT LOKI_HTTP_PORT PROMETHEUS_HTTP_PORT GRAFANA_HTTP_PORT
export LOG_FILE
export PROMETHEUS_SCRAPE_TARGET TEST_SERVICE_NAME E2E_SERVICE_NAME

# Test packages (exclude smoke — needs optional live server).
# Include the grpc middleware internal package so coverage_test.go files that
# test unexported functions (splitFullMethod, metadataCarrier, clientIPFromContext)
# are executed and counted toward coverage.
INTERNAL_TEST_PKGS := github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware
TEST_PKGS := $(shell $(GO) list ./test/... | grep -v /test/smoke$$) $(INTERNAL_TEST_PKGS)

# Production packages measured for coverage when running tests under ./test/
# (tests live in a separate package tree, so -coverpkg is required).
COVER_PKGS := $(shell $(GO) list ./internal/... ./pkg/... ./cmd/...)
COVER_PKG_LIST := $(shell $(GO) list ./internal/... ./pkg/... ./cmd/... | paste -sd, -)
# Mirrors CI's validate-test gate (.github/scripts/coverage-gate.sh). Do not lower.
COVERAGE_THRESHOLD ?= 98

# CI-built demo image the smoke script runs (see test-smoke-image).
SMOKE_IMAGE ?= platform-gincommon-ci-test

# -----------------------------
# SETUP
# -----------------------------

.PHONY: setup
setup:
	@test -f .env || cp .env-example .env
	@$(MAKE) --no-print-directory install-hooks
	@echo "Environment ready (.env)"

# install-hooks: copy .githooks/pre-commit into the repository's hooks
# directory (git rev-parse resolves it for worktrees too).
.PHONY: install-hooks
install-hooks:
	@hooks=$$(git rev-parse --git-path hooks 2>/dev/null) || { echo "Not a git checkout; skipping hooks"; exit 0; }; \
	 mkdir -p "$$hooks" && cp .githooks/pre-commit "$$hooks/pre-commit" && chmod +x "$$hooks/pre-commit" && \
	 echo "Installed git hooks ($$hooks/pre-commit)"

# godoc: serve package documentation locally using pkgsite.
# Opens http://localhost:8080 — browse to the module path in the UI.
.PHONY: godoc
godoc:
	@echo "Starting pkgsite at http://localhost:8080 — press Ctrl-C to stop"
	$(GO) run golang.org/x/pkgsite/cmd/pkgsite@latest -open .

# pin-base-images: pin every Dockerfile FROM line to the current digest of its
# tag, and write the digests to .docker-digests (a checked-in provenance
# record). CI rejects unpinned FROM lines and a Dockerfile/.docker-digests
# mismatch (.github/scripts/check-docker-digests.sh). Requires docker.
.PHONY: pin-base-images
pin-base-images:
	@echo "Fetching SHA digests for Dockerfile base images..."
	@: > .docker-digests; \
	 for ref in $$(sed -nE 's/^FROM ([^ @]+)(@sha256:[0-9a-f]+)?( AS .*)?$$/\1/p' Dockerfile); do \
		digest=$$(docker buildx imagetools inspect "$$ref" --format '{{json .Manifest.Digest}}' | tr -d '"'); \
		echo "  $$ref -> $$digest"; \
		sed -i.bak -E "s#^FROM $$ref(@sha256:[0-9a-f]+)?( AS |$$)#FROM $$ref@$$digest\2#" Dockerfile && rm -f Dockerfile.bak; \
		echo "$$ref $$digest" >> .docker-digests; \
	 done
	@echo "Digests written to .docker-digests — commit both Dockerfile and .docker-digests"

.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make setup             - copy .env-example to .env if missing, install git hooks"
	@echo "  make install-hooks     - install .githooks/pre-commit into .git/hooks"
	@echo "  make tidy              - go mod tidy"
	@echo "  make fmt               - gofmt -w (rewrite files)"
	@echo "  make fmt-check         - verify gofmt formatting (mirrors CI)"
	@echo "  make vet               - go vet ./..."
	@echo "  make lint              - run golangci-lint"
	@echo "  make mod-verify        - go mod verify"
	@echo "  make vuln-check        - govulncheck on internal + pkg (alias: vulncheck)"
	@echo "  make test              - unit + integration + e2e tests (excludes smoke)"
	@echo "  make test-ci           - all tests with the race detector"
	@echo "  make test-cover        - race + coverage run writing coverage.out (CI test gate input)"
	@echo "  make test-unit         - unit tests only"
	@echo "  make test-int          - integration tests only"
	@echo "  make test-e2e          - e2e tests only"
	@echo "  make test-smoke        - smoke tests against a live server (SMOKE_BASE_URL)"
	@echo "  make test-smoke-image  - CI image gate: size, fail-fast identity, /health, /metrics (builds $(SMOKE_IMAGE))"
	@echo "  make race              - alias for test-ci"
	@echo "  make run               - run the demo/reference server (go run)"
	@echo "  make build             - build bin/platform-gincommon + bin/metricslint; verify library packages compile"
	@echo "  make docker-build      - build the demo Docker image locally (injects BUILD_VERSION from git)"
	@echo "  make cover             - coverage HTML report"
	@echo "  make cover-func        - coverage summary by function"
	@echo "  make cover-check       - coverage run + ≥$(COVERAGE_THRESHOLD)% gate"
	@echo "  make ci                - fmt-check + tidy + vet + lint + metrics-lint + cover-check + build"
	@echo "  make docker-up         - start local observability stack (Prometheus, Grafana, Tempo, Loki)"
	@echo "  make docker-down       - stop local observability stack"
	@echo "  make godoc             - serve local godoc/pkgsite at http://localhost:8080"
	@echo "  make pin-base-images   - fetch + pin SHA digests for Dockerfile base images"
	@echo "  make clean             - remove build and coverage artefacts"
	@echo ""
	@echo "Observability standard (Platform Observability Registry):"
	@echo "  make metrics-lint      - registry self-check, rule refs, promtool, inventory drift (CI gate)"
	@echo "  make metrics-registry  - validate internal/obsregistry/registry.json"
	@echo "  make metrics-refs      - reference rules must use registered platform_* names only"
	@echo "  make metrics-rules     - promtool check + unit tests of docker/prometheus/rules"
	@echo "  make metrics-inventory - regenerate docs/observability/metric-registry.md"

# -----------------------------
# GO BASICS
# -----------------------------

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: fmt
fmt:
	@gofmt -l -w .

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "FAIL: unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "gofmt: all files formatted"

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: mod-verify
mod-verify:
	$(GO) mod verify

# Library packages only — cmd/server is a demo binary and its stdlib call
# graph (net/tls, html/template) produces noise, not library risk.
.PHONY: vuln-check
vuln-check:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./internal/... ./pkg/...

# vulncheck: historical name, kept for scripts and docs that use it.
.PHONY: vulncheck
vulncheck: vuln-check

# -----------------------------
# LINT
# -----------------------------

.PHONY: lint
lint:
	@echo "Running linter..."
	$(GO) tool golangci-lint run ./...

# -----------------------------
# TESTS
# -----------------------------

.PHONY: test
test:
	$(GO) test $(TEST_PKGS) -count=1 -timeout 120s -v

# test-ci: all tests with the race detector (no coverage).
.PHONY: test-ci
test-ci:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s

# test-cover: one race-detector run of unit + integration + e2e that also writes
# coverage.out (CI's validate-test job gates it with .github/scripts/coverage-gate.sh).
.PHONY: test-cover
test-cover:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 180s -coverpkg=$(COVER_PKG_LIST) -coverprofile=coverage.out

.PHONY: test-unit
test-unit:
	$(GO) test ./test/unit/... -count=1 -timeout 60s -v

.PHONY: test-int
test-int:
	$(GO) test ./test/integration -count=1 -timeout 60s -v

.PHONY: test-e2e
test-e2e:
	$(GO) test ./test/e2e -count=1 -timeout 60s -v

# test-smoke: Go smoke tests against a live server at SMOKE_BASE_URL.
.PHONY: test-smoke
test-smoke:
	$(GO) test ./test/smoke -count=1 -timeout 60s -v

# test-smoke-image: CI's smoke job locally — build the linux/amd64 demo image
# and run .github/scripts/smoke-tests.sh against it (needs docker and GNU
# timeout(1); on macOS: brew install coreutils).
.PHONY: test-smoke-image
test-smoke-image:
	docker buildx build --platform linux/amd64 --load -t $(SMOKE_IMAGE) --build-arg BUILD_VERSION=$(BUILD_VERSION) .
	IMAGE=$(SMOKE_IMAGE) bash .github/scripts/smoke-tests.sh

.PHONY: race
race: test-ci

# -----------------------------
# RUN (demo / reference server)
# -----------------------------

.PHONY: run
run:
	@echo "Running example server..."
	$(GO) run ./cmd/server/main.go

# -----------------------------
# BUILD
# -----------------------------

.PHONY: build
build:
	@echo "Building binaries..."
	@mkdir -p bin
	$(GO) build -o bin/$(APP_NAME) ./cmd/server
	$(GO) build -o bin/metricslint ./cmd/metricslint
	@echo "Verifying library packages compile..."
	$(GO) build ./internal/... ./pkg/...

# -----------------------------
# DOCKER
# -----------------------------

.PHONY: docker-build
docker-build:
	@echo "Building Docker image (version: $(BUILD_VERSION))..."
	docker build -t $(APP_NAME):local --build-arg BUILD_VERSION=$(BUILD_VERSION) .

.PHONY: prometheus-config
prometheus-config:
	@sed 's|__PROMETHEUS_SCRAPE_TARGET__|$(PROMETHEUS_SCRAPE_TARGET)|g' docker/prometheus/prometheus.yml.tpl > docker/prometheus/prometheus.yml

.PHONY: docker-up
docker-up: prometheus-config
	@echo "Starting observability stack..."
	docker compose --env-file .env up -d

.PHONY: docker-down
docker-down:
	@echo "Stopping observability stack..."
	docker compose --env-file .env down

# -----------------------------
# CI
# -----------------------------

# cover-check runs the race-detector suite once (with coverage), so test-ci is
# not repeated here.
.PHONY: ci
ci: fmt-check tidy vet lint metrics-lint cover-check build

# -----------------------------
# COVERAGE
# -----------------------------

.PHONY: cover
cover: test-cover
	$(GO) tool cover -html=coverage.out

.PHONY: cover-func
cover-func: test-cover
	$(GO) tool cover -func=coverage.out

.PHONY: cover-check
cover-check: test-cover
	@pct=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	 awk -v p="$$pct" -v t="$(COVERAGE_THRESHOLD)" 'BEGIN { if (p+0 < t) { print "FAIL: " p "% is below " t "%"; exit 1 } else { print "PASS: " p "%" } }'

# -----------------------------
# OBSERVABILITY STANDARD
# -----------------------------

# Reference rules that must reference platform_* metrics only.
METRICS_REF_PATHS ?= docker/prometheus/rules
PROMTOOL ?= $(shell command -v promtool 2>/dev/null)
PROMETHEUS_IMAGE ?= prom/prometheus:v3.5.0

.PHONY: metrics-registry
metrics-registry:
	$(GO) run ./cmd/metricslint registry

.PHONY: metrics-refs
metrics-refs:
	$(GO) run ./cmd/metricslint refs -deny-deprecated $(METRICS_REF_PATHS)

.PHONY: metrics-inventory
metrics-inventory:
	$(GO) run ./cmd/metricslint inventory -o docs/observability/metric-registry.md

.PHONY: metrics-rules
metrics-rules:
ifneq ($(PROMTOOL),)
	cd docker/prometheus/rules && $(PROMTOOL) check rules platform-*.yml && $(PROMTOOL) test rules promtool-tests.yml
else
	docker run --rm --entrypoint /bin/sh -v $(CURDIR)/docker/prometheus/rules:/rules:ro -w /rules $(PROMETHEUS_IMAGE) \
		-c 'promtool check rules platform-*.yml && promtool test rules promtool-tests.yml'
endif

# metrics-lint: registry self-check, inventory drift, rule references and promtool checks.
.PHONY: metrics-lint
metrics-lint: metrics-registry metrics-refs metrics-rules
	@$(GO) run ./cmd/metricslint inventory | diff -u docs/observability/metric-registry.md - \
		|| (echo "docs/observability/metric-registry.md is stale: run make metrics-inventory"; exit 1)

# -----------------------------
# CLEAN
# -----------------------------

.PHONY: clean
clean:
	rm -rf bin
	rm -f coverage.out coverage.html
	rm -rf *.log
	rm -f logs/*.log
