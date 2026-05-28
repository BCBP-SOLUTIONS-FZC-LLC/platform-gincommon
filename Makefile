# -----------------------------
# CONFIG
# -----------------------------
-include .env

APP_NAME ?= platform-gincommon
APP_ENV ?= dev
APP_PORT ?= 8080
GO ?= go
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

export APP_NAME APP_ENV APP_PORT GO SMOKE_BASE_URL
export OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT
export OTEL_COLLECTOR_GRPC_PORT OTEL_COLLECTOR_HTTP_PORT LOKI_HTTP_PORT PROMETHEUS_HTTP_PORT GRAFANA_HTTP_PORT
export PROMETHEUS_SCRAPE_TARGET TEST_SERVICE_NAME E2E_SERVICE_NAME

# Test packages (exclude smoke — needs optional live server).
# Include the grpc middleware internal package so coverage_test.go files that
# test unexported functions (splitFullMethod, metadataCarrier, clientIPFromContext)
# are executed and counted toward coverage.
INTERNAL_TEST_PKGS := github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware
TEST_PKGS := $(shell $(GO) list ./test/... | grep -v /test/smoke$$) $(INTERNAL_TEST_PKGS)

# Production packages measured for coverage when running tests under ./test/
COVER_PKGS := $(shell $(GO) list ./internal/... ./pkg/... ./cmd/...)
COVER_PKG_LIST := $(shell $(GO) list ./internal/... ./pkg/... ./cmd/... | paste -sd, -)

# -----------------------------
# SETUP
# -----------------------------

.PHONY: setup
setup:
	@test -f .env || cp .env-example .env
	@echo "Environment ready (.env)"

.PHONY: help
help:
	@echo "Available commands:"
	@echo " make tidy        - tidy go modules"
	@echo " make vet         - go vet all packages"
	@echo " make lint        - run golangci-lint"
	@echo " make test        - run tests locally (excludes smoke; no server required)"
	@echo " make test-ci     - run tests with race detector (used in CI and release)"
	@echo " make test-unit   - run unit tests"
	@echo " make test-int    - run integration tests"
	@echo " make test-e2e    - run e2e tests"
	@echo " make test-smoke  - smoke tests (needs app; set SMOKE_BASE_URL in .env)"
	@echo " make setup       - copy .env-example to .env if missing"
	@echo " make build       - build binary"
	@echo " make run         - run example server"
	@echo " make docker-build - build Docker image locally (injects BUILD_VERSION from git)"
	@echo " make docker-up   - start local stack"
	@echo " make docker-down - stop local stack"
	@echo " make ci          - full CI pipeline (tidy + vet + lint + test-ci + build)"
	@echo " make cover       - coverage profile + open HTML report in browser"
	@echo " make cover-func  - coverage summary by function (terminal output)"

# -----------------------------
# GO BASICS
# -----------------------------

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check:
	@files=$$(gofmt -l .); if [ -n "$$files" ]; then echo "gofmt violations:"; echo "$$files"; exit 1; fi

.PHONY: vet
vet:
	$(GO) vet ./...

# -----------------------------
# LINT
# -----------------------------

.PHONY: lint
lint:
	@echo "Running linter..."
	$(GO) tool golangci-lint run

# -----------------------------
# TESTS
# -----------------------------

.PHONY: test
test:
	$(GO) test $(TEST_PKGS) -count=1 -timeout 120s -v

# test-ci: used by CI and the release workflow; enables the race detector.
.PHONY: test-ci
test-ci:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s

.PHONY: test-unit
test-unit:
	$(GO) test ./test/unit/... -count=1 -timeout 60s -v

.PHONY: test-int
test-int:
	$(GO) test ./test/integration -count=1 -timeout 60s -v

.PHONY: test-e2e
test-e2e:
	$(GO) test ./test/e2e -count=1 -timeout 60s -v

.PHONY: test-smoke
test-smoke:
	$(GO) test ./test/smoke -count=1 -timeout 60s -v

# -----------------------------
# BUILD
# -----------------------------

.PHONY: build
build:
	@echo "Building binary..."
	@mkdir -p bin
	$(GO) build -o bin/$(APP_NAME) ./cmd/server

.PHONY: docker-build
docker-build:
	@echo "Building Docker image (version: $(BUILD_VERSION))..."
	docker build -t $(APP_NAME):local --build-arg BUILD_VERSION=$(BUILD_VERSION) .

# -----------------------------
# RUN (example usage)
# -----------------------------

.PHONY: run
run:
	@echo "Running example server..."
	$(GO) run ./cmd/server/main.go

# -----------------------------
# DOCKER (LOCAL STACK)
# -----------------------------

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
# CLEAN
# -----------------------------

.PHONY: clean
clean:
	rm -rf bin
	rm -rf *.log

# -----------------------------
# CI HELPERS
# -----------------------------

.PHONY: vulncheck
vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./internal/... ./pkg/...

.PHONY: cover-check
cover-check:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s -coverpkg=$(COVER_PKG_LIST) -coverprofile=coverage.out
	@pct=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	 awk -v p="$$pct" 'BEGIN { if (p+0 < 95) { print "FAIL: " p "% is below 95%"; exit 1 } else { print "PASS: " p "%" } }'

.PHONY: ci
ci: fmt-check tidy vet lint test-ci cover-check build

# -----------------------------
# COVERAGE
# -----------------------------

.PHONY: cover
cover:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s -coverpkg=$(COVER_PKG_LIST) -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out

.PHONY: cover-func
cover-func:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s -coverpkg=$(COVER_PKG_LIST) -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out

# -----------------------------
# DEBUG HELPERS
# -----------------------------

.PHONY: race
race:
	$(GO) test $(TEST_PKGS) -race -count=1 -timeout 120s