# Session

The Session is the Project's one long-lived coding conversation. It is a single Dex Flow,
`SessionAgentFlow` (`internal/agents/session/session_agent_flow.go`), that owns the
conversation, its turns, the questions and the requirements card the agent puts to the
user, the Stop button, automatic turns, and the request side of the Sandbox instance the
agent works in: it asks for an instance, holds the accepted lease and the Sandbox deadline,
and decides when the instance is idle, while its `SessionWorkspaceFlow` provisions and
reclaims the instances. This document owns those behaviors and the complete Flow contract.

Other topics own what the Session uses:

| Subject | Owner |
|---|---|
| Model calls, entries, segments, compaction, effort, caching | [context management](context-management.md) |
| Tool catalog, routing, serial and parallel waves, failure ownership | [tool orchestration](tool-orchestration.md) |
| The workspace, provisioning and Preview Flows, the lease, command ledger, reclamation | [sandbox](sandbox.md) |
| Subagents and their worktrees | [subagents](subagents.md) |
| Notification path, delivery feedback, Preview auto-deploy | [notifications and feedback](notifications-and-feedback.md) |
| Streams, Snapshot projection, Studio views | [streaming and Studio](streaming-and-studio.md) |
| Wiring of Flows, clients, bootstraps and Services | [composition](composition.md) |

## Identity and lifecycle

### One Session per Project

- FlowID `session-<projectID>`, for example `session-a7k2`. The Project ID is the
  server-assigned business identity; no Session ID exists in the API, Attributes, labels,
  storage keys or Sandbox identity.
- ProjectFlow creates the Session. When Project setup commits the Project to READY, it
  schedules the Step `StartProjectSession`, whose Execute calls
  `SessionLifecycle.StartForProject`, which calls `SessionAgentFlowClient.StartAccepted`.
  There is exactly one creation trigger and it sits inside a durable Execute, as the
  [StartFlow admission](../start-flow-admission.md) rules require. No API start, ensure-on-demand
  path or "Session not started" state exists.
