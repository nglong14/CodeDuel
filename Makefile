COMPOSE_FILE := deploy/docker-compose.yml
BINARY := bin/codeduel
GO := go

.PHONY: help up down logs deps build run-gateway run-match run-judge run-reaper migrate migrate-down

help:
	@echo "CodeDuel targets:"
	@echo "  make up            Start Postgres + Redis"
	@echo "  make down          Stop Postgres + Redis"
	@echo "  make logs          Tail compose logs"
	@echo "  make deps          Download Go module dependencies"
	@echo "  make build         Build codeduel binary"
	@echo "  make run-gateway   Run gateway role"
	@echo "  make run-match     Run match role"
	@echo "  make run-judge     Run judge role"
	@echo "  make run-reaper    Run reaper role"
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

run-gateway: deps
	$(GO) run ./cmd/codeduel --role=gateway

run-match: deps
	$(GO) run ./cmd/codeduel --role=match

run-judge: deps
	$(GO) run ./cmd/codeduel --role=judge

run-reaper: deps
	$(GO) run ./cmd/codeduel --role=reaper

migrate: deps
	$(GO) run ./cmd/codeduel --role=migrate --direction=up

migrate-down: deps
	$(GO) run ./cmd/codeduel --role=migrate --direction=down
