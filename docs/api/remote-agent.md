# Remote agent connections

Bridge delivery reports are public durable Session facts: `delivery_failed` says the local agent did not receive the referenced message and the person should send it again; `delivery_uncertain` says it may have arrived; `delivery_delayed` is emitted once after two minutes while the Bridge continues its bounded retry. They name the originating entry sequence, are admitted through `/remote-agent/v1/report`, and append a visible notice to `CurrentMessages`/`ArchivedMessages`; they do not start the platform model in remote mode.

This contract covers the connection layer and its Studio executor integration. Pairing authorizes a Bridge
credential; it does not select, activate or run an executor. The Session alone
owns executor mode, epoch and current connection. The executor layer supplies activation,
takeover, sync, reports, cancellation and the remote execution branch separately.
The Bridge wire contract is generated neither from the business proto nor from
Studio: its single owner is `internal/dexai/agents/remoteagent/bridgeapi`.

## Authentication and admission

The Studio BFF exposes only `CreateRemoteAgentPairing`,
`ListRemoteAgentConnections`, `RevokeRemoteAgentConnection` and
`SetSessionExecutor`, each with its
own protobuf request/response and `CallerContext caller = 100`. The server
requires the current ACTIVE account's authenticated login and the Project's
`CapabilityBuild`, including the existing platform-admin rules. Studio cannot
supply a different issuing login ID. Creation records the Session's live epoch
as immutable `basis_epoch`. List returns safe connection metadata separately
from the Session's current connection and owner. Revocation reads the live
executor at admission and refuses its current connection; a stale list confers
no authority. The executor layer implements activation/revocation compensation fences
without moving authority into the credential table.

Pairing codes have ten uniformly random Crockford base32 characters (50 bits)
and expire ten minutes after creation. Input normalization removes ASCII spaces
and hyphens and uppercases before validating the alphabet. One transaction locks
the existing row, checks its expiry and current issuing login/Project access,
clears the pairing hash and records SHA-256 of a newly generated 32-byte token.
Only the successful response returns the canonical base64url token, once.
Codes, tokens and credential hashes never appear in connection-list DTOs,
receipts, logs or Git.

Every authenticated Bridge request hashes its token and performs one indexed
SQL join of the PAIRED row, ACTIVE account, issuing `login_sessions` row with
matching account/auth_epoch and unexpired expiry, Project and memberships.
Current `CapabilityBuild` uses the same owning Project rules as business APIs.
There is no Flow read on this authentication path and no application write to
Dex-owned projections. Logout, login expiry, auth_epoch change, revocation and
lost build capability invalidate the next request. Missing, DELETING and DELETED
Projects are `not_found`; role loss is `forbidden`; unavailable SQL or storage
is an availability error. Liveness writes are conditional and limited to one
per connection per 60 seconds. A separate exact scoped PAIRED timestamp read
may supply transient current-remote-executor display; missing/revoked/null rows
return no timestamp, and dependency errors propagate. It grants no authority
and creates no duplicate durable state.

## HTTP routes

The independent listener defaults to `:8088`; bootstrap owns its one server and
graceful drain. The browser continues to use the BFF. Hosted environments reuse
the existing Studio host, HTTPS ALB and server Ingress with `/remote-agent/`.
Kind uses the server Service and explicit loopback port-forward, without an
Ingress controller. Native uses the configured per-instance loopback port. The pairing response and
Connect dialog publish the origin only (scheme, host and port), without a path.
The Bridge appends each shared `bridgeapi.Path...` route to this origin.

