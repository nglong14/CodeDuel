# Docker Executor Review Guide

This document explains the implementation in
[`internal/judge/docker_executor.go`](../../internal/judge/docker_executor.go) and
provides a checklist for reviewing future revisions. It describes the current code,
not just the intended Phase 4 design.

## Purpose

`DockerExecutor` compiles and runs untrusted Python, C++, and Java submissions in
short-lived Docker containers. It implements the small `Executor` interface from
[`internal/judge/executor.go`](../../internal/judge/executor.go):

```go
type Executor interface {
	Execute(context.Context, ExecutionRequest) (ExecutionOutcome, error)
}
```

This boundary keeps Docker-specific behavior separate from queue processing,
submission persistence, and winner selection. Another sandbox implementation could
replace it without changing those systems.

The Judge role constructs and closes `DockerExecutor`, consumes Redis Stream jobs, and
calls `Execute` after claiming authoritative submission data from PostgreSQL. See
[`internal/judge/run.go`](../../internal/judge/run.go) for the role wiring and
[`internal/judge/service.go`](../../internal/judge/service.go) for orchestration.

## What Moby Does Here

[Moby](https://github.com/moby/moby) is the open-source engine and API used by Docker.
This project imports its Go API and client modules:

```go
github.com/moby/moby/api
github.com/moby/moby/client
```

The code uses Moby as a client for an already-running Docker daemon. It does not embed
the daemon and does not invoke the `docker` CLI. Conceptually, calls map as follows:

| Moby call | Similar CLI operation | Purpose |
| --- | --- | --- |
| `Ping` | `docker version` | Check daemon access and negotiate an API version |
| `Info` | `docker info` | Check host isolation capabilities |
| `ImageInspect` | `docker image inspect` | Resolve configured image tags to image IDs |
| `VolumeCreate` | `docker volume create` | Create the in-memory attempt workspace |
| `ContainerCreate` | `docker create` | Define a staging, compile, or runtime container |
| `CopyToContainer` | `docker cp` | Put the source archive in the workspace |
| `ContainerAttach` | `docker attach` | Connect stdin, stdout, and stderr |
| `ContainerStart` | `docker start` | Start a prepared container |
| `ContainerWait` | `docker wait` | Obtain completion status and exit code |
| `ContainerInspect` | `docker inspect` | Detect runtime state such as an OOM kill |
| `ContainerKill` | `docker kill` | Stop code after a timeout or output overflow |
| `ContainerRemove` | `docker rm -f` | Remove an attempt container |
| `VolumeRemove` | `docker volume rm` | Remove an attempt workspace |

`client.New(client.FromEnv)` reads standard Docker client environment configuration,
including `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, and `DOCKER_CERT_PATH`.

The Moby packages have distinct jobs:

- `client` provides daemon operations and their option/result types.
- `container` provides container configuration, resource limits, and wait types.
- `mount` describes the workspace volume mount.
- `stdcopy` separates Docker's multiplexed stdout and stderr attach stream.

## Recommended Reading Order

Review the implementation in this order instead of reading top to bottom:

1. `Executor`, `ExecutionRequest`, `Limits`, and `ExecutionOutcome` in `executor.go`.
2. `languageSpecs` to understand what each language executes.
3. `Execute` for the complete attempt lifecycle.
4. `sandboxContainerOptions` for the security boundary.
5. `runContainer` for I/O, timeout, and process handling.
6. `attemptResources.cleanup` and `cleanupStaleResources` for failure recovery.
7. `NewDockerExecutor` and `validateDockerHost` for startup requirements.

This order starts with behavior and then moves into Docker mechanics.

## Main Types

### `dockerEngine`

`dockerEngine` is the narrow subset of Moby operations used by this file. The real
Moby client satisfies it, while tests can supply a fake. When adding a Docker API call,
add only the required method to this interface rather than depending on the complete
client type.

### `languageSpec`

Each supported language has a source filename, compile command, and run command:

| Language | Source | Compile | Run |
| --- | --- | --- | --- |
| Python | `main.py` | isolated `py_compile` syntax check | isolated Python process |
| C++ | `main.cpp` | `g++`, C++20, optimized binary | `/workspace/main` |
| Java | `Main.java` | UTF-8 `javac` into workspace | Java 21 classpath execution |

Commands are argument slices, not shell strings. Source and test input are never
interpolated into a command, which avoids shell injection.

### `DockerExecutor`

The executor retains:

- A Docker API client.
- An immutable image ID for each language.
- A random Judge instance ID used in resource labels.
- The age after which abandoned resources are considered stale.

### `attemptResources`

This helper tracks every container and the workspace volume created for one attempt.
Its deferred cleanup is the final safety net for all success and error paths.

## Initialization

`NewDockerExecutor` creates a Moby client and delegates validation to
`newDockerExecutor`. Initialization fails unless all of these conditions hold:

1. Judge configuration is valid.
2. The Docker daemon responds to `Ping`.
3. The daemon is Linux-based.
4. Memory, swap, CPU quota, and PID limits are available.
5. The daemon reports seccomp support.
6. Every configured language image already exists locally.
7. Old labeled sandbox resources can be cleaned successfully.

Configured image tags are inspected once and converted to image IDs. An image tag can
be changed later without silently changing the image used by an already-running Judge.
The executor deliberately does not pull images during startup.

The default image references are configured in
[`internal/config/config.go`](../../internal/config/config.go), and their Dockerfiles
are under [`deploy/sandbox`](../../deploy/sandbox). Build them with:

```bash
make sandbox-images
```

## One Execution Attempt

`Execute` processes an `ExecutionRequest` in the following order:

```text
validate request and select language image
                |
create size-limited tmpfs workspace volume
                |
create and start staging container
                |
copy source tar archive into /workspace
                |
create and run compile container (workspace read-write)
                |
      nonzero exit -> compile_error
                |
for each test, create a fresh runtime container
                |
mount compiled workspace read-only
                |
attach streams, start, write stdin, close stdin, wait
                |
check output limit, timeout, exit code, OOM, and stdout
                |
remove attempt containers and workspace volume
```

The outer `TotalTimeout` covers setup, compilation, and every test. Compilation also
has `CompileTimeout`, and each test has its own `TestTimeout`.

### Workspace and source staging

`createWorkspace` creates a named Docker volume backed by `tmpfs`. Its options enforce:

- A configured byte limit.
- Ownership by UID/GID `10001`.
- Mode `0700`.
- `nosuid` and `nodev`.

The source is wrapped in a tar archive because Docker's copy API accepts a tar stream.
`sourceArchive` gives the file mode `0400` and sandbox ownership. A staging container
mounts the shared volume while `CopyToContainer` extracts that archive into
`/workspace`.

### Compilation

The compile container mounts the workspace read-write so it can create C++ binaries,
Java class files, or Python bytecode. A nonzero process exit becomes
`OutcomeCompileError`. Compilation output is bounded but is not currently returned to
the caller.

### Runtime tests

Each test gets a fresh container and the workspace is mounted read-only. This prevents
one test from changing compiled artifacts for a later test. Programs can still use the
bounded `/tmp` mount.

Expected output remains in the Judge process and is never copied into a container.
Tests stop at the first failure, and `TestsPassed` counts only preceding successful
tests.

## Process I/O

`runContainer` attaches before starting the container so it cannot miss early output.
It then:

1. Starts a goroutine that demultiplexes stdout and stderr with `stdcopy.StdCopy`.
2. Starts the container.
3. Writes test input to the attached stdin connection.
4. Closes the write side so programs can observe EOF.
5. Waits for either process completion, output failure, or context cancellation.

Docker's attach protocol frames stdout and stderr on one connection. `stdcopy.StdCopy`
decodes those frames into separate buffers.

Both streams share one `outputBudget`, defined in
[`internal/judge/output.go`](../../internal/judge/output.go). If their combined output
exceeds `MaxOutputBytes`, writing returns `errOutputLimit`; the executor kills the
container and reports `OutcomeOutputLimit`.

`stderr` is captured to enforce the shared limit, but the current public outcome does
not expose stdout or stderr diagnostics.

## Verdict Classification

| Condition | Outcome |
| --- | --- |
| Compile command exits nonzero | `compile_error` |
| Runtime exits nonzero | `runtime_error` |
| Docker reports runtime OOM kill | `runtime_error` |
| Actual stdout differs from expected | `wrong_answer` |
| Combined stdout/stderr exceeds limit | `output_limit` |
| Compile, test, or total deadline expires | `timeout` |
| Every test matches | `pass` |
| Docker API or cleanup operation fails | Infrastructure error returned to caller |

Output comparison converts CRLF to LF and removes one final newline. It does not trim
other whitespace or apply numeric tolerance.

## Sandbox Restrictions

`sandboxContainerOptions` defines the security-sensitive container settings:

| Control | Current setting | Reason |
| --- | --- | --- |
| User | `10001:10001` | Do not run submissions as root |
| Network | Disabled and `NetworkMode=none` | Prevent external and service access |
| Linux capabilities | Drop `ALL` | Remove unnecessary kernel privileges |
| Privileged mode | `false` | Prevent privileged container access |
| Root filesystem | Read-only | Prevent modification of the language image |
| Privilege escalation | `no-new-privileges=true` | Block setuid-style escalation |
| Logging driver | `none` | Avoid unbounded daemon-side log storage |
| `/tmp` | Size-limited tmpfs, `nosuid,nodev,noexec` | Provide bounded temporary storage |
| Workspace | Size-limited tmpfs volume | Bound source and build artifacts |
| CPU | `NanoCPUs` | Bound CPU allocation |
| Memory/swap | Explicit byte ceilings | Bound memory consumption |
| Processes | PID limit and `nproc` ulimit | Limit fork/process bombs |
| Files | `fsize` and `nofile` ulimits | Bound file growth and descriptors |
| Core dumps | Disabled | Avoid large or sensitive dump files |

These controls are defense in depth, not VM-grade isolation. Access to the Docker
daemon is effectively host-root access. A Judge accepting hostile public code should
run on dedicated worker infrastructure rather than beside Gateway, PostgreSQL, Redis,
or secrets.

## Cleanup and Labels

Every resource has labels that identify it as a CodeDuel sandbox resource, its attempt,
its Judge instance, and its role:

```text
com.codeduel.sandbox=true
com.codeduel.attempt=<attempt UUID>
com.codeduel.instance=<Judge instance UUID>
com.codeduel.resource=workspace|staging|compile|runtime
```

`Execute` defers `attemptResources.cleanup` immediately after creating the tracker.
Cleanup uses new background contexts rather than the canceled execution context, and
removes containers in reverse creation order before removing the volume.

If a process crashes before deferred cleanup runs, `cleanupStaleResources` scans for
the sandbox label during the next executor initialization. Resources older than
`AttemptLease` are force-removed.

An important behavior is that cleanup errors are not ignored. They are joined with the
execution error and clear an otherwise successful outcome, because leaked sandbox
resources are treated as an infrastructure failure.

## Configuration Map

`Limits` values originate in `JudgeConfig`:

| Environment variable | Used for |
| --- | --- |
| `JUDGE_MAX_CODE_BYTES` | Request source limit |
| `JUDGE_MAX_OUTPUT_BYTES` | Combined stdout/stderr limit |
| `JUDGE_COMPILE_TIMEOUT` | Compile wall-clock timeout |
| `JUDGE_TEST_TIMEOUT` | Per-test wall-clock timeout |
| `JUDGE_TOTAL_TIMEOUT` | Whole-attempt timeout |
| `JUDGE_CLEANUP_TIMEOUT` | Individual kill/removal timeout |
| `JUDGE_ATTEMPT_LEASE` | Stale resource age |
| `JUDGE_CPU_NANOS` | Docker CPU quota |
| `JUDGE_MEMORY_BYTES` | Memory ceiling |
| `JUDGE_MEMORY_SWAP_BYTES` | Combined memory/swap ceiling |
| `JUDGE_PID_LIMIT` | PID and process ulimits |
| `JUDGE_WORKSPACE_BYTES` | Workspace tmpfs and file-size limits |
| `JUDGE_TMPFS_BYTES` | `/tmp` and shared-memory size |
| `JUDGE_PYTHON_IMAGE` | Python sandbox image |
| `JUDGE_CPP_IMAGE` | C++ sandbox image |
| `JUDGE_JAVA_IMAGE` | Java sandbox image |

Defaults are documented in [`.env.example`](../../.env.example).

## Revision Review Checklist

Use this checklist whenever the executor changes.

### Execution correctness

- Does every supported language still have a fixed source name and argument-slice
  commands without a shell?
- Is source validated before any Docker resource is created?
- Is compilation performed once and each test run in a fresh container?
- Is runtime workspace access read-only?
- Are expected answers kept outside the sandbox?
- Does execution stop at the first failed test and preserve `TestsPassed`?
- Are exit code, OOM state, output overflow, and timeout classified distinctly?

### Isolation

- Are network access, privileged mode, and all capabilities still disabled?
- Are rootfs and runtime workspace still read-only?
- Does the process still run as the non-root sandbox user?
- Are CPU, memory, swap, PID, file, descriptor, output, and wall-clock limits present?
- Are new mounts free of host paths, sockets, devices, credentials, and secrets?
- Are environment variables fixed rather than inherited from the Judge?
- Are images selected only from trusted configuration, never user input?

### Lifecycle and concurrency

- Is every created resource recorded for cleanup even when creation partially fails?
- Does cleanup avoid the already-canceled execution context?
- Can output-reading goroutines or stdin-writing goroutines block after return?
- Is attach established before container start?
- Are stdout and stderr both bounded by the same total budget?
- Does cancellation kill the running container before cleanup?
- Can stale cleanup accidentally match non-CodeDuel resources?

### Moby API changes

- Does `dockerEngine` remain a minimal, mockable interface?
- Are API version negotiation and daemon capability validation preserved?
- Are create warnings and empty IDs handled as errors?
- Are image references resolved to IDs at startup?
- Are all API errors wrapped with the operation being attempted?
- If attach settings change, is `stdcopy` still correct for the selected stream mode?

### Tests

- Do unit tests cover new container options and error classification?
- Do Docker integration tests cover all languages and new failure modes?
- Does cancellation leave no labeled containers or volumes?
- Are memory, PID, output, and timeout boundaries tested?

Run the focused Docker integration suite with:

```bash
make test-docker-integration
```

For ordinary changes that do not require a live daemon, run:

```bash
go test ./internal/judge
```

## Known Boundaries of the Current Implementation

- The production Judge runs one Docker execution per configured worker and does not
  prefetch work beyond `JUDGE_CONCURRENCY`.
- Shutdown drains in-flight executions for a bounded grace period before cancellation.
- Attempt cleanup shares one cleanup deadline across all remaining containers and the
  workspace volume, keeping shutdown and attempt-lease budgets bounded.
- Interrupted jobs remain pending until the Phase 5 Reaper reclaims their leases.
- Compiler and runtime diagnostics are not included in `ExecutionOutcome`.
- An OOM kill is grouped under `runtime_error`, not a separate memory-limit outcome.
- Tests execute sequentially and stop at the first failure.
- The startup stale-resource sweep is recovery after a process failure; no periodic
  sweep runs while the same Judge process remains alive.
- Docker isolation is appropriate for the controlled MVP described in the Phase 4
  plan, but it is not a sufficient standalone boundary for hostile public multi-tenant
  execution.

For the broader submission queue, persistence, and winner design, see
[`docs/plan/phase_4_submission_judge_sandbox.md`](../plan/phase_4_submission_judge_sandbox.md).
