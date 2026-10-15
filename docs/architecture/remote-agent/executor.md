# Executor

The executor is whoever runs the Session's turns: the platform model, as today, or one remote
agent connected through a [Bridge](bridge.md). This document owns the executor, remote turns
and the changes to `SessionAgentFlow`. The rest of the Session (identity, turns, questions, the
requirements card, Stop, automatic turns, the Sandbox lease, the Preview handoff) is unchanged
and stays in [agent harness: session](../agent-harness-design/session.md).

## Modes

| Mode | Who reasons | Who runs tools | Model calls by the platform |
|---|---|---|---|
| `platform` (default) | The platform model (`CallModel`, `CompactContext`) | `RouteTool` / `ExecuteTool` in the Sandbox | As today |
| `remote` | The connected Codex | `RouteTool` / `ExecuteTool` in the Sandbox, on calls that Codex makes through the Bridge | None |

A new Session starts in `platform` mode. Only an explicit act changes the mode: a Builder
activating their Bridge ([connection](connection.md#activation-and-takeover)), or a Builder
choosing "Use the Superverse agent" in Studio. Losing the Bridge's connection never changes it:
the Session stays in remote mode, queues work, and Studio shows the agent as offline.

## The remote branch of a turn

Remote mode reuses the turn as it is and replaces exactly one decision: what happens when the
turn needs the next model step.

- `planModelCall` returns `nextAwaitRemote` in remote mode, before its workspace, segment,
  compaction and model checks. Every place that continues the turn with a model step
  (`AnsweredInput`, `CheckSteered` for `StartTurn`, `CompactContext`, `CallModel`) therefore
  goes to `AwaitRemoteAgent` instead. No model Step runs.
- **Admission across a takeover.** The activation's commit is the boundary: no model call is
  admitted after it. Every Step that schedules `CallModel` or `CompactContext` (the callers of
  `planModelCall`, the compaction and failure routes, the session-start open) reads `Mode` and
  `Epoch` under `executorLock` and passes the admitted epoch to the model Step, so `SetExecutor`
  waits only for that short Step. The takeover's interrupt plan cancels an in-flight model Step on
  a best-effort basis, as a Stop does, and every model attempt, a retry included, compares the
  Executor of its invocation snapshot with its admitted epoch and returns without calling the
  provider when they differ. Requests admitted before the activation therefore proceed within the
  existing retry and cancellation policy and may be billed; no retry is newly admitted once the epoch
  change is visible. A reply that still commits cannot act: the commit that sets `PendingToolCalls`
  also sets `AgentState.PendingToolsAdmission` (`remoteexec.Fence{Epoch, Generation}`) from the batch's original
  admission (the admitted epoch and generation for a model reply, the current ones for a remote call;
  a branch and its retries keep the batch's value). `RouteTool` and the parallel batch builder, under
  `executorLock` in both modes, answer every NOT-YET-DISPATCHED pending call "interrupted" when that
  value differs from the current `Executor` epoch or generation, then end the turn as the interrupt
  plan does; this covers a takeover, a platform-remote-platform cycle and a remote Stop, which raises
  only the generation. A call already dispatched is not refused by this check: it settles under the
  existing rules (an uninterruptible call finishes and records its result; a cancellable one is
  cancelled and settled from the command ledger; uncertain effects are reconciled before any retry).
  **Settlement is proved by `AgentState`, never by a history lookup.** The commit that appends a
  call's `tool_result` also settles it in `AgentState`: `PendingToolIndex` moves past it, the wave
  records it, or `endStoppedTurn` clears the batch and appends one `interrupted` result per unsettled
  call. A retried `ExecuteTool` or `ExecuteParallelTool` attempt compares its own Step input (the
  original turn, `PendingToolsAdmission`, index or wave slot and call ID) with `AgentState` under its
  existing locks before any effect. If the call is already settled, the attempt writes and appends
  nothing and ends its branch (`DeadEnd`): the branch that settled the call owns routing, so a stale
  attempt never schedules the current state's next Step. Every write that resets or replaces
  `PendingToolCalls`, `PendingToolIndex` or the wave settles or answers the calls it removes in the
  same commit, which is what makes a mismatch proof of settlement. Call IDs are unique per call
  (the provider's tool-use IDs, or `remote-<epoch>-<sequence>`). An input that carries a call ID but
  no turn or admission binds by call ID and index alone. A serial input written before RAEXEC is
  null and cannot be bound at all: it is not dispatched, it is reported as ambiguous through the
  failure path, and it is never applied to a replacement call. An unsettled call that an earlier attempt may have
  dispatched goes through the existing uncertain-effect recovery: the command ledger for `bash`, and
  query-before-mutate for `commit_and_push`. An outcome that cannot be proved is recorded once as
  `EffectUnknown`, and no executor, remote or platform, runs that call again. Entries are never
  searched, so retention plays no part in deciding a call's outcome, and absence from the conversation
  is never evidence of one.
  Absent values are 0, so every Session without an executor dispatches as today. Stale model,
  compaction or failure results never clear pending work admitted under a newer epoch or generation. Activation never waits for a model call. A steady
  remote Session makes no platform model call. The long `CallModel` and `CompactContext` calls hold no
  lock and commit nothing themselves: their outcome goes to the short `RecordPlatformModelResult`
  Step, which re-reads the current authority under the conversation, executor and interrupt locks
  and applies the outcome only when its admitted epoch, generation and turn still match, so a stale
  reply, compaction or failure is dropped (without any promise of order or retention) and can never
  overwrite work admitted later. The model Steps therefore keep none of their current state writes
  (status, running work, the reply, the segment): the admitting Step and this one own every mutation.
  A compaction whose first summary reply contains tool calls, which today makes a second request with
  `tool_choice: none` inside the same invocation, instead returns that need to
  `RecordPlatformModelResult`, which re-admits the second request as a new fenced `CompactContext`
  under the same rule, so the compaction keeps its retry and never sends after a visible takeover.
- `AwaitRemoteAgent` waits for the `RemoteInbox` Channel, steering and (within the existing
  rule `agentloop.TakesNotifications`) notifications, and handles what arrived in sequence
  order:
  - **Public events** (an assistant message, a user message typed directly in Orca, imported
    history) are appended as conversation entries. Activity without a durable meaning (Codex
    progress, its own subagents starting and finishing) is written to the `AgentActivity`
    Stream only.
  - **Tool calls**: each report carries one call. The Step takes the consecutive tool-call reports
    at the head of the inbox, at most 4, and appends them as one assistant entry
    of `tool_use` blocks, exactly the shape of a model reply, become the turn's
    `PendingToolCalls`, increment `TurnModelCalls`, and continue with
    `CheckSteered(ContinueRouteTool)`. From there `RouteTool`, the parallel read-only wave,
    `ExecuteTool`, workspace admission, failures and platform issue reporting run unchanged.
    Their results are ordinary `tool_result` entries, which the Bridge returns to Codex.
  - **Turn completed** continues with `CheckSteered(ContinueAwaitUser)` and returns to
    `AwaitUser`, as at the end of any turn.
- **Immediate Preview.** Remote mode adds nothing here. Every verified, pushed,
  unblocked `commit_and_push` deploys its Preview at once, in both modes: `DeployPreviewHandoff`
  runs from the tool result's decision beside `CheckSteered`, and no turn-end publish path is
  left. A long remote turn that pushes several commits therefore updates the Preview after each
  one, through that path and with the per-result identity and newest-wins rules of
  [the agent harness](../agent-harness-design/notifications-and-feedback.md). RAEXEC must not add a
  remote-mode or turn-end publish, which would deploy twice.
  - **Steering** is applied by `beginSteeredTurn`, which appends the entries the Bridge then
    delivers to Codex as a steer of its running turn.
- `AwaitUser` additionally waits for `RemoteInbox` in either mode: in remote mode, work Codex starts
  on its own (the connecting Builder typing in Orca) opens a turn, like an automatic turn; in either
  mode, a takeover's control item wakes it, because a wait created in platform mode must wake after
  an activation.
- A remote tool call's `CallID` is `remote-<epoch>-<sequence>`, for example `remote-7-42`. It is
  the Sandbox invocation identity (`sandboxd.InvocationIdentity{FlowID, CallID}`), so a retried
  report or a retried Step reaches the same command ledger entry and never runs a command twice.
- The per-turn model-call limit applies unchanged: each admitted tool batch counts as one model
  call, so a runaway remote turn reaches the existing turn-limit question. "Stop and report"
  starts a reporting turn in which every remote tool call is answered `tool_disallowed_in_report`.

### Questions and the requirements card

`request_user_input` and `confirm_requirements` are offered to Codex. `RouteTool` sets
`PendingUserInput` and the turn waits in `AwaitUser` as today. Codex's MCP call returns when the
answers are recorded (the answer entry carries `AnsweredInputCallID`), or after 30 minutes with
"the question is still open; the answers will arrive as a message", in which case the Bridge
delivers the answer entry later as ordinary input.

### Tool approvals

A remote tool call goes through `RouteTool`, so a tool that requires approval waits in
`AwaitToolApproval` exactly as for the platform model, and the Builder who decides is recorded
([collaboration](collaboration.md#actors)). No catalog tool requires approval today. The MCP call
returns when the decision is recorded (a rejection is the `RejectedByUser` failure). A takeover or
Stop while the approval waits cancels `AwaitToolApproval`, which `InterruptAgent` already cancels,
and the call is answered as interrupted; approvals in Orca's own prompts concern only Codex's local
tools, which the platform does not see.

### Stop and takeover

- **Stop** is the existing `InterruptAgent`. `AwaitRemoteAgent` sets the running work to the new
  kind `agentloop.RunningRemote`, which is cancellable, so Stop ends the turn at once;
  `AwaitRemoteAgent` joins the Steps that `InterruptAgent` cancels (`CancelSteps`). The Bridge sees
  the Session's `AgentInterrupt` in its next read and cancels the Codex turn
  (`agentSession.cancel`).
- **The work generation.** `Executor.Generation` is a counter the platform owns. It is never reset
  and never 0 once the Session has entered remote mode: the first activation sets it to 1, and every
  later activation, switch, Stop and takeover increases it. A report stamped 0 (a turn the Bridge
  cannot place) is therefore always refused, also right after the first activation. Every Stop and
  every takeover increases it, under `executorLock`, in the same commit as the interrupt.
  The Bridge stamps every report with the generation of the Codex turn that produced it, and binds
  each turn to a generation by the turn's start, never by when the Bridge first sees an event:
  - a turn the Bridge starts (a delivery) is bound to the generation recorded with that delivery in
    the Bridge's state, before the send;
  - when the Bridge adopts a new generation from `sync`, it cancels the session's running turn, waits
    until Orca reports no turn running, and records Orca's event position at that moment as the
    generation's boundary; a turn whose start event comes before the boundary belongs to an older
    generation, whenever its events reach the Bridge, and one that starts after it (for example the
    Builder typing in Orca) belongs to the new one;
  - after a restart without state, or for a turn whose start the Bridge cannot place, it stamps the
    oldest possible generation, 0, which the platform refuses; only turns that start after the
    Bridge's first boundary are reported.

  That Orca's subscription and history give each event a position in one order is a capability gate
  in ORCACHECK. The platform never needs to remember stopped provider turn IDs.
- **Three fences keep stopped work from running**, so no report produced before a Stop can act
  after it, whether or not it was already admitted, consumed or bound to a turn:
  1. *Admission*: `ReportRemote` refuses a report whose generation is not the current one (or whose
     epoch is not current). That covers the first report of a turn admitted just before the Stop
     and any older stopped turn, however many Stops ago.
  2. *Consumption*: `ReportRemote` writes the generation into the admitted report.
     `AwaitRemoteAgent` and `AwaitUser` drop a consumed report whose generation or epoch is no longer
     current; a dropped tool call is answered `interrupted` through its `tool_result` entry, so the
     Bridge's MCP call returns.
  3. *Admission to run*: a consumed tool call is admitted to run in the same commit that checks for a
     Stop, under `interruptLock`, which `InterruptAgent` also holds:
     - a serial call is admitted by `CheckSteered(ContinueExecuteTool)`, which already writes
       `RunningTool` and the movement to `ExecuteTool` in the commit in which it reads
       `AgentInterrupt`;
     - **changed for both modes**: `RouteTool` gains `interruptLock` and checks `AgentInterrupt` first.
       It runs built-ins (`write_todos`, `request_user_input`, `confirm_requirements`) and starts a
       parallel wave (`RunningWave` and the `ExecuteParallelTool` movements) only in a commit in which
       no Stop of the turn is recorded; otherwise it ends the turn with `endStoppedTurn`, which answers
       every pending call with `InterruptedToolResult`. Today a Stop that commits after
       `CheckSteered` and before `RouteTool` could let `RouteTool` start a wave; this closes that
       window for the platform model too.

     *Admitted* means the commit that wrote `RunningTool` or `RunningWave`; *started* means the
     Execute that calls sandboxd. A Stop after admission finds the running work recorded and cancels
     the Steps `InterruptAgent` lists, which include `ExecuteTool` and `ExecuteParallelTool`; a Step
     cancelled before it calls sandboxd made no effect, and one cancelled after it settles from the
     command ledger. No Flow read is added to the tool path: the check reads the Session's own
     Attribute under a lock the Step already takes.

  A call already executing when Stop commits behaves as today: cancellable work is cancelled and
  settled from the command ledger (`SettleForegroundCommand`); an uninterruptible call
  (`commit_and_push`) finishes, its result is recorded, and the turn ends at the next boundary.
- **Takeover during a pending question** cancels nothing. `AwaitUser` wakes on the `ExecutorChanged`
  item in `RemoteInbox`. Because a takeover never cancels `AwaitUser`, an answer its satisfied wait
  already selected (Dex 1.5.0 consumes the messages of a satisfied condition and persists them as the
  Step's condition results, which its Execute receives) is always recorded by that Execute; only then
  does it answer an unanswered question "interrupted by takeover" and end the turn at the boundary,
  as after a Stop.
- **Takeover** (another connection activates, or a Builder switches to the platform agent) is the
  RPC `SetExecutor`, admitted only against the epoch the caller saw
  ([connection](connection.md#activation-and-takeover)). In one commit it increases the epoch and
  the generation, replaces the connection and, when a turn is running, applies the same interrupt
  plan as Stop (`admitInterrupt`). Only then can a tool Step of the new executor run, because the
  Step graph is sequential. Background commands keep running, as after a Stop, and appear in the new
  executor's view.

### Entering and leaving remote mode

- **Entering**: `SetExecutor` starts an independent `RequestSessionWorkspace` branch with
  `ContinueAwaitRemote` while the existing `AwaitUser` or `AwaitRemoteAgent` Step continues to
  consume `RemoteInbox`. A report admitted during provisioning therefore does not wait for the
  Sandbox outcome. `PlanRequest` joins an existing request or reads context from a usable lease;
  `ApplySessionWorkspace` records the bounded repository context. The branch ends without starting
  another inbox consumer. `ReadRemote` only reads the Workspace Attribute and reports context
  `pending`, `ready`, or `unavailable`; a failed request does not block the remote agent. No new
  Attribute, Channel, Stream, database table, or Flow RPC is involved. A remote activation requests
  the Session's Sandbox only while its stored repository context is empty, including when the agent
  never calls a tool; its existing hold, deadline
  and reclamation bound its lifetime. The Bridge composes the first message from current state.
- **Leaving** (to `platform`, owner decision 2026-10-12): the platform builds a fresh context, with
  no model call and without compacting the old segment, whose earlier entries may already have been
  dropped by retention. The first platform turn after the switch opens a new segment (`SegmentChange`
  reason `executor`) that stays one contiguous range of the conversation, in chronological order:
  1. **Its start** is chosen by scanning the retained entries from the newest back, adding whole
     units until they fill half of `agent.compactionTokens` (estimated at four bytes per token). A
     unit is a user entry, or an assistant entry together with the `tool_result` entries of its
     `tool_use` calls. The scan stops at the first unit that is incomplete (a tool call without its
     result), so the range never has a hole; because the switch interrupts the turn and
     `endStoppedTurn` answers every pending call first, such a unit can only be older than the
     interrupt. The range starts at the first user-role entry of the units taken; `FirstSequence` is
     that entry.
  2. **Its end** is a new handoff entry appended after the last entry (`user` role, kind `notice`,
     display `marker`), so the model reads the recent conversation first and the handoff last,
     immediately before the next message. Studio shows it as one marker ("Switched to the Superverse
     agent"), never as a copy of history.
  3. **Where the facts come from.** `SetExecutor` to `platform` clears `Remote` but, in the same
     commit, writes `Executor.Handoff`: the new platform epoch, `From` (the `ActivatedAt` of the
     remote connection, or, when an unconsumed `Handoff` already exists because the Session went
     remote and back again without a platform turn, that one's `From`), `To` (the switch's commit time),
     the owners (appended; only the latest four are kept) and the reason. It survives retention because it is not an
     entry. No new Step and no wait are needed for an uninterruptible call: the switch interrupts the
     running turn like a Stop, `commit_and_push` finishes and records its result, the turn ends at its
     boundary, and only the next platform turn, whose `planModelCall` sees `Handoff` with
     `Handoff.Epoch == Executor.Epoch` and `Mode == platform`, opens the `executor` segment; that Step
     takes `executorLock` with its existing conversation locks, builds the segment from the retained
     entries (which now include the commit's result) and deletes `Handoff` in the same commit. A
     `SetExecutor` back to `remote` before that turn leaves `Handoff` in place but makes it stale (its
     epoch no longer matches), and a later switch to `platform` rewrites it as above, so an obsolete
     handoff is never built. A Session without `Handoff` (all Sessions before RAEXEC) opens no
     `executor` segment.
  4. **The handoff entry** holds, within 16 KiB in total: the summary of the last platform segment
     before remote mode, if one exists, read from `ContextSegment.Summary` (below) and never from a
     `segment_start` entry that retention may already have dropped, cut to 8 KiB and marked as older; the requirements card;
     HEAD; the todo list; running background commands. It opens with the canonical first line that
     `remoteexec` renders and parses (owners, the remote interval, the reason and HEAD), followed by
     one fixed sentence, also a `remoteexec` constant that the parser requires: "Only the recent part
     of that work is in the context above; older work is in the repository and its commits."

  **Two supporting changes.** `ContextSegment` gains `Summary string`: the summary `openSegment`
  wrote into the segment's `segment_start`, cut to 8 KiB, so the platform segment that was current
  when the Session went remote keeps its summary for the handoff after retention drops its entries
  (an optional field; older segments have none and the handoff then has no older summary). Request
  assembly gains a typed shape for the `executor` segment only: its range may start at a user entry
  that is not `segment_start` and not a `tool_result`, and adjacent assistant entries without an
  outstanding `tool_use` are merged into one assistant message; every `tool_use`/`tool_result`
  pairing check and the trailing-user check stay as they are, and other segments assemble exactly as
  today.

  Remote-era entries carry no thinking blocks, which the API accepts for earlier turns. Because the
  request shape changes, RAEXEC asks the owner whether and with which model to run
  `make test-claude-live`; it does not run a real model on its own.
- **Settlement watermark.** `settledThrough` is the highest report sequence of the current connection
  and epoch whose outcome the Session durably recorded. It is derived, not stored:
  `min(Consumed, sequence of the first pending remote call of the epoch - 1)`, computed by `ReadRemote`
  from `Executor.Remote.Consumed` and the AgentState pending-call suffix it reads in the same query.
  `Consumed` advances under `executorLock` in the AwaitUser or AwaitRemoteAgent commit that consumes a
  report, stale-generation and idle `TurnCompleted` reports included, and that commit also stores the
  report's tool calls as pending. A call stops capping the watermark in the commit that appends its
  canonical `tool_result` (real, `interrupted` or `EffectUnknown`) and moves the pending cursor: the serial
  result, the parallel join (branches finish out of order and do not move the cursor), a Stop
  (`FinishInterrupt`), a takeover drain, or the stale-batch rejection. So the watermark never decreases,
  and no lock-free tool execution writes `Executor`. A refused report was never admitted and has no outcome
  to settle. `sync` exposes it as `settledThrough` inside the state digest for the current connection and
  epoch only; the Bridge's restart drain waits for it ([bridge](bridge.md)). Admission answers
  `duplicate` for any sequence at or below `Reported` of the same connection and epoch before it checks the
  generation, so a `stale_generation` refusal proves the report was not admitted and the Bridge may reuse
  its number. Pre-launch, an executor persisted before `Consumed` existed reads it as 0 and is not
  supported across the change (explicit reset); historical settlement is never inferred from an empty
  inbox or from admission alone.
- **Retention**: today `trimRetainedEntries` drops only archive chunks before the current
  segment. A remote Session never moves its segment, so in remote mode the 1,000-entry retention
  window counts back from `LastSequence` alone. The fresh platform segment references only retained
  entries, so no request ever points to a dropped chunk.

## Why these primitives and no others

The earlier draft of this design proposed ten new Attributes and AttributeMaps, a Channel, six
Steps and a separate Run-observing lifecycle inside the Session. Each was checked against the
existing Session; this design keeps only what the existing primitives cannot do.

| Proposed | Decision | Reason |
|---|---|---|
| `Executor` Attribute | **Kept** | The single fence every remote write checks; must commit atomically with turn interruption. |
| `PendingRemoteConnection`, `RemoteCredential` | Dropped | Credentials are authenticated per request through SQL ([connection](connection.md)), as AGENTS.md requires for authentication; a Flow read per request is avoided. |
| `RemoteSync` (cursors and watermarks) | Only `Reported` kept, in `Executor.Remote` | The admission watermark must commit with the report. The delivery cursor is the Bridge's own (it reads entries after a sequence it keeps), so reading never writes the Flow. |
| `RemoteTurn` | Replaced by `Executor.Generation` | The turn is the Session's existing turn; one platform-owned counter fences all stopped work, instead of remembering provider turn IDs. |
| `RemotePublicActivity` | Dropped | Progress is Stream activity; durable facts are conversation entries. |
| `RemoteOutbox` AttributeMap | Dropped | The conversation is the outbox: the Bridge delivers the entries after the sequence it last delivered, which it keeps itself. |
| `RemoteOperations` AttributeMap | Dropped | A tool call's result is its `tool_result` entry, recorded in sequence order; the call ID `remote-<epoch>-<sequence>` matches it to the call. |
| `ObservedRuns` AttributeMap | Kept, in [run feedback](run-feedback.md) | Needed for its own feature in both modes; not part of remote mode. |
| `RemoteInbox` Channel | **Kept** | The only way a Bridge report can wake `AwaitUser` or `AwaitRemoteAgent`. |
| Steps `AwaitRemoteActivity`, `DispatchRemoteInput`, `ApplyRemoteEvents`, `RouteRemoteTool`, `QuiesceExecutor` | Replaced by one Step, `AwaitRemoteAgent` | Delivery is a read; routing, execution and quiescing are `RouteTool`, `ExecuteTool` and the existing interrupt. |
| Remote turn deadline and operation budget | Dropped | The existing per-turn model-call limit counts tool batches; Stop and takeover end a turn. |
| Immediate Preview handoff after each push | **Kept**, without a primitive or any remote-mode code | Every verified push in either mode deploys through the existing `DeployPreviewHandoff`. |

## Dex Flow Changes

### SessionAgentFlow

**Identity and execution lifecycle.** Unchanged: Flow type `SessionAgentFlow`, FlowID
`session-<projectID>` (for example `session-a7k2`), Start RequestID
`flowid.Fingerprint(struct{ProjectID string})`, `IDReuseDisallow`, started once by ProjectFlow and
completed only by `CompleteProjectSession` when the Project is deleted. Pairing, activation,
takeover, reconnection and switching modes never start a Flow, never create a new FlowID or Run,
and never complete the Session. Retries of every new RPC and Step are idempotent as stated below.

**Attributes.**

| Name | Type | Contents | Locking, growth, retention |
|---|---|---|---|
| `Executor` (new) | `remoteexec.Executor` (ordinary struct) | `Mode` (`platform`/`remote`, a typed closed set), `Epoch uint64` (starts at 0, +1 on every change), `Generation uint64` (0 until the first activation, then at least 1, +1 on every change and every remote Stop; never reset), `Remote *remoteexec.RemoteExecutor`, `Handoff *remoteexec.PlatformHandoff` (the pending switch back, below) | New `executorLock`. Fixed size. Kept for the Session's life; not indexed; no secrets. |
| | `remoteexec.PlatformHandoff` | `Epoch uint64` (the platform epoch it belongs to), `From time.Time`, `To time.Time`, `Owners []conversation.Actor` (the latest four remote connections' owners, oldest first, so the newest owner is always kept), `Reason` (a typed closed set: `switched`, `connection_removed`) | Written by `SetExecutor` in the commit that switches to `platform`, consumed (deleted) by the Step that opens the `executor` segment; see [Leaving](#entering-and-leaving-remote-mode). |
| | `remoteexec.RemoteExecutor` | `ConnectionID`, `Owner conversation.Actor` (the connecting Builder), `Binding` (Orca and Codex versions, Orca session ID and provider thread ID, for display), `Reported uint64` (highest admitted report sequence), `Consumed uint64` (highest admitted report sequence the Session consumed; `settledThrough` is derived from it, see below), `ActivatedAt time.Time` (the commit time of the activation) | Cleared on a switch to `platform`; replaced on takeover, so both watermarks restart at 0 per epoch and stay O(1). `Reported` is written only by `SetExecutor`, `ReportRemote` and `InterruptAgent`; `Consumed` only by the consuming AwaitUser or AwaitRemoteAgent commit, all under `executorLock`; never by a read. |

Every other Attribute is unchanged; [collaboration](collaboration.md#dex-flow-changes) adds the
actor fields.

**AttributeMaps.** No new AttributeMap for remote mode. `CurrentMessages` keys stay unpadded
decimal sequences (`37`) and `ArchivedMessages` chunks stay keyed by their first sequence
(`131`); the retention change above applies in remote mode. [Run feedback](run-feedback.md)
adds `ObservedRuns`.

**Channels and ChannelMaps.**

| Name | Payload | Producers and consumers | Admission, ordering, duplicates, cleanup |
|---|---|---|---|
| `RemoteInbox` (new Channel) | `remoteexec.InboxItem`, a typed union of exactly one of `Report *RemoteReport` and `Control *ExecutorChanged` (`{Epoch, Generation}` for diagnostics only; consumers read the current `Executor`). Only `ReportRemote` builds a `Report`, from the Bridge's report DTO, which has no control field, so a Bridge cannot submit a control item; only `SetExecutor` builds a `Control`. `RemoteReport`: `Epoch`, `Generation`, `Sequence uint64`, `ProviderTurn` (display only), and one of `Events []RemoteEvent` (at most 32; kinds `assistant_message`, `user_message`, `imported_message`, `activity`, `delivery_uncertain`, `delivery_failed`; text at most 64 KiB each), `ToolCall{Name, Arguments}` (arguments at most 256 KiB), `TurnCompleted` | Produced by the RPC `ReportRemote`, and by `SetExecutor`, which adds a server-generated `ExecutorChanged` wake item in the same commit only when no control item is pending: it loads `RemoteInbox`'s pending messages under `executorLock` and publishes only if none of them is a control item. The item carries no state and its consumer reads the current `Executor`, so a pending item already covers every later change. The bound is enforced by that admission check: at most one control item is pending, and at most one more has been selected by the single active consumer (`AwaitUser` or `AwaitRemoteAgent`, never both) and not yet committed. A consumer cancelled after selecting one loses nothing: the next `SetExecutor` finds none pending and publishes again, and every consumer applies the current `Executor` whatever woke it. No flag mirrors the backlog. The report limit counts only pending report items and their payload bytes, read the same way. Control items do not count toward the report limit, which counts only pending reports and their actual payload bytes; consumed by `AwaitRemoteAgent` and by `AwaitUser` in either mode, because an idle wait created in platform mode must wake after an activation | `ReportRemote` admits under `executorLock` only when `Mode` is `remote`, the connection, epoch and generation are current, fewer than 8 reports are pending and their encoded size together with this one stays within 2 MiB, and `Sequence == Reported + 1`; otherwise it answers 429 and the Bridge retries the same report after backoff. `Sequence <= Reported` is a duplicate and returns the current state without publishing; a gap is refused with the expected sequence. Consumption is FIFO by sequence. A consumer drops a report whose epoch or generation is no longer current and answers its tool call `interrupted` ([the three fences](#stop-and-takeover)). On a switch, pending reports of the old epoch are drained the same way. `CompleteProjectSession` leaves the Channel unconsumed, as it does the existing Channels. |

No new ChannelMap. Remote tool calls reuse `ParallelToolResults` and `ToolApprovals` as any
tool call does.

**Streams.** No new Stream. `AwaitRemoteAgent` writes Codex's text to `AssistantText` as each
complete item arrives and Codex activity to `AgentActivity` with one new event kind,
`remote_activity` (a short public summary: Codex subagent started or finished, local tool used,
or waiting for approval in Orca). `ReasoningSummary` is never written in remote mode; hidden
reasoning never leaves Orca. The 10 MiB budgets stay shared per Flow type and Stream name
(pinned SDK); Studio and the Bridge recover from the Snapshot and the conversation entries.

**Steps.**

| Step | Kind | Purpose | Primitives |
|---|---|---|---|
| `RecordPlatformModelResult` (new) | short Execute only, `conversationStepOptions` plus `executorLock` and `interruptLock`; reached only from `CallModel` and `CompactContext` (including their Execute-failure routes) | Applies a platform model call's outcome in a fresh commit: the reply (with its tool calls and their `PendingToolsAdmission`), the compaction's new segment, or the failure, but only when the movement input's admitted `{Epoch, Generation, Turn}` still equals the current `Executor` and `AgentState.Turn`; otherwise it drops the outcome and never clears or overwrites newer state. The long provider call holds no lock and writes nothing, as AGENTS.md asks of long external calls. Its input is the model Step's outcome (the existing permitted provider response blocks the Session already stores, never more hidden reasoning than today and never exposed to the remote agent, the UI or logs; at most the model's output limit plus the failure detail, already truncated) plus the admission `remoteexec.Fence{Epoch, Generation}` and the turn. That input is a durable Dex execution payload: it adds the outcome to the Session's execution history for as long as the engine retains that history (for an open Session, possibly its whole life), and creates no new Attribute, Channel or table. The encoded outcome is capped at 16 MiB (blocks, text, tool calls, metrics and the admission together); a larger reply is never persisted or cropped (a cut could split tool JSON or a signature) and becomes a declared model failure instead, with no raw content in its diagnostics and no retry of the successful provider request; open executions whose model Step started before RAEXEC carry no admission and are treated as epoch 0, generation 0, which matches every Session without an executor. | Reads `Executor`, `AgentState`; appends the reply or failure entry; sets `PendingToolCalls` and `PendingToolsAdmission`; opens a segment for a compaction |
| `AwaitRemoteAgent` (new) | WaitFor + short Execute, `conversationStepOptions` (takes `MessageWindowLock`) plus `executorLock` | Wait for the remote agent's reports, steering and notifications; apply them as described above | Reads `Executor`, `AgentState`; consumes `RemoteInbox`, `SteeredUserMessages`, `SessionNotifications`; appends entries; sets `PendingToolCalls`; writes `AssistantText`, `AgentActivity` |
| `AwaitUser` (changed) | as today, plus `executorLock` | Also waits for `RemoteInbox` in either mode: a report opens a turn (remote mode), a control item applies the current `Executor` (takeover during a pending question, above) | as today, plus `RemoteInbox` |
| `CheckSteered`, `AnsweredInput` (changed) | as today, plus `executorLock` (as every Step that admits a model call) | `planModelCall` returns `nextAwaitRemote` in remote mode and admits a model call with the epoch read under the lock | also reads `Executor` |
| `RouteTool` (changed, both modes) | as today, plus `interruptLock` | Checks `AgentInterrupt` before running a built-in or starting a wave, and ends the turn with `endStoppedTurn` when a Stop of the turn is recorded. In remote mode it resolves the call against `agentloop.RemoteTools()` instead of the segment's tool names, for `SegmentTool` and `ParallelToolBatch` alike, and never reads `ContextSegment` | also `AgentInterrupt` |
| `InterruptAgent` RPC (changed) | as today, plus `executorLock` | Also cancels `AwaitRemoteAgent` and, in remote mode, increases `Executor.Generation` in the same commit | also `Executor` |

`AwaitRemoteAgent` has no timer: it waits until a report, steering, a notification, Stop or a
takeover arrives. An offline Bridge leaves the turn waiting; Studio shows that and offers Stop and
"Use the Superverse agent". A waiting turn does not keep the Sandbox: the
[Sandbox deadline](../agent-harness-design/session.md#sandbox-deadline) moves only when a tool with
workspace access is dispatched, and `AwaitSandboxDeadline` runs beside the turn as it does today, so
the Pod is reclaimed on schedule and the next remote tool call re-provisions it on the same volume.

**RPCs.**

| RPC | Kind | Purpose and checks | Primitives |
|---|---|---|---|
| `SetExecutor` (new) | write, `ShortRPC` locking `SessionStatus`, `Executor`, `AgentInterrupt` | Input `{Mode, ConnectionID, Owner, Binding, BasisEpoch}`; output `{Epoch, Generation}`. Admitted only when `Executor.Epoch == BasisEpoch` (nobody switched since the caller looked), or as an idempotent repeat when the connection is already the executor (returns its epoch); otherwise refused `executor_changed` with the current epoch. Interrupts a running turn as `InterruptAgent` does, except that it never cancels `AwaitUser` waiting on a question (the control item wakes it instead). Loads `RemoteInbox`'s pending messages and publishes one `ExecutorChanged` control item only when none is pending. Called only by the Session Service after it authorized a Builder ([connection](connection.md#activation-and-takeover)). | `Executor`, `AgentInterrupt`, publishes `RemoteInbox`, cancels in-flight Steps other than a question's `AwaitUser` |
| `ReportRemote` (new) | write, `ShortRPC` locking `Executor` | Admit one report as described under `RemoteInbox`; the report must carry the current epoch and generation. Returns the current epoch, generation and `Reported`. | `Executor`, publishes `RemoteInbox` |
| `ReadRemote` (new) | read, unlocked; loads only `CurrentMessages` plus, when asked, exact `ArchivedMessages` chunk keys | For the current connection and epoch only: the current `Generation`, `LastSequence`, the projected entries after the sequence the Bridge names ([connection](connection.md#the-remote-agent-endpoint): first entry whole, then a 1 MiB soft budget, `examinedThrough`, the dropped range), `AgentInterrupt`, `PendingUserInput`, `ConfirmedRequirements`, `TodoList`, HEAD, the state digest and the digest of the remote tool set. A stale connection gets `executor_changed` and nothing else. **Bounded loading.** `ReadRemote` never loads a whole message map: each pass loads by exact keys either at most 16 `CurrentMessages` entries after `after` or exactly one `ArchivedMessages` chunk (10 entries), so one pass holds at most 16 entries at their admission maximum. A sync waiting for a wake-up holds no messages; only a pass that loads entries takes one of `remoteAgent.maxConcurrentReads` (default 4 per replica) slots, waiting for one within the request's deadline and answering `rate_limited` (429) after it, so a thousand idle long polls never become a thousand hydrations. **Archived entries** (a Bridge that fell behind the current window): a first pass that finds `after` below the current window answers `NeedArchiveKeys` with the next exact chunk key derived from the retained range and `after`; the client invokes `ReadRemote` again with exactly those keys loaded, and that pass re-checks connection and epoch and re-derives the keys. If retention moved the floor in between, the client retries once; a second move answers `unavailable` (503) and the Bridge retries after backoff. One deadline covers the whole operation; nothing durable is written and no full archive is loaded. **Identifiers it exposes** stay bounded: an entry's `answeredInputCallId` is sent only when it is a canonical remote call ID (`remote-<epoch>-<sequence>`, at most 48 bytes) and omitted otherwise, and a pending question's call ID is omitted unless it is one, because the Bridge can answer only its own calls; the question's text is still sent. | reads only |

**Recovery and external effects.** Tool effects keep their identity, ledger and query-before-
mutate recovery. A lost `ReportRemote` response is retried by the Bridge with the same sequence
(duplicate, same answer). A lost `SetExecutor` response is retried with the same connection
(idempotent). No new external effect happens in a Step: the Bridge, not the Flow, talks to Orca.

**Where the code lives.** `session_agent_flow.go` (1,479 lines, limit 1,600) gains only the
declarations of `Executor`, `RemoteInbox`, `executorLock`, the `AwaitRemoteAgent` Step type with its
StepOptions, the three RPC methods and their registration, each body a one-line call. The bodies of
the remote branch live with the platform model's turn code: RAEXEC renames `session_model.go` (the
model side of a turn, 396 lines) to `session_executor.go`, the code of whoever produces a turn's
next step, the platform model or the remote agent. It is one responsibility with two
implementations; the package is at its limit of 10 files, so it gets no new file, and the merged
file stays under 1,000 lines. The rules themselves stay in `remoteexec`. RAEXEC also moves the
option builders and admission helpers of the workspace Steps into `session_workspace.go`. After
these moves, the Flow file stays at or below 1,520 lines.

**Compatibility.** Pre-launch: no Step is renamed or removed; `Executor` absent means `platform`
with epoch 0, so open Sessions continue after the Worker is deployed. The Worker that reads the
new primitives is deployed before Studio offers pairing.

**Tests.** Listed in [Tests](#tests).

### Other Flows

PreviewFlow, SessionWorkspaceFlow, SandboxProvisionFlow, SubagentFlow, BackgroundCommandFlow,
PlatformIssueFlow and ConnectorContributionFlow are unchanged by remote mode. Codex never starts
a platform subagent: `start_agent` and `merge_agent_changes` are not in the remote tool set.

## Remote tool set

`agentloop.RemoteTools()` (in `agentloop`, which owns the catalog; `remoteexec` sits below it and
does not import the catalog) is the Session catalog without the tools that call a platform model or
only make sense to the platform model:

- **Offered**: `read`, `edit`, `write`, `grep`, `glob`, `bash`, `read_task_output`, `stop_task`
  (commands only), `commit_and_push`, `connector_catalog`, `read_delivery_status`,
  `read_platform_flow`, `read_live_app_runs`, `request_user_input`, `confirm_requirements`,
  `write_todos` (shown as the progress list in Studio), and `watch_preview_run` once RUNFEEDBACK
  adds it ([run feedback](run-feedback.md#scope)).
- **Not offered**: `start_agent`, `merge_agent_changes` (platform subagents), `set_effort`,
  `start_timer` (platform model controls).

The remote `agentContext` contains `remote_agent.md`, the shared Session instructions,
the source handoff and delivery rules, and the repository context. The platform-only
connector contribution, subagent delegation and effort instructions remain in the
platform model's segment in their original order. Until remote connector contribution
is available, Codex chooses published connectors with `connector_catalog` and the Dex
skills; for a new connector or a fix, it tells the user through `request_user_input`
that remote mode cannot contribute it yet and suggests a workaround when one exists.

**Routing.** Today `RouteTool` resolves a call against `ContextSegment.ToolNames`, the set frozen for
the platform model's segment. Remote mode never opens or compacts a segment, so a fresh Session has
none and an older platform segment names tools Codex must not use (`start_agent`). In remote mode
`RouteTool` therefore resolves every call, and builds every parallel batch, against the names of
`RemoteTools()`, and refuses any other name with the existing "tool not offered" result: the allow-list
is enforced by the server, whatever the Bridge lists. The platform segment is neither read nor changed,
and the platform model keeps its own set when the Session switches back.

The server sends this set (names, descriptions and input schemas from the same
`toolinput/schema/*.json` files) in the `activate` answer and its digest in every `sync` answer; the
Bridge re-lists its MCP tools when the digest changes. A Bridge therefore never carries its own copy
of the catalog, and its minimum version covers only the protocol. The strict decoders validate a
remote call exactly like a model's.

## Database Schema Changes

None in the Session. The connection credential table is in [connection](connection.md).

## UI/UX

- The chat header shows the executor: "Superverse agent" or "Codex via Orca · connected by
  sam@example.com", with online, offline (last seen) or "working" state.
- While a remote turn runs, the composer works as today: messages queue or steer, Stop stops.
  Remote assistant replies render as assistant messages with a small "Codex" label; tool calls
  render in the work log like the platform agent's.
- "Connect my agent" and "Use the Superverse agent" are in the executor menu; both say what
  happens to a running turn ("the current work stops").
- Offline: a banner "Codex is offline; messages are kept until it reconnects" with the two
  actions above. Nothing switches automatically.

## Tests

Dex integration tests in `internal/agents` against the real lane (Dex, Sandbox, local Git host,
PostgreSQL), driving the real remote agent endpoint over HTTP as a Bridge would; the remote
HTTP admission case checks `pending` at the first context read, reports a real Sandbox read
while provisioning is in flight, then checks its tool result, `SettledThrough`, `ready`, and
the AGENTS.md content in `<repository_context>`. A forced real-dependency provisioning failure
needs a local Git fixture whose committed template manifest is malformed before activation;
until it exists, the `unavailable` path is unverified by integration tests.
agent's decisions come from the test, the way an E2E test drives Studio:

- Activation, a delivered message, a tool call that edits and commits in the Sandbox, the
  result returned, the Preview handoff deployed right after the push while the turn continues, turn
  completed. No platform model call is made in that interval, which the test shows without a
  counter. It starts from a quiescent Session: fresh or finished, with no model, compaction or
  subagent work running or retrying. It then reads the Session's Dex Step history, fully paginated
  (missing or truncated history fails the test), and finds no `CallModel`, `CompactContext` or
  `RecordPlatformModelResult` Step and no subagent start between the activation and the switch-back
  commits. Those are the only places a Session calls the provider, and the remote catalog has no
  `start_agent`. Positive controls before activation and after the switch-back show those Steps
  running, and the conversation shows no model failure, which the scripted scenario would produce
  if the platform asked the mock for a reply. This does not cover work already admitted before an
  activation: such work may still finish or be billed under the cancellation rule above. The
  takeover tests check that it is fenced instead.
- Two pushes in one remote turn in quick succession, with the first branch's request delayed until
  after the second's: the Preview deploys the second commit, the first answers `DUPLICATE`, and a
  retried branch requests nothing twice. Status: required and not yet met at runtime. A bounded hold
  of the first branch did not fit the second push's real source gate (the first gate took 37.4 s;
  the second gate's duration was not established). The captured-identity property is verified by the
  immediate-deploy case; the ordering rule itself is PreviewFlow's, covered by
  `TestPreviewDeploymentAnswersDexIntegration`. Whether that narrower evidence suffices is the
  owner's decision at publication.
- Parallel read-only tool calls form one wave; a mutating call runs alone.
- Duplicate, out-of-order and gapped report sequences; a report of a stopped provider turn.
- Takeover while a `bash` command and a `commit_and_push` are in flight: no duplicate command,
  commit or Preview request; the old connection's reports and reads are refused.
- Switch back to `platform`: the next turn opens the `executor` segment and the platform model
  continues (mock model). The scenario `remote-handoff` scripts the remote agent's replies as
  `remote` steps, one per rendered assistant message, that the test submits through the real
  endpoint, and marks the platform step after the handoff with `expect_handoff`, which requires the
  canonical handoff entry right before it. The mock checks every message of the fresh segment
  exactly: remote steps by text, tool, input and their exact `remote-<epoch>-<sequence>` IDs,
  platform replies with their signed blocks as always. No marker, input kind or request field
  relaxes the check.
- Questions: the Session records a typed waiting result for `request_user_input`, which `sync` marks with its status, and the Bridge keeps the MCP call open until the answer entry arrives; an answered `request_user_input` returns as the MCP result; an unanswered one after
  the timeout arrives later as input.
- The turn-limit question and "Stop and report" in remote mode. Known limitation: the explicit
  report instruction is a hidden system-role notice, and the remote projection delivers only
  user-role input, so the remote agent learns the choice from the user's answer and from the refusal
  of its mutating calls, not from that instruction. The owner decided (2026-10-13) to keep it that
  way: the refusal enforces the report-only turn. If the acceptance with real Codex shows that it
  does not write a report, the first fix is clearer wording in the refusal hint or in the delivered
  answer, not a new projection of system instructions.
- Retention in remote mode keeps at most 1,000 entries.
- Catalog: the first tool call of a fresh Session in remote mode (no segment yet) is routed; a remote
  call of `start_agent` in a Session whose earlier platform segment offered it is refused as not
  offered; a parallel read-only batch is built from the remote set.
- Generation: a report stamped 0 right after the first activation is refused.
- A remote Session of more than 1,000 entries switched to `platform`: the fresh segment is one
  contiguous, chronological range of whole units within half the compaction budget, ending with the
  handoff entry of at most 16 KiB, never touching a dropped chunk or an incomplete batch; the
  platform model (mock) continues; with a pre-remote summary and without one; Studio shows one
  marker and no repeated history.
- Stop races still needing coverage: a report of a turn stopped two Stops ago; a turn that started
  before a Stop whose first event reaches the Bridge after it, and after a second Stop; a call executing
  when Stop commits (cancellable and `commit_and_push`). Each ends with one `interrupted` or real result,
  never a second execution. `TestRouteToolStopFenceDexIntegration` covers the built-in and parallel
  wave `CheckSteered` to `RouteTool` windows in remote and platform modes (W1-W4).
  `TestStaleFirstRemoteReportAfterIdleStopDexIntegration` covers the first Codex turn report admitted
  before Stop and consumed afterward by `AwaitRemoteAgent` and `AwaitUser` (W5).
- Two `SetExecutor` calls with the same basis epoch (two Builders activating at once): one wins; a
  delayed retry of the loser, and of a connection that was superseded, is refused.
- Worker restart during `AwaitRemoteAgent` and during a remote tool Step.
- A remote turn waiting in `AwaitRemoteAgent` with the Bridge offline past `sandbox.hold`: the Pod
  is reclaimed, the turn keeps waiting, and the next remote tool call provisions a new instance on the
  same volume.

Studio: one spec drives pairing, a remote reply and takeover against the Native lane. The three
E2E happy paths are unchanged.

## Documentation

- [agent harness: session](../agent-harness-design/session.md): the step graph gains
  `AwaitRemoteAgent` and the `Executor` row; link here.
- `internal/agents/session/README.md` and `agentloop/README.md`: the Flow contract rows.
- AGENTS.md, CLAUDE.md and `.cursor/rules/planning.mdc`: the list of the Session's Flow RPCs
  already says "the Preview deployment request each pushed commit makes," which covers
  remote mode unchanged.
- The same files: remote mode makes no platform model call,
  added to the list of SessionAgentFlow behaviours. The paragraph on the Session's Flow RPCs needs no
  new exception: remote mode makes no Flow RPC, and Run observation is registered by the API with
  PreviewFlow, not by the Session.

## Resource Scalability

No new cloud, Kubernetes, or database resource type. Per Session: one fixed-size Attribute; at most
one existing Sandbox workspace is requested when remote activation finds no stored repository context, even if no tool runs.
`PlanRequest` joins an in-flight request or uses a READY lease; the Sandbox hold, deadline, and
reclaim path bound its lifetime. No new resource type or control-plane call per sync is added.
8 pending `RemoteInbox` reports and 2 MiB in total (one report is at most 512 KiB: the endpoint's body
limit); at most one pending and one selected control item (a few bytes each). Per server replica:
`remoteAgent.maxConcurrentReads` (4) message-loading `ReadRemote` passes, each holding at most 16
entries at their admission maximum plus the SDK's decode copies (RAEXEC records the measured
worst case per entry); waiters for a slot are bounded by `remoteAgent.maxLongPolls` and give up at
their request deadline. Dex load per connected
Bridge is in [connection](connection.md#resource-scalability).
