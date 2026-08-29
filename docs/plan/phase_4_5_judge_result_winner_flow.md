# Phase 4.5 - Judge Result and Winner Flow

Phase 4.5 connects durable Redis Stream jobs to the Docker executor and completes the
submission lifecycle in PostgreSQL. This document records the implemented contract for
future revisions.

## Scope

Phase 4.5 implements:

- Sequential Judge Stream consumption.
- PostgreSQL-authoritative claim and load.
- Attempt-token and lease ownership.
- Sandbox execution and typed outcome mapping.
- Token-fenced terminal completion.
- Atomic match winner selection.
- Stable per-recipient result events.
- Completed-job reconstruction.
- Publish followed by atomic Redis finalization (`XACK`, then `XDEL`).

Phase 4.6 now layers bounded concurrency, graceful shutdown, and race/failure tests on
this contract. PEL reclaim, expired lease reset, retry caps, and poison-job handling
remain Phase 5 work. Redis Pub/Sub remains best-effort and has no durable client replay.

## Components

| Component | Path |
| --- | --- |
| Production wiring | `internal/judge/run.go` |
| Queue orchestration | `internal/judge/service.go` |
| Claim, completion, winner SQL | `internal/judge/store.go` |
| Outcome and result events | `internal/judge/result.go` |
| Sandbox contract | `internal/judge/executor.go` |
| Docker implementation | `internal/judge/docker_executor.go` |
| Stream API | `internal/redisx/judge_queue.go` |
| Result envelope | `internal/proto/messages.go` |

## Processing Order

```text
XREADGROUP one entry
        |
claim pending submission in PostgreSQL
        |
load source, language, problem tests, and both players
        |
execute outside a database transaction
        |
lock match, then submission
        |
verify current attempt token
        |
persist verdict and conditionally select winner
        |
commit
        |
build and publish all stable result events
        |
atomic Redis script: XACK then XDEL exact entry
```

The match-before-submission completion lock order must be preserved by Phase 5 to avoid
deadlocks with winner selection and match finalization.

## Claim States

| Submission state | Judge behavior |
| --- | --- |
| `pending` | Set `running`, increment attempts, assign token and lease, execute |
| `running` with live token/lease | Treat this Stream entry as a duplicate; ack/delete |
| `running` with expired or invalid lease | Leave pending for Phase 5 |
| `completed` | Rebuild events without executing; publish/ack/delete |
| Missing submission | Treat as permanently malformed; ack/delete |
| Invalid durable source or tests | Return an invariant error and leave unacknowledged |

Stream jobs contain only `schema_version` and `submission_id`. Source, tests, players,
and match state always come from PostgreSQL.

## Outcome Mapping

| Sandbox outcome | Submission result | Failure kind |
| --- | --- | --- |
| `pass` | `pass` | none |
| `wrong_answer` | `fail` | `wrong_answer` |
| `compile_error` | `error` | `compile_error` |
| `runtime_error` | `error` | `runtime_error` |
| `output_limit` | `error` | `output_limit` |
| `timeout` | `timeout` | none |

Docker, PostgreSQL, Redis, and shutdown errors are not user verdicts. They leave the
submission attempt recoverable rather than writing `failed`. Phase 5 owns retry-cap
exhaustion and the `failed` verdict.

## Completion and Winner

Execution happens outside database transactions. Completion starts a new transaction,
locks the match and submission, and verifies both `status='running'` and the current
`attempt_token`. Lost ownership cannot change the verdict or winner.

A passing result conditionally updates:

```sql
UPDATE matches
SET status = 'finished', winner_id = $player
WHERE id = $match
  AND status = 'active'
  AND winner_id IS NULL;
```

There is no completion-time deadline check. A submission accepted before the deadline
can win after it while the match remains active. A later passing submission remains a
valid `pass` but reports the already-established winner.

## Result Events

When no winner exists, only the submitter receives the terminal submission result and
`winner_id`/`outcome` are omitted. When a winner exists, both match players receive the
same submission verdict plus recipient-specific `win` or `loss` outcomes.

Event IDs are deterministic UUIDv5 values derived from:

```text
codeduel:result:<event-kind>:<submission-id>:<recipient-id>
```

The event kind distinguishes submitter-only submission results from winner-aware
events. Completed duplicate jobs reconstruct the same payloads while the underlying
durable match state is unchanged. Problem test cases are treated as immutable so
`total_tests` can be reconstructed from `problems.test_cases`.

## Failure Ordering

| Failure | Publish | Acknowledge | Delete |
| --- | --- | --- | --- |
| Claim/load or execution infrastructure error | No | No | No |
| Completion transaction error or stale token | No | No | No |
| Event encoding error | No | No | No |
| Any recipient publication error | Attempt all recipients | No | No |
| Redis finalization error | Already published | Unknown; retry/recovery remains safe | Atomic with delete |
| Success | Yes | Yes | Yes |

Publishing to zero Redis subscribers counts as success under the best-effort Pub/Sub
contract. A crash after commit or publication may cause Phase 5 recovery to republish
the same event IDs.

## Verification

Unit tests use fake queue, store, executor, and publisher dependencies:

```bash
go test -race ./internal/judge ./internal/proto
```

PostgreSQL and Redis integration tests use a fake executor:

```bash
CODEDUEL_INTEGRATION=1 go test -race -count=1 ./internal/judge
```

Real sandbox boundary tests remain separate:

```bash
make test-docker-integration
```

The full non-Docker integration target is:

```bash
make test-integration
```

## Phase 4.6 Hardening

The production runner creates exactly `JUDGE_CONCURRENCY` workers. Each worker has a
unique Redis consumer name, reads one entry at a time, and completes that entry before
reading another. This bounds active sandboxes without accumulating claimed work in
memory.

Shutdown uses separate intake and work contexts:

1. Stop all new `XREADGROUP` calls immediately.
2. Keep in-flight work alive for `TotalTimeout + 2*CleanupTimeout`.
3. Allow successful work to complete, publish, acknowledge, and delete normally.
4. Cancel remaining work when the grace period expires.
5. Leave interrupted attempts unacknowledged for Phase 5.

Race-enabled integration tests prove that concurrent duplicate entries execute once,
simultaneous passing completions select one winner while both submissions remain
passes, stale tokens cannot complete, and publication failures retain a completed PEL
entry. Unit tests cover completion, publish, acknowledgment, deletion, graceful drain,
and forced-cancellation boundaries. The idempotent Redis finalization Lua script
prevents an acknowledged entry from being stranded in the Stream when a separate
delete fails.

## Next Phase

Phase 5 must reclaim expired PostgreSQL leases and abandoned Stream PEL entries without
violating attempt-token fencing or the match-before-submission lock order.