- Start options: Start RequestID `flowid.Fingerprint(struct{ProjectID string})`, the complete
  logical input; `dex.IDReuseDisallow` with same-request `AlreadyStarted.IgnoreError`; no
  Flow timeout; the ASYNC durability override. The server-selected profile, default model,
  prompt and HEAD stay outside the RequestID. The start has no input: it installs the identity
  and lifecycle Attributes (`ProjectID`, `Profile`, the AppManifest commit as `HeadCommit`,
  `StartIdentity`, ACTIVE, `SessionModel` with the default model) atomically with StartFlow.
  Studio applies the model the user picked on the creation page (`UpdateSessionModel`, free
  before the first message) and sends the creation intent as the first `SendMessage` (see
  [UI/UX](#uiux)).
- A retried or replayed `StartProjectSession` joins the running execution. The FlowID is never
  started again: the Step runs only on the READY transition, and Project IDs are never
  reused. No explicit rerun exists. Only an authorized `dexcli flow time-travel` runs a
  stopped `session-<p>` again (see [stopped-engine detection](#stopped-engine-detection)).
- Start failure. `StartProjectSession` uses ProjectFlow's provider Step options (SYNC, 30 s
  attempts, retried for 2 minutes). The Step makes one idempotent StartFlow
  call, so a failure that outlasts the window is an outage or a defect. Exhausted retries reach
  `RecordProjectSessionStartFailure`, which records `LastFailure` `SESSION_START_FAILED`; the
  Project stays READY. The Session's own `RegisterChildFlow` clears the failure, because a start
  whose response was lost still admits itself.

### Admission

The first Step, `InitializeSessionAgent`, calls `SessionAgentFlowDependency.AdmitSession` in a SYNC
Step under the SessionStatus lock. That registers the existing execution with ProjectFlow as
its only Session-scoped child; the ChildFlows key is exactly `session-<projectID>`. A Project
that has begun deletion denies it: the Session is archived with `admission_denied` and completes
the engine execution with a graceful receipt. Transport errors remain errors. Session start
performs no Sandbox RPC, StartFlow or Sandbox read; the Sandbox starts on demand
([sandbox](sandbox.md)).

### Never expires; closes only with the Project

- The Session has no Flow timeout and no inactivity close. Idle time, a reclaimed Sandbox and a
  failed model call never end it.
- The only close is the deletion of its Project by the user. No Session-level stop API exists in
  the business API, and the Session has no close tail of its own. `CleanupProjectSession` (called
  by ProjectFlow for a DELETING Project) calls the Session RPC `CompleteProjectSession` once. The
  RPC archives the Session (status ARCHIVED, close reason `project_cleanup_complete`), cancels
  every Agent branch, a workspace command in flight and the deadline wait included, publishes the
  stop of each running background command's watch (`BackgroundCommandStops`) and moves to the
  Step `CompleteProjectSession`, which completes the engine execution gracefully with the cleanup
  receipt once every background command's wait ended. A repeated call is accepted without effect.
  A Session of a deleting Project, or one denied at admission, is the only Session whose engine
  execution completes.
- The Session releases no resource on its way out; the deleting Project does. After the
  completion it ends the `SessionWorkspaceFlow` (which cancels a provisioning or reclamation in
  progress) and waits for a provisioning that may still run, stops the Session's subagents and
  waits for their ends (they are no Project children, so it finds them by their FlowType and the
  indexed `ProjectID` and status), closes the Session's connector contributions, ends the
  PreviewFlow and releases the Session's Pod, Preview route objects, grants and data, volume and
  isolation boundary by label ([composition](composition.md#reads-preview-configuration-and-deletion)).
  The receipt therefore proves that the Session ended, not that its resources are gone; the
  Project's pass checks both before it answers.
- Project cleanup calls the completion RPC with at most two seconds of lock wait. A call cut off by
  that wait, cancelled or unavailable while the cleanup round still runs leaves the Session pending,
  and the next round reads the Session's state before calling again; only a passed cleanup limit
  fails the deletion.

### Business completion, archival and engine completion

| State | Meaning | Retention consequence |
|---|---|---|
| ACTIVE | Conversation open | State retained for the Session's lifetime |
| ARCHIVED | Business completion: a read-only entity that accepts no command, set by `CompleteProjectSession` or by an `admission_denied` start | Retained reads of the completed execution last for the engine's retention period |
| Engine completed | Right after `CompleteProjectSession` archived the Session (the Step writes the cleanup receipt), or after `admission_denied` | Same |

An engine termination by failure, cancellation or operator termination is none of these: it
leaves the retained status ACTIVE (see [stopped-engine detection](#stopped-engine-detection)).

### Stopped-engine detection

A defect or an operator stop can end the engine execution while the retained `SessionStatus`
still reads ACTIVE. The Session never reports such a Session as running.

1. `SessionFlowService.readSummary`, the read behind every Session authorization
   (`AuthorizeSession`, every Agent command, the archive read), uses
   `SessionAgentFlowClient.GetLiveSessionSummary`. When the retained status is ACTIVE it joins a
   bounded `GetFlowSummary` of the exact FlowID and type.
2. For a terminal engine every such operation, and `RenewWorkspace`, fails with
   `FAILED_PRECONDITION` and the ErrorInfo reason `SESSION_EXECUTION_FAILED`,
   `SESSION_EXECUTION_CANCELED` or `SESSION_EXECUTION_TERMINATED`: no engine text, no RunID. A
   command that races the end of the execution (`FlowNotActiveOrNotFoundError`) maps to the same
   status, not NotFound.
3. `GetSnapshot` and Watch keep joining engine status and report `SESSION_STATUS_FAILED` with
   interaction disabled and the conversation preserved. It never claims ARCHIVED or completed
   cleanup. Unavailable liveness reads fail explicitly; they never manufacture running or
   completed state.
4. The plain `GetSessionSummary` stays for internal retained-state reads: `CleanupProjectSession`
   and `ReadPreviewTarget`. Preview configuration reads only stored state, so Session liveness
   does not gate it ([composition](composition.md#reads-preview-configuration-and-deletion)).
5. The instance Flows apply the same rule: a provisioning whose engine execution stopped before it
   decided its outcome reads as failed `WORKSPACE_EXECUTION_STOPPED`, which the workspace Flow
   settles when its bounded wait for the report ends, and a stopped Preview execution reads with `PREVIEW_EXECUTION_STOPPED`
   ([sandbox](sandbox.md)).

Recovery is an authorized `dexcli flow time-travel` to the last `AwaitUser-<n>` boundary after the
defect is fixed and the external effects of the Steps that will run again are reconciled; the
default replay keeps later user messages and workspace outcome reports. The runbook is in
rollout reconciliation. Once the engine no longer
retains the history, the Project must be recreated.

### Model and compaction failures never end the Session

`CallModel` and `CompactContext` route Execute failure to `RecordModelFailure` and
`RecordCompactionFailure`. Each records one bounded turn failure (activity and `AgentState`) and
ends the turn through `CheckSteered(ContinueAwaitUser)` like every other turn end. History and
queues are kept and the next user message retries. Non-retryable provider errors end the turn the
same way. Classification and retry budgets are in [context management](context-management.md).

### State retained for months

Dex retains every Attribute, AttributeMap instance and pending message for the Flow's lifetime,
across Worker restarts, retries, recovery and the several Runs one FlowID's execution may span.
RunIDs are diagnostic only. Content is bounded: fixed singletons, `CurrentMessages` with fewer than 20
instances, `ArchivedMessages` inside a 1,000-message window, and the capped task maps below.
Entries carry Claude blocks (signed thinking adds a few KiB per assistant entry); the largest
payloads go to the Dex blob store. Streams add nothing per Session. If the measured Snapshot read
becomes too slow because of those blocks, the thinking blocks move to their own AttributeMap that
the Snapshot does not load.

## Turns

### What a turn is

A turn is the Agent work that one input starts: a queued message, steered messages, the answers to
a question, or an automatic turn opened by a notification. It ends when the Agent waits for the
user again. `AgentState.Turn` increases by one at every turn start. The counters that bound a turn
(the automatic-turn counters and `SubagentsStartedWithoutUser`) are described below.

The `AgentState.Status` values are `initializing`, `waiting_for_message`, `compacting_context`,
`calling_model`, `routing_tool`, `waiting_for_tool_approval`, `executing_tool` and
`waiting_for_timer`. `AgentState.Running{Kind, CallID,
...}` names the interruptible work in flight (`model`, `tool`, `wave`, `workspace` or `remote`); a Step sets it
when it schedules the work and clears it when the work ends.

### Step graph

```text
InitializeSessionAgent -> ActivateSession -> AwaitUser

AwaitUser -> CheckSteered -> CompactContext -> CallModel
  CompactContext -> RecordPlatformModelResult -> CheckSteered ; failure -> RecordCompactionFailure -> CheckSteered(await_user)
  CallModel -> RecordPlatformModelResult -> CheckSteered -> RouteTool ; failure -> RecordModelFailure -> CheckSteered(await_user)
    RouteTool -> AwaitParallelToolResults + ExecuteParallelTool x N -> CheckSteered
    RouteTool -> CheckSteered -> ExecuteTool -> CheckSteered ; failure -> RecordToolFailure
    RouteTool(request_user_input, confirm_requirements) -> CheckSteered(await_user)
    ExecuteParallelTool failure -> RecoverParallelToolExecution
    ExecuteTool(bash) -> SettleForegroundCommand on timeout or retry exhaustion

AwaitUser -> AnsweredInput -> CallModel          (platform answers start the turn directly)
AwaitUser | AnsweredInput | CheckSteered -> AwaitRemoteAgent (remote mode before model/workspace planning)
AwaitRemoteAgent -> CheckSteered -> RouteTool | AwaitUser
RPC SetExecutor -> RemoteInbox(ExecutorChanged) -> AwaitUser | AwaitRemoteAgent
RPC ReportRemote -> RemoteInbox(Report) -> AwaitUser | AwaitRemoteAgent
CheckSteered -> AwaitAgentTimer -> CheckSteered  (start_timer)
CheckSteered -> AwaitToolApproval -> CheckSteered

G1 prompt / G2 dispatch / G3 lost transport / RPC RequestSessionWorkspace
  -> RequestSessionWorkspace -> SendWorkspaceCommand(provision) -> AwaitSessionWorkspace
  -> ApplySessionWorkspace -> the returning continuation via CheckSteered
  ApplySessionWorkspace(READY) arms AwaitSandboxDeadline -> SendWorkspaceCommand(reclaim)
  Command refused or retries exhausted -> RecordWorkspaceFailure
  (the SessionWorkspaceFlow provisions and reclaims; see sandbox.md)

RPC InterruptAgent -> InterruptAgent -> FinishInterrupt -> AwaitUser   (FinishInterrupt ends the turn only if the Stop cancelled its work)
ExecuteTool | SettleForegroundCommand (bash background) -> CheckSteered + AwaitBackgroundCommand
  AwaitBackgroundCommand waits for its BackgroundCommandFlow SubFlow or its stop; retries exhausted -> AbandonBackgroundCommand
ExecuteTool(recorded verified push) -> CheckSteered + DeployPreviewHandoff (both executor modes)
  DeployPreviewHandoff retries exhausted -> AbandonPreviewHandoff
ExecuteTool | SettleForegroundCommand | RecordToolFailure | AwaitParallelToolResults | CallModel
  (a platform failure) -> CheckSteered + ReportPlatformIssue  (side branch)
  ReportPlatformIssue start retries exhausted -> AbandonPlatformIssue

RPC CompleteProjectSession -> CompleteProjectSession                (Project deletion)
```

`CheckSteered` is the one boundary Step between model calls, tool waves and turn ends. In one
commit it applies the Stop request, steering, notification delivery, the compaction and rollover
decision, and the workspace gates; every other
Step hands over to it. That is what lets a single place enforce the append-only context rules
([context management](context-management.md#segments)).

### Actors

`conversation.Actor{AccountID, Email}` is a frozen authenticated author. The API authenticates an
ACTIVE account; `SessionFlowService.AuthorizeSession` constructs `SessionCaller{FlowID, Actor,
Role}` from that account and the current SQL authorization for `CapabilityBuild`. The platform
administrator acts as OWNER. No browser request supplies an author. Operators remain forbidden.

User messages and answers retain `UserMessage.Actor` through their Channels and immutable history
entries (`Meta.Actor`). Steering preserves the author and message ID and records a separate
`PendingUserMessage.SteeredBy`, retained as `Meta.SteeredBy` on consumption. Requirements
confirmations record `ConfirmedBy`; model selections record `ChangedBy`; the auto-deploy toggle
records `PreviewAutoDeployChangedBy` in the same locked commit as its value. A no-op toggle or
acknowledged model-revision retry preserves the previous changer. The first Stop of a turn keeps
its actor through cancellation or boundary consumption and records it on the Stop marker.
Automatic platform messages have no invented author; missing historical fields stay absent.
Preview commands retain their existing `ActorAccountID` ownership without another requester field.

Each attributed user entry stores one immutable text block: `Superverse message (JSON): ` plus
JSON encoding of `{sender, via, text}`. Only the outer `sender` names the author; `via` is null for
Studio. A full apparent envelope in the body remains escaped text. `Entry.PublicMessage` decodes the outer
text field for the raw public projection and returns the actor, never the envelope. Authorless legacy
entries retain their stored blocks. The current prompt describes this structure and equal
collaborator treatment; existing segments roll over at a turn boundary when their prompt digest
changes, preserving the frozen old segment. Tests prove the request shape, not model immunity.

### Queued and steered messages

- `SendMessage` publishes a validated `PendingUserMessage` to `QueuedUserMessages`. The Agent
  client creates the application message ID once before its RPC retry loop; Snapshot returns the
  ID so edit, delete and steer address the exact payload. `SendMessage` is refused while a
  question awaits its answer.
- `SteerMessage` moves one queued message, by ID, to `SteeredUserMessages`, preserving the ID; a
  repeated or stale ID is rejected. `DeleteQueuedMessage{MessageID, Actor, IsOwner}` checks and
  deletes under the transactional queue RPC lock. Only the author or an OWNER (including the
  platform admin) may delete; historical authorless entries require an owner. `{Deleted, NotAuthor}`
  distinguishes stale identity from an authorization refusal, which the API returns as
  `PERMISSION_DENIED` with `AGENT_COMMAND_NOT_ADMITTED`. This never authorizes an uncertain retry.
- `AwaitUser` consumes the Channels in this order: answers to a pending question, steered
  messages, one queued message, and last the notifications. With a question pending it waits only
  for the answer.
- Steering is consumed only at safe Step boundaries (`CheckSteered`, `AwaitAgentTimer`,
  `AwaitToolApproval`) and starts a new turn there. It never claims to cancel an in-flight model
  call or tool; the Stop button does that. A steered turn interrupts the calls that follow the
  current batch with the result `superseded_by_steered_user_message`.
- Notification entries delivered at a boundary precede the steering text in the log.
- Commands are not deduplicated beyond message IDs: a retry after a lost response may apply twice,
  so the caller reads the Snapshot to reconcile.

### Questions

- `request_user_input` stores `PendingUserInput{Kind: "", CallID, Questions}` and ends the turn.
  The answer is the next input. The turn ends through `CheckSteered(await_user)` like every other
  turn end, so steering that is already pending when a question (or the requirements card) is
  asked overrides it: the steered turn deletes the pending input and the steering text starts the
  next turn.
- `AnswerQuestions` verifies the exact pending call ID and every question ID. In one commit it
  deletes `PendingUserInput` and publishes the validated answer to `AnsweredUserInputs`. `AwaitUser`
  waits only on that Channel while a question is pending and checks for an already published
  answer before the Attribute; it appends the answer to `CurrentMessages`, writes a content-free
  `user_input_answered` activity and enters `AnsweredInput`, which starts the answer turn without
  looking at either queue.
- `PendingUserInput.Kind` is `""` (a `request_user_input` batch) or `requirements`.
  The `WaitingInputRound` watermark advances at each durable wait so that Studio can tell a new
  question from a stale one; it stays within JavaScript's safe-integer range.
- Prompts reserve questions for facts only the user owns and never for routine confirmation.

### Requirements confirmation

The requirements card is the one approval gate before the agent builds. There is no mode switch,
no "execute plan" command and no forced tool call.

- The main agent calls `confirm_requirements` (a non-strict schema the decoder validates; subagents do not have it). Input:
  `title`, `summary`, `features[{id: "R1", text}]` (at most 20), `connections[{need, connector,
  operations[], status existing or new, user_setup}]`, `user_setup[]`, `acceptance[]` and, for a
  card that adds, removes or changes Flows, the optional `design`
  ([design before implementation](../design-first/flow-design.md)).
- The effect follows the question pattern: `PendingUserInput{Kind: requirements, Requirements}` is
  written and the turn ends. The version the confirmation will have is the stored
  `ConfirmedRequirements.Revision` plus one. A card with a design writes its body to the
  `FlowDesigns` instance `pending` in the same commit, and `Requirements.Design` carries its
  reference `{Digest, Summary, Outline}`; `flowdesign` validates it first.
- Studio shows the requirements card: features, connections (existing and new), what the user must
  prepare, and acceptance. Two actions: confirm and start building, or request changes. Confirming
  answers with the user message "Requirements confirmed (version N)"; requesting changes answers
  with the user's text.
- On confirmation `AnswerQuestions` writes `ConfirmedRequirements{Revision, Spec, ConfirmedAt,
  LastFeatureNumber}`, in the same commit that publishes the answer. `SessionAgentFlow` is its only owner. It is shown in
  Snapshot, carried into every `segment_start` entry, and its numbering (R1, R2...) is the one the
  `commit_and_push` `requirements` map is checked against
  ([commit gate and application tests](commit-gate-and-app-tests.md)).
- **Flow design.** A confirmation copies the `pending` design to `confirmed` after checking its
  digest against the card's reference. A card without a design carries the confirmed design
  forward, and one that replaces the requirements without a design deletes it. The commit of a
  pushed, unblocked `commit_and_push` result writes `BuiltFlowDesign`, the digest of the confirmed
  design it built, and `BuiltFlowDesignCommit`, the commit it verified; the Snapshot's
  `ConfirmedDesignBuilt` compares the digests, and `ConfirmedDesignBuiltCommit` carries the commit
  while the design is built, so Studio knows which push's processes replace the design. The outline is in the
  confirmed requirements, so every `segment_start` carries it without a map load
  ([build completion](../design-first/flow-design.md#build-completion)).
- Rules in the prompt: confirm once before writing code for a new application or a large change;
  make small changes directly; never ask again for the same version; the connections part comes
  from the connector subagent's selection report ([connectors](connectors.md)); when several
  connections are possible, ask with `request_user_input` first.
- **Large and small changes.** The confirmed card is the boundary, so the confirmed requirements
  always match the application.
  - A large change would alter the confirmed requirements: add, remove or change a feature, add or
    replace a connection, change what the user prepares or the acceptance, or build any feature while
    nothing is confirmed. It gets a revised card that keeps the confirmed features' numbers and
    continues them; confirming it makes the next version.
  - A small change leaves the confirmed requirements as they are (wording, styling, layout, a fix that
    restores confirmed behavior, a refactor, finishing a confirmed feature) and is made directly.
  - A completely different request gets a card that sets `replaces_requirements`: it replaces the
    requirements and numbers its features from R1 again.
- **Numbering.** Feature IDs are increasing `R<n>`, and a removed feature's number is never reused:
  the server refuses a card that breaks either rule, and keeps the highest number used in
  `ConfirmedRequirements.LastFeatureNumber`. A dropped feature that comes back gets the next free
  number.

### Progress list

`write_todos` is the agent's own progress list, never an approval gate. `TodoList{Revision, Items[{Content,
Status}]}` is replaced as a whole, has no draft, execution or approval state, and uses business
language. Studio shows one line, for example "Progress 3/7: generating the summary".

### Stop button

The Stop button stops everything: the main agent's current turn, every running subagent and every
background command. There is no per-task stop button; the model can still stop one task with
`stop_task` (for example when the user says "stop doing X").

State: `AgentState.Turn`, `AgentState.Running` and the Attribute `AgentInterrupt{Turn, RequestedAt,
EndsTurn, Actor, MarkerWritten}`, where `EndsTurn` says that the Stop cancelled the turn's work and so ends the turn at once.

`InterruptAgent{Turn}` (the RPC, called by Studio):

- Answers false when the Session is not ACTIVE, when no turn and no task is running, or when the
  request names a different `Turn` than the running one (stale-request protection). A task counts as
  running while it is a running background command or an unfinished subagent, a lost one excluded.
- The RPC locks only SessionStatus and `AgentInterrupt`. It never waits on `MessageWindowLock`,
  because a running model call holds it. It loads the task records.
- A repeated Stop joins any pending Stop of the current turn, including a non-ending Stop
  during an uninterruptible tool. The first accepted actor stays unchanged; no task can be
  admitted while that Stop is pending.
- When the main agent runs an interruptible model call, compaction, tool or wave, the RPC cancels
  `CallModel`, `CompactContext`, `ExecuteTool`, `ExecuteParallelTool`,
  `RecoverParallelToolExecution` and `AwaitParallelToolResults`, records `AgentInterrupt` with
  `EndsTurn` and schedules the Step `InterruptAgent`.
- When the running tool cannot be interrupted (`commit_and_push`, `start_agent`,
  `merge_agent_changes`), or no turn runs, the RPC records `AgentInterrupt` and schedules
  `InterruptAgent` to stop the tasks; the turn ends at the next boundary, where `CheckSteered` sees
  the matching `AgentInterrupt`.

The Step `InterruptAgent` holds no lock and is idempotent. Its external calls (at most 1 + 4 + 3, each
idempotent by identity) are, in order:

1. The foreground command's execution ledger `stop --id`, when the Stop ends a turn that runs a `bash`
   call.
2. `stop --id` for every running `BackgroundCommands` record on the current instance; its watch sees
   the command stopped, and `AwaitBackgroundCommand` records it `stopped` with no notification.
3. `StopSubagent` through the Service for every RUNNING `Subagents` record. A stopped subagent saves
   its work and its STOPPED report returns as a notification.

It tries every one of them before it reports a failed one, so a task that cannot be stopped, for example
a subagent whose status lock a lifecycle Step held past the client's wait, leaves no command running, and
the retry repeats them all. If its retries run out, `FinishInterrupt` still commits the Stop and its
activity says why tasks may still run, so a Stop always ends the turn. The Step `FinishInterrupt` commits
the Stop under `MessageWindowLock` and the AgentInterrupt lock. Its Step input carries the
admitted `{Turn, RequestedAt}`; settlement and STARTING-subagent cleanup match both fields, so
an older completion cannot mutate a newer Stop of the same turn:

4. It marks every STARTING `Subagents` record STOPPED, while the Stop of its own turn is still the
   pending one, so the admission of its subagent answers that a Stop stopped it. `start_agent` cannot be
   interrupted, so a call whose `ExecuteTool` already started the SubagentFlow still answers with a task
   ID, and its subagent ends STOPPED without an effect and reports it
   ([subagents](subagents.md#stop-and-project-deletion)).
5. If the Stop cancelled the work in flight, it ends the turn. Wave results already published are kept.
   The other calls of the turn answer `interrupted_by_user`; a mutating tool's effect is `unknown`.
   It clears `AgentState.Running` and the in-flight call cursor, deletes `AgentInterrupt`, pauses
   automatic turns (`AutomaticTurnsPaused`), clears a not-yet-published Preview deployment request,
   writes the `agent_interrupted` activity and returns to `AwaitUser`. Otherwise `CheckSteered` applies
   the same turn end at the next boundary, and stops the STARTING records that appeared since.

The existing `AgentInterrupt.MarkerWritten` owns exactly-once attribution. The first locked
writer appends the marker with the pending Stop's frozen actor and sets this flag in the same
commit. An idle task-only `FinishInterrupt` leaves that Stop pending after writing its marker,
so automatic-turn suppression remains available to `AwaitUser`. If a task notification or new
input wakes `AwaitUser` first, it writes an unwritten marker before consuming the Stop and
before appending the notification or user input. `CheckSteered`/`endStoppedTurn` skip an already
written marker but always perform turn termination, unfinished-call answers, cleanup and pause.
Explicit user input starts the next turn as before, with the Stop marker preceding it and no
extra pause requirement. No new primitive or raw-text projection is introduced.

Open-execution compatibility: an absent `MarkerWritten` decodes as false. New Step inputs
always require exact `{Turn, RequestedAt}` matching. Persisted older inputs lack `RequestedAt`:
an ending input may recover only an unwritten ending pending Stop of the same current turn,
taking its identity and actor from the Attribute. Only one ending Stop cancels a turn; repeats
join until consumption, after which that turn is idle and a later same-Turn Stop can only be
task-only. A newly running turn increments Turn. This narrow recovery therefore cannot match
a later task-only Stop or a new turn. An older non-ending input preserves only idempotent
STARTING-subagent cleanup for a non-ending pending Stop of the same current turn; it writes
no marker, changes no interrupt fields, and never copies its own actor or consumes the Stop.
The current pending Stop stays owned by AwaitUser/CheckSteered. A mismatch is a dead end.

When the user then says "continue", the agent sees the results published before the stop and the
subagent reports, and resumes from there. Notifications that arrive after a stop are recorded but
open no turn.

Latency. Heartbeat timeout stays one minute. After the press the Flow continues at once and the
state returns to waiting for the user; commands and tasks stop at once through the ledger and the
subagent stop. The model call in flight learns of the cancellation at its next heartbeat, up to
about 48 seconds later; Claude keeps generating (and billing) until then and the result is
discarded. Studio drops text that arrives from that Step execution after the stop, so the user never
sees it. Pushing cancellation to a running Step immediately is a Dex feature and would remove the
wasted generation.

### Automatic turns

Notifications from subagents, background commands, connectors and delivery outcomes
([notifications and feedback](notifications-and-feedback.md)) enter through the Channel
`SessionNotifications`:

- While a turn runs, `CheckSteered` takes at most 16 notifications at a model-continuation
  boundary and appends them as user-role entries (projected as `ROLE_NOTIFICATION`).
- While the Session is idle, `AwaitUser` takes them last, `AtLeastAtMost(1, 16)`; it takes none
  while a question is pending. A batch opens at most one turn, counted against the kind that
  triggered it (the platform bound when both did).
- A `delivery` notification with outcome `ready` does not enter the conversation and opens no turn.
  The RPC that received it has already extended the Sandbox deadline ([below](#sandbox-deadline)).

Bounds, so that automation can never run away:

| Counter (`AgentState`) | Counts | Limit |
|---|---|---|
| `AutomaticTaskTurns` | Consecutive turns opened by task and connector notifications | `agent.taskTurns`, default 5 |
| `AutomaticPlatformTurns` | Consecutive turns opened by a failure of the current HEAD that the application owns | `agent.platformTurns`, default 3 |
| `SubagentsStartedWithoutUser` | Subagents started since the last user-initiated turn | 6 |

A user-initiated turn start resets every automatic counter and `AutomaticTurnsPaused`. When Stop is
pressed or a cap is reached, `AutomaticTurnsPaused` is set and the Session records once "automatic
continuation paused; reply to continue"; while paused, notifications are appended to the log but open
no turn.

### Model selection

The user selects the Session's model with `UpdateSessionModel{model, expected_revision}`, where
`model` is the enum `AgentModel`: `AGENT_MODEL_OPUS_5_5`, `AGENT_MODEL_SONNET_5_5`,
`AGENT_MODEL_HAIKU_4_5` or `AGENT_MODEL_MOCK`. A Claude model needs the server's Claude key; the
mock model needs a server that offers it (never prod) and, outside the local lanes, the
administrator ([context management](context-management.md#mock-model-for-tests)). The default is
the server configuration (`agent.model`).

- The choice is stored in the Attribute `SessionModel{Model, Revision, ChangedBy}`. The RPC may be called at
  any time, including while a turn runs; it locks only SessionStatus and `SessionModel`, never
  `MessageWindowLock`.
- The Revision is compare-and-set: the exact next revision with the same model acknowledges a
  repeated accepted request without a second increment; any other revision, including an A to B to
  A change, is rejected.
- It takes effect when the next turn starts: that `CheckSteered` finds that the current
  `ContextSegment` was built for another model and opens a new segment by rollover (summary by the
  old model, new segment with the new one) ([context management](context-management.md#segments)).
  Studio shows "takes effect at the next turn".
- Subagents start with the Session's model at the time they start.
- No provider call, message, question, tool result, workspace or engine execution is created by the
  command.

## Sandbox deadline

The Session decides when its Pod is reclaimed, and its `SessionWorkspaceFlow` reclaims it at the
Session's command. The instance, its lease, its provisioning, its reclamation and its Preview are
in [sandbox](sandbox.md); this section owns the rule and its writer.

- `Workspace.SandboxDeadline` is a point in time, zero unless the current request is READY. At the
  deadline the Pod is reclaimed and the volume is kept. It only moves later, never earlier. Only
  `SessionAgentFlow` writes it.
- It is set to acceptance + `sandbox.hold` when a READY lease is accepted by
  `ApplySessionWorkspace`, and moved to `max(current, now + hold)` by:

| Event | New deadline | Applied by |
|---|---|---|
| The main agent dispatches a tool with workspace access (including `start_agent` and background commands, which are tool calls) | now + `sandbox.hold` | `CheckSteered` and `RouteTool` at the admission of the wave, under the Workspace lock they already hold |
| A task completion notification (subagent or background command) | now + `sandbox.hold` | `CheckSteered` or `AwaitUser`, the Step that consumes the notification, under the Workspace lock |
| The Session lends its READY instance to a Preview command (the answer to the Preview's `RequestSessionWorkspace`, also for a deployment) | now + `sandbox.hold` | the RPC `RequestSessionWorkspace`, under the Workspace lock it holds, atomically with the deadline's decision. A command lent the instance just before a deadline that the last turn left would otherwise meet the reclamation while it runs |
| A Preview that ends READY (its `ready` `delivery` notification, which PreviewFlow's `NotifyPreviewOutcome` reports) | now + `sandbox.previewHold` | the RPC `ReportSessionNotification`, under the Workspace lock, when an ACTIVE Session receives the notification and the instance it names is the Session's READY one; this precedes the admission checks, so a pending question or a full Channel does not delay it ([notifications](notifications-and-feedback.md#admission)) |
| `RenewSessionWorkspace` (business API `RenewWorkspace`; Studio "Keep running for 7 days") | now + 7 days | the RPC, true only while the Session is ACTIVE and the workspace READY |

- Hold times: `sandbox.hold` (default 1h, range 1m to 168h) and `sandbox.previewHold` (default
  168h, range 1m to 168h). One hour is not shorter than a subagent's longest run (45 minutes),
  so a subagent started at the last moment can finish.
- The deadline is rewritten only when it moves by at least a minute, so a busy turn does not turn
  every tool call into a durable write.
- Not counted as use: user messages and model calls without workspace tools, Studio reads and Watches, Preview
  configuration reads and edits, Runs calls, visits to the public Preview URL. Counting public
  traffic would put a Flow-writing proxy in the request path and let unauthenticated traffic keep
  compute alive.
- No Flow RPC is involved on the normal tool path: the dispatching Step computes the extension from
  its own Attributes. A subagent's tool calls do not notify the Session; the Session knows tasks are
  running from its own `Subagents` and `BackgroundCommands` records.

The Step `AwaitSandboxDeadline(workspaceDemand{RequestID})` enforces it. The acceptance that makes a
request READY arms it, and so does `RecordWorkspaceFailure` when a READY request's post-grant
effects fail, so a READY Pod is never left without a deadline; `RequestSessionWorkspace` cancels it
when it opens the next request. Its WaitFor reads `Workspace.SandboxDeadline` and waits only on a
business timer until that time. Because the deadline only moves later, no wake Channel is needed:
the Step re-arms when the timer fires and finds a later deadline. Its Execute runs under the
Workspace lock (30-second handler, short retry) and loads `Subagents` and `BackgroundCommands`:

- the request changed or is not READY: dead end;
- the deadline is later: re-arm;
- a wave is still marked in `WorkspaceUse` for this instance, or a subagent or background command is
  unfinished: deadline = now + `sandbox.hold`, re-arm (every task has a hard limit, so this never pins the
  Sandbox). A subagent record that is still unfinished one hour after its wall-clock limit is treated
  as lost here and in `start_agent` admission, so a subagent that never reports cannot pin the
  Sandbox;
- otherwise it revokes the lease in the same commit (`Workspace.State` REVOKED) and moves to
  `SendWorkspaceCommand`, which sends the `reclaim` command to the workspace Flow
  ([sandbox](sandbox.md#sandbox-deadline)).

The lock makes the decision atomic with dispatch: a wave admitted earlier has already moved the
deadline, and a later wave sees REVOKED and takes the cold path. The Session asks no other Flow
whether its tools stopped and receives no callback, so there is no lock cycle.

After reclamation the Session stays ACTIVE and the workspace reads CLOSED. The next demand provisions
a new instance on the same volume, and uncommitted work is still there.
`SessionWorkspaceSnapshot.expires_at` shows `SandboxDeadline`.

## SessionAgentFlow contract

### Identity and execution lifecycle

- **Flow type and FlowID.** `SessionAgentFlow`; `session-<projectID>`, for example `session-a7k2`
  (Project ID `a7k2`).
- **Business ID source and creation trigger.** The server-assigned Project ID. ProjectFlow's
  `StartProjectSession` Step starts the Session when the Project becomes READY.
- **Start RequestID.** `flowid.Fingerprint(struct{ProjectID string})`, stored as `StartIdentity`.
  Reuse policy `dex.IDReuseDisallow` with same-request `AlreadyStarted.IgnoreError`; no Flow timeout.
- **Duplicates, retries, recovery, reruns.** A duplicate start with the same RequestID joins the
  accepted execution without a new RunID. A different RequestID for the same FlowID returns
  `*dex.FlowAlreadyStartedError` and maps to a conflict. Step and RPC retries, Worker restarts and
  Run transitions never create a FlowID or reset a value. An Agent turn, a workspace instance and
  a Sandbox reclamation never create a FlowID or execution. There is no explicit rerun. A Run
  switch retains every Attribute, AttributeMap instance and pending message; messages, approvals
  and subscriptions are never reset.
- **Business versus engine completion.** See [above](#business-completion-archival-and-engine-completion).
- **Relationships.** An independent top-level Flow, not a Dex SubFlow. It is the Project's only
  Session-scoped cleanup child. Its workspace commands start the Project's
  `SessionWorkspaceFlow` (through the Service), which owns the instances and starts one
  `SandboxProvisionFlow` per instance; the Session lends the instance to the Project's
  `PreviewFlow`; none of these is registered with ProjectFlow ([sandbox](sandbox.md#dex-flow-changes)).
  It also owns the SubagentFlow executions it starts ([subagents](subagents.md)), independent
  top-level Flows that are not registered with ProjectFlow either; the Session is the only caller
  that starts a SubagentFlow (from `ExecuteTool`, through the Service). Each background command's
  watch is a Dex SubFlow of the Session, `BackgroundCommandFlow`
  ([tool orchestration](tool-orchestration.md#background-commands)). A platform failure starts or
  joins the `PlatformIssueFlow` of its signature ([tool orchestration](tool-orchestration.md#platform-issue-reporting)),
  and the Service starts a `ConnectorContributionFlow` for a connector a subagent developed
  ([connectors](connectors.md#contributions)); neither is the Session's child.

### Attributes

All are ordinary struct or scalar Attributes, not AttributeMaps. None holds a secret and none is
indexed except as noted. The owner of every one is `SessionAgentFlow`. Ordinary Attributes load
automatically for every Step and RPC.

| Attribute | Value type | Purpose and rules |
|---|---|---|
| `ProjectID` | string, indexed (keyword2) | Owning Project; immutable |
| `Profile` | string | Coding profile (`session-v1`); immutable |
| `StartIdentity` | string | Start fingerprint; immutable |
| `HeadCommit` | string | The confirmed coding HEAD, owned by the Session alone; set from a 40-hex `Lease.Repository.ExpectedHeadSHA` and by the tool-result commit of a verified push |
| `SessionStatus` | string, indexed (keyword3) | ACTIVE, ARCHIVED; the lock that fences close |
| `CloseReason` | string | `project_cleanup_complete` or `admission_denied` |
| `Revision` | uint64 | Lifecycle revision |
| `CreatedAt`, `UpdatedAt` | time.Time | |
| `SessionModel` | `{Model, Revision, ChangedBy *conversation.Actor}` | The user's model choice ([above](#model-selection)) |
| `ContextSegment` | struct (about 0.5 KB) | Current segment, last usage, pending bytes, force-compaction flag ([context management](context-management.md#segments)) |
| `AgentState` | struct | Sequence range (`FirstRetainedSequence`, `CurrentFirstSequence`, `LastSequence`), `Status`, `PendingToolCalls`, `PendingToolIndex`, `TurnModelCalls` (shared struct field, unused by Session turns), `HandoffContinuations`, `TurnRequestSequences`, `SourceChangedSinceHandoff`, `Turn`, `Running`, `AutomaticTaskTurns`, `AutomaticPlatformTurns`, `AutomaticTurnsPaused`, `SubagentsStartedWithoutUser`, `ConnectorSlugs` (every connector slug an admitted `connector_develop` call named, at most 16) |
| `MessageWindowLock` | bool | Coordination only: serializes every writer of the message maps |
| `WaitingInputRound` | int64 | Monotonic reconciliation watermark |
| `PendingUserInput` | struct | `{CallID, Kind, Questions, Requirements, StartedAt}`; Kind is `""` or `requirements` |
| `PendingApproval` | struct | The admitted ToolCall awaiting approval, with its wait start |
| `PendingTimer` | struct | A model-requested durable wait (`start_timer`) |
| `TodoList` | `{Revision, Items[{Content, Status}]}` | The agent's progress list |
| `ConfirmedRequirements` | `{Revision, Spec, ConfirmedAt, LastFeatureNumber, ConfirmedBy *conversation.Actor}` | Written when the user confirms the requirements card; `Spec.Design` is the confirmed Flow design's reference |
| `BuiltFlowDesign` | string, at most 71 bytes | The digest of the confirmed Flow design that a pushed, unblocked `commit_and_push` built; written only in that result's commit |
| `BuiltFlowDesignCommit` | string, a commit SHA | The commit that push verified, written beside `BuiltFlowDesign` |
| `AgentInterrupt` | `{Turn, RequestedAt, EndsTurn, Actor *conversation.Actor, MarkerWritten bool}` | A Stop request for the running turn; also the lock that serializes the changes of a STARTING `Subagents` record (see Locking) |
| `NotificationReceipts` | []string, at most 128, FIFO | IDs of the last 128 admitted notifications and workspace outcome reports (`workspace/<WorkspaceRequestID>`), written in the commit that publishes them ([notifications](notifications-and-feedback.md)) |
| `PreviewHandoff`, `PreviewAutoDeploy` | `{Commit, Sequence, PublishedSequence}`, bool | Preview auto-deploy: the newest pushed commit a turn hands to the Preview, and whether the Preview deploys it (true at initialization). Fixed-size singletons; only Steps write `PreviewHandoff`, under `MessageWindowLock` ([notifications](notifications-and-feedback.md)) |
| `PreviewAutoDeployChangedBy` | `conversation.Actor`, optional | Author of the last effective toggle; reads as nil when absent, no index; locks with `PreviewAutoDeploy`, fixed-size singleton for the Session lifetime |
| `Workspace` | `SessionWorkspace` | The request side of the Session's Sandbox instance: the request, the accepted lease, the Sandbox deadline and the Preview's hold; see below |
| `WorkspaceUse` | `{Active, RequestID}` | A dispatched wave is in flight on that instance; only the reclamation decision reads it |

`Workspace` fields. The request: `State` (`""`, `REQUESTED`, `READY`, `REVOKED`, `FAILED`) and
`RequestID` (WorkspaceRequestID `ws-<n>`, for example `ws-42`, where `<n>` is the numeric suffix of
the StepExecutionID of the `RequestSessionWorkspace` Step that opened the request), `Lease`
(`workspace.Lease`, the accepted capability replayed to the transport: request ID, binding,
repository; it has no expiry; see [sandbox](sandbox.md)), `Failure` (safe code: a provisioning
code, `WORKSPACE_TIMEOUT`, `WORKSPACE_START_FAILED`, `WORKSPACE_RECLAIM_FAILED` or
`WORKSPACE_NOT_PROVISIONED`), `FailureDetail` (at most 2 KiB), `JoinedResumeAt` (the continuation
that found the request in flight), `RepositoryContext` (trusted repository prompt context, at most
8 KiB, recorded when the lease takes effect), `LostCallID` and `Losses`, and `SandboxDeadline`. The
Preview: `PreviewWaits` (the ID of the Preview command that waits for the next request to settle,
which the settlement names; persisted as `previewWaitsFor`) and `PreviewHolds`
(the WorkspaceRequestID of the instance lent to the Preview, which every workspace command names so
that the reclamation has the Preview release it first). The instance itself (its state, resources,
volume state and phases) is the `SessionWorkspaceFlow`'s `Instance` Attribute; the Session keeps no
copy of it. Lease, binding and internal endpoints hold no token or credential and never reach the
model or the browser.

Locking:

- Every writer of `Workspace` locks that Attribute for its handler: the dispatching Steps
  `CheckSteered` and `RouteTool`, `AwaitUser` (which, like `CheckSteered`, extends the deadline for a
  consumed task notification), the cold-path Steps `RequestSessionWorkspace`,
  `ApplySessionWorkspace` and `RecordWorkspaceFailure`, `AwaitSandboxDeadline`, and the RPCs
  `RequestSessionWorkspace`, `ReportWorkspaceOutcome`, `ReportSessionNotification` (for a `ready`
  Preview) and `RenewSessionWorkspace`. These handlers are short. `SendWorkspaceCommand` and
  `AwaitSessionWorkspace` call the workspace Flow without a lock and write nothing; the Step after
  them reads `Workspace` again under the lock.
- `ExecuteTool`, `ExecuteParallelTool` and `DeployPreviewHandoff` never take the lock: a Step holds its
  locks for its whole Execute including retries, and a locked RPC fails fast against it. A dispatcher
  sets `WorkspaceUse` under the Workspace lock; the finishing paths clear it without a lock;
  revocation only reads it.
- The WorkspaceRequestID comparison is the compare-and-set: an outcome report, a settlement or a
  reclamation for another RequestID never changes state. There is no generation or other counter.
- Every writer of the message maps locks `MessageWindowLock`, and so do the writers of
  `PreviewHandoff`, which no RPC writes. `UpdateSessionModel` locks SessionStatus and
  `SessionModel`; `InterruptAgent` locks SessionStatus and `AgentInterrupt`.
- A `Subagents` record has these writers. `RouteTool` creates the STARTING record, which no one
  else knows yet, after it deletes the oldest finished records beyond the retained 50. From then
  on a STARTING record whose subagent may have started changes only under the `AgentInterrupt`
  lock, so a Stop and an admission decide each record once: `AdmitSubagent` (RUNNING, or STOPPED
  when a Stop or the Session's end overtook the start), `FinishInterrupt`, while its Stop is still
  pending, and `CheckSteered` at a Stop's turn end (STOPPED) and `RecordToolFailure` (FAILED, when
  the `start_agent` retries ran out). `AdmitSubagent` and the task notification of
  `ReportSessionNotification`, which records the subagent's end, are invoked by the Service with
  the record's instance locked and loaded, and `RequestSessionWorkspace` with `ForAgentID` with it
  loaded. A STARTING record whose subagent cannot have started is deleted without that lock, in
  the commit that answers its call: by `ExecuteTool` when the Sandbox refuses the worktree, by
  `CheckSteered` when steering supersedes the call, and by the cold-path Step that answers it
  `workspace_unavailable`; these Steps load the `Subagents` records for it. `merge_agent_changes`
  closes an ended record (MERGED or DISCARDED) for a discard or a merge of every file, which
  nothing else writes any more.

Growth and retention: every Attribute is a fixed-size singleton or bounded as stated. `Workspace`
holds one instance's bounded set of references, which the next instance replaces; nothing resets it,
so what outlives a Pod stays for the Session's lifetime. All of this state is retained for the
Session's lifetime.

### AttributeMaps

| AttributeMap | Instance key, source and example | Value | Bounds and loading | Writers |
|---|---|---|---|---|
| `CurrentMessages` | Unpadded decimal message sequence from `LastSequence + 1`; `37` | `Entry` ([context management](context-management.md)) | Fewer than 20; loaded whole | Steps and RPCs holding `MessageWindowLock` |
| `ArchivedMessages` | Unpadded first sequence of a 10-message chunk; `131` holds messages 131 to 140 | `ArchivedEntryChunk` | About 1,000 messages retained; exact-instance loads of the chunks that cover the current segment, and exactly the requested chunk for archive paging | Same |
| `ContextSegmentPrompts` | Segment number; `3` | `{System, Tools}`, about 40 KB | One instance (the current segment); exact load in `CallModel` and `CompactContext` | Same |
| `BackgroundCommands` | Task ID derived from the execution digest; `cmd-qjo5x7m2k4ab` | Status, exit code, end reason, output bytes and the WorkspaceRequestID it runs on; about 500 B | At most 64; at most 4 running; finished records beyond 32 drop the oldest | Created by `ExecuteTool` and `SettleForegroundCommand`; its end is recorded once by `AwaitBackgroundCommand` under the `NotificationReceipts` lock, or as lost by `AbandonBackgroundCommand` |
| `FlowDesigns` | `pending` or `confirmed`, two fixed keys declared as `flowdesign.Slot` constants | `flowdesign.Design`, at most 24 KiB of canonical JSON | Two instances; exact-instance loads only: `AnswerQuestions` loads `pending`, `GetFlowDesign` both; never loaded by other Steps, RPCs or the Snapshot | `RouteTool` writes `pending` for a card with a design; `AnswerQuestions` copies it to `confirmed`, or deletes `confirmed` for a replacing card without a design ([flow design](../design-first/flow-design.md#storage)) |
| `Subagents` | `flowid.AgentTaskID(sessionFlowID, callID)`, stable under retries; `k3n8q5r1` (the task ID shown to the model is `agent-k3n8q5r1`) | `SubagentRecord`, at most 2 KiB, no prompt text | At most 3 unfinished and 50 finished (a record unfinished one hour after its kind's duration counts as lost: no running place, no Sandbox hold, and the Snapshot marks it `lost`); the whole map for the Snapshot, `RouteTool` and `ExecuteTool` of `start_agent`, an exact instance for the RPCs and the other tools | `RouteTool` creates STARTING; `AdmitSubagent` writes RUNNING; the outcome notification writes the terminal state; a discard or a merge of every file writes MERGED or DISCARDED; a Stop and a failed start write STOPPED and FAILED, and a call answered without its `ExecuteTool` deletes its STARTING record. Locks: see Locking under Attributes |

Instance keys come only from server-derived values (sequence, task ID, slug), never from model or
browser text. `CurrentMessages` and `ArchivedMessages` keep their key formats and exact-load rules.
`Entry.Blocks` hold signed thinking as opaque replay data: it is never projected into a Snapshot, the
history API, compaction input or a log. History older than the window survives only in the context
summary; the archive stops at `FirstRetainedSequence`.

### Channels and ChannelMaps

| Channel | Payload | Producer | Consumer and admission |
|---|---|---|---|
| `QueuedUserMessages` | `PendingUserMessage` | `SendMessage` | `AwaitUser`, FIFO. The client message ID is stable across the client's own retries; the Flow does not deduplicate a repeated send |
| `SteeredUserMessages` | `PendingUserMessage` | `SteerMessage` | Safe boundaries, ahead of the ordinary queue; the ID is preserved |
| `AnsweredUserInputs` | `AnsweredUserInput` | `AnswerQuestions` | `AwaitUser`; admitted only for the exact pending CallID and every question ID |
| `SessionNotifications` | `conversation.SessionNotification` (at most 4 KiB; a subagent report at most 24 KiB; fields `{ID, Kind task, delivery or connector, Source, OccurredAt}` and the kind's content) | `ReportSessionNotification` (internal; subagents, delivery sources and connector contributions, through the Service only) and this Flow's `AwaitBackgroundCommand` | `CheckSteered` (at most 16 per boundary, before steering text) and `AwaitUser` (last, `AtLeastAtMost(1, 16)`, never during a question) |

`SessionNotifications` admission: the Session is ACTIVE, fewer than 32 are pending, and the ID is
new (not in `NotificationReceipts`). A duplicate ID answers true and publishes
nothing, so a retried report is safe. A delivery failure whose commit is not HEAD arrives only as a
notice. Pending notifications of a closed Session are never consumed; their sources keep the facts.

| ChannelMap | Instance key and example | Payload | Producer and consumer |
|---|---|---|---|
| `ToolApprovals` | The tool call ID; `toolu_01B2c3d4` | `ToolApproval` | `ApproveTool` (exact pending CallID) to the matching `AwaitToolApproval` |
| `WorkspaceOutcomes` | The WorkspaceRequestID the Session chose; `ws-41` | `WorkspaceReport{WorkspaceRequestID, Grant}` | The RPC `ReportWorkspaceOutcome`, admitted only while the Session waits for exactly that request, to `AwaitSessionWorkspace` of that request, which takes its grant; never the conversation. A message still pending when the Session completes is discarded with it ([sandbox](sandbox.md#gates-and-the-cold-path)) |
| `BackgroundCommandStops` | Task ID of a running background command; `cmd-qjo5x7m2k4ab` | `backgroundCommandStop` | `CompleteProjectSession` publishes one per running `BackgroundCommands` record in the commit that archives the Session; a repeated call publishes nothing. Consumed by the `AwaitBackgroundCommand` of that task, which races it with its SubFlow in `AnyOf`; a message whose Step already ended stays pending until the Session completes |
| `ParallelToolResults` | `tool-batch-<LastSequence>-<startIndex>`; `tool-batch-37-0` | `parallelToolResult` | Each `ExecuteParallelTool` branch to the `ForN(expectedCount)` join in `AwaitParallelToolResults`; `InterruptAgent` also consumes it, reads the already published results and clears the instance |

The join validates branch index uniqueness and CallID correspondence. A branch result carries
`workspace_lost` when the branch was not dispatched, and a tool result carries the durable
`head_commit` of a verified push. Validate the pending identity or revision before accepting an
answer, approval or notification; a stale message is rejected, never applied.

### Streams

| Stream | Payload | Producers | Consumers |
|---|---|---|---|
| `ReasoningSummary` | string chunks | `CallModel` | Conversation UI |
| `AssistantText` | string chunks | `CallModel` | Conversation UI |
| `AgentActivity` | `AgentEvent` (with `target`; kinds include `compacted`, `compaction_failed`, `agent_interrupted`, `user_input_requested`) | Steps (state, model and compaction, tools, input consumption, interrupts) | Conversation UI |

Each Stream is declared with `10 << 20` bytes. In the pinned Dex SDK that budget is shared by every
execution with the same Flow type and Stream name; it is not per Session and it is not a durable
archive. Delivery is best effort: a Stream may show content before its Step commits and does not
roll back with a failed Step. Consumers read the Snapshot first and resume with tokens; the Snapshot
is the canonical record of messages, pending input, requirements, tasks and workspace. Reasoning
summaries are not recoverable after a page refresh by design. No business fact is derived from a
Stream. The wire contract is in [streaming and Studio](streaming-and-studio.md).

### Steps

Mode and options are given where they matter; unlisted Steps use short conversation options (one
minute method timeout, `CurrentMessages` loaded, `MessageWindowLock` held). Only `CallModel` and
`CompactContext` load archived chunks and the segment prompts, by exact instance.

| Step | Purpose | Options and routing |
|---|---|---|
| `InitializeSessionAgent` | Admit with ProjectFlow, initialize primitives | Start Step; SYNC; 30 seconds, retried for 2 minutes; locks SessionStatus and Workspace. Denial archives and completes with `admission_denied`. Then `ActivateSession` |
| `ActivateSession` | Set status `waiting_for_message` | To `AwaitUser` |
| `AwaitUser` | Wait for answers, steering, a queued message or a notification; start the turn | Locks `MessageWindowLock`, `PendingUserInput`, `PendingApproval`, `Workspace` |
| `AnsweredInput` | Start the answer turn without checking queues | To `CompactContext` or `CallModel` |
| `CheckSteered` | Boundary Step (see the Step graph) | Loads `CurrentMessages`; locks `MessageWindowLock`, `PendingUserInput`, `Workspace` |
| `CompactContext`, `CallModel` | Summarize, or one model reply | SYNC; 20 minutes per attempt; heartbeat 1 minute; 3 attempts within 60 minutes, retries starting 30 seconds apart; failure routes `RecordCompactionFailure` and `RecordModelFailure` |
| `RecordModelFailure`, `RecordCompactionFailure` | Record one bounded turn failure; end the turn | To `CheckSteered(await_user)` |
| `RouteTool` | Admit and route the pending call | Admission for `start_agent` (the limits and the STARTING `Subagents` record, [subagents](subagents.md#start_agent)), `confirm_requirements`; wave fan-out; workspace gate G2 |
| `ExecuteTool`, `ExecuteParallelTool` | Run one tool or one wave branch; `ExecuteTool` also runs the delegation tools (`start_agent` creates the worktree and starts the SubagentFlow, `stop_task` of an agent, `merge_agent_changes`) | Policy attached per call by the dispatcher ([tool orchestration](tool-orchestration.md)); neither takes the Workspace lock; a delegation tool loads the `Subagents` records it reads (`start_agent` all of them, the others their own) |
| `AwaitParallelToolResults` | Join a wave | `ForN` on `ParallelToolResults`; re-routes a lost read-only wave |
| `RecordToolFailure`, `RecoverParallelToolExecution` | Record `tool_unavailable` after retries are exhausted, serial and parallel; `RecordToolFailure` also fails the STARTING record of a `start_agent` call | The model sees the result and continues; `RecordToolFailure` locks `AgentInterrupt` |
| `SettleForegroundCommand` | Foreground `bash` timeout or retry exhaustion: stop and record, or settle from the ledger | [tool orchestration](tool-orchestration.md) |
| `AwaitToolApproval`, `AwaitAgentTimer` | Wait for an approval or a durable timer, or steering | Steering ends the wait and starts a turn |
| `RequestSessionWorkspace`, `SendWorkspaceCommand`, `AwaitSessionWorkspace`, `ApplySessionWorkspace`, `RecordWorkspaceFailure` | The workspace cold path: open a request, send the workspace Flow its `provision` command, wait for its outcome, accept or fail it | Send: provider options (SYNC, retried for three minutes), no lock; Await: waits for the `WorkspaceOutcomes` message of its request or a timer of `provision.Budget` plus 30 minutes, no lock; Request and Record: ASYNC, one-minute method timeout, lock Workspace; Apply: SYNC, 30 seconds, retried for 2 minutes, locks Workspace. The Steps that commit load `CurrentMessages` and `Subagents`, because the Step that settles a request resumes the conversation in its own commit, which may answer a `start_agent` call whose workspace failed and forgets that call's STARTING record. Details in [sandbox](sandbox.md#gates-and-the-cold-path) |
| `AwaitSandboxDeadline` | Decide at the deadline whether to reclaim ([above](#sandbox-deadline)) | Business timer; SYNC; 30-second handler, retried for 2 minutes; Workspace lock; loads the task maps; to `SendWorkspaceCommand` with the `reclaim` command |
| `AwaitBackgroundCommand`, `AbandonBackgroundCommand` | Wait for the `BackgroundCommandFlow` SubFlow that watches a background command, or for the stop of its watch; record the end and publish its completion notification unless it was stopped; record lost a command whose recording exhausted its retries | WaitFor `AnyOf` the SubFlow and the task's `BackgroundCommandStops` instance, a failed WaitFor recording the command lost; Execute 30 seconds, retried for 2 minutes, locks `NotificationReceipts`, loads the task maps; failure to `AbandonBackgroundCommand`. When the stop won it stops the SubFlow (`StopBackgroundCommandWatch`). Details in [tool orchestration](tool-orchestration.md#background-commands) |
| `InterruptAgent` | Stop the tasks of a Stop ([above](#stop-button)) | No lock; idempotent; SYNC, 3 minutes per attempt, retried for 6 minutes; the failure route and the movement go to `FinishInterrupt` |
| `FinishInterrupt` | Commit only the matching `{Turn, RequestedAt}` Stop: write an idle marker once while keeping suppression pending; stop STARTING subagents and end a cancellable turn ([above](#stop-button)) | Locks `MessageWindowLock`, `PendingUserInput`, `PendingApproval` and `AgentInterrupt`; loads `CurrentMessages` and `Subagents`; to `AwaitUser`, or a dead end when `CheckSteered` ends the turn |
| `DeployPreviewHandoff`, `AbandonPreviewHandoff` | One-shot deployment request per handoff, and the end of a handoff whose request exhausted its retries | [notifications](notifications-and-feedback.md) |
| `ReportPlatformIssue`, `AbandonPlatformIssue` | Side branch that starts or joins the `PlatformIssueFlow` of a platform fault's signature, which files it; the end of a report whose start exhausted its retries | Provider options, no lock; the turn never waits for it ([tool orchestration](tool-orchestration.md#platform-issue-reporting)) |
| `CompleteProjectSession` | Complete the Session of a deleting Project ([above](#never-expires-closes-only-with-the-project)) | Reached from the RPC of the same name, which archives the Session, cancels every branch and stops the background command watches; the Step checks that admission again, because the completion is irreversible, and completes gracefully with the cleanup receipt |

Workspace gates. G1 (first segment): a model call that finds no context segment yet and no
`Workspace.RepositoryContext` goes through `RequestSessionWorkspace` first; the cold path opens the
Session's first segment itself, with the repository context if the lease provided it, and returns to
the model continuation through `CheckSteered` without asking G1 again, so a workspace that cannot be
prepared delays the model but never blocks it. G2 (dispatch): before a wave or a serial call with
workspace access, `admitWorkspaceUse` requires `State == READY` and a usable lease, then sets
`WorkspaceUse` under the Workspace lock; otherwise `RequestSessionWorkspace{ResumeAt: route_tool}`. G3
(lost transport): the Runner reports `ErrWorkspaceUnavailable`, which proves the call was not
dispatched: `ExecuteTool` returns `RequestSessionWorkspace` as a decision (no Dex retry consumed, call
stays pending); a parallel branch publishes `workspace_lost` and the join re-routes the whole
read-only wave; more than two losses of one call finish as `workspace_unavailable`. A mutating call
whose earlier attempt may have been dispatched, when the workspace is then lost, completes with
`command_interrupted` (effect unknown) for the model to reconcile; it is never repeated silently.

Post-grant effects of the request's owner are idempotent: a 40-hex `ExpectedHeadSHA` becomes
`HeadCommit`; `RepositoryContext` is read once through the pooled sandboxd transport
(`repositoryContextReader`, no Flow RPC); and `AwaitSandboxDeadline` is armed.

Tool execution uses only the lease:

- `ExecuteTool` and `ExecuteParallelTool` pass `Workspace.Lease` as the trusted `sandboxtools.Call.Lease`
  (never taken from the model) while `WorkspaceUse.Active` and `WorkspaceUse.RequestID ==
  Workspace.Lease.RequestID`. An admitted wave keeps using its lease until its join, even after the
  State became REVOKED.
- Git uses `Lease.Repository` with `ExpectedHeadSHA` set to the Session's `HeadCommit`; tokens come from
  `*git.HostCredentials`, which mints a short-lived token for exactly the admitted installation and
  repository without reading any Flow.
- HEAD handoff: `commit_and_push` verifies the exact remote SHA and returns it as `head_commit`. The
  field is durable (it crosses `ParallelToolResults` as JSON) and never goes to the model. The funnel
  that appends tool results sets `HeadCommit` and `UpdatedAt` in the same Step commit as the result
  when the value is a 40-hex SHA different from the current one. No Session or Sandbox RPC confirms HEAD.
- The finishing paths clear `WorkspaceUse`: the end of a tool batch and the lost-transport decisions.
  The reclamation decision only reads it, under the Workspace lock, so no reclamation removes the Pod
  under a running tool.
- Recorded limitation: sandboxd does not verify a request-level identity or deadline. Identity is fenced
  by the isolation boundary and the Service name, by the Sandbox UID checked when the provisioning
  verifies the instance, and by transport failure detection before dispatch.

### RPCs

| RPC | Locks and options | Behavior |
|---|---|---|
| `SendMessage` | SessionStatus, `PendingUserInput` | Publish to `QueuedUserMessages`; false when not ACTIVE or a question is pending |
| `SteerMessage` | SessionStatus; transactional; loads the queue | Move one queued message to steering by ID |
| `DeleteQueuedMessage` | SessionStatus; transactional; loads the queue | Check author/owner and remove one queued message atomically; `{Deleted, NotAuthor}` |
| `AnswerQuestions` | SessionStatus, `PendingUserInput` (and `ConfirmedRequirements` for a confirmation); loads `FlowDesigns` `pending` | Exact CallID and question IDs; delete the pending input and publish the answer; a confirmation moves the Flow design (above) |
| `ApproveTool` | SessionStatus, `PendingApproval`; transactional | Exact pending CallID |
| `UpdateSessionModel` | SessionStatus, `SessionModel` | Enum model, expected revision; takes effect at the next turn |
| `InterruptAgent` | SessionStatus, `AgentInterrupt`; loads the task records | The Stop button ([above](#stop-button)) |
| `UpdatePreviewAutoDeploy` | SessionStatus, `PreviewAutoDeploy` and `PreviewAutoDeployChangedBy` | Set `PreviewAutoDeploy` and its optional changer under both locks; called through the Preview service ([notifications](notifications-and-feedback.md)) |
| `GetSnapshot` | No lock; loads `CurrentMessages`, the two message Channels and the task maps | Lifecycle plus `AgentSnapshot`: projected history page (no thinking, no raw tool payloads), queued and steered messages, and a description (status, waiting round, current `Turn`, model and revision, pending input, todo list, confirmed requirements and whether their Flow design is built, task records, available tools). No RunID |
| `GetArchivedMessages` | No lock | Exactly the requested chunk, projected |
| `GetFlowDesign` | No lock; loads both `FlowDesigns` instances | The design a slot holds, or not found; the client compares its digest with the caller's and reports a stale one as `ErrFlowDesignRevisionChanged` |
| `GetSessionSummary` | No lock | Lifecycle, identity, HEAD and `WorkspaceSummary{State, RequestID, Failure, FailureDetail, SandboxDeadline, Instance, InstanceID, Resources, VolumeState, InstanceUpdatedAt}`, the only projection of `Workspace` outside the Flow; it carries no lease, binding or credential |
| `GetDexSummary`, `GetDexDisplay` | No lock | Dex Web display |
| `CompleteProjectSession` | SessionStatus, `Revision`; loads `BackgroundCommands` | Internal; the deleting Project's one completion call: the scope must match; archives the Session with `project_cleanup_complete`, cancels every Agent branch and the workspace Steps, publishes the stop of each running background command's watch, and moves to the Step of the same name. A repeat is accepted without effect |
| `RequestSessionWorkspace` | Short RPC (10 seconds); SessionStatus, Workspace; loads the record of the subagent that asks | Internal; takes `WorkspaceDemandRequest{PreviewCommand, ReadyOnly, ForAgentID, Lost}`; reached only through the Service after the caller was authorized, or on behalf of the Project's Preview or of a running subagent; a Session that is not ACTIVE, or a subagent whose record is not RUNNING, answers `Closed`, a lend of the READY instance to the Preview extends the Sandbox deadline ([above](#sandbox-deadline)), and a ready-only demand, a Preview deployment's, wakes no Sandbox ([sandbox](sandbox.md#gates-and-the-cold-path)). A subagent takes the lease of a usable instance, and its `Lost` request revokes the instance if it is still the current one ([subagents](subagents.md#lease-hand-over-and-refresh)) |
| `ReportWorkspaceOutcome` | Short RPC (10 seconds); SessionStatus, NotificationReceipts and Workspace | Internal; reached only through the Service on behalf of the `SessionWorkspaceFlow`; a `reclaimed` report is always admitted, a ready or failed one only for the request the Session waits for ([sandbox](sandbox.md#gates-and-the-cold-path)) |
| `RenewSessionWorkspace` | Short RPC; SessionStatus, Workspace | Extend the Sandbox deadline by 7 days; true only while ACTIVE and READY |
| `ReportSessionNotification` | Short RPC; SessionStatus, the receipts and Workspace; for a subagent's report also its record, locked and loaded by the Service | Internal; admission as above. A `ready` delivery of the Preview also holds the instance it names for `sandbox.previewHold` ([notifications](notifications-and-feedback.md#admission)). A subagent's `task` notification first records the subagent's end in its record, whatever the admission answers, also for a Session that is no longer ACTIVE |
| `AdmitSubagent` | Short RPC; SessionStatus and `AgentInterrupt`, and the `Subagents` instance, locked and loaded by the Service | Internal; takes `SubagentAdmission{AgentID, Fingerprint, BaseCommit}` and answers `SubagentAdmissionAnswer{Admitted, Stopped}`. Admitted for the call its STARTING record names, which becomes RUNNING with the snapshot commit, while the Session is ACTIVE and no Stop is pending, and again for that RUNNING record. A STARTING record that meets a Session that is not open becomes STOPPED, and the denial of a record stopped before its subagent started, by a Stop or by the Session's end, is marked `Stopped`, so the subagent reports it; any other answer is a plain denial, which ends the subagent without an effect. A Stop that committed first has marked the record STOPPED, so a late admission is denied |

Locked RPC handlers may run up to three times and are idempotent. `SessionAgentFlowClient` retries
`*dex.RPCLockConflictError` within the 20-second command deadline (10 seconds for the workspace
RPCs). For a missing or closed Session, the outcome report reports false, and the Service drops a delivery notification (its source keeps the fact).
`RequestSessionWorkspace` answers `Closed` for a Session that is not ACTIVE, which the typed client
reports as `ErrSessionNotActive`; user commands at a stale revision answer false instead.

### Cancellation lists

Two hard-coded `CancelSteps` lists exist. The one of the `CompleteProjectSession` RPC names every
long-lived or in-flight Step type of the Session except the background command waits: the
conversation Steps, the failure Steps, `SettleForegroundCommand`, `ReportPlatformIssue`,
`AbandonPlatformIssue`, `InterruptAgent`, `FinishInterrupt`, `AwaitAgentTimer`, the five workspace
cold-path Steps, `AwaitSandboxDeadline`, `DeployPreviewHandoff` and `AbandonPreviewHandoff`, and it
gains a Step the moment the Step is registered. `AwaitBackgroundCommand` is not cancelled, because
Dex does not stop a SubFlow with its parent: the RPC publishes each watch's stop instead, and the
Session completes after every such Step. The `InterruptAgent` RPC's cancel set is the model, tool,
wave, timer and approval Steps (`CompactContext`, `CallModel`, `RecordModelFailure`,
`RecordCompactionFailure`, `ExecuteTool`, `ExecuteParallelTool`, `RecoverParallelToolExecution`,
`AwaitParallelToolResults`, `RecordToolFailure`, `SettleForegroundCommand`, `AwaitAgentTimer` and
`AwaitToolApproval`). Both lists are spelled out in the Flow file instead of a shared slice or helper,
because the pinned dexcli follows only literal Step references.

### Recovery, external effects and compatibility

Every external operation has an identity, and a query before a mutation:

| Operation | Identity | Recovery |
|---|---|---|
| Model call | None needed: no business effect, a retry only bills again | Dex retry, then `RecordModelFailure` |
| Workspace command | `ws-<n>/provision` or `ws-<n>/reclaim`; `ws-<n>` is also the provisioning's FlowID suffix | A repeated command is accepted without a second message; `ObserveWorkspace` reads the outcome from the workspace Flow when the report did not arrive in time |
| Reclamation | The instance's WorkspaceRequestID; each stage queries its object before it changes it | Owned by the workspace Flow: a new attempt repeats only what the previous one did not finish; the Preview's `reclaim` command ID is `reclaim-<WorkspaceRequestID>` |
| Background command watch | The SubFlow ID Dex derives from the Session and the Step execution | Started once per Step execution; a stop of a SubFlow that no longer runs counts as stopped |
| Platform issue report | `{SessionFlowID, CallID, Signature}`, the start's RequestID | A report while the signature's Flow runs joins it ([tool orchestration](tool-orchestration.md#platform-issue-reporting)) |
| Tool call | Flow ID plus call ID | Per tool: ledger query, intent records, query-before-write ([tool orchestration](tool-orchestration.md)) |
| Subagent start | Flow ID plus request ID (fingerprint) | A duplicate joins the same execution |
| Stop | Execution ledger ID, subagent Flow ID | `stop` is idempotent |
| Notification | Notification ID | Duplicate answers true |
| Preview deployment request | The handoff `Sequence` | A lost answer is retried and answered `DUPLICATE` (`QUEUED` while the request still waits); a request that exhausts its retries ends in `AbandonPreviewHandoff` |

Recovery owners: a failed cold path is owned by `RecordWorkspaceFailure`; a reclamation that cannot
finish by the workspace Flow's `RecordReclaimFailure`, then by the next command or the deleting
Project; an uncertain tool effect by the model through the ledger query; everything else by the
failure Step named in the Step table. Open executions
are compatible only with this inventory. Local Kind and AWS drop their old Projects before a
rollout; no general migration exists. Persistence changes are regenerated into
`docs/flows/sv2-session-agent-flow.json`, and `check-flow-definitions.py` carries every Channel.

Removing the prelaunch Session model-call limit keeps open local executions decodable:
`SessionAgentFlow` no longer reads or increments `AgentState.TurnModelCalls` (which
remains the subagent's kind-specific counter), and `AgentState` ignores the old
`interaction_mode` JSON field.
A pending `turn_limit` question already written before the rollout is projected as an
ordinary question and its exact CallID and options remain answerable; either answer
starts an ordinary turn. New executions never create that pending kind.

### Line budget

`session_agent_flow.go` has a hard limit of 1,600 lines, like every `*_flow.go` file, because the
pinned dexcli analyzes only that file: it holds the primitives, registration, StepOptions, every
`GoTo`, `GoToMany`, `DeadEnd` and `CancelSteps` decision, and thin Steps and RPCs, and its comment
blocks are at most two lines (`scripts/check-go-file-length.sh`); the contract detail that does not
fit belongs here and in the package README. Decision bodies live in `agentloop` and in the
responsibility-named files of the package. A change that would exceed the limit first moves
decision bodies out and never raises it.

## Configuration

These settings are keys of the server configuration, which defaults and range-checks each of them
([reference](../../../cmd/superverse-server/config/README.md)).

| Setting | Owner of the value |
|---|---|
| `agent.taskTurns` (default 5, range 0 to 100), `agent.platformTurns` (default 3, range 0 to 100; 0 opens none) | This document |
| `sandbox.hold` (default 1h, range 1m to 168h), `sandbox.previewHold` (default 168h, range 1m to 168h) | This document |
| `agent.model`, `agent.effort`, `agent.subagentInvestigateEffort`, `agent.subagentChangeEffort`, `agent.subagentConnectorDevelopEffort`, `agent.compactionTokens`, `agent.thinkingPrefixMismatch`, `agent.mockModel` | [Context management](context-management.md) |

## Dex Flow Changes

The [SessionAgentFlow contract](#sessionagentflow-contract) remains the Flow's primitive,
identity, Step, RPC and recovery inventory. `CheckSteered` no longer compares a turn's
model-call count or creates a `turn_limit` pending question; model replies in both
executor modes can continue beyond the former bound. Session turns no longer
write the shared counter or reporting-only mode; SubagentFlow still counts its
replies against each kind's bound. Already open prelaunch questions use
the [compatibility behavior](#recovery-external-effects-and-compatibility) above.

## Database Schema Changes

None. Every durable fact of the Session is a SessionAgentFlow primitive, as listed above. The Session
introduces no database, projection, counter or mirrored lifecycle. The request side and the deadline
are fields of the `Workspace` Attribute, the instance is the workspace Flow's `Instance` Attribute,
and the runtime (Kubernetes or Docker) owns object existence.

## UI/UX

- One conversation per Project: the Build page opens the Project's Session directly. There is no
  Session list, `?session=` parameter or "start a session" action. While Setup runs the page shows
  "Preparing project..."; when the Project is READY it shows "Starting the agent..." and retries a
  not-found Session read for at most 30 seconds. The create page reuses the Session composer: the
  person picks the model (Mock as well where the server offers it) and the first message, and the
  stored creation intent carries both. Once the Session waits, Studio applies the model with
  `UpdateSessionModel` when it differs from the Session's, then sends the first `SendMessage` with
  the intent key as the message ID; a failed delivery keeps the intent and shows the draft.
- A Stop button next to the input, also bound to Escape, shown while a turn or task runs. It stops
  everything and sends the running `Turn` for stale-request protection.
- The requirements card, the confirmed requirements (viewable at any time), and a one-line progress
  indicator. The model selector below the Session controls (Opus 5.5 strongest, Sonnet 5.5 faster and
  cheaper, Haiku 4.5 cheapest, for tests), with "takes effect at the next turn" after a change, and the
  current model and effort ("Claude Opus 5.5 - medium").
- The workspace shows its status and expiry, with "Keep running for 7 days"; after a reclaim it reads
  "Paused; your files are kept". A stopped engine presents as FAILED with interaction disabled.
- Accessibility, keyboard behavior, cancellation and stale-response protection are kept. The Studio
  mock models the same behavior explicitly. Views and projections are in
  [streaming and Studio](streaming-and-studio.md).

## Tests

Agent paths are covered by Dex integration tests with the mock model
([context management](context-management.md#mock-model-for-tests)); every other dependency (Dex, Kind or
Docker, the Sandbox, the repository host) is real. End-to-end coverage is only the three happy paths. A missing
service or credential is an incomplete result, never a skip. The final acceptance before AWS runs the
three E2E cases with real Opus 5.5.

| Area | Scenario |
|---|---|
| Identity | Exactly one `session-<p>` per READY Project with no Flow timeout (`dexcli flow summary`); a Worker restart inside `StartProjectSession` leaves one execution and one ChildFlows entry; a repeated setup commit starts it once; deletion before READY leaves no Session, deletion right after READY ends in `admission_denied` or a cleaned child |
| Liveness | An execution stopped with the pinned dexcli on a throwaway Project reads FAILED; commands answer `FAILED_PRECONDITION SESSION_EXECUTION_TERMINATED` while Preview configuration reads still succeed; an authorized time travel brings it back with history, queue and HEAD intact and each recovered effect applied once |
| Model failure | A blocked provider egress past the retries runs `RecordModelFailure`; the Session stays ACTIVE and the next message succeeds; a compaction failure runs `RecordCompactionFailure` |
| Instance | A cold start runs exactly one workspace Flow and one provisioning, and a repeated command joins; a Worker restart during a provisioning or a reclamation leaves one execution; a replacement reclaims the lost instance first; a late outcome after a replacement is declined; the `WorkspaceOutcomes` instance `ws-<n>` is consumed only by the Step that waits for that request, and a report that never arrives settles through the workspace Flow's own state |
| Sandbox deadline | A shortened hold makes the deadline reclamation exactly the `reclaim` command and the `reclaimed` report, keeping the volume; a running task postpones reclamation; a READY Preview's `ready` notification extends it to `sandbox.previewHold`, also while a question waits; `RenewSessionWorkspace` moves the deadline; a wave dispatched seconds before the deadline makes the timer re-arm; the lend of the READY instance to a Preview command moves it one hold ahead; a Worker restart while it waits reclaims once; a running Preview is stopped first; an uncommitted file survives reclaim and resume |
| Requirements | A new application shows the card and a small change does not; confirming writes `ConfirmedRequirements`; a repeated confirmation of the same version is not requested |
| Flow design | A card's design is the pending one, which a revised card replaces and a stale digest cannot read; a confirmation copies it to the confirmed slot, unbuilt; a compaction keeps its outline; a blocked checkpoint leaves it unbuilt and a pushed, unblocked commit builds it; each rule a design breaks refuses the card as `invalid_user_input` |
| Stop | One press stops the model call, a background command and a subagent; a stale `Turn` answers false; a non-interruptible tool ends the turn at the next boundary; a subagent that is still starting is marked STOPPED and answered `Stopped` at its admission, and reports STOPPED when its start was racing the Stop; with no turn running the press stops a running subagent, and its report opens no turn; "continue" resumes with published results and the STOPPED report; notification-first, input-first and Finish-first commits write one first-actor marker; a late same-Turn settlement cannot alter a newer Stop; persisted MarkerWritten survives a replacement Worker |
| Background commands | A command's watch runs as a SubFlow, so the Session routes tools while it runs; its end publishes one notification; a stopped command publishes none; Project deletion stops each running watch before the Session completes |
| Notifications and automatic turns | A duplicate ID publishes once; admission refuses a 33rd pending notification; caps and `AutomaticTurnsPaused` hold; a notification after Stop opens no turn |
| Model selection | A change during a turn applies at the next turn through rollover; a stale expected revision is rejected |
| Primitives | Exact archive loads and `ContextSegmentPrompts` loads with all maps present; Channel races at the gates; Stream loss recovered from the Snapshot; after 60 or more turns with parallel waves `dexcli flow state --no-hydrate` shows `CurrentMessages` at most 19 and records whether consumed ChannelMap instances persist (a persisting instance is a follow-up, not a pass) |
| Zero Flow RPC | After READY a turn of read, search, edit, command and `commit_and_push` logs no `session workspace cold path` line and adds no event to the workspace, provisioning or Preview executions |
| Close | Project deletion calls `CompleteProjectSession` once, ends the workspace Flow, finds and stops the running subagents by FlowType and `ProjectID`, ends the Preview and releases every Sandbox resource by label; a deletion during a provisioning, during a reclamation and after an abnormal Session end leaves nothing; a release failure keeps the Project DELETING until a retried deletion |

The scenarios run in the Dex integration tests of `internal/agents` (`session_*_integration_test.go`)
and of the Session's sub-packages; their package READMEs list them.

Not verified without a safe repeatable trigger: refusals and fallback. Behavior that depends on model
output is recorded as observed.

## Documentation

This document owns the Session contract. [Composition](composition.md) owns the wiring;
[superverse-v2.md](../superverse-v2.md#sessionagentflow) carries a summary and links here, and the
[Session package README](../../../internal/agents/session/README.md) keeps the file table and the
Step graph narrative. the current contract records one
Session per Project and [the current contract](sandbox.md) the
instance lifecycle. The prompt `internal/agents/session/conversation/prompts/v1/session.md` and the
notices carry the rules for requirements, progress, delegation and business-language progress text.
The append-only context, Flow RPC scope and retained capabilities are specified in the owning topic documents.

## Remote executor

The [owning Session package inventory](../../../internal/agents/session/README.md#executor-and-remote-turn-contract)
defines Executor/executorLock, RemoteInbox Report/Control union, SetExecutor,
ReportRemote, ReadRemote, the three Stop fences, remote retention and bounded
executor handoff. One sole active AwaitUser or AwaitRemoteAgent consumes this
Channel. A takeover never cancels AwaitUser: persisted selected answers remain
owned by that Step, which records them before takeover settlement. Control
admission inspects actual pending messages and coalesces changes without a
mirrored flag; consumers always read current Executor.

`SetExecutor` writes no Stream. The consumer Step emits `snapshot_required`
even when its wake contains only the executor control message. Streams remain
best effort; snapshot reconciliation recovers a missed notification. Decisions,
waits and RPC movements stay explicit in the Flow's DSL file, while data helpers
preserve the exact-key invocation loads of `ReadRemote`.

Remote mode never calls the platform model or starts a platform subagent; offline
execution does not fall back. ReadRemote uses canonical current/archive entries,
not execution history, and Bridge delivery owns its cursor. Each verified
unblocked push in either executor mode publishes its immutable Preview request
immediately from ExecuteTool after recording the result.

The real-dependency quiescent handoff test checks bounded, fully paginated engine
history at explicit checkpoints, with exact typed activation/switch commits and
current wait anchors. An exhaustive source call-site audit connects rejected
model Steps and subagent admission/record writes to provider traffic, with actual
CallModel positive controls before and after. This is an engine-plus-source proof
of zero newly admitted work when run, not an observed HTTP request counter.
Already admitted preactivation traffic requires its separate takeover test.

Platform model scheduling captures original epoch/generation and Turn under
executorLock. CallModel and CompactContext do not commit authoritative context
or AgentState from their transport snapshots. Their typed outcome is a durable
Dex movement payload capped at 16 MiB encoded total, with field preflight before
whole serialization. RecordPlatformModelResult takes message/executor/interrupt
locks and rejects a changed authority/turn before writing. A compaction correction
with ToolChoiceNone requires a fresh admission there. Already admitted provider
transport may race cancellation; this boundary prevents stale durable results,
not wire traffic from an earlier valid admission. Private provider replay blocks
retain their existing visibility and never enter remote sync or public history.

The existing pending batch carries PendingToolsAdmission. Serial and parallel
Step inputs retain its original fence and Turn. InterruptAgent captures original
foreground CallID and accepted credential-free Lease without arguments; retries
stop that command on that instance. Legacy fallback requires the exact pending
Stop identity. An unprovable or unreachable original Stop is explicit uncertainty,
not proof of an operation's success or abandonment. Each result and pending-cursor removal commits atomically. Serial and parallel
retry/recovery/join compare original call and cursor/wave before effects or
publication; a settled mismatch ends its own branch without new-owner routing.
There is no history query or retention-based settlement inference. Legacy
serial NULL cannot bind: one diagnostic preserves pending effects for owner
reconciliation/Stop, without fake tool results or automatic replay.

Legacy NULL CallModel has no original Turn and is a platform epoch/generation0
compatibility case. Its invocation captures Turn before provider transport for
the fresh result boundary. Stop must cancel that old Step before a new turn
starts; a delayed NULL attempt cannot safely infer its old identity from the
replacement state. Raw legacy NULL/provider cancellation and owner Stop need
real dependency acceptance; source does not promise seamless recovery.

## Resource Scalability

None. Removing the Session model-call limit creates no cloud, Kubernetes,
Temporal, Dex or database resource and adds no control-plane call or tenant scan
on a request, poll or event. The existing Session and workspace lifecycles and
retention rules in this contract still govern their resources.
