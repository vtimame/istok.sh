GOEXE := $(shell go env GOEXE)

COMMAND := istok$(GOEXE)
BINARY ?= $(COMMAND)
BUILD_OUTPUT ?= bin/$(BINARY)
INSTALL_DIR ?= $(HOME)/.local/bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || printf unknown)
RELEASE_BASE_URL ?= https://istok.s26.dev/releases/
UPDATE_CERTIFICATE_B64 ?=

REPORT_DIR ?= tmp/index-acceptance
ISTOK_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
ISTOK_DIRTY ?= $(shell if test -n "$$(git status --porcelain 2>/dev/null)"; then printf true; else printf false; fi)
ISTOK_HARDENING_FILES ?= 2500

HARDENING_COMPOSE := docker compose -f compose.hardening.yml --profile hardening

LDFLAGS := -s -w \
	-X github.com/vtimame/istok.sh/internal/buildinfo.Version=$(VERSION) \
	-X github.com/vtimame/istok.sh/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/vtimame/istok.sh/internal/buildinfo.BuildDate=$(BUILD_DATE) \
	-X github.com/vtimame/istok.sh/internal/buildinfo.ReleaseBaseURL=$(RELEASE_BASE_URL) \
	-X github.com/vtimame/istok.sh/internal/buildinfo.CertificateBase64=$(UPDATE_CERTIFICATE_B64)

.PHONY: build install update-dev update-dev-down test-update test-index-heavy test-index-benchmark test-index-release mcp-inspect release-secret

build:
	@mkdir -p "$(dir $(BUILD_OUTPUT))"
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BUILD_OUTPUT)" ./cmd/istok
	@echo "Built $(BUILD_OUTPUT)"

install:
	@mkdir -p "$(INSTALL_DIR)"
	CGO_ENABLED=1 go build \
		-trimpath \
		-ldflags "$(LDFLAGS)" \
		-o "$(INSTALL_DIR)/$(COMMAND)" \
		./cmd/istok
	@echo "Installed $(INSTALL_DIR)/$(COMMAND)"

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

test-index-heavy:
	@mkdir -p "$(REPORT_DIR)"
	@set -eu; \
		HOST_UID="$$(id -u)"; \
		HOST_GID="$$(id -g)"; \
		export HOST_UID HOST_GID; \
		export REPORT_DIR="$(abspath $(REPORT_DIR))"; \
		export ISTOK_COMMIT="$(ISTOK_COMMIT)"; \
		export ISTOK_DIRTY="$(ISTOK_DIRTY)"; \
		export ISTOK_HARDENING_FILES="$(ISTOK_HARDENING_FILES)"; \
		$(HARDENING_COMPOSE) build index-hardening; \
		image_ref="$$( $(HARDENING_COMPOSE) config --images | sed -n '1p' )"; \
		image_id="$$(docker image inspect "$$image_ref" --format '{{.Id}}')"; \
		test -n "$$image_id" || { \
			printf '%s\n' 'unable to determine the index-hardening image ID' >&2; \
			exit 1; \
		}; \
		ISTOK_HARDENING_IMAGE="$$image_id" $(HARDENING_COMPOSE) run --rm index-hardening heavy

test-index-benchmark:
	@mkdir -p "$(REPORT_DIR)"
	@set -eu; \
		HOST_UID="$$(id -u)"; \
		HOST_GID="$$(id -g)"; \
		export HOST_UID HOST_GID; \
		export REPORT_DIR="$(abspath $(REPORT_DIR))"; \
		export ISTOK_COMMIT="$(ISTOK_COMMIT)"; \
		export ISTOK_DIRTY="$(ISTOK_DIRTY)"; \
		export ISTOK_HARDENING_FILES="$(ISTOK_HARDENING_FILES)"; \
		$(HARDENING_COMPOSE) build index-hardening; \
		image_ref="$$( $(HARDENING_COMPOSE) config --images | sed -n '1p' )"; \
		image_id="$$(docker image inspect "$$image_ref" --format '{{.Id}}')"; \
		test -n "$$image_id" || { \
			printf '%s\n' 'unable to determine the index-hardening image ID' >&2; \
			exit 1; \
		}; \
		ISTOK_HARDENING_IMAGE="$$image_id" $(HARDENING_COMPOSE) run --rm index-hardening benchmark

test-index-release:
	@if test -n "$$(git status --porcelain)"; then \
		printf '%s\n' 'refusing index release validation from a dirty worktree' >&2; \
		exit 1; \
	fi
	@$(MAKE) test-index-heavy \
		REPORT_DIR="$(REPORT_DIR)" \
		ISTOK_COMMIT="$(ISTOK_COMMIT)" \
		ISTOK_DIRTY=false \
		ISTOK_HARDENING_FILES="$(ISTOK_HARDENING_FILES)"

mcp-inspect: install
	pnpm dlx @modelcontextprotocol/inspector istok mcp

release-secret:
	@go run ./tools/release secret
