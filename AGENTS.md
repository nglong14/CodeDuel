# CodeDuel Agent Guide

## Project Shape

- This is one Go module (`go 1.26.6`), built around the single binary in `cmd/codeduel`; select its role with `--role=gateway|match|judge|reaper|migrate`.
- PostgreSQL is the durable system of record and Redis provides matchmaking, presence, and event delivery. Core role implementations are under `internal/gateway`, `internal/match`, `internal/judge`, and `internal/reaper`; shared infrastructure is under `internal/infrastructure` and Redis primitives under `internal/redisx`.
- Database migrations are embedded from `internal/infrastructure/migrations`; change migration files rather than adding a separate migration runner.

## Local Workflow

- Use Go 1.26.6 or newer and Docker Compose. `make up` starts Postgres on `localhost:5433` and Redis on `localhost:6379`; run `make migrate` before starting application roles.
- Configuration optionally loads `.env` (use `.env.example` as the variable reference). `.env` is gitignored; do not add secrets.
- Run long-lived roles in separate terminals, for example `make run-gateway` and `make run-match`; `make run-cli USER_ID=<seeded UUID>` connects a WebSocket client.
- `make build` runs `go mod tidy` before building `./cmd/codeduel`, so use `go build ./...` for a non-mutating full build check.

## Verification

- Follow CI order for broad checks: `make lint`, `go mod verify`, `go build -v ./...`, then `go test ./... -race`.
- `make lint` installs/checks golangci-lint `v2.12.2`; formatting is enforced through its `gofmt` and `goimports` formatters.
- CI also runs `gosec` after the build; this is not part of a Make target.
- Integration tests require running Redis/Postgres and `CODEDUEL_INTEGRATION=1`; `make test-integration` starts Compose and runs the Redis/match integration packages with `-race -count=1`.
- Integration tests default to Redis DB 15 and a temporary Postgres database based on the admin DSN; override with `REDIS_TEST_ADDR` and `POSTGRES_TEST_DSN`. A focused example is `CODEDUEL_INTEGRATION=1 go test -race -count=1 ./internal/match -run TestCreateMatchIntegration`.
- Stop local services with `make down`; `make migrate-down` rolls back the database and is destructive.
