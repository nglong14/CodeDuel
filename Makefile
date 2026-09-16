COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_JUDGE_FILE := deploy/docker-compose.judge.yml
BINARY := bin/codeduel
GO := go
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := $(shell $(GO) env GOPATH)/bin/golangci-lint

USER_ID ?= 11111111-1111-1111-1111-111111111111

DEV_SEED_FILE := deploy/dev/seed_users.sql

K8S_DIR := deploy/k8s
K8S_RENDER_DIR := bin/k8s
K8S_OVERLAYS := staging/migration staging/runtime production/migration production/runtime
KUBECONFORM_VERSION := v0.8.0
KUBECONFORM := $(shell $(GO) env GOPATH)/bin/kubeconform
KUBERNETES_VERSION := 1.34.0
# CRD schemas pinned to a CRDs-catalog commit. Add the cert-manager entry back when the
# edge gains a TLS listener.
CRD_CATALOG_REF := ad3b08c5045129d7bb1eeffd8e61719b2c8dd1e2
CRD_SCHEMA := https://raw.githubusercontent.com/datreeio/CRDs-catalog/$(CRD_CATALOG_REF)/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json

.PHONY: help up up-infra up-judge down down-judge logs logs-judge deps build lint compose-check test-integration test-integration-run sandbox-images test-docker-integration run-gateway run-match run-judge run-reaper run-cli migrate migrate-down seed-dev reset k8s-render k8s-validate

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
	@echo "  make k8s-render    Render all Kubernetes overlays into $(K8S_RENDER_DIR)"
	@echo "  make k8s-validate  Render, then schema-validate with kubeconform (strict)"

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

k8s-render:
	rm -rf $(K8S_RENDER_DIR)
	mkdir -p $(K8S_RENDER_DIR)
	@set -e; for overlay in $(K8S_OVERLAYS); do \
		out="$(K8S_RENDER_DIR)/$$(echo $$overlay | tr '/' '-').yaml"; \
		echo "rendering $$overlay -> $$out"; \
		kubectl kustomize "$(K8S_DIR)/overlays/$$overlay" > "$$out"; \
	done

k8s-validate: k8s-render
	@# go install pins the version; a stamp file avoids reinstalling on every run because
	@# a go-installed kubeconform reports its version as "development".
	@if [ ! -x "$(KUBECONFORM)" ] || [ ! -f "bin/.kubeconform-$(KUBECONFORM_VERSION)" ]; then \
		$(GO) install github.com/yannh/kubeconform/cmd/kubeconform@$(KUBECONFORM_VERSION); \
		mkdir -p bin && touch "bin/.kubeconform-$(KUBECONFORM_VERSION)"; \
	fi
	$(KUBECONFORM) -strict -summary \
		-kubernetes-version $(KUBERNETES_VERSION) \
		-schema-location default \
		-schema-location '$(CRD_SCHEMA)' \
		$(K8S_RENDER_DIR)/*.yaml $(K8S_DIR)/overlays/*/namespace.yaml
