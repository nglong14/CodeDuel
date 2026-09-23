COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_JUDGE_FILE := deploy/docker-compose.judge.yml
BINARY := bin/codeduel
GO := go
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := $(shell $(GO) env GOPATH)/bin/golangci-lint

USER_ID ?= 11111111-1111-1111-1111-111111111111

DEV_SEED_FILE := deploy/dev/seed_users.sql

.PHONY: help up up-infra up-judge down down-judge logs logs-judge deps build lint compose-check k8s-check test-integration test-integration-run sandbox-images test-docker-integration run-gateway run-match run-judge run-reaper run-cli migrate migrate-down seed-dev reset infra-venv infra-deps infra-test

help:
	@echo "CodeDuel targets:"
	@echo "  make up            Start the core stack (Postgres, Redis, migrate, gateway, match, reaper)"
	@echo "  make up-infra      Start only Postgres + Redis"
	@echo "  make up-judge      Start the standalone Judge (requires 'make sandbox-images')"
	@echo "  make down          Stop the core stack"
	@echo "  make down-judge    Stop the Judge"
	@echo "  make logs          Tail core compose logs"
	@echo "  make logs-judge    Tail Judge logs"
	@echo "  make deps          Download Go module dependencies"
	@echo "  make build         Build codeduel binary"
	@echo "  make lint          Run golangci-lint"
	@echo "  make compose-check Validate both Compose files"
	@echo "  make k8s-check     Render both Kustomize overlays (deploy/k8s/overlays/{dev,prod})"
	@echo "  make test-integration  Start infra and run Redis/PostgreSQL integration tests"
	@echo "  make test-integration-run  Run integration tests only (services must already exist)"
	@echo "  make sandbox-images  Build pinned Judge sandbox images"
	@echo "  make test-docker-integration  Run opt-in Judge sandbox tests"
	@echo "  make run-gateway   Run gateway role"
	@echo "  make run-match     Run match role"
	@echo "  make run-judge     Run judge role"
	@echo "  make run-reaper    Run reaper role"
	@echo "  make run-cli       Run duelcli (USER_ID=$(USER_ID))"
	@echo "  make migrate       Apply database migrations"
	@echo "  make migrate-down  Roll back database migrations"
	@echo "  make seed-dev      Load development-only fixture users (Alice/Bob)"
	@echo "  make reset         Destructively reset development data (removes volumes)"
	@echo "  make infra-deps    Install Pulumi Python dependencies in infra/.venv"
	@echo "  make infra-test    Run Pulumi mock unit tests in infra/tests"

up:
	docker compose -f $(COMPOSE_FILE) up -d --wait

up-infra:
	docker compose -f $(COMPOSE_FILE) up -d --wait postgres redis

up-judge:
	docker compose -f $(COMPOSE_JUDGE_FILE) up -d

down:
	docker compose -f $(COMPOSE_FILE) down

down-judge:
	docker compose -f $(COMPOSE_JUDGE_FILE) down

logs:
	docker compose -f $(COMPOSE_FILE) logs -f

logs-judge:
	docker compose -f $(COMPOSE_JUDGE_FILE) logs -f

deps:
	$(GO) mod download

build:
	mkdir -p bin
	$(GO) build -o $(BINARY) ./cmd/codeduel

lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ] || ! "$(GOLANGCI_LINT)" version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"; then \
		$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi
	$(GOLANGCI_LINT) run ./...

compose-check:
	docker compose -f $(COMPOSE_FILE) config --quiet
	docker compose -f $(COMPOSE_JUDGE_FILE) config --quiet

k8s-check:
	@which kubectl >/dev/null || { echo "kubectl not found"; exit 1; }
	@kubectl kustomize deploy/k8s/overlays/dev > /tmp/dev.rendered.yaml
	@kubectl kustomize deploy/k8s/overlays/prod > /tmp/prod.rendered.yaml
	@if command -v kubeconform >/dev/null 2>&1; then \
		kubeconform -strict -summary -ignore-missing-schemas /tmp/dev.rendered.yaml /tmp/prod.rendered.yaml; \
	elif [ -x "$$(go env GOPATH)/bin/kubeconform" ]; then \
		"$$(go env GOPATH)/bin/kubeconform" -strict -summary -ignore-missing-schemas /tmp/dev.rendered.yaml /tmp/prod.rendered.yaml; \
	else \
		echo "kubeconform not found, rendered YAML syntax check only"; \
	fi
	@rm -f /tmp/dev.rendered.yaml /tmp/prod.rendered.yaml

