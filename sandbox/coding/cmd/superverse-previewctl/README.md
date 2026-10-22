# superverse-previewctl

`superverse-previewctl` owns the Preview environment of one coding Sandbox
(coding profile v4, [the current contract](../../../../docs/architecture/agent-harness-design/sandbox.md)):
a local Temporal and Dex engine whose state lives on the workspace volume,
isolated source trees of prepared commits, and the Preview application built
from them. One detached daemon per Pod, `supervise`, owns
every process; the other commands ask it over the Pod-local socket
`/tmp/superverse-preview/control.sock` (mode 0600).
`apptest run` is separate: it runs one application test command against a fresh,
short-lived engine of its own ([Application test engine](#application-test-engine)).

## Command contract

The platform's PreviewFlow runs these commands through sandboxd, and the
Session's reclamation stops the engine with `environment stop`; the Agent may
run `environment ensure` for its own Worker. Every command but `apptest run`
([below](#application-test-engine)) prints one JSON object on stdout:

- exit 0: a definite answer, `{"state": ..., ...}`;
- exit 1: `{"state": "error", "reason": ...}`, a supervisor, transport or usage
  error. It may be retried and is never an answer about the application.

Every command needs `SUPERVERSE_PREVIEW_STATE_DIR` (the profile sets
`/var/lib/superverse/preview`); `supervise` also needs `SUPERVERSE_POD_UID`.

| Command | States | Behavior |
| --- | --- | --- |
| `environment ensure` | `starting`, `ready`, `full`, `inconsistent`, `failed` | Launches the daemon when none runs and answers its state. `ready` carries `temporalClusterId`, `retentionDays` (30, read back), `temporalVersion` and `dexVersion`. A refusal is answered once; the next `ensure` starts a new attempt and answers `starting`. |
| `environment status` | the above, `stopping`, `stopped` | Observation only. |
| `environment stop` | `stopping`, `stopped` | Idempotent. `stopped` once the daemon exited and released the state lock. Stops the application, Dex, then Temporal: SIGTERM to each process group, SIGKILL after 10 seconds. |
| `source ensure --commit SHA` | `ready` (`commit`, `path`), `source_missing` | Exports `git archive SHA` from `/workspace` into `$S/source/SHA`, then deletes every other tree no daemon uses. Idempotent; a commit missing from the repository changes nothing. Needs no daemon. |
| `begin --commit SHA` | `building`, `starting`, `running`, `failed`, `source_missing` | Starts the application of SHA with the identity (SHA, `DEX_PROJECT_CONFIG_DIGEST`, `SUPERVERSE_PREVIEW_GRANT_GENERATION`) of the calling process's environment, which the build and the application inherit. Requires a `ready` environment, else `error`. A running application of the same identity answers `running` without a rebuild. A different identity supersedes a start in progress. |
| `status` | `building`, `starting`, `running`, `failed`, `source_missing`, `stopping`, `stopped` | A start in progress, else the application that serves (`running` with its identity, `applicationPort` 3000 and `workerListening` for 127.0.0.1:18811), else the latest start's outcome. A failed build leaves the previous application `running`. |
| `stop` | `stopping`, `stopped` | Idempotent. Ends a start in progress and the application; keeps the trees and the environment. |
| `shutdown [--timeout 30s]` | `stopped`, `stopping` | The preStop hook: stops the environment and waits for the state lock. |
| `supervise` | `stopped` | The daemon. A second one answers `error`. |

`begin` answers `failed` only for the application's own failures, with the
redacted tail (at most 4 KiB) of `logs/build.log` or `logs/app.log`:

- a build step failed; the previous application keeps running;
- the started application exited, or did not pass `GET /api/health` (2xx) on
  127.0.0.1:3000 within 5 minutes of starting; it is stopped and no
  application runs.

A failure is answered once per start; the next `begin` of the same identity
builds again. Redaction removes the values of the `AWS_*` and `DEX_PROJECT_*`
variables and of the shared credentials file, when at least 8 bytes long.

## Build and application

`begin` builds in `$S/source/SHA`, never in the workspace: `npm --prefix web ci`
(or `install` without a lockfile) `--ignore-scripts`, `make generate`,
`npm --prefix web run build` and `go build -o .preview/server ./cmd/server`.
Only after a successful build does it stop the previous application and start
`.preview/server` in the tree. The previous tree is deleted once the new
application passes its health probe, so at most the running tree and the
candidate remain.

The application inherits `begin`'s environment followed by these fixed values,
which the caller cannot override: `PORT=3000`, `HOST=0.0.0.0`,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801` and
`DEX_BLOB_CACHE_DIR=/tmp/superverse-preview/app-blobs`.

## Application test engine

`superverse-previewctl apptest run [--run-id ID] [--timeout 6m] [--hold-until FILE [--hold-timeout 15m]] [--report-dir DIR] -- COMMAND...`
runs COMMAND against a fresh engine that exists only for this run. Unlike the
other commands it prints no JSON answer: it passes the command's output through,
prints one summary line and exits with the command's code. The template's
`make test-app` calls it, and the commit gate starts
`apptest run --run-id <gate-...> --timeout 6m --hold-until /tmp/superverse-apptest-release-<gate-...> --hold-timeout 12m -- make test-app`
with its static stage and releases it once the static and definitions stages
passed.

- **Foreground, in the caller's process group.** sandboxd's kill of the group
  ends the command and the engine with it; SIGTERM or SIGINT stops the command
  and tears the engine down (exit 143).
- **One engine at a time.** A run takes the exclusive lock
  `/tmp/superverse-apptest/lock` before it starts anything and queues behind a
  holder; no run kills another. The deadline (`--timeout`, at most 30 minutes)
  covers the queue: a deadline that passes while queued exits 124 before any
  engine starts. The holder that the lock admits first kills, after their
  identity check, the engine processes a run that died without its teardown
  left in `/tmp/superverse-apptest/engine.json`.
- **Engine.** The real binaries of `/usr/local/libexec/superverse`: an in-memory
  `temporal server start-dev --headless` on 127.0.0.1:7235 (HTTP 7245, metrics
  7246), ready within 15 seconds, then `dexcli dev` on 127.0.0.1:8805 (Web 8806)
  with `--external-temporal-address 127.0.0.1:7235`, its blob, log and connector
  directories in the run directory, healthy within 20 seconds. A taken port
  fails the start; a start that fails is tried once more, and a second failure
  exits 125 without running the command.
- **Command environment.** The caller's environment without `DEX_PROJECT_*`, so
  no project configuration and no credential, plus
  `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8805`, `DEX_WORKER_BIND_ADDRESS` and
  `DEX_WORKER_TARGET` `127.0.0.1:18813`, `DEX_BLOB_CACHE_DIR`,
  `SUPERVERSE_APPTEST_REPORT_DIR` and `DEX_CONNECTOR_MOCK_REPORT_DIR` (both the
  report directory, `report/` of the run by default, empty at the start of the
  run; the connector mocks write their calls there) and
  `SUPERVERSE_APPTEST_RUN` (the run directory).
- **Hold.** With `--hold-until FILE` (an absolute path), the run takes the
  lock and starts the engine as usual, then looks for FILE every 100 ms. Once
  FILE exists, the run removes it and runs the command, and its deadline starts
  again with the whole `--timeout`; `durationMs` then counts from the release.
  Until the release the deadline is `--hold-timeout` (default 15 minutes, at
  most 30), which also bounds the queue wait; a hold that passes it exits 124
  with phase `held` after its teardown. A caller that does not release kills
  the run's process group, which ends the engine with it.
- **Timeout.** At the deadline the command and every descendant get SIGTERM,
  then SIGKILL after 5 seconds; the run exits 124 after its teardown. Whenever
  the command ends, the processes it left in the run's process group that PID 1
  adopted, such as a server a test started in the background, are stopped the
  same way.
- **Run directories.** `/tmp/superverse-apptest/runs/<run-id>` holds
  `output.log` (the command's whole output; stdout shows its first 512 KiB),
  `summary.json`, `report/` and `logs/` (Temporal, dexcli and Dex Server). A
  passing run deletes its directory; a failing run keeps it, and each run that
  takes the lock keeps only the three newest failed directories (its own
  included) and removes directories without a summary older than two hours.
- **Teardown and summary.** Before the engine stops, the run counts the
  executions by Flow type and status (`dexcli flow search --all --no-hydrate`)
  and reads `flow state --no-hydrate` of up to 25 running executions: one whose
  active Step failed at least 3 attempts, or whose last failure is an SDK usage
  error (`*dex.AttributeMapNotLoadedError`, `*dex.ChannelMessagesNotLoadedError`
  or a panic), is unhealthy, with an excerpt of at most 1 KiB for up to 5. It
  merges the harness's `report/tests.json` (`{"tests":[{"name","outcome"}]}`)
  and the connector mocks' `report/connector-calls.json`
  (`{"calls":[{"connection","operation","branch",...}]}`, which the mocks of
  `sdkgo/connectormock` write themselves) and the `--- FAIL:` blocks of up to 6 failing tests from `output.log` into
  `summary.json`, stops Dex, then Temporal (SIGTERM, SIGKILL after 5 seconds),
  and prints `SUPERVERSE-APPTEST-SUMMARY <summary.json>` as its last line, at
  most 64 KiB.

`summary.json` is the contract with the commit gate
(`internal/dexai/agents/sandbox/workspace/git/commit_application_tests.go`):
`runId`, `directory`, `kept`, `phase` (`queued`, `engine`, `held` or `command`),
`exitCode`, `timedOut`, `durationMs`, `engineError`, `executions` (Flow type to
status to count), `executionsError`, `unhealthy` (`flowId`, `flowType`,
`status`, `step`, `attempt`, `usageError`, `failure`), `unhealthyTotal`,
`unchecked`, `tests`, `testFailures` (`name`, `output`) and `connectorCalls`.

## Ports

| Port | Bind | Owner |
| --- | --- | --- |
| 8801 | 127.0.0.1 and the Pod's addresses | Dex; the Sandbox Service port for the platform server |
| 8802 | 127.0.0.1 | Dex Web, never exposed |
| 7233, 7243, 7244 | 127.0.0.1 | Temporal gRPC, HTTP and metrics |
| 3000 | 0.0.0.0 | The Preview application, for the Preview route |
| 18811 | 127.0.0.1 | The Preview application's Worker |
| 18812 | 127.0.0.1 | Reserved: a local application run |
| 7235, 7245, 7246, 8805, 8806, 18813 | 127.0.0.1 | The application test engine (`apptest run`): Temporal gRPC, HTTP and metrics, Dex, Dex Web and the tests' Worker |

dexcli v1.6.2 binds Dex and Dex Web to one address, and its local connector
setup requires it to be loopback. The daemon therefore relays the Pod's own
addresses on 8801 to Dex on 127.0.0.1:8801; Linux allows a specific address
beside loopback on one port, but not the wildcard address. Temporal's
development server also listens on ephemeral loopback ports for its internal
services. A taken fixed port fails the environment start.

## State

`$S` (`/var/lib/superverse/preview`, the volume's `preview-state` subPath):

- `supervisor.lock`, held by the daemon for its lifetime;
- `owner.json`: the Pod UID and the identity (PID, start time, command line) of
  the daemon and every process group it started;
- `environment.json`: the engine's cluster ID, retention and versions, and the
  last clean stop;
- `temporal/temporal.db*`, `dex/blobs`, `dex/connectors`;
- `source/SHA/` with the marker `.superverse-source`, and `source.lock`;
- `logs/`: `supervisor.log`, `temporal.log`, `dexcli.log`, `build.log`,
  `app.log` and the Dex Server logs; a log above 100 MiB is copied to `.1` and
  truncated.

The daemon resumes in this order: it takes the state lock (held: it refuses);
stops the process groups `owner.json` records for this Pod after their identity
check, or takes over from another Pod; refuses `inconsistent` when the engine
store is missing while `environment.json` or Dex blobs remain, or the store's
cluster ID differs from the record, because a new store would make Dex delete
those blobs; refuses `full` below 512 MiB free; fails when a fixed port is
taken; then starts Temporal, sets the 720-hour retention and reads it back,
starts Dex and its relay, and checks Dex health. A Temporal or Dex that exits
is restarted up to 3 times within 10 minutes; one more exit fails the
environment.

The coding image keeps the real `dexcli` and `temporal` in
`/usr/local/libexec/superverse`; the PATH shims refuse `dexcli dev` and
`temporal server start-dev`, and pass only the read subcommands of `dexcli`
(`flow search|summary|state|history|inspect|watch`, `health`, `version`,
`visualize`) unless `--allow-mutation` is given.

## Verification

`apptest run` is verified by `TestApplicationTestEngineActualSandbox`
(`internal/dexai/agents/sandbox/workspace/sandboxd`) in a container of the coding
image that the test owns, and through the commit gate by
`TestApplicationSourceGateActualSandbox` ([runbook](../../../../internal/dexai/agents/README.md)).
The Preview commands have no integration test package; they are verified with
the real binaries in a container of the coding image (Part B test 1):

1. Build the image from the repository root as in
   [the coding image README](../../README.md#build), with a test tag.
2. Run it the way the profile runs the Pod: the image entrypoint (tini, then
   sandboxd with `--root-dir=/workspace --rest-port=8080 --grpc-port=9090`),
   `SUPERVERSE_PREVIEW_STATE_DIR=/var/lib/superverse/preview`,
   `SUPERVERSE_POD_UID`, and named volumes on `/workspace` and the state
   directory. Commit a fixture application in `/workspace` with a Dex Worker,
   a Flow and `/api/health`, plus commits that exit at startup, never pass
   the health probe and fail to build.
3. Check, with `docker exec` as UID 1000:
   - shims: `dexcli version`, `flow state` and `visualize` pass; `flow stop`
     needs `--allow-mutation`; `dexcli dev` and `temporal server start-dev`
     are refused, `dev` even with `--allow-mutation`;
   - a taken 8802 fails `ensure`; 20 concurrent `ensure` leave one daemon; a
     second `supervise` refuses;
   - listeners from `/proc/net/tcp*`: only sandboxd and the Pod-address relay
     beyond loopback; another container reaches Dex on `<container IP>:8801`
     but not Dex Web; the namespace retention reads back `2592000s`;
   - `source ensure` is idempotent, answers `source_missing` without deleting
     anything, and keeps workspace edits out of the tree;
   - `begin` runs the commit although the workspace was edited; the Flow
     started by the application completes and its payload is a Dex blob; a
     failed build leaves the old application serving; exit at startup and a
     never-healthy application answer `failed` with the redacted tail within
     the 5-minute budget, stop the group and leave the running and candidate
     trees only;
   - `environment stop` empties every process group and frees the lock;
     `ensure` resumes the same cluster ID with the Flow and its blob after a
     clean stop, after SIGKILL of the daemon (whose orphans the new daemon
     stops) and after SIGKILL of the daemon and its children; `shutdown`
     stops within its timeout; a container with another Pod UID on the same
     volumes takes over;
   - removing `temporal.db*` answers `inconsistent` and keeps the blobs; a
     state directory with less than 512 MiB free answers `full`.
4. Remove the containers and volumes the check created.

The Kind and hosted behavior of the profile (subPath ownership, the Service
port, gVisor flock and SQLite on the volume, ReadWriteOncePod) needs the Kind
scenarios of the Preview and the Session's reclamation and the AWS receipts of
Part B.

Preview writes one fixed `runtime.yaml` at `/tmp/superverse-app/runtime.yaml` and supervises separate `web` and `worker` process groups of the same binary. Both groups have durable owner identities for restart cleanup. Readiness requires web health and Worker readiness; execution stays push on `previewWorkerAddress` (`127.0.0.1:18811`). Stopping either failed generation reaps both groups before releasing the source tree.

`make check-test-lanes` pins the supervisor's `previewWorkerAddress` to the platform's `PreviewWorkerAddress` and requires the runtime writer to use that address for both bind and target.
