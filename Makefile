COMPOSE_FILE := deploy/docker-compose.yml
BINARY := bin/codeduel
GO := go
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := $(shell $(GO) env GOPATH)/bin/golangci-lint

USER_ID ?= 11111111-1111-1111-1111-111111111111

.PHONY: help up down logs deps build lint run-gateway run-match run-judge run-reaper run-cli migrate migrate-down

help:
	@echo "CodeDuel targets:"
	@echo "  make up            Start Postgres + Redis"
	@echo "  make down          Stop Postgres + Redis"
	@echo "  make logs          Tail compose logs"
	@echo "  make deps          Download Go module dependencies"
	@echo "  make build         Build codeduel binary"
	@echo "  make lint          Run golangci-lint"
	@echo "  make run-gateway   Run gateway role"
	@echo "  make run-match     Run match role"
	@echo "  make run-judge     Run judge role"
	@echo "  make run-reaper    Run reaper role"
	@echo "  make run-cli       Run duelcli (USER_ID=$(USER_ID))"
	@echo "  make migrate       Apply database migrations"
	@echo "  make migrate-down  Roll back database migrations"

up:
	docker compose -f $(COMPOSE_FILE) up -d

down:
	docker compose -f $(COMPOSE_FILE) down

logs:
	docker compose -f $(COMPOSE_FILE) logs -f

deps:
	$(GO) mod tidy

build: deps
	$(GO) build -o $(BINARY) ./cmd/codeduel

lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ] || ! "$(GOLANGCI_LINT)" version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"; then \
		$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi
	$(GOLANGCI_LINT) run ./...

run-gateway: deps
	$(GO) run ./cmd/codeduel --role=gateway

run-match: deps
	$(GO) run ./cmd/codeduel --role=match

run-judge: deps
	$(GO) run ./cmd/codeduel --role=judge

run-reaper: deps
	$(GO) run ./cmd/codeduel --role=reaper

run-cli: deps
	$(GO) run ./tools/duelcli -user=$(USER_ID)

migrate: deps
	$(GO) run ./cmd/codeduel --role=migrate --direction=up

migrate-down: deps
	$(GO) run ./cmd/codeduel --role=migrate --direction=down