test-integration: up-infra
	CODEDUEL_INTEGRATION=1 $(GO) test -race -count=1 ./internal/infrastructure/... ./internal/auth/... ./internal/redisx/... ./internal/match/... ./internal/submission/... ./internal/judge/... ./internal/reaper/... ./internal/gateway/...

test-integration-run:
	CODEDUEL_INTEGRATION=1 $(GO) test -race -count=1 ./internal/infrastructure/... ./internal/auth/... ./internal/redisx/... ./internal/match/... ./internal/submission/... ./internal/judge/... ./internal/reaper/... ./internal/gateway/...

sandbox-images:
	docker build --pull -t codeduel/sandbox-python:3.13 deploy/sandbox/python
	docker build --pull -t codeduel/sandbox-cpp:gcc14 deploy/sandbox/cpp
	docker build --pull -t codeduel/sandbox-java:temurin21 deploy/sandbox/java

test-docker-integration: sandbox-images
	CODEDUEL_DOCKER_INTEGRATION=1 $(GO) test -race -count=1 ./internal/judge -run Sandbox

run-gateway:
	$(GO) run ./cmd/codeduel --role=gateway

run-match:
	$(GO) run ./cmd/codeduel --role=match

run-judge:
	$(GO) run ./cmd/codeduel --role=judge

run-reaper:
	$(GO) run ./cmd/codeduel --role=reaper

run-cli:
	$(GO) run ./tools/duelcli -user=$(USER_ID)

migrate:
	$(GO) run ./cmd/codeduel --role=migrate --direction=up

migrate-down:
	$(GO) run ./cmd/codeduel --role=migrate --direction=down

seed-dev:
	docker compose -f $(COMPOSE_FILE) exec -T postgres \
		psql -v ON_ERROR_STOP=1 -U codeduel -d codeduel < $(DEV_SEED_FILE)

reset:
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans
	docker compose -f $(COMPOSE_JUDGE_FILE) down -v --remove-orphans

INFRA_DIR := infra
INFRA_VENV := $(INFRA_DIR)/.venv
INFRA_PIP := $(INFRA_VENV)/bin/pip
INFRA_PYTEST := $(INFRA_VENV)/bin/pytest
INFRA_RUFF := $(INFRA_VENV)/bin/ruff

.PHONY: infra-venv infra-deps infra-test infra-lint infra-preview-shared infra-preview-dev infra-preview-prod infra-up-shared infra-up-dev infra-up-prod

infra-venv:
	@if [ ! -d "$(INFRA_VENV)" ]; then python3 -m venv $(INFRA_VENV); fi

infra-deps: infra-venv
	$(INFRA_PIP) install --upgrade pip
	$(INFRA_PIP) install -r $(INFRA_DIR)/requirements.txt

infra-test: infra-venv
	$(INFRA_PYTEST) -v $(INFRA_DIR)/tests

infra-lint: infra-venv
	$(INFRA_RUFF) check $(INFRA_DIR)

infra-preview-shared: infra-venv
	cd $(INFRA_DIR)/shared && pulumi preview --diff

infra-preview-dev: infra-venv
	cd $(INFRA_DIR)/env && pulumi preview --stack dev --diff

infra-preview-prod: infra-venv
	cd $(INFRA_DIR)/env && pulumi preview --stack prod --diff

infra-up-shared: infra-venv
	cd $(INFRA_DIR)/shared && pulumi up

infra-up-dev: infra-venv
	cd $(INFRA_DIR)/env && pulumi up --stack dev

infra-up-prod: infra-venv
	cd $(INFRA_DIR)/env && pulumi up --stack prod