| Exact route | Method | Contract |
| --- | --- | --- |
| `/remote-agent/v1/pair` | POST | No bearer; strict `{code,bridge:{version,os,arch}}`, at most 4 KiB. Returns `{connectionId,token,projectId,projectName}` once. |
| `/remote-agent/v1/activate` | POST | Authenticated `{binding}`; atomically selects this connection using its immutable pairing basis. Returns `{epoch,generation,lastSequence,reported,tools,toolSetDigest}`. `lastSequence` is the conversation head captured by `SetExecutor` in the activation commit, including zero; a repeat by the current executor returns the same activation head and current fence/tool catalog even after new messages. For an epoch activated before this field was stored, a repeat reads the current head; the existing Bridge retains its initialized cursor on reconnect. |
| `/remote-agent/v1/sync` | POST | Authenticated `{epoch,after,stateDigest,context?}`; reads canonical public Session entries and current typed state, then waits for activity when unchanged. `context: true` asks for `agentContext` and is answered at once, never held as a long poll: everything the platform tells the remote agent, built by `agentloop.RemoteAgentContext` from the same sources as the platform agent's prompt (the remote agent prompt with the message envelope, member and Dex skill rules, the platform tools the remote catalog lacks, the Session system prompt, the Session's `<repository_context>`, which names the Dex skill manifest and carries AGENTS.md, and after it, for a Project that started from an example, its `<starting_example>` section: a matched example's until the Session's first build, a picked example's while the Session awaits the person's first message, then its `<card_design>` section once the agent's cards carry no Flow design: for a Project that started from an example or holds a pushed, unblocked `commit_and_push` of the agent's), at most 80 KiB; it is outside the state digest. `agentContextState` is `pending` while the workspace request is in flight, `ready` when repository context was read, and `unavailable` after preparation or context reading failed. A Bridge waits at most three minutes for a pending brief, then sends it and later delivers one platform-provided `<repository_context>` section if the state becomes ready. MCP `instructions` are read once at startup. The state includes `settledThrough`: the highest report sequence of this connection and epoch whose outcome the Session durably recorded (the report consumed and, for a tool call, its canonical `tool_result` appended; a parallel wave settles at its join). It never decreases within an epoch, restarts at 0 in a new epoch and is part of the state digest. `reported` proves only admission; a Bridge reads an absent `settledThrough` (an older server) as 0. A projected tool result may carry `status`; its closed set is `waiting_for_user`, which the server sets only on `request_user_input`'s placeholder result (classified from the Session's typed result payload, `agentloop.ToolResultStatusWaitingForUser`): the question is open and its answer arrives as an input entry whose `answeredInputCallId` is the call, which is the call's actual result. `confirm_requirements`' result carries no status: it is the ordinary result `{"status":"recorded","version":N}` of a card the Session recorded, and the turn goes on. |
| `/remote-agent/v1/report` | POST | Authenticated epoch/generation/sequence-fenced public events, one tool call or turn completion. Admission is not tool completion. For the current connection and epoch, a sequence at or below `reported` answers `duplicate` before the generation is checked: an admitted number stays admitted after Stop, so `stale_generation` proves the report was never admitted and its sequence is free. |
| `/remote-agent/v1/disconnect` | POST | One canonical `Authorization: Bearer <token>` header, strict `{}` of at most 1 KiB; revokes exactly that credential. It does not switch executors. |
| `/remote-agent/v1/downloads/manifest` | GET | Public environment manifest; missing local builds produce an empty artifact list. |
| `/remote-agent/v1/downloads/<fixed-artifact>` | GET | Only a manifest-listed fixed build filename within the configured distribution root; no arbitrary filesystem access. |

The four allowed Bridge names remain `superverse-bridge-{darwin,linux}-{arm64,amd64}` for the existing download protocol;
the fifth allowed name is `superverse-orca-plugin.tgz`. `manifest.json` uses
`bridgeapi.DownloadsManifest` directly, is capped at 64 KiB and five artifacts,
and declares each artifact's version, OS/architecture, SHA-256 and fixed path.
The response uses the configured minimum version. Invalid manifests and escaping
symlinks are unavailable; missing artifacts are 404. Artifacts are regular files
opened under `os.Root`, and consumers verify the declared digest.

Unknown paths return JSON 404 `not_found`; wrong methods return JSON 405 with
`Allow`. Encoded paths and query parameters return 400. JSON must be one object,
with no unknown fields or trailing value. Oversize bodies return 413 `too_large`.
Errors use the shared `ErrorBody`, capped at 8 KiB:
`{code, message, expectedSequence?, epoch?, generation?}`. The optional fields
carry the expected report sequence and current Session fence when applicable.
Status mappings are 400 `malformed`, 401 `unauthorized`
or `pairing_code_invalid`, 403 `forbidden` or `bridge_outdated`, 404 `not_found`,
409 `executor_changed`, `stale_generation` or `sequence_gap`, 429
`rate_limited` or `reports_pending` (with `Retry-After`), and 503 `unavailable`.
Routing overrides the malformed-body status only for an unsupported method:
405 carries `malformed`, message `method not allowed`, and `Allow`; the shared
`ErrorCode.Status()` continues to map body-validation `malformed` to 400.
The connection layer emits `rate_limited` only from its independent fixed 16-slot pairing gate,
acquired before body/auth work without waiters and released at request end.
Executor/report-specific refusals belong to the executor routes when enabled.
Minimum Bridge version defaults to `0.1.0`; only explicit
local profiles accept the exact `0.0.0-dev` build. The executor layer implements
activate/sync/report; the connection layer alone does not establish execution.

