# Streaming and Studio

What reaches the browser while the agent works, how the durable conversation is
projected for the public API, how Studio keeps the live and durable views consistent,
and what Studio shows.

The model side is in [context management](context-management.md), the Flow that
produces the events in [session](session.md), and tasks and subagents in
[subagents](subagents.md) and [notifications](notifications-and-feedback.md).

## Principles

- **Snapshot first.** Every connection starts with the canonical Session snapshot.
  Durable state lives in Flow Attributes, AttributeMaps and Channels; the Snapshot is
  how a reader recovers it.
- **Streams are best effort.** They can lose events and can show events before a Step
  commits. They never establish a business result.
- **Show what the agent is doing, not raw output.** Tool name, target and status stream
  live, together with the assistant's words and a reasoning summary. File contents,
  diffs, command output and signed thinking never leave the Flow.
- **Reasoning is not kept.** A reasoning summary is live presentation only. It is not
  recoverable after the reply commits or the page reloads, by design.
- **The shared Agent UI is transport-free.** Studio owns page orchestration and a thin,
  typed mapping of the generated contract.

## Actor projection

The canonical response contract carries `Actor{account_id,email}` on public and pending user
messages, `steered_by` on pending steering and retained public messages, `confirmed_by` on a version of the requirements confirmed while cards waited (unset on a recorded card) and the
model/auto-deploy changer on AgentDescription. Requests carry no actor. Public content remains
raw user text, while the immutable provider block contains the sender envelope. A Stop marker
carries its first actor. Historical and automatic entries may have no actor. Snapshot and archive
reads own recovery; best-effort activity remains advisory. Human-facing author labels and queue
controls are the separate ACTORSTUDIO slice.

## Streams

Three Dex Streams carry live progress. SessionAgentFlow and SubagentFlow each declare
them; the Stream names are the same. SessionAgentFlow declares a fourth,
`RequirementsDraft`, for the requirements card the agent is writing.

| Stream | Payload | Written by | Read by |
| --- | --- | --- | --- |
| `AgentActivity` | `AgentEvent` | The Flow's Steps: model lifecycle, tool lifecycle, input consumption, compaction, interrupt | The Session Watch; for a RUNNING subagent, the subagent's activity as a one-line status |
| `AssistantText` | string chunks | `CallModel` | The Session Watch (live assistant text) |
| `ReasoningSummary` | string chunks | `CallModel`, from summarized thinking | The Session Watch (reasoning area) |
| `RequirementsDraft` | `RequirementsDraft`, the whole draft per event | `CallModel`, from a `confirm_requirements` call's partial input | The Session Watch (the Brief, drafting) |

### From Claude events to Streams

| Claude event | Destination |
| --- | --- |
| `thinking_delta` (summarized) | `ReasoningSummary` |
| `text_delta` | `AssistantText` |
| `content_block_stop` of a `tool_use` | `AgentActivity` `model_tool_call{tool, target}`; for `confirm_requirements`, also its complete draft |
| `input_json_delta` of `confirm_requirements` | `RequirementsDraft`, at most once a second (below) |
| A `fallback` block starts | `AgentActivity` notice naming the model that continues |
| `signature_delta`, `input_json_delta` of any other tool | Accumulated only; never streamed |

Summarized reasoning and the text between tool calls are the "what is happening" signal.
The Haiku profile uses budget thinking and sends no `display` parameter; its thinking
deltas feed the same Stream.

### Requirements drafts

