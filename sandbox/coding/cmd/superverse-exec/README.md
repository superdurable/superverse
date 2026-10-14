# superverse-exec

`superverse-exec` is the command ledger of one coding Sandbox. It starts each
command under its own detached supervisor, records it under a stable execution
ID in the Pod's `/tmp` and answers what became of it. A command therefore
survives the sandboxd call that started it, a Worker restart and a Step retry,
and a retried call asks the ledger instead of guessing. The platform starts
every recoverable command through it; the
[Sandbox contract](../../../../docs/architecture/agent-harness-design/sandbox.md#command-ledger)
owns the design and this README the exact command line and JSON.

The ledger is a reference for the Session, never a gate, and its confinement is
for isolation and correctness, not security: it runs as the same UID as the
commands it starts.

## Exit codes and answers

Every subcommand prints one JSON object on stdout.

- Exit code 0 is a definite answer about the execution ID.
- Exit code 1 is a usage, helper or transport error, `{"error": "..."}`. It says
  nothing about the command and may be retried.

An answer about a run has these fields; absent fields are omitted:

| Field | Meaning |
| --- | --- |
| `id` | The execution ID |
| `state` | `not_found`, `running`, `exited` or `lost` |
| `refusal`, `reason` | Set when `begin` or `background` refused the request; nothing was started or changed |
| `background` | The run is a background command |
| `startedAt`, `deadline`, `endedAt` | RFC 3339 times: the admission (the command's start once it exited), the end of its lifetime, its end |
| `exitCode` | The exit code of a command that exited normally |
| `signal` | The signal that ended the command's leader, such as `SIGKILL` |
| `endReason` | `exit`, `lifetime_limit`, `output_limit` or `stopped` |
| `output` | `{path, bytes}` of the merged output file, and the requested `projection` or `window` |

`output.projection` is `{head, tail, omittedBytes}`: at most `--head` bytes of
the beginning, ending after its last complete line, and at most `--tail` bytes
of the end, starting after its first line break. When the whole output fits,
`head` holds all of it and nothing is omitted. `output.window` is
`{from, next, data}`: at most `--max` bytes from the byte offset `--from`, cut
at a character boundary; `next` is the offset to read from next. Text is valid
UTF-8: invalid bytes become U+FFFD.

States: `exit.json` present is `exited`; the run's lock held is `running`;
neither is `lost` (the supervisor is gone without recording the end, for
example after it was killed or its container restarted); no admission record is
`not_found`, which includes every ID on a new Pod.

## Subcommands

### begin

```sh
superverse-exec begin --id ID --cwd DIR --lifetime DURATION [--not-before TIME] \
  [--background] [--wait DURATION] [--head N --tail N] -- ARGV...
```

Decides under the run's lock, which the started supervisor inherits and holds
until the command ended, so concurrent begins of one ID start one command:

1. A record with the same digest (SHA-256 of the argv, the working directory and
   the background flag): the current state, never a second start.
2. A record with another digest: the current state with refusal
   `execution_conflict`.
3. No record, `--not-before` given and PID 1 started after it: refusal
   `instance_replaced`. The caller passes the time of its first attempt from the
   second attempt on; an absent record on an instance that started later means
   an earlier instance may have run the command.
4. `--cwd` missing or not a directory: refusal `working_directory_unavailable`.
5. `--background` while four background commands run in the Sandbox: refusal
   `background_limit`.
6. Otherwise it writes the admission record and starts the supervisor.

`--lifetime` is required and at most `1h55m` (6,900 s); `--wait` holds the answer
until the command ends or the time passes, at most 25 s. A refusal has state
`not_found`, except `execution_conflict`, which reports the existing run.
Durations use Go syntax (`150s`, `5m`); `--not-before` is RFC 3339.

Every begin then collects garbage: finished runs that ended more than two hours
ago are removed, then the oldest finished runs until the ledger holds at most
256 MiB. A running run and the run of the calling begin are never removed.

### status

```sh
superverse-exec status --id ID [--wait DURATION] [--from OFFSET --max BYTES] [--head N --tail N]
```

The run's state, end and output view. `--wait` (at most 25 s) holds the answer
while the run is running.

### background

```sh
superverse-exec background --id ID --lifetime DURATION
```

Turns a running foreground command into a background command whose lifetime
ends `--lifetime` from now, and never more than 6,900 s after its admission.
Refusal `background_limit` when four background commands already run. A run
that is not running, or already in the background, is answered unchanged.

### stop

```sh
superverse-exec stop --id ID [--grace DURATION] [--head N --tail N]
```

Records a stop request, which the supervisor executes: SIGTERM to the process
group and SIGKILL after the grace period (default 10 s, at most 1 min). It
waits for the end, at most 25 s, and answers the final state with end reason
`stopped`. Stopping an exited or unknown ID answers its state; a `lost` run has
its recorded process group stopped directly. Repeating a stop is harmless.

## Limits

The supervisor enforces these itself; no Flow, user or agent has to kill a
process.

| Limit | Value | End reason |
| --- | --- | --- |
| Lifetime | `--lifetime`; the platform passes the tool timeout plus 30 s for a foreground command and 3,300 s by default for a background command; never more than 6,900 s | `lifetime_limit`: SIGTERM, SIGKILL after 10 s |
| Output | 16 MiB of merged stdout and stderr; the file keeps exactly the first 16 MiB | `output_limit`: SIGKILL |
| Background commands | 4 running per Sandbox, all agents together | refusal `background_limit` |
| Ledger | finished runs kept 2 hours; 256 MiB in total | garbage collection |

When the command's leader exits, members of its process group that still hold
the output open get one second, then the group is killed: a command cannot
leave processes behind.

## Files

`/tmp/superverse-exec/runs/<id>/`:

| File | Content |
| --- | --- |
| `admission.json` | The request (argv, working directory, background flag), its digest, the admission time and deadline, the supervisor's and the command leader's process identities (PID, start time, command line) and a stop request |
| `lock` | Held by the deciding begin and then by the supervisor until the end |
| `output.log` | stdout and stderr merged in output order, one offset space |
| `exit.json` | Written once, atomically, at the end: start and end times, exit code or signal, end reason, output bytes |

`/tmp/superverse-exec/admission.lock` serializes every rewrite of an admission
record and the background limit. The command runs with the environment of the
calling `begin`, stdin from `/dev/null` and its own process group; the
supervisor runs in its own session, outside sandboxd's process group.

## Verification

`TestCommandLedgerActualSandbox` in
`internal/agents/sandbox/workspace/sandboxd` drives this command through the real
sandboxd of a coding image container: one start for concurrent begins, offset
windows, the lifetime and output limits, stop, garbage collection, `lost` after
a container restart and `instance_replaced` on a new container. The behavior of
`Setsid`, `flock` and the PID 1 start time under gVisor is proven only on AWS.
