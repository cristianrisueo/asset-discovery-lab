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

COMPOSE_ALL   := docker compose --profile core --profile lake --profile app
COMPOSE_CORE  := docker compose --profile core

GATE ?=
SERVICE ?=
N ?=
ID ?=
SITE ?=

.PHONY: help env host-prep generate build up up-core down clean ps logs test lint \
        acceptance acceptance-gate reconcile chaos-on chaos-off burst move

help: ## List the targets
	@echo "asset-discovery-lab — make targets"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env: ## Create .env from .env.example if it does not exist
	@test -f .env || { cp .env.example .env && echo "created .env from .env.example"; }

host-prep: ## Set vm.max_map_count in the Docker VM (needed by the Doris BE)
	docker run --rm --privileged --pid=host $(ALPINE_IMAGE) \
	  nsenter -t 1 -m -u -n -i sh -c 'sysctl -w vm.max_map_count=2000000'

generate: ## Generate Go and Java code from the contracts (buf generate)
	$(BUF) generate

build: ## Build every image
	@echo "build: not available until sprint A"; exit 1

up: ## Start the full stack (core, lake, app) and wait until it is ready
	@echo "up: not available until sprint A"; exit 1

up-core: ## Start the core profile only, without the lake
	@echo "up-core: not available until sprint A"; exit 1

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
	@if [ -f processor/pom.xml ]; then \
	  echo "processor tests: not available until sprint B"; exit 1; \
	else echo "processor: not present yet, skipped"; fi
	@if [ -f explorer/Cargo.toml ]; then \
	  echo "explorer tests: not available until sprint D"; exit 1; \
	else echo "explorer: not present yet, skipped"; fi
	@if [ -f web/package.json ]; then \
	  echo "web tests: not available until sprint D"; exit 1; \
	else echo "web: not present yet, skipped"; fi

lint: ## Linters: buf, golangci-lint, Spotless, rustfmt and clippy, ESLint and tsc
	$(BUF) lint
	@if git rev-parse --verify -q main >/dev/null && [ "$$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then \
	  echo "buf breaking --against '.git#branch=main'"; $(BUF) breaking --against '.git#branch=main'; \
	else echo "buf breaking: skipped (on main, or no main branch)"; fi
	$(GOLANGCI_LINT) run ./...
	@if [ -f processor/pom.xml ]; then \
	  echo "processor lint: not available until sprint B"; exit 1; \
	else echo "processor: not present yet, skipped"; fi
	@if [ -f explorer/Cargo.toml ]; then \
	  echo "explorer lint: not available until sprint D"; exit 1; \
	else echo "explorer: not present yet, skipped"; fi
	@if [ -f web/package.json ]; then \
	  echo "web lint: not available until sprint D"; exit 1; \
	else echo "web: not present yet, skipped"; fi

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
