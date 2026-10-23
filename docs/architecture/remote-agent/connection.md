# Connection

A connection ties one Bridge, run by one Builder, to one Project's Session. This document owns
pairing, the connection credential, the remote agent endpoint the Bridge calls, activation and
takeover, liveness and revocation. The Session's side of a takeover (the epoch fence) is in
[executor](executor.md#stop-and-takeover); the Bridge's side is in [bridge](bridge.md).

## Ownership

| Fact | Owner |
|---|---|
| Who may connect (account, login, Project role) | PostgreSQL: `accounts`, `login_sessions`, the Project and member read model |
| The connection credential, its status and liveness | PostgreSQL: `remote_agent_connections` (server-written, this design) |
| Which connection may act now, and its epoch | `SessionAgentFlow` `Executor` |
| Codex's thread, history and private reasoning | The local provider on the Builder's machine |

Every request is authenticated through SQL, as AGENTS.md requires for authentication; the Flow
is read or written only by the RPCs that the request needs anyway, and those check the epoch.

## Pairing

1. A Builder chooses "Connect your local agent" in Studio. `RemoteAgentService.CreateRemoteAgentPairing`
   (through the BFF, with `CallerContext`) requires `CapabilityBuild` on the Project, reads the
   Session's current executor and epoch (an admission read before a write, which reads the Flow
   like every Agent command), and inserts a `PAIRING` row with the hash of a new pairing code and
   that epoch as `basis_epoch`. Studio shows the code (10 characters of Crockford base32, 50 bits),
   who is connected now, the endpoint URL and the warning of the [accepted risk](README.md#current-behavior).
2. The Builder gives the code interactively
   to `dexai connect` (never as a command-line argument, never logged).
3. The Bridge calls `POST /remote-agent/v1/pair` with the code. The server finds the row by the
   code's hash, requires it unexpired (10 minutes) and unused, and in one transaction clears the
   code hash, stores the hash of a new connection token and sets `PAIRED`. The token (32 random
   bytes, base64url) is returned once and stored by the Bridge in a 0600 file under its state
   directory.
4. The client creates a local agent session and supplies its scoped MCP server ([bridge](bridge.md#binding)),
   then activates.

## Activation and takeover

**The Session is the only authority on who the executor is.** PostgreSQL holds credentials only;
no row status says "active", so no SQL write can make or unmake an executor, and a late SQL write
can never undo a newer activation.

`POST /remote-agent/v1/activate` makes the connection the Session's executor:

1. Authenticate the token (below); the row must be `PAIRED`.
2. `SessionFlowService.ActivateRemoteAgent` calls the Session RPC `SetExecutor` with the connection
   ID, the owner `Actor`, the local agent binding and `BasisEpoch = basis_epoch` of the row. The Session
   admits it only when its epoch still equals that basis, that is when nobody connected or switched
   since this pairing was created, or as an idempotent repeat when this connection is already the
   executor. It then increases the epoch and interrupts a running turn
   ([executor](executor.md#stop-and-takeover)).
3. In that same commit, `SetExecutor` captures `AgentState.LastSequence` in
   `Executor.Remote.ActivationSequence` and returns it with the new epoch. A repeat
   returns the saved sequence even if later messages have been appended. The Bridge
   begins sync after this sequence, so a message sent as soon as Studio observes
   remote mode remains eligible for delivery.

An execution activated before `ActivationSequence` was introduced has no saved
value. Its repeat activation reads the current head through `ReadRemoteHead`;
the existing Bridge retains its already initialized delivery cursor on reconnect,
so this fallback does not move that cursor. A new activation always saves the
head, including when it is zero. A Bridge whose local cursor was lost while
using an older activation has only the historical head fallback and cannot
recover entries it did not previously deliver.

Consequences:

- **The activation's identity** is the connection ID, and its expected version is the basis epoch.
  A retried activation after a lost answer is an idempotent repeat. A delayed activation of a
  connection that has since been replaced is refused, because the epoch moved past its basis and it
  is not the executor, so an old Bridge can never take control back.
- **Two Builders activating at once** with the same basis: one commit wins and the other is refused
  `executor_changed`; Studio tells the loser who is connected and offers to pair again, which takes
  the new epoch as its basis.
- **A crash anywhere** in this sequence leaves either the old executor or the new one, never both,
  and the Bridge's retry converges.
- **Replaced connections**: their tokens still authenticate, but `sync` and `report` are refused
  `executor_changed` by the Session, and `activate` is refused for the stale basis. On that answer
  the Bridge calls `disconnect`, which revokes its own row. A replaced Bridge that never returns
  leaves a `PAIRED` row that can do nothing and is cleaned up as below.

Taking over needs no consent from the current connection: any Builder may activate their own Bridge,
and Studio's connect dialog names who is connected now and says their agent will stop. "Use the
Platform agent" (`RemoteAgentService.SetSessionExecutor`, mode `platform`, with the epoch Studio showed as
the basis) calls `SetExecutor` and suspends the current local connection without revoking
its credential. `Executor.Suspended` retains exactly that connection's bounded identity;
`Executor.Mode` remains the sole execution authority. Studio can resume that exact
connection through `SetSessionExecutor.connection_id` with its observed epoch, without
pairing again. Resume increments epoch/generation, resets report watermarks and captures
its activation head. The server reauthorizes both the selecting Builder and the connection's
original Builder/login, and refuses a saved Bridge build below the configured minimum.
A reconnecting `activate` only discovers suspended idle state; it cannot resume itself.
A different connection's activation clears suspension permanently.

## Authentication of each request

A Bridge request carries `Authorization: Bearer <token>`. The handler:

1. hashes the token (SHA-256) and reads, in one query, the row joined with its account, its login
   session and the Project access (`projectdao`'s authorization query, the same one
   `ProjectFlowClient.Authorize` uses);
2. requires the row `PAIRED` and unexpired, the account ACTIVE, the login session unexpired with
   its current auth epoch, and `CapabilityBuild` on the Project;
3. for `sync`, admits only the current or explicitly suspended connection; suspended sync
   returns fence/idle metadata, no work, and keeps the Bridge cursor unchanged. For `report`,
   admits only current remote mode, connection, epoch and generation. All idle reports and
   replaced-connection reads/reports are refused with `executor_changed`.

The token never outlives the login session it was paired under: logging out, a password change,
the login session expiring, losing the Builder role or the Project's deletion all make the next
request fail, and the Bridge then shows "Reconnect from Studio".

## The remote agent endpoint

A separate listener, `remoteAgent.listen`, serves only the Bridge protocol. It is outside the
Studio BFF and outside `superverse.proto`, carries no `CallerContext`, and authenticates with the
connection token: the same kind of exception as the [credential read endpoint](../../api/credential-read.md),
which AGENTS.md lists. Unlike that endpoint it is public in hosted environments, where the existing
load balancer routes `/remote-agent/` on the Studio host to the listener (`remoteAgent.listen`, port
8088). The local lanes bind loopback ports, as their other listeners do: Local Kind forwards
`127.0.0.1:28089` to the server's remote agent port (Kind has no Ingress controller), and Native binds
`26089` plus the instance's port offset. The contract will be
documented in `docs/api/remote-agent.md`, which the implementing pull requests create.

All requests are `POST` with a JSON body, unknown fields refused, and a body limit per request:

| Path | Body limit | Purpose |
|---|---|---|
| `/remote-agent/v1/pair` | 4 KiB | Redeem a pairing code: `{code, bridge: {version, os, arch}}` → `{connectionId, token, projectId, projectName}`. No bearer. |
| `/remote-agent/v1/activate` | 8 KiB | `{binding: {codexVersion, providerThreadId, hostLabel}}` → `{epoch, generation, lastSequence, tools}` (the remote tool set with its schemas) |
| `/remote-agent/v1/sync` | 4 KiB | Long poll, at most 25 s: `{epoch, after}` → the Session's `ReadRemote` answer (generation, entries after `after`, interrupt, pending question, tool set digest). `after` is the Bridge's own cursor; a sync never writes the Flow. |
| `/remote-agent/v1/report` | 512 KiB | One report `{epoch, generation, sequence, providerTurn, events | toolCall | turnCompleted}` → `{epoch, generation, reported, duplicate}`; a stale epoch or generation is 409 |
| `/remote-agent/v1/disconnect` | 1 KiB | Revoke this connection's row. The Session stays in remote mode and shows the agent offline. |
| `/remote-agent/v1/downloads/...` | — | `GET` the Bridge and plugin builds for this environment ([bridge](bridge.md#distribution)). No bearer. |

**What `sync` returns.** Only user-role input entries the Bridge did not report itself (Studio
messages, answers, notifications, Stop and turn-limit notices), with their text exactly as the model
reads it, which is the canonical sender envelope and so carries the author; and the `tool_result`
entries of this connection's own calls (`remote-<epoch>-<sequence>`). Never assistant entries,
thinking, system markers or platform tool arguments. An answer always holds the first entry after
the cursor in full and adds more only within a 1 MiB soft budget, so an entry larger than the budget
(an answer may be up to 1,000,000 bytes) is delivered alone and never blocks the cursor. Every entry
is bounded by its own admission limit; the server computes the worst encoded answer (one maximum
entry escaped twice, the capped state fields and the fixed fields), asserts it before writing, and
`bridgeapi.MaxSyncResponseBytes` (16 MiB) is at least that. The todo list, pending question and
requirements are each capped at 256 KiB when projected (a larger legacy value is cut with "[truncated:
open Studio for the full …]"; Studio keeps the full value) and are sent only when the digest of the
projected state differs from the `stateDigest` the Bridge echoes, so nothing durable is added for it.
The answer's `examinedThrough` is the highest sequence examined, filtered-out entries included; the
Bridge sends it back as `after`, so an answer with no eligible entry still advances and nothing
eligible is skipped. If entries after the cursor were already dropped by retention, the answer names
the dropped range and the Bridge tells Codex so before continuing.

`sync` reads the Session through `ReadRemote`; when nothing is new it waits on the Session's
`AgentActivity` Stream with the existing `SessionAgentFlowClient.ReadEvent` long poll, then reads
again before answering. In remote mode every change the Bridge must see (an appended input entry,
a recorded tool result, an interrupt) writes an activity event; the existing kinds cover tool
results and interrupts, and input consumption writes its existing consumption event. The Stream is only a wake-up: a lost event delays the Bridge to the end of its long poll. A pass
that loads entries holds its `maxConcurrentReads` slot through aggregation and the final socket write,
under a 35 s write deadline, and clears its payloads before it releases the slot; the long poll
between passes holds no slot and no payload, and a re-read takes a slot again (first come, first
served within the request's deadline, then 429). No other gate or setting bounds response residency;
the per-replica memory figures in [composition](composition.md) are estimates until measured.

Errors carry a status and a JSON body `{code, message, expectedSequence?, epoch?, generation?}` with
a typed code (`bridgeapi.ErrorCode`): 400 `malformed`, 401 `unauthorized` or `pairing_code_invalid`
(the Bridge stops and asks for reconnection), 403 `forbidden` (the Builder role is gone) or
`bridge_outdated`, 404 `not_found` (an unknown path, or a connection whose Project is gone or being
deleted; the Bridge stops as for 401), 409 `executor_changed`, `stale_generation` or `sequence_gap` (with the expected
sequence), 413 `too_large`, 429 `rate_limited` or `reports_pending` (the Bridge resends the same
report after backoff), 503 `unavailable`. Pairing codes are compared after removing spaces and
hyphens and upper-casing. `activate` also returns `reported` and the tool set's digest; the Bridge
fetches a changed tool set by activating again, which is idempotent for the current executor. Tools
carry no timeout. The Bridge relay owns the one wait budget, `toolWaitBudget` (1,770 s), which bounds how
long the Bridge holds any MCP tool call open so it answers before Codex's 1,800 s
`tool_timeout_sec`, a constant beside it. A call still unresolved at the end of the
budget is answered with a non-error result: the outcome is pending, the result will arrive as a
message, and the call must not be run again. Expiry never means the effect failed or was cancelled;
nothing is replayed and no new call ID is made; the canonical `tool_result` reaches Codex later
through the projection. Nothing promises that a call completes within the budget. The Bridge uses plain HTTP only for a loopback endpoint (the Native lane).

## Flow reads from outside the Flow

`sync` is a read of the Session from outside the Flow, which AGENTS.md otherwise sends to the SQL
read model. It is the same kind of read as Studio's `WatchSession` and is listed with it in the
[read model's exceptions](../teamspace/read-model.md#reads-that-still-go-to-a-flow):

- **Why the Flow**: the Bridge needs the conversation entries and the Stop state, which exist only
  in the Session, in the order they were committed, with the epoch fence.
- **Frequency and bound**: at most one active connection per Project, so concurrent `sync` loops
  equal the Projects with a connected agent. Each cycle is one `ReadRemote` and at most one
  `AgentActivity` long poll (30 s), the shape of one `WatchSession` connection without its
  snapshot rereads every 5 s. The `sync` handler reuses the Watch's per-connection pattern: it
  reauthenticates (SQL) on every cycle and ends on the first refusal.

## Studio API

A new business service, `RemoteAgentService` (through the BFF, with `CallerContext`; every RPC
requires `CapabilityBuild`), holds the browser's side of connections:

| RPC | Purpose |
|---|---|
| `CreateRemoteAgentPairing` | Create a pairing code (above); refused `connections_full` at the cap. |
| `ListRemoteAgentConnections` | The Project's connections for the dialog: owner, Bridge host and versions, status, created, last seen, whether it is the current executor. |
| `RevokeRemoteAgentConnection` | Remove a connection that is not the current executor. |
| `SetSessionExecutor` | "Use the Platform agent" with the epoch Studio showed as the basis. |

A separate service keeps the full `AgentService` (8 RPCs) for the Session's conversation commands
and gives the remote agent its own route, BFF allowlist entry and mock. The business protocol then
has 16 services.

## Liveness and revocation

- A request updates `last_seen_at` when it is older than 60 seconds. Studio shows the connection
  online while `last_seen_at` is within 90 seconds; `SessionFlowService.Snapshot` reads it with the
  executor's connection ID.
- Rows stay `PAIRED` on "Use the Platform agent". The Bridge remains authenticated and idle,
  and only Studio can resume it. Idle access lasts no longer than the remaining lifetime
  of its original seven-day issuing login; switching never renews that lifetime.
- Rows become `REVOKED` on the Bridge's disconnect (a replaced Bridge disconnects itself)
  and on a Builder's "Remove" in Studio. Logout, expiry, auth-epoch change, role loss,
  revocation, dexai exit and Project deletion still terminate access.
- **A fixed cap of six rows per Project**, enforced in the transaction that creates a pairing. A row
  lock cannot serialize this (an empty Project has no row to lock, and existing rows do not stop an
  insert), so the transaction first takes a transaction-scoped advisory lock on the Project,
  `pg_advisory_xact_lock(<constant for this table>, hashtext(project_id))`, which serializes every
  pairing of the Project whether it has zero rows or six and is released at commit. A hash collision
  between two Projects only serializes them. Under that lock it first deletes rows that can never act again,
  that is `REVOKED` rows and `PAIRING` rows past their code's expiry; then, if six rows remain, it
  refuses the new pairing with `connections_full`. No pairing ever evicts a `PAIRED` connection, so a
  stale read can never remove a connection that is activating. Studio's connect dialog then lists
  the Project's connections (owner, Bridge host, created, last seen, whether it is the current
  executor) with "Remove" on each one that is not the executor; removing the executor's connection
  is "Use the Platform agent" first.
- **"Remove" racing an activation.** `RevokeRemoteAgentConnection` reads the Session's executor and
  refuses the current one, but that read cannot stop the same connection's `activate` from
  committing right after it. The order makes the race harmless: the handler revokes the row first
  (the credential is the database's fact), then reads the executor again. If the connection became
  the executor in between, it calls `SetExecutor` with mode `platform` and that executor's epoch as
  the basis, which a newer activation would refuse, so it never undoes a different connection.
  Without that second step the outcome would still be safe: a revoked token fails authentication at
  every endpoint, so the connection can neither act nor report, and Studio shows the executor
  offline with "Use the Platform agent". An `activate` that authenticated before the revocation
  and commits after both of the handler's reads is closed from the other side: after `SetExecutor`
  commits, `activate` reads its own row again and, when it is `REVOKED` or gone (a pairing's cleanup deletes revoked rows), calls `SetExecutor` with
  mode `platform` and the epoch it just received as the basis, then answers 401. Only a crash or a failed call between
  those two steps leaves the offline residual described above (the handler then answers 401 without
  claiming the switch happened), and a Builder clears it with "Use the
  Platform agent". Revocation is a credential fact, not a fence: it refuses new requests and does
  not cancel work the Session already admitted. Each switch to `platform` increases the epoch and the
  generation, so later reports are refused, and a running remote turn ends by the
  [Stop fences](executor.md#stop-and-takeover). RAPAIR revokes only; RAEXEC, which adds
  `SetExecutor`, adds both rechecks and a Dex test whose activation commits after both reads of the
  revocation.
- A Project's deletion deletes its rows in `SessionFlowService.CleanupProjectSession`, in its existing
  reconciliation passes (also when the Session is missing or ended abnormally). A pass deletes only
  once the read model shows the Project `DELETING` or `DELETED`; while it still shows `READY` the pass
  reports itself incomplete and the existing retry runs it again, so the deletion is never
  acknowledged early. The delete and `CreateRemoteAgentPairing`'s transaction take the same
  per-Project advisory lock, and the pairing transaction re-reads the Project status, the login, the
  account and the role in SQL after taking it. So a pairing either commits before the delete and is
  deleted, or sees the Project no longer `READY` and is refused; `pair` only updates an existing row
  under its lock and cannot create one. No row outlives the Project, and no tombstone or Flow read per
  request is needed. Authentication refuses a Project that is `DELETING` or `DELETED`. No background
  job exists.

## Dex Flow Changes

`SessionAgentFlow` owns `SetExecutor`, `ReportRemote` and `ReadRemote`; their contract is in
[executor](executor.md#dex-flow-changes). `SetExecutor` now saves the admission
head inside its existing `Executor` Attribute; no new primitive is introduced.
No other Flow changes. ProjectFlow's deletion keeps its
single `CompleteProjectSession` call.

## Database Schema Changes

Migration `0007_remote_agent_connections.sql`:

```sql
CREATE TABLE remote_agent_connections (
  connection_id TEXT PRIMARY KEY,                 -- random, server-assigned
  project_id TEXT NOT NULL,                       -- no FK: projects is a Dex-synced table
  account_id TEXT NOT NULL REFERENCES accounts (account_id),
  login_session_id TEXT NOT NULL,                 -- checked by join; login sessions are deleted on expiry
  status TEXT NOT NULL CHECK (status IN ('PAIRING', 'PAIRED', 'REVOKED')),
  pairing_code_hash TEXT UNIQUE,                  -- cleared at redemption
  pairing_expires_at TIMESTAMPTZ NOT NULL,
  token_hash TEXT UNIQUE,                         -- set at redemption
  basis_epoch BIGINT NOT NULL,                    -- the Session epoch when the pairing was created
  binding JSONB,                                  -- Local agent version and provider thread ID; no secrets
  last_seen_at TIMESTAMPTZ,
  revoke_reason TEXT CHECK (revoke_reason IN ('platform', 'disconnected', 'removed')),
  created_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);
CREATE INDEX remote_agent_connections_project ON remote_agent_connections (project_id, status);
GRANT SELECT, INSERT, UPDATE, DELETE ON remote_agent_connections TO superverse_server;
```

- **Writer**: only `internal/dexai/agents/remoteagent` through `remoteagentdao`. Not a Dex-synced table.
- **Why a table and not the Flow**: authentication of every Bridge request must not read a Flow
  (AGENTS.md: authentication goes through SQL), and pairing exists before any Session write. The
  table holds credentials, their basis epoch and liveness only; the executor and the current epoch
  stay in the Flow, which alone decides who acts.
- **Migration**: additive, applied by `superverse-server migrate` like the others; no backfill.

## UI/UX

- **Connect dialog**: who is connected now (if anyone) and that their agent will stop; the
  accepted-risk warning; the code with a countdown and copy button; "Waiting for your Bridge…",
  then the connected local agent and host label.
- **Agent picker**: beside the composer, its local-agent level shows connection
  status, Connect and, while remote owns the Session, Use the Platform agent.
  The Connect dialog retains one-time code handling and saved connections.
- **Project creation**: a local-agent creation opens the Project as soon as creation is accepted without
  waiting for pairing. Until its first local connection, the chat shows inline
  instructions and risk acknowledgement, with a pairing code requested on demand
  and `dexai connect --endpoint <origin>`. Expired codes renew without losing the
  editable browser draft. Activation sends the current creation draft once;
  manual Send uses the same admission boundary. Example drafts remain manual.
- Errors in plain words: an expired code, a Bridge whose version Studio does not accept, the
  connection replaced by another Builder.

## Tests

- DAO integration tests on the lane's PostgreSQL: pairing redemption is single use under
  concurrency; hash lookups; the per-Project bound; deletion.
- Endpoint integration tests over HTTP against the lane: every status above; a logged-out or demoted
  Builder's token stops working on the next request; a revoked connection's sync and report are
  refused; activation retried after a lost response returns the same epoch; two Bridges activating
  at once with the same basis end with one executor, and the loser's retry, like a replaced
  connection's delayed activation, is refused; the cap on PostgreSQL: many concurrent first pairings
  of a Project with zero rows, and concurrent pairings at five rows, never exceed six; repeated
  pairings without redemption are refused at six; a full Project whose rows include an activating
  connection never loses it.

## Documentation

- `docs/api/remote-agent.md` (new, with the implementing pull requests): the endpoint contract.
- AGENTS.md, CLAUDE.md and `.cursor/rules/planning.mdc`: add the remote agent endpoint to the
  exception that today names only the credential read endpoint.
- `cmd/superverse-server/config/README.md`: the `remoteAgent` keys.
- The Kind chart, Native profile and EKS Ingress documentation: the new port and path.

## Resource Scalability

- **Rows**: at most six per Project at any time ([cap](#liveness-and-revocation)); deleted with the
  Project. At 10,000 Projects, at most 60,000 rows.
- **Hot path**: no control-plane call. Per request one indexed SQL authorization query; at most one
  `last_seen_at` update per connection per minute; `sync` makes one Dex read RPC and at most one Dex
  Stream long poll (30 s) per cycle; `report` one Dex write RPC.
- **Estimate (2026-10-12, not measured)**: an idle Bridge makes one `sync` cycle about every
  30 seconds, that is one Dex read RPC, one Dex Stream long poll and one SQL query, plus one row
  update per minute. 1,000 connected Bridges therefore cost about 67 Dex calls, 33 SQL queries and
  17 updates per second; 10,000 cost ten times that. The platform Dex runs one replica today (its
  Streams are in-process), so connected Bridges count against the same Dex capacity as open Studio
  Watches. **Acceptance target for RAEXEC**: a load test on Local Kind with 1,000 simulated Bridges
  idle and 50 in active turns (one report and one sync per second each) keeps the platform Dex's read
  latency p99 under 1 second and its CPU under 70%, and the Studio Watches running beside it
  unaffected. This is an estimate to confirm by measurement in a shared environment, not by a local
  load test (owner rule: scale is shown by design, and local lanes run normal tests only). No
  per-environment connection cap is configured; connections are bounded per Project (six rows) and
  per replica (below), and a cap is added only if a measurement shows the platform Dex needs one,
  until its Streams move to the shared Redis Stream store planned for production.
- **Per server replica**: at most `remoteAgent.maxLongPolls` (default 1,000) concurrent `sync`
  requests, at most `remoteAgent.maxConcurrentReports` (default 16) `report` requests (the slot is
  taken before the body is read, so a full replica answers 429 at once without decoding anything),
  and 4 reports in flight per connection; beyond any of these 429, and the Bridge backs off. `pair`
  has its own fixed cap of 16 requests per replica and `activate` with `disconnect` share another
  fixed cap of 16, separate from the report and sync gates so that traffic there can never starve
  them. Each gate is a non-blocking slot held by its request and released when it ends; a full gate
  answers 429 at once and no gate spawns a waiting goroutine.

### Switching back to a paired local agent

A resumed Bridge keeps its token, provider thread, history-import marker and delivery cursor.
It first reconciles an attempted provider submission from public history. Entries through
the captured resume head, including public Platform assistant text without thinking or tool
arguments, are delivered once as bounded historical context, never as new commands. Only
entries above that head are new inputs. The cursor advances through the historical interval
only with positive context-delivery proof; uncertainty retains the obligation and blocks
new delivery. The context is at most 128 KiB with shortened text and explicit retention/
truncation disclosure. Idle reads use the existing bounded long poll and no message-map
loads; they never invoke a model or extend the Sandbox deadline.

Studio starts a display turn at a delivered user message, an idle-consumed notification or a
typed executor boundary. A notification consumed during active work retains its card inside
that turn and shares its single Worked summary. The Session records that consumption fact as
`notification_continues_turn` on the canonical entry and projects it in `AgentMessage`, for both
platform and remote turns. Older entries lack the flag and retain their existing display boundaries.
Notification timing uses the entry's consumption timestamp, rather than the producer's occurrence
time or the preceding turn's end, so idle time before a notification-driven turn is not counted as work.
