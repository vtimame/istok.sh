GOEXE := $(shell go env GOEXE)
GOBIN := $(shell go env GOBIN)
GOPATH := $(shell go env GOPATH)

COMMAND := istok$(GOEXE)
BINARY ?= $(COMMAND)
BUILD_OUTPUT ?= bin/$(BINARY)
INSTALL_DIR ?= $(if $(GOBIN),$(GOBIN),$(GOPATH)/bin)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || printf unknown)
RELEASE_BASE_URL ?= https://istok.s26.dev/releases/
UPDATE_CERTIFICATE_B64 ?=

LDFLAGS := -s -w \
	-X s26.dev/istok-cli/internal/buildinfo.Version=$(VERSION) \
	-X s26.dev/istok-cli/internal/buildinfo.Commit=$(COMMIT) \
	-X s26.dev/istok-cli/internal/buildinfo.BuildDate=$(BUILD_DATE) \
	-X s26.dev/istok-cli/internal/buildinfo.ReleaseBaseURL=$(RELEASE_BASE_URL) \
	-X s26.dev/istok-cli/internal/buildinfo.CertificateBase64=$(UPDATE_CERTIFICATE_B64)

.PHONY: build install update-dev update-dev-down test-update mcp-inspect

build:
	@mkdir -p "$(dir $(BUILD_OUTPUT))"
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BUILD_OUTPUT)" ./cmd/istok
	@echo "Built $(BUILD_OUTPUT)"

install: export GOBIN := $(INSTALL_DIR)
install:
	@mkdir -p "$(GOBIN)"
	CGO_ENABLED=1 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/istok
	@echo "Installed $(GOBIN)/$(COMMAND)"

update-dev:
	go run ./tools/updatefixture serve
	@for n in $$(seq 1 30); do curl -fsS http://127.0.0.1:8080/istok/cli/manifest.yaml >/dev/null 2>&1 && exit 0; sleep 1; done; curl -fsS http://127.0.0.1:8080/istok/cli/manifest.yaml >/dev/null

update-dev-down:
	docker compose down --remove-orphans

test-update:
	@set -e; data=$$(mktemp -d); trap 'status=$$?; rm -rf "$$data"; docker compose down --remove-orphans >/dev/null 2>&1 || true; exit $$status' EXIT; \
	$(MAKE) update-dev; \
	tmp/update-fixture/istok-old$(GOEXE) update --yes --database "$$data/istok.db"; \
	test "$$(tmp/update-fixture/istok-old$(GOEXE) version)" = "v0.0.2"; \
	go run ./tools/updatefixture verify "$$data/istok.db"

mcp-inspect: install
	pnpm dlx @modelcontextprotocol/inspector istok mcp
