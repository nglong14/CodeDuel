# CodeDuel

Real-time backend for 1v1 programming duels. Two players enter a FIFO queue, receive the same problem, and race to the first fully correct submission.

**Stack:** Go 1.26+, PostgreSQL, Redis, Docker.
**Client today:** `duelcli`, a WebSocket test client. A React SPA is planned.


https://github.com/user-attachments/assets/6572aa6e-ee60-47d7-ae2c-93df52a441c4


## Run it locally

**Requires:** Go 1.26+, Docker, and Docker Compose.

1. Start PostgreSQL, Redis, migrations, Gateway, Match, and Reaper:

   ```sh
   make up
   ```

2. Build the sandbox images and start Judge in a separate Compose stack:

   ```sh
   make sandbox-images
   make up-judge
   ```

3. Load the development-only fixture users (Alice and Bob). These are not part of
   the production migration sequence, so seed them explicitly:

   ```sh
   make seed-dev
   ```

4. Open two terminals and start the fixture players:

   ```sh
   make run-cli USER_ID=11111111-1111-1111-1111-111111111111
   make run-cli USER_ID=22222222-2222-2222-2222-222222222222
   ```

5. Type `join` in each client. Both receive `match_start`.

Stop services with `make down` and `make down-judge`. `make migrate-down` and `make reset` delete development data.

## What runs

The single `codeduel` binary selects a role with `--role=`.

| Role | Job |
| --- | --- |
| `gateway` | REST auth/readiness endpoints, player WebSockets, JWT validation, presence, and event fan-out. |
| `match` | Pairs the two oldest live players and creates matches. |
| `judge` | Runs submitted Python, C++, or Java programs in disposable Docker sandboxes. |
| `reaper` | Reclaims abandoned work and finishes expired matches. |
| `migrate` | Applies embedded SQL migrations. |

![CodeDuel architecture](./assets/codeduel-architecture.png)

Diagram source: [Eraser](https://app.eraser.io/workspace/QuL5MAUh4xotoI8tQm1w?diagram=aE7Tf70aCQ-DPEWmgvoi).

## How a duel works

1. Players send `join_queue` over WebSocket.
2. Match creates an active match and sends `match_start` to both players.
3. Gateway stores each submission, then puts its ID on a Redis Stream.
4. Judge runs the authoritative code and tests from PostgreSQL.
5. A full pass atomically claims the winner. Reaper retries interrupted work and resolves timeouts.

PostgreSQL is the source of truth. Redis provides matchmaking, presence, queueing, and best-effort event delivery.

## API

| Endpoint | Purpose |
| --- | --- |
| `POST /api/auth/register` | Create an account and receive a JWT. |
| `POST /api/auth/login` | Sign in and receive a JWT. |
| `GET /api/me` | Get the current user; requires `Authorization: Bearer <token>`. |
| `GET /healthz` / `GET /readyz` | Liveness / dependency readiness checks. |

Example registration request:

```sh
curl -i -X POST localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"player@example.com","password":"hunter2hunter2"}'
```

## Configuration and safety

Copy or edit `.env` as needed; [.env.example](.env.example) lists every setting. The defaults use PostgreSQL at `localhost:5433`, Redis at `localhost:6379`, and Gateway at `:8080`.

Set a strong `JWT_SECRET` in production. The Gateway rejects missing, default, or shorter-than-32-byte production secrets.

Judge containers have no network, run as non-root, use a read-only root filesystem, drop capabilities, and have CPU, memory, PID, output, and timeout limits. Docker daemon access is effectively host-root access: run public Judge workers separately from the Gateway and databases. For hostile multi-tenant workloads, use stronger isolation such as gVisor, Kata Containers, or microVMs.

## Verify changes

```sh
make lint
go mod verify
go build -v ./...
go test ./... -race
```

Run Redis/PostgreSQL integration tests with:

```sh
make test-integration
```

`make test-docker-integration` also builds and exercises real sandbox containers; run it only on a disposable local or dedicated Judge host.

## MVP boundaries

No rating system, spectators, replay, Kubernetes, message broker, or web client yet. Matchmaking is FIFO and event fan-out uses Redis Pub/Sub.
