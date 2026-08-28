# CodeDuel

CodeDuel is a Go backend for real-time programming duels. One binary runs as a
Gateway, Match, Judge, or Reaper role, with PostgreSQL as the system of record and
Redis for matchmaking and event delivery.

Phase 4 is in progress. Durable submission intake and Redis Stream dispatch are
implemented, and the Judge role has a Docker Engine sandbox executor for pinned
Python, C++, and Java runtimes. Result persistence and winner selection follow in
Phase 4.5.

## Requirements

- Go 1.26 or newer
- Docker with Compose

## Start Phase 3

Start Redis and PostgreSQL, then apply the schema and seed data:

```sh
make up
make migrate
```

Run the Gateway and Match roles in separate terminals:

```sh
make run-gateway
make run-match
```

Run two clients in two more terminals. The migrations seed both users shown here:

```sh
make run-cli USER_ID=11111111-1111-1111-1111-111111111111
make run-cli USER_ID=22222222-2222-2222-2222-222222222222
```

Enter `join` in the first client. It receives no immediate echo and remains queued.
Enter `join` in the second client. Both clients receive the same `match_start` event,
including the PostgreSQL-generated match ID, problem ID, and deadline.

To verify stale-player cleanup, stop a queued client and then join with two live
clients. Graceful disconnects remove presence immediately; ungraceful disconnects
become ineligible after the 75-second presence lease expires.

## Verification

Run unit and race tests, real Redis/PostgreSQL integration tests, the build, and lint:

```sh
go test -race ./...
make test-integration
go build ./...
make lint
```

Integration tests use Redis databases 14 and 15 and create a temporary PostgreSQL
database per test. Override dependency addresses with `REDIS_TEST_ADDR` and
`POSTGRES_TEST_DSN` when needed.

## Judge Sandboxes

Build the digest-pinned language images before starting the Judge role:

```sh
make sandbox-images
make run-judge
```

Run Docker boundary tests explicitly:

```sh
make test-docker-integration
```

The normal Go test suite does not connect to Docker. Sandbox tests create real
containers and intentionally exercise output, memory, PID, network, and timeout
limits. Run them only on a disposable local or dedicated Judge worker host.

Access to the Docker daemon is effectively host-root access. Do not co-locate a
public Judge with Gateway, PostgreSQL, or Redis and treat Docker as a VM-grade
security boundary. Production hostile multi-tenant execution requires stronger
isolation such as gVisor, Kata Containers, or microVMs on dedicated workers.

Stop local infrastructure with:

```sh
make down
```