## Credential storage

The additive migration `0007_remote_agent_connections.sql` creates only the
credential table owned by `remoteagentdao`. It records connection/project/account/
issuing-login IDs, status, unique nullable SHA-256 pairing/token hashes,
pairing expiry, nonnegative immutable basis epoch, optional binding/liveness/
revocation metadata, creation and revocation times. Status is the closed set
PAIRING/PAIRED/REVOKED, with state-shape constraints and fixed revocation reasons.
`account_id` references the authoritative account table; Project and login IDs
are checked by joins rather than foreign keys to a Dex projection or expiring
login. `(project_id,status)` bounds the list/cleanup path; unique hash indexes
serve pair/authentication lookup. Existing installations add this migration in
sequence; no destructive migration or background cleaner is introduced.

Creation and Project cleanup share a transaction-scoped advisory lock with a
private namespace and the Project ID hash, including an empty Project. After
locking, creation rereads current account/login/Project roles/status, cleans
REVOKED and expired PAIRING rows, then refuses a seventh remaining row. PAIRED
rows are never evicted. Hash collisions serialize only, without mixing scopes.
`SessionFlowService.CleanupProjectSession` idempotently deletes exact Project
credentials in its existing DELETING reconciliation passes, including missing
or abnormally ended Sessions. Cleanup cannot acknowledge a stale READY or missing
SQL Project projection: the existing Project retry observes it again. Redemption
only updates an existing locked row and cannot recreate one after deletion.

## Session and cleanup ownership

The existing `SessionAgentFlow` owns executor authority in its unindexed ordinary
`Executor` Attribute under `executorLock`. Absence in historical Sessions means
platform at epoch 0 without a write. `ReadExecutor(SessionScopeInput)` checks
exact Project identity, ACTIVE Session and execution liveness; every other error
propagates. Dialog open, explicit refresh and pairing/revocation admission may
use this read. SQL authentication and periodic liveness do not.

