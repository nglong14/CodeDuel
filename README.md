# CodeDuel

CodeDuel is a Go backend for real-time 1v1 programming duels. Two players are matched
from a FIFO queue, race on the same problem within a time limit, and the first correct
full-pass submission wins. PostgreSQL is the durable system of record; Redis provides
matchmaking, presence, and event delivery.

Stack: **Go, PostgreSQL, Redis, Docker**. One binary runs every role, selected with
`--role=gateway|match|judge|reaper|migrate`.

## Architecture

![CodeDuel architecture](./assets/codeduel-architecture.png)

> Diagram source: [Eraser workspace](https://app.eraser.io/workspace/QuL5MAUh4xotoI8tQm1w).

| Role | Responsibility |
| --- | --- |
| `gateway` | Terminates player WebSockets, validates JWTs, owns presence leases, admits submissions, and fans published events out to the sockets it owns. |
| `match` | Atomically pairs the two oldest live players, creates the authoritative match, and redispatches submissions whose immediate Redis enqueue failed. |
| `judge` | Consumes the Redis Stream, executes untrusted code in a disposable Docker sandbox, persists the verdict, and claims the winner. |
| `reaper` | Single-leader periodic loop that reclaims expired leases, redispatches abandoned Stream work, and finalizes matches that pass their deadline. |
| `migrate` | Applies or rolls back the embedded SQL migrations. |

Every Go role is stateless and horizontally scalable. Redis and PostgreSQL are the only
stateful tiers, and no authoritative game state lives in process memory.

### Correctness primitives

Concurrency safety rests on five mechanisms rather than distributed locks:

- **Lua pop-pair.** Matchmaking is a Redis ZSET scored by enqueue time. A single
  embedded Lua script pops the two oldest live members atomically, so concurrent Match
  replicas can never pair the same player twice. Calling `ZPOPMIN` twice from Go would
  not be safe.
- **Idempotent submissions.** A submission is keyed by `(player_id, request_id)`, so a
  client retry after a lost WebSocket response returns the original submission ID
  instead of creating a second row.
- **Conditional winner update.** On a full pass the Judge runs
  `UPDATE matches SET status='finished', winner_id=$1 WHERE id=$2 AND status='active'`.
  PostgreSQL row locking makes this the tiebreaker: one row updated means this player
  won, zero rows means the opponent already did.
- **Attempt-token fencing.** Each Judge claim writes an attempt token and a lease. A
  worker whose lease was reclaimed cannot overwrite the newer worker's verdict.
- **Acknowledge last.** The Judge persists the result, claims the winner, publishes
  events, and only then calls `XACK`. Acknowledging first would lose the job on a crash.

Redis Streams do not requeue abandoned work on their own: an entry read by a worker
that dies before `XACK` sits in that consumer's Pending Entries List forever. The
Reaper exists to close that gap, which is what makes the system self-healing.

### End-to-end flow

1. Both players send `join_queue` over WebSocket. The Gateway writes a presence lease
   and enqueues each player into the Redis ZSET.
2. The Match role pops a live pair, creates an `active` match with a database-generated
   deadline, and publishes the same `match_start` payload to both players.
3. A player submits. The Gateway validates membership and deadline, persists a pending
   submission, replies `judging` with the durable ID, and adds the submission ID to the
   Redis Stream.
4. A Judge worker claims the submission in PostgreSQL, loads the authoritative code and
   tests, and runs them in a fresh Docker sandbox.
5. The Judge persists the verdict and, on a full pass, claims the winner. It publishes
   recipient-specific results, then acknowledges the Stream entry.
6. If a worker died mid-job, the Reaper returns the lease to `pending` and redispatches.
   If the deadline passes with no winner, the Reaper finalizes on most tests passed or a
   draw and publishes `match_end`.

Stream entries carry only a schema version and submission ID. Code, tests, ownership,
and lifecycle state always come from PostgreSQL, so a stale or malformed Redis field
can never override authoritative state.

## Requirements

- Go 1.26 or newer
- Docker with Compose

## Quick start

Start PostgreSQL on `localhost:5433` and Redis on `localhost:6379`, then apply the
schema and seed data:

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

Stop local infrastructure with `make down`. `make migrate-down` rolls back the database
and is destructive.

## Judge sandboxes

Build the digest-pinned language images before starting the Judge role:

```sh
make sandbox-images
make run-judge
make run-reaper
```

Submissions are complete stdin/stdout programs in Python, C++, or Java. Each attempt
gets a fresh container with no network, a read-only root filesystem, dropped
capabilities, `no-new-privileges`, a non-root user, and hard CPU, memory, PID, output,
and wall-clock limits.

`JUDGE_CONCURRENCY` bounds active sandbox workers, with one Redis consumer per worker
and no job prefetch beyond available slots. Shutdown stops intake immediately, lets
in-flight jobs drain for the total execution and cleanup budget, then force-cancels
remaining work. Interrupted jobs keep a PostgreSQL attempt lease; the Reaper returns
expired leases to `pending`, redispatches a replacement Stream entry, and marks a
submission `failed` after `REAPER_MAX_ATTEMPTS`. Expired matches with no open
submissions are finished by most tests passed or a draw and published as `match_end`.
Redis Pub/Sub result delivery is best-effort and has no durable replay.

### Security boundary

Access to the Docker daemon is effectively host-root access. Do not co-locate a public
Judge with Gateway, PostgreSQL, or Redis, and do not treat Docker as a VM-grade
security boundary. Production hostile multi-tenant execution requires stronger
isolation such as gVisor, Kata Containers, or microVMs on dedicated workers.

## Configuration

Configuration is read from the environment and optionally from a `.env` file; see
`.env.example` for every variable and its default. The values worth knowing first:

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_DSN` | `postgres://codeduel:codeduel@localhost:5433/codeduel?sslmode=disable` | System of record |
| `REDIS_ADDR` | `localhost:6379` | Matchmaking, queue, and fan-out |
| `GATEWAY_ADDR` | `:8080` | WebSocket and health listener |
| `JWT_SECRET` | `codeduel-dev-secret` | HS256 signing key for dev tokens |
| `MATCH_DURATION` | `10m` | Deadline applied at match creation |
| `JUDGE_CONCURRENCY` | `2` | Maximum concurrent sandboxes per Judge |
| `JUDGE_ATTEMPT_LEASE` | `1m` | Must exceed total execution plus cleanup |
| `REAPER_INTERVAL` | `10s` | Sweep period |
| `REAPER_MAX_ATTEMPTS` | `3` | Infrastructure retries before `failed` |
| `REAPER_STREAM_MIN_IDLE` | `2m` | Must exceed `JUDGE_ATTEMPT_LEASE` |

Auth is intentionally minimal for the MVP: `duelcli` mints an HS256 JWT whose `sub` is a
seeded user UUID, and the Gateway verifies the signature and expiry. Real registration
and login are out of scope.

## Verification

Follow CI order for broad checks:

```sh
make lint
go mod verify
go build -v ./...
go test ./... -race
```

Integration tests require running Redis and PostgreSQL:

```sh
make test-integration
```

They default to Redis DB 15 and a temporary PostgreSQL database created from the admin
DSN; override with `REDIS_TEST_ADDR` and `POSTGRES_TEST_DSN`. Docker boundary tests are
opt-in and separate:

```sh
make test-docker-integration
```

The normal Go test suite never connects to Docker. Sandbox tests create real containers
and intentionally exercise output, memory, PID, network, and timeout limits, so run
them only on a disposable local or dedicated Judge host.

## Scope

Deliberately left out of the MVP: rating and ELO banding, spectators and replay,
Kafka or RabbitMQ, Kubernetes, and a React client. Matchmaking is pure FIFO, event
fan-out is Redis Pub/Sub, and `docker compose` is enough to prove the design.