`confirm_requirements` streams its input eagerly ([tool orchestration](tool-orchestration.md#strict-schemas)),
so the provider sends the card as the model writes it. The Claude adapter passes the input a card's
`tool_use` block holds so far to the request's tool input writer after each `input_json_delta` and once
when the block stops; no other tool's input reaches it. `CallModel` hands it to an
`agentloop.RequirementsDrafter`:

- **Cadence.** While the input grows it is read at most once a second; a read writes a draft only when it
  holds something and differs from the call's previous draft. The complete input is always written, with
  `complete` set. A new call ID starts over.
- **Lenient read.** `conversation.ClosePartialJSON` closes the open JSON: an open string value ends where it was cut
  (a partial escape dropped), an unfinished key, number or literal falls back to the last complete value,
  and every open array and object is closed. The result is read into the draft; a field of another type is
  skipped, and a malformed input, or one over 96 KiB, writes nothing.
- **Payload.** `RequirementsDraft{call_id, turn, base_revision, title, summary, features{id,text},
  connections{need,connector}, design_summary, flows{flow_type, step_types}, complete}`, bounded to the
  card schema's lengths and counts (at most 20 features, 10 connections, 6 Flows of 30 Steps). `turn` is the
  model call's turn and `base_revision` the confirmed requirements' revision when it started, 0 when none.
  Every event is the whole draft, so a lost event only means that a later one shows.
- **Not the card.** RouteTool records or refuses the complete input as before; the draft never becomes the
  card. A draft is over once a Snapshot's `confirmed_requirements` revision passes `base_revision`, a tool
  result answers its call, or its turn ended (`turn` passed, or the same turn waits for a message).

### Activity events

`AgentEvent` fields: `kind`, `message` (condensed), `call_id`, `tool_name`, `target`,
`message_sequence`, `attempt` and `input_consumption`.

- **`target`** is at most 200 characters and says what the call works on: `bash` uses its
  `description`, the file tools use the path, `grep` and `glob` the pattern. The per-tool
  rule is in [tool orchestration](tool-orchestration.md).
- **`tool_progress`** carries only the attempt number. Command output is not streamed.
- **Kinds.** The input, answer, steering, snapshot, model, tool, approval and timer kinds
  stay. Added: `agent_interrupted` and the fallback notice. `compacted` and
  `compaction_failed` stay. Plan kinds and fields and the tool-recovery kinds do not exist.
- `snapshot_required` tells live readers that durable interaction state changed and the
  Snapshot should be read again.

## Public projection

The Flow's read RPCs (`GetSnapshot`, `GetArchivedMessages`) convert stored
[entries](context-management.md#entry-log) into the public `AgentMessage` inside the Flow.
Signed thinking, full tool arguments and tool results never leave it.

| Entry kind | Projected as |
| --- | --- |
| `user_message` | `ROLE_USER` with the text. |
| `model_reply` | `ROLE_ASSISTANT` with the text and, for each tool call, its id, tool name and target. No arguments, no thinking. |
| `tool_result` | `ROLE_TOOL` with the call id, tool name and status. A failure adds the code and owner (`call`, `application` or `platform`). The result text is not included. |
| `notification` | `ROLE_NOTIFICATION` with a bounded display text. It is not a user message. |
| `segment_start` | Nothing for the first; a context marker after that. |
| `notice` | Nothing, except the stop marker. |

**Markers** are divider entries with no conversation text:

| Marker | Appears when |
| --- | --- |
| Context compacted | A compaction opened a segment. |
| Context refreshed after a platform update | A rollover opened a segment. |
| Earlier context could not be summarized | An `unsummarized` segment opened. |
| Stopped by user | The user pressed Stop. |

The summary text of a compaction is never projected.

**Metrics** ride on the assistant message: the model that completed the call, the effort
sent, duration, time to first event, input, cache read, cache write, output and thinking
tokens. An absent counter is unknown, not zero.

**Tasks in the Snapshot.** `AgentDescription.subagents` lists the Session's subagents in start order, the
ones that may still run and the latest 50 ended, as `AgentSubagent`: the task ID (`agent-<id>`), kind
(`investigate`, `change`, `connector_select` or `connector_develop`), description, status (`STARTING`, `RUNNING`, `COMPLETED`, `FAILED`, `STOPPED`,
`TIMED_OUT`, `MERGED` or `DISCARDED`), start and end times, the one-line summary of its end and, when it
saved changes, the files they touch with their added and deleted lines (`changed_files`, `insertions`,
`deletions`). It carries no prompt and no report. `lost` marks a subagent that has not reported within an hour
after its kind's time limit: the Session no longer counts it as running, so every view leaves it out of the
running work, and a late report still ends it. `AgentDescription.background_commands` lists background
commands the same way. A subagent's end also arrives as a `task` notification (`ROLE_NOTIFICATION`) whose
`task_status` is `COMPLETED`, `FAILED`, `STOPPED` or `TIMED_OUT`; a background command's is `EXITED` or
`LOST`. `SessionSnapshot.connector_contributions` lists the connectors the Session's develop subagents
contributed, each read from its `ConnectorContributionFlow` ([connectors](connectors.md#contributions)).

**Snapshot fields the UI reads** (names are in `api/superverse/v2/superverse.proto` and the
[agent protobuf contract](../../api/agent-protobuf-contract.md)):

| UI element | Source |
| --- | --- |
| Conversation and work log | Retained history (`AgentMessage` pages) and paged archive reads |
| Busy state, Stop button | Agent status, the current `Turn`, running tasks |
| Model and effort line, model selector | The segment's model and effort, the Session's selected model and its revision |
| Questions | Pending user input and its kind (ordinary question, or `REQUIREMENTS` for a card an old Session still waits at) |
| Requirements card and Process Brief | The current requirements (`confirmed_requirements`) with their version and design reference, and the designed Steps the agent wrote (`design_written_steps`) |
| The Brief, drafting | The newest `requirements_draft` event of a card call that is not over |
| Progress line | The todo list |
| Tasks panel | `AgentDescription.subagents` and `background_commands`: the Session's records (Tasks in the Snapshot, above) |
| Workspace status, "Keep running" | `SessionSnapshot.workspace` (status, `expires_at`, phase timings), joined from the `SessionWorkspaceFlow` |
| Preview panel | `SessionSnapshot.preview`, joined from the `PreviewFlow` |
| Connector contributions | `SessionSnapshot.connector_contributions` |
| Queued and steered messages | Pending message lists |

## Transport contract

`api/superverse/v2/superverse.proto` is the only business transport schema; generated Go
and TypeScript types describe snapshots, history, events and actions.

- **SessionService** reads a Session, watches it and pages its archive. **AgentService**
  carries the commands. Every command's response is empty: success confirms admission. A
  refused command carries `AGENT_COMMAND_NOT_ADMITTED`, which tells Studio nothing
  happened. A command to a stopped Session engine fails instead with FAILED_PRECONDITION and
  the reason `SESSION_EXECUTION_FAILED`, `SESSION_EXECUTION_CANCELED` or
  `SESSION_EXECUTION_TERMINATED`, which Studio presents as a FAILED Session.

`AgentPendingUserInput.kind` is `KIND_QUESTION`, or `KIND_REQUIREMENTS` for a card a Session
waited at before cards stopped waiting, which a `SendMessage` answers. The
persisted kind of a question batch is empty, so `KIND_QUESTION` is the one value that is not the
enum prefix plus the persisted value.

| Command | Request essentials |
| --- | --- |
| `SendMessage` | `message_id`, `text`; to an old waiting card it is the card's reply and is not queued |
| `SteerQueuedMessage`, `DeleteQueuedMessage` | `message_id` |
| `AnswerQuestions` | `call_id` and one answer per question of the pending batch |
| `ResolveToolApproval` | `call_id`, `is_approved` |
| `UpdateSessionModel` | model enum (`AGENT_MODEL_OPUS_5_5`, `AGENT_MODEL_SONNET_5_5`, `AGENT_MODEL_HAIKU_4_5`, `AGENT_MODEL_MOCK` where offered), `expected_revision` |
| `InterruptAgent` | `turn`, taken from the Snapshot; a stale or no-op stop is refused as not admitted |

- The conversion section of `internal/dexai/api/agent.go` maps domain values to typed protobuf;
  the nested Agent enums map through `boundary.Enum` by their prefixed names
  (`ROLE_ASSISTANT`, `KIND_TOOL_PROGRESS`). Studio's `agent-view` projection maps them
  back to the shared UI's lowercase literals. Pending-approval arguments remain opaque
  JSON.
- Studio owns the authenticated, same-origin BFF. It forwards typed calls through
  `/api/rpc/<Method>` to the merged server; Go owns project scope and validation. Authority
  and revision checks are the server's; browser roles grant none. See
  [the resource API contract](../../api/resource-grpc.md).
- **`WatchSession`** is a resource-level stream. Its progress frame is a `SessionProgress`
  whose `content` is an activity event, a reasoning summary, assistant text or a requirements
  draft (`AgentRequirementsDraft`), with `created_at` and `source` (the producing Step
  execution). Resume cursors are signed and scoped to the principal, project, session and
  source vector.
- **Sources.** Each snapshot read determines the Watch's sources: the Session's four
  Streams, plus the `AgentActivity` Stream of every RUNNING subagent listed in the
  Snapshot. No other subagent Stream is read, a STARTING or lost subagent's included. The
  first resolution of a connection reads the snapshot, so that its sources hold the running
  subagents' Streams too and a reconnect while one runs resumes the Session's own Streams from
  their cursors instead of realigning on fresh tails; later resolutions confirm the sources of
  the latest snapshot by authorizing the Session, and the five-second reread notices a changed
  set of running subagents. A frame from a subagent identifies that subagent by
  `SessionProgress.task_id` (such as `agent-k3n8q5r1`), which is empty for the Session's own
  Streams. A subagent Stream that cannot be read, such as one of an execution that already
  ended, stays silent instead of ending the Watch; the next snapshot that no longer lists the
  subagent as running drops it.
- Bounds (one outstanding frame, ten-second delivery deadline, five-second authoritative
  reads, thirty-second read bound) are those of the resource contract.

## Durable and live reconciliation

- Durable business state is owned by Attributes, AttributeMaps and Channels. Assistant
  text, reasoning summaries and activity are best effort and share per-type retention
  budgets. BlobCache, provider connections and browser state are disposable.
- Studio opens one Watch. Every connection, reconnects included, begins with the canonical
  joined Session snapshot. The server subscribes with bounded readers before it reads the
  snapshot and reconciles changed snapshots every five seconds. A source change cancels and
  joins the previous readers and sends a fresh snapshot. The Snapshot replaces history,
  description, queues and tasks; activity and text frames never establish a business
  result.
- The browser orders transient content with durable messages by stable identities: the
  message sequence, the call ID and the model call's `source`. `model_started` announces the
  sequence a call will commit. Streamed text for a call is shown only while that sequence is
  uncommitted and is dropped once a snapshot contains the committed message. Text that
  arrives before its model call is known waits in a bounded buffer.
- A failed provider call can write `model_started` in `CallModel` and `model_failed` in
  `RecordPlatformModelResult`, with different Stream sources but the same announced
  message sequence. Studio pairs them by sequence and uses the durable failure marker
  when the best-effort terminal event is lost. The snapshot's waiting status ends any
  remaining live clock. The work log shows the marker's explanation outside its fold,
  both on the live Watch and after a reload.
- After `agent_interrupted`, text and activity from calls that started before it are
  ignored. A cancelled call can keep streaming for a short time; Studio drops it by
  `source`, so the user never sees it.
- Studio keeps only the newest requirements draft and drops it once its call is over
  ([requirements drafts](#requirements-drafts)): when a snapshot carries the recorded version, the
  call's tool result or the end of its turn. A draft that arrives after its call is over is
  ignored, and one that arrives before the snapshot of its turn is kept until that snapshot.
- Pending questions and approvals remain explicit user actions; the requirements card waits for
  none and changes by message.
  Archive paging keeps the reader's position.
- The Watch owns cancellation, visibility recovery and signed reconnect cursors. A command
  keeps the Watch open; the next snapshot owns the visible result. An ambiguous ordinary
  command is reported as uncertain and is never replayed automatically. A resource epoch
  rejects stale requests from an earlier Session, and a newer Watch snapshot supersedes an
  older Get response.
- The shared UI stays transport-free, including chronology, keyboard behavior, pending
  actions and local submission rendering.

## UI/UX

The Studio UI of the harness. A Project page is the Builder frame
([the current contract](../studio-setup/studio.md)): the Project's one chat column is
mounted once in the frame and stays the same conversation on every surface (Flow, Runs, Inbox,
Interface, Tools), Process and Preview | Live version; it folds to a rail but never closes. The
sections below describe that chat column and the Preview it controls.

### Conversation

- User and assistant messages render as before. Assistant text streams live and is replaced
  by the committed message when the snapshot contains it.
- A **reasoning area** per model call fills live from `ReasoningSummary`. It can be
  collapsed. It is open while streaming and is not restored after a reload.
- **Markers** from the table above separate segments and show a stop.
- A **work log** per turn lists model calls and tool calls. The header shows the turn's
  model and tool call counts and wait, work and turn durations. Each tool row shows the
  tool name, its **target**, a status (running, complete, failed, unknown), the attempt count
  when it is more than one, and the duration. A failed call shows its code; a platform
  failure also says the maintainers have been notified. Rows show no outputs, no diffs and
  no arguments, so cards that rendered tool content are not part of the UI.
- Repeated calls of one tool group into one disclosure.
- A **metrics line** per model call shows the model, effort, input, cache read, cache
  write, output and thinking tokens, and which model completed the call when a fallback
  took over.

### Composer, Stop and model

- The send button always reads "Build". There is no Plan mode checkbox.
- **Stop** appears beside the composer while a turn or a task is running; Esc does the
  same. One press stops everything: the main agent's turn, every subagent and every
  background command. It sends `InterruptAgent` with the turn from the Snapshot, so a stale
  press cannot stop a later turn. Completed work stays and "continue" resumes it. There is
  no per-task stop button.
- A **model selector** sits under the Session button:

  | Choice | Description |
  | --- | --- |
  | Claude Opus 5.5 | Strongest. |
  | Claude Sonnet 5.5 | Faster and cheaper. |
  | Claude Haiku 4.5 | Cheapest, suited to testing. |
  | Mock | The [mock model](context-management.md#mock-model-for-tests), where `ListMockScenarios` says the caller may select it; the composer then offers the scenario's next input |

  After a change the selector says "Takes effect at the start of the next turn" until the
  Snapshot's model matches. Subagents follow the Session's model. The create-project page shows
  the same picker and composer for the first message ([session](session.md#uiux)).
- A **model line** shows the current model and effort, for example "Claude Opus 5.5,
  medium effort".

### Tasks panel

`AgentTasksPanel` is read-only. It lists subagents and background commands, each with its
description, status, a live one-line status and, for a finished subagent, the diffstat of
its changes.

- The running tasks come first, subagents before background commands; the finished ones are
  under a disclosure that counts them ("N finished"); a lost subagent is among the finished ones.
  A subagent row shows its description and kind. The status reads "Starting", "Running",
  "Finished", "Failed", "Stopped", "Stopped at its time limit", "Merged" or "Discarded", and
  "Lost, no report" for a lost subagent.
- A subagent's line is the condensed message of its latest activity event, from the activity
  Stream relayed by the Watch for RUNNING subagents only. A background command has no
  Stream; its line comes from the Snapshot record.
- After a reload or a Stream loss the line is blank until the next event; the status and
  counts come from the Snapshot. A missing live line is never an error.
- A subagent that saved changes shows "N files changed (+x -y)" once it ended. The panel has
  no stop, merge or discard button, and the Stop button counts a STARTING or RUNNING subagent
  as running work, a lost one excluded.

### Preview panel

The Preview panel shows the Preview's stable URL and status from the Session snapshot. The Preview, its
phases and its commands are in [sandbox](sandbox.md#preview); the switch for automatic deployment, its status
and the gap list are in [notifications](notifications-and-feedback.md#uiux). Two actions follow the state the
snapshot reports:

- **Start Preview** is offered while the Preview is `NOT_STARTED` or `STOPPED` and the prepared source and the
  configuration are ready. A deployment that ended before READY leaves the Preview `STOPPED` with its failure
  once its `release` ran ([sandbox](sandbox.md#preview-auto-deploy-on-previewflow)), so the same action starts it
  again.
- **Retry Preview cleanup** takes the place of Stop while the Preview stays `STOPPING` with a retained failure in
  the phase `stop-preview` or `release`: a stop, or the release after a deployment, whose cleanup failed. It
  sends the stop again through `StopPreview`. `is_recoverable` of the failure is advisory; when it is false,
  the displayed prerequisite must be addressed before the retry.

### Requirements card

The agent shows a requirements card and goes on building it; the Session records it as the
current version ([session](session.md#requirements-cards)). The card shows four parts: the
features (R1, R2, ...), the connections (existing and new, with their operations), what the
user must prepare, and how acceptance works. A card with a Flow design draws on the Flow tab,
with its **Process Brief** kept small while the design is built: the title and status on the
canvas's corner, the card's summary and one More details under it
([design before implementation](../design-first/studio.md#the-process-brief)).

- **While the agent writes the card**, the Flow tab shows its newest draft: "Drafting" on the
  corner over the title, the designed Steps' names per process as a row of dashed boxes, the
  summary line as it fills in and the features behind More details. No drawing is made of a
  draft. A Project created in this browser leaves its first conversation for the Flow tab at its
  first draft. A revision's draft takes the place of the current version's Brief until it is
  recorded.
- There is no Confirm, no Request changes and no Change something, and the composer stays
  enabled: a chat message changes the card, queued while a turn runs. The agent answers with a
  revised card, version N + 1.
- In the chat, the card is a work item, "Recorded what to build", and after that turn's work one
  line, "Recorded version N: <title>", which opens to the card's text; nothing stays pinned
  ([the card in the chat](../design-first/studio.md#the-card-in-the-chat)).
- A card a Session still waits at from before cards stopped waiting shows read only with
  "Reply in the chat to build or change it"; the reply is an ordinary chat message.
- From a card with a design until its build is complete, the Flow tab draws the designed Flows
  and shows them by itself once per card version
  ([the Flow tab](../design-first/studio.md#the-flow-tab)). While the agent builds it, a Step whose
  code is not written yet is drawn dashed and the corner's chip counts "N of M steps written"
  ([written Steps](../design-first/flow-design.md#written-steps)); once it is built the corner,
  the line and More details go.

### Progress, notices and markers

- A **progress line** shows the todo list as, for example, "Progress 3/7: generating the
  summary". It reads the Snapshot's todo list, so it is eventually consistent.
- A **notification notice** (`ROLE_NOTIFICATION`) is rendered as a notice card, never as a
  user bubble. Variants: a task result (subagent or background command), a delivery failure
  or notice (with the owning side and, for configuration gaps, an entry to configure
  Preview), and a connector update (a pull request that could not be opened, or a release
  candidate that was published or failed). The card shows
  the notification's summary; a subagent's full report is for the model.
- When automatic continuation has paused (after Stop or at its limit), one line says that a
  reply continues the work.

### Stopped and deleting Sessions

- A Project being deleted shows "This Project is being deleted" and disables the composer.
- A Session whose engine execution stopped shows "The agent stopped unexpectedly. An operator
  must recover it; your conversation and code are kept." and disables the composer
  ([recovery](session.md#stopped-engine-detection)).

### Cross-cutting

- Accessibility and keyboard behavior are kept: focusable controls, labelled regions,
  Enter to send and Shift+Enter for a new line, IME composition respected.
- Every request is cancellable and every response carries stale-response protection by
  resource epoch and revision.
- Studio mock development stays functional with explicit fixture behavior for the new
  events, markers, tasks and notices. It is never a production fallback and is not
  business E2E evidence.
- The UI imports no transport. Studio's `lib/session` owns the Watch, the reducer and the
  commands; `components/agent` renders their output.

## Flow contract: Streams

This topic adds no Flow, Attribute, AttributeMap, Channel, Step or RPC; those are owned by
[session](session.md) and [subagents](subagents.md). It owns the contract of their Streams.

| | SessionAgentFlow | SubagentFlow |
| --- | --- | --- |
| Streams | `AgentActivity` (`AgentEvent`), `AssistantText` (string), `ReasoningSummary` (string), `RequirementsDraft` (`agentloop.RequirementsDraft`) | The first three names; no `RequirementsDraft` |
| Producers | `CallModel` writes text and reasoning, the model and tool activity and the drafts of a requirements card; other Steps write lifecycle activity | The subagent's model and tool Steps |
| Consumers | The Session Watch, which Studio's BFF relays | The Session Watch reads only `AgentActivity` of RUNNING subagents |

- **Budget and scope.** Each Stream declares a 10 MiB buffer (`10<<20`). In the pinned Dex
  SDK (`sdk-go` v1.10.1, `dex/stream.go`), all Flow instances that share a Flow type and a
  Stream name share that budget, and it is approximate. It is not a per-Flow budget:
  every Session's events compete for the same space, and SubagentFlow has its own budget
  separate from SessionAgentFlow.
- **Visibility.** A write is visible immediately and is not rolled back when its Step fails
  or retries, so events can precede a commit and can repeat across attempts.
- **Resume.** A source is identified as `<FlowType>/<FlowID>/<Stream name>`. Resume tokens
  are opaque and carry a message ID, never a Run ID. Cursors are signed and scoped as
  described under [Transport](#transport-contract).
- **Truncation and loss.** When the shared budget is exceeded the oldest messages are
  trimmed, and a reader that resumes behind the trim loses events silently. Delivery is
  best effort and never exactly-once.
- **Recovery.** The canonical state a consumer reads to recover is the Snapshot (history,
  pending inputs, todo list, tasks), and for a subagent the Session's record. A Stream is
  never the authoritative business record. Reconnection starts with a fresh snapshot and
  reasoning summaries are not recovered.
- **`RequirementsDraft`.** Its budget is its own 10 MiB, shared by every SessionAgentFlow, like the
  others. A card writes at most one draft a second while it streams, each at most about 90 KB and
  typically a few KiB, plus its complete one: a card of a minute's writing retains under a
  megabyte at worst. Delivery is best effort: a reader that misses events, a trimmed Stream or a
  reconnect (which starts on the fresh tail) shows the next draft, or none once the card is recorded.
  The recorded version in the Snapshot is canonical and a draft is never recovered: after the card
  is recorded, Studio shows the card. A Stop or a failed model call leaves a draft without its card;
  the end of its turn in the Snapshot drops it.
- **Compatibility.** Stream names and payload shapes of open executions are preserved; the
  changed `AgentEvent` fields are replaced before launch without a compatibility layer.
  `RequirementsDraft` is new: an open Session writes it from its next model call, and a Watch
  resume cursor from before it has no position for it, so the Watch starts it at its tail.

## Database Schema Changes

None. No database, projection or mirror is added; the Snapshot and the Streams read
Flow-owned state.

## Tests

- **Verification boundary.** `make check` builds, vets and compiles the retained test
  binaries and runs the static checks; it is not integration evidence. Retained tests call
  real dependencies. The one allowed substitute is the
  [mock model](context-management.md#mock-model-for-tests).
- **Dex integration tests with the mock model.** Stream content for the turn of a mock scenario:
  text, reasoning and tool activity with `target`; a reader that reconnects after loss
  reaches the same state from the Snapshot; the projection never contains thinking, tool
  arguments or results; stop writes `agent_interrupted` and the cancelled call's text is
  not shown; subagent activity is relayed for RUNNING subagents only.
  `TestSessionMockSummaryApprovalDesignBuiltDexIntegration` reads the `RequirementsDraft`
  Stream of `summary-approval`, whose card streams at 100 ms per delta: several drafts, all
  received before the card was recorded, growing, the last one complete with the recorded
  card's call, title and designed Steps.
- **Studio source checks.** `make check-web` runs the Studio rules of
  [the current contract](../../../web/studio/README.md) and the typecheck, which
  covers the generated Agent types; it is a static check, not integration evidence.
- **E2E.** The three cases (`registration.spec.ts`, `create-project.spec.ts` and `template-approval.spec.ts`),
  run on the lanes `make required-lanes` names, exercise the work log, the requirements card and the
  conversation against the real BFF and gRPC path. `template-approval.spec.ts` on the mock model sees
  "Drafting" with the designed Steps' names while the card streams, the overview while the design is
  built, the chat's "Recorded version 1" line, the design's "6 of 6 steps written" with the approval
  Step solid after the scripted edit, and the overview gone once the push built the design. No new E2E is added for edge cases; Studio-only
  behaviors (model selector rendering, Stop, tasks panel) are covered by the static checks and observed
  during those cases.
- **Not verified.** Stream trimming under the real 10 MiB shared budget at scale, and the
  exact stop latency (up to about 48 seconds of cancelled generation).

## Documentation

This document owns the Stream, projection, transport and Studio presentation contracts.
Related: [the resource API contract](../../api/resource-grpc.md), the
[typed Agent wire contract](../../api/agent-protobuf-contract.md), the
[transport audit](../../api/agent-transport-parity.md), the Studio component READMEs and
the E2E README that lists the journeys.