The executor layer adds remote branching, fenced report admission and bounded
handoff to the same Session. Its `RemoteInbox` Channel, existing interrupt/tool
admission fields, Steps, RPCs, persistence and recovery are owned by the
[Session inventory](../../internal/dexai/agents/session/README.md#executor-and-remote-turn-contract)
and [typed domain seam](../../internal/dexai/agents/remoteexec/README.md).
Connection rows authenticate credentials; they never confer executor authority.

`SetSessionExecutor` is a Builder-authorized BFF command with the exact observed
protobuf decimal `basisEpoch`; it returns execution to the platform, and a stale
CAS is refused. After a committed switch, `UsePlatform` revokes only the SQL
credential that exact switch replaced. Its next HTTP request therefore returns
401 `unauthorized`. A still-valid credential replaced by another remote activation
remains authenticated but receives 409 `executor_changed` from executor admission.
Revocation or Bridge disconnect is credential removal, not an implicit fallback.

ProjectFlow keeps its existing deletion Steps and retry contract; its Session
cleanup dependency adds an idempotent SQL effect described above, before existing
Session/workspace/Preview/resource reconciliation. There is no new completion
receipt, cleanup loop, tombstone or Flow RPC, and uncertain deletion is reconciled
by another existing pass. All other Flow primitives remain unchanged.

## Studio connection dialog

Studio's Connect dialog belongs to the Session orchestration layer; the shared
Agent UI remains transport-free. Builders acknowledge the approved warning:
connected Codex runs with the connecting Builder's local Orca permissions, and
any Builder's message or application failure text can reach it and ask it to run
something locally. The dialog shows endpoint, current owner when available,
one-time code/countdown/copy and safe saved connection metadata/removal. PAIRED
is visibly distinct from current executor. Current-executor removal is disabled
in the UI and checked by the server. Native modal focus/Escape behavior, request
cancellation and stale-response guards are retained. Project-keyed state resets
risk acknowledgement, codes and old owner/list display on a Project change;
code labels and owner-specific removal labels remain accessible. Codes are
hidden and their local timer stops at expiry. A countdown is local;
there is no periodic Flow-backed list polling. The Project's Builder permission controls actions in the Agent picker; server authorization
remains authoritative. The Agent picker's local level opens this single dialog and exposes
Use the Superverse agent only in remote mode. Switching is single-flight,
preserves the decimal epoch, and refreshes canonical Session state on admission
or refusal without retrying an uncertain command. Project, permission, Session
status or executor-fence changes cancel old switching work. Offline is a local
ninety-second last-seen display; it leaves remote authority and ordinary
send/steer/answer/Stop controls intact. Remote mode keeps the Agent picker
visible. Its Superverse level offers an epoch-fenced switch back before model
selection; the mock composer stays hidden. Public remote replies retain the
existing typed projection.

At Project creation, a local-agent choice omits the initial message and model
from CreateProject. Studio retains the first message in its Project intent,
opens the Project as soon as creation is accepted and shows inline connection instructions in its chat.
The description is an editable composer draft retained through browser reload.
Pairing codes are requested on demand and renewed after their ten-minute expiry.
A Session Snapshot with remote authority triggers one send of the current draft;
manual Send shares that boundary and neither path updates the platform model. A lost SendMessage response is uncertain because that RPC does not
deduplicate IDs; Studio does not resend it automatically.

## Verification scope

Real PostgreSQL tests cover concurrent first admission and five-row admission,
single-use concurrent redemption, hash clearing and basis preservation, expired/
revoked cleanup, paired retention, liveness write budget, exact nullable/scoped liveness reads,
admin rules, login/
auth_epoch/role/membership/revocation/Project invalidation, and actual lock-bound
creation/redemption races with deletion. Real gRPC/HTTP tests cover authenticated
pair/list/revoke/logout, canonical formatted code, typed cap refusal, strict
path/body/method constraints and actual filesystem downloads/
containment, and fixed pre-body pairing admission with no waiters. A Studio spec uses an explicit private disposable READY Project
fixture, real BFF and Bridge HTTP, acknowledgement/countdown/refresh/removal and
keyboard reopen behavior without recording credentials.

Both `connect-agent.spec.ts` and `executor.spec.ts` use the real lane: the first
checks pairing/removal without activation, the second activation/public replies/
observed-epoch takeover and exact credential refusal. Recordings are disabled.
These spec sources have not run on this integrated candidate.

Source and static checks are not runtime acceptance. Compile/exports, generated
drift, required affected package tests/specs, both Native and Kind lanes selected by
`make required-lanes` and the three standard E2E journeys remain required before Ready. Takeover, current-executor
revocation races and activation end-to-end belong to executor-layer coverage. The current
account API has no password-change operation; real SQL auth_epoch invalidation
covers the credential contract, not a nonexistent password-change journey.

## Related contracts

This document owns implemented connection behavior. Configuration is in the
[server reference](../../cmd/superverse-server/config/README.md); local and hosted
operation is in [the connection runbook](../runbooks/remote-agent.md). Component
READMEs describe ownership and the SQL/Session boundary. The independent
endpoint exception is specified by this API contract.
The separately reviewed design and Bridge/executor documents retain their owners.

## Resource bounds

The table adds at most six rows per Project: at 10,000 Projects, at most 60,000
credential rows before existing Project deletion removes them. Revoked/expired
pairings are bounded within that cap and cleaned on creation, never by a global
scan. Auth is a unique-hash lookup plus indexed identity joins; lists/cleanup
are exact Project scans of at most six rows. No control-plane request occurs
per pair/auth/list/liveness event. The singleton uses the existing bounded SQL
pool, no per-connection goroutine, pool, cache or cleaner. Pairing has a separate
fixed 16-slot per-replica gate with no waiters. Activate/disconnect share another
fixed 16-slot gate in the executor layer. Pair/disconnect work has a ten-second
request deadline; HTTP timeouts bound duration, not concurrency. Graceful shutdown
releases disposable connections. The executor layer uses `maxLongPolls` (default 1000,
range 1..10000 per replica, including its waiting queue) and
`maxConcurrentReads` (default 4, range 1..64 per replica, FIFO message-loading
slots released before an activity wait), and `maxConcurrentReports` (default 16, range 1..256 per replica,
fail-fast admission before body/auth, no waiters), with independent gates.
The retained inbox stays capped at eight reports / 2 MiB, and per-connection
in-flight reports at four per replica. A provisional three-copy estimate of
512 KiB request plus 2 MiB inbox and control/SDK copies is about 8 MiB per report
and 128 MiB at the default report cap. This is an estimate, not a proven worst-case
bound: stored payload/copy audit and measurement remain an acceptance gate.
The connection layer creates no long poll. These application limits are approved contract limits, not cloud
quota claims. The shared Service port/Ingress path adds no tenant/cloud resource,
LB, listener or certificate; distributions are fixed environment files.
