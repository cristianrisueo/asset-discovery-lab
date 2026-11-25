# asset-discovery-lab — every target of spec §15.9.
# Host prerequisites are only Go, GNU make, git and Docker (spec §15.1): the
# Go-based tools run through `go run` at pinned versions, and Java, Rust and
# Node run in builder containers.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BUF           ?= go run github.com/bufbuild/buf/cmd/buf@v1.75.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
ADLCTL        := go run ./adlctl
ALPINE_IMAGE  := alpine:3.24.4
# Maven builder for the processor (spec §15.9): the same image as processor/Dockerfile.
# The cache volume is outside the compose project, so make clean keeps it.
MAVEN_IMAGE   := maven:3.9.16-eclipse-temurin-17
MAVEN         := docker run --rm -v "$(CURDIR)/processor:/workspace" -v adl-maven-repo:/root/.m2 \
                 -w /workspace --memory 1536m -e MAVEN_OPTS=-Xmx512m $(MAVEN_IMAGE) mvn -B
# Cargo builder for the explorer (spec §15.9): the toolchain stage of
# explorer/Dockerfile, with rustfmt and clippy. The registry and target caches
# are volumes outside the compose project, so make clean keeps them and the gate
# run (with the stack up) is incremental.
RUST_TOOLCHAIN_IMAGE := adl/explorer-toolchain:0.1.0
CARGO         := docker run --rm -v "$(CURDIR)/explorer:/workspace" -v adl-cargo-registry:/usr/local/cargo/registry \
                 -v adl-cargo-target:/workspace/target -w /workspace --memory 3g -e CARGO_BUILD_JOBS=4 \
                 $(RUST_TOOLCHAIN_IMAGE) cargo
# npm builder for the web (spec §15.9): node_modules lives in a volume, so the
# host's web/ stays clean; the npm cache is a volume too.
NODE_IMAGE    := node:24.22.3-bookworm-slim
NPM           := docker run --rm -v "$(CURDIR)/web:/workspace" -v adl-web-node-modules:/workspace/node_modules \
                 -v adl-npm-cache:/root/.npm -w /workspace --memory 1536m $(NODE_IMAGE)

COMPOSE_ALL   := docker compose --profile core --profile lake --profile app
COMPOSE_CORE  := docker compose --profile core

GATE ?=
SERVICE ?=
N ?=
ID ?=
SITE ?=

.PHONY: help env host-prep generate build up up-core down clean ps logs test lint \
        acceptance acceptance-gate reconcile chaos-on chaos-off burst move rust-toolchain

help: ## List the targets
	@echo "asset-discovery-lab — make targets"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env: ## Create .env from .env.example if it does not exist
	@test -f .env || { cp .env.example .env && echo "created .env from .env.example"; }

host-prep: ## Set vm.max_map_count in the Docker VM (needed by the Doris BE)
	ALPINE_IMAGE=$(ALPINE_IMAGE) ./infra/scripts/host-prep.sh

generate: ## Generate Go and Java code from the contracts (buf generate)
	$(BUF) generate

build: env ## Build every image
	$(COMPOSE_ALL) build

up: env host-prep ## Start the full stack (core, lake, app) and wait until it is ready
	$(COMPOSE_ALL) up -d
	$(ADLCTL) wait --timeout 15m

up-core: env host-prep ## Start the core profile only, without the lake
	ICEBERG_ENABLED=false $(COMPOSE_CORE) up -d
	$(ADLCTL) wait --core --timeout 15m

down: ## Stop the stack with a Flink savepoint; keeps volumes
	@$(ADLCTL) flink stop-with-savepoint || echo "warning: stop-with-savepoint failed; the next start restores from the newest retained checkpoint"
	$(COMPOSE_ALL) stop

clean: ## Remove containers, networks and every volume
	$(COMPOSE_ALL) down --volumes --remove-orphans

ps: ## Show the status of the services
	$(COMPOSE_ALL) ps -a

logs: ## Show logs (SERVICE=name to filter)
	$(COMPOSE_ALL) logs --tail 200 $(SERVICE)

test: ## Unit tests: Go on the host; Maven, Cargo and npm in builder containers
	go test ./...
	$(MAVEN) test
	$(MAKE) --no-print-directory rust-toolchain
	$(CARGO) test --locked
	$(NPM) sh -c "npm ci --no-audit --no-fund && npm test"

lint: ## Linters: buf, golangci-lint, Spotless, rustfmt and clippy, ESLint and tsc
	$(BUF) lint
	@if git rev-parse --verify -q main >/dev/null && git cat-file -e main:buf.yaml 2>/dev/null; then \
	  echo "buf breaking --against '.git#branch=main'"; $(BUF) breaking --against '.git#branch=main'; \
	else echo "buf breaking: skipped (no main branch with contracts yet)"; fi
	$(GOLANGCI_LINT) run ./...
	$(MAVEN) spotless:check
	$(MAKE) --no-print-directory rust-toolchain
	$(CARGO) fmt --check
	$(CARGO) clippy --all-targets --locked -- -D warnings
	$(NPM) sh -c "npm ci --no-audit --no-fund && npm run lint && npm run typecheck"

# Builds (or reuses from the cache) the Cargo builder image. Not a §15.9 target,
# so it has no "##" help line.
rust-toolchain:
	@docker build -q --target toolchain -t $(RUST_TOOLCHAIN_IMAGE) explorer > /dev/null

acceptance: ## Run every acceptance gate, A to E, against the running stack
	go test -tags acceptance -count=1 -timeout 120m -v ./acceptance/...

acceptance-gate: ## Run one acceptance gate (GATE=A|B|C|D|E)
	@test -n "$(GATE)" || { echo "usage: make acceptance-gate GATE=A"; exit 2; }
	go test -tags acceptance -count=1 -timeout 120m -v -run '^TestGate$(GATE)_' ./acceptance/...

reconcile: ## Compare Doris and the lake with datacore
	go run ./adlctl reconcile --timeout 3m

chaos-on: ## Turn datacore chaos on (spec §7.9)
	$(ADLCTL) chaos on

chaos-off: ## Turn datacore chaos off
	$(ADLCTL) chaos off

burst: ## Apply N generator changes now (N=…)
	@test -n "$(N)" || { echo "usage: make burst N=1000"; exit 2; }
	$(ADLCTL) burst $(N)

move: ## Move an asset to another site (ID=… SITE=…)
	@test -n "$(ID)" -a -n "$(SITE)" || { echo "usage: make move ID=LT-0042 SITE=Madrid"; exit 2; }
	$(ADLCTL) move $(ID) "$(SITE)"
