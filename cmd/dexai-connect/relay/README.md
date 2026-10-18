# Bridge relay

Purpose: Runs provider-neutral delivery, tool and event loops for Orca and direct local-agent bindings.

Callers: `cmd/dexai-connect`, `cmd/dexai-connect/orca` and the direct `cmd/dexai-cli/codexdaemon` provider.

Dependencies: `cmd/dexai-connect/mcpserver`, `cmd/dexai-connect/platform`, `cmd/dexai-connect/state`, `internal/dexai/agents/remoteagent/bridgeapi` and `internal/comm`; it calls the remote agent endpoint through `platform` and the local provider through `Provider`.

The [remote agent protocol](../../../docs/api/remote-agent.md) defines the platform boundary; this README describes the relay.

The relay stamps a call just before its first platform `report`, after the report answer, and when
the matching result first appears in `sync`. These times and the remote call ID travel on the
private local socket to `mcp`, which writes the bounded [call trace](../README.md) after its response
write. Retried reports keep the first send time and latest report answer time. Trace failures do not
delay or fail a tool call.

## Provider boundary

`Provider` owns session identity, version and process identity, turn cancellation, public history,
events, positions and a declared submission mode. `Supervisor` and the delivery/event loops depend
only on it. `Orca` extends it with the tab/worktree inventory and `Reload` used by the current
`dexai-connect connect` flow. `cmd/dexai-cli` imports these packages and selects
`codexdaemon`, which implements `OneWriteProvider`; the Orca command still selects Orca.
`VersionPolicy` declares a minimum and newest verified version for each provider.
Versions below the minimum fail; newer versions return one warning and may connect.
An unsupported protocol answer holds that binding with a clear status problem
instead of retrying it.

`SubmissionJournal` resends the same operation ID to Orca's idempotent journal, as before.
`SubmissionOneWrite` requires `OneWriteProvider`: the relay persists a random marker and the
platform key, generation, operation ID and fingerprint before the first write. It persists
`Attempted` before calling `Send`. A restarted relay never calls `Send` for that record again.
`Send` may report accepted only with a positive canonical user item ID. It reports a permanent
rejection only for a provably invalid-content refusal; until Codex exposes a structured such
classification, every definite app-server refusal is `retry`, as is a failure before admission.
An ambiguous response, lost process after a turn request, or pending result gets one
read-only `FindSubmittedMessage` check, bounded to ten seconds, for the exact marker in canonical
public user history. A match accepts; absence or failure reports `delivery_uncertain` and advances
without another write. A direct adapter must put the marker in its user content and make no
internal send retry. Its event IDs must match its deterministic `SubmittedMessageID` for echo
suppression. Provider history and event cursors exclude hidden reasoning.
`Progress.BriefSent` records that the first-brief attempt settled; `Progress.BriefAccepted`
records positive admission. The direct command opens the TUI only for an accepted brief.

`state.EventPosition` keeps Orca's existing `epoch` and `sequence` JSON while a direct provider
can store an opaque `cursor`. The provider compares its own event positions at generation
boundaries. Existing Orca binding JSON keys and the bridgeapi fields remain unchanged. An empty
local `providerId` identifies a pre-existing Orca binding; direct bindings use their provider ID
and address the MCP socket by the connection ID. Orca MCP processes keep using the Orca session ID.

## Orca

`Orca` is the narrow interface the relay needs of Orca's structured session protocol
used by the Orca Bridge:
`Runtime` (versions and the runtime ID, which changes when Orca restarts), `OpenConversations`
(the Codex conversations open in Orca, structured or in a terminal), `ResolveWorktree` (a new
conversation's worktree selector), `CreateSession` (in a resolved worktree), `ResumeSession`, `Reload` (stop Codex so the next send
starts it with the current MCP servers on the same thread, and show the chat), `Send` (with the client operation ID, answering `SubmissionState`
accepted, pending, retry, rejected or unknown), `Cancel` (returns once no turn runs, with the event
position: a generation's boundary), `History` and `Subscribe` (each event once, in batches with
the position to resume from; a turn's assistant messages, each with its final text, come in the
batch of its completion, just before it). An Orca `state.EventPosition` is the journal's `{epoch, sequence}`; an event's
position is where its item was first written, so a turn's position is its start.
`cmd/dexai-connect/orca` implements the interface with its method allow-list
and the shared version policy. `VersionPolicy` declares a minimum and newest
verified version for each provider. Versions below the minimum fail; newer
versions return one warning and may connect. An unsupported protocol answer
stops the affected binding.

## Supervisor

`Supervisor` runs one relay per binding in the state directory and implements
`mcpserver.Backend`: an MCP session gets the tools of the binding whose Orca session it is, and
none otherwise. It refuses to start a second binding of an Orca session, so that binding is unique. A relay that fails restarts after 30 s; one that is detached is removed.

## One binding

- **Start.** `Orca.Conversation` reads the session's provider thread and latest turn first. A
  provider thread other than the bound one (the conversation was cleared or rebound) stops the
  relay before anything changes (no provider thread is not a change: the start is retried): it serves no MCP call and delivers nothing, and `status` shows "the
  Orca conversation changed; reconnect from Studio" until the Builder reconnects or disconnects
  (which removes the binding without touching the Orca session). The same check guards every
  later mutation (Known gaps). Then `activate` (a repeat is
  idempotent) gives the epoch, generation, conversation end, `reported` and the tool set. The first
  activation sets the delivery cursor and immutable brief cap to the conversation's end; every activation sets the
  acknowledged report sequence to `reported`. A new generation is adopted (below). Work the Bridge
  that ran before left is drained (below). Then, once per binding, the history import (the newest public
  messages within 200 messages and 512 KiB, after the last imported one, as `imported_message`
  events), and once the relay is ready, the brief (the platform's `agentContext`, then the Project's
  name, the current requirements under "Current requirements", HEAD, todo list and recent public messages no later than the activation cap). Later messages remain with the delivery loop even while the brief waits. The relay's first
  `sync` after it starts, and the brief's, ask for `agentContext` (`context: true`). The brief
  waits up to three minutes while `agentContextState` is `pending`. After a timeout, a later
  `ready` answer delivers the platform's `<repository_context>` section once. Follow-up checks
  are at least 15 seconds apart and stop after 30 minutes or on `unavailable`; an idle delivery
  loop has no timer after the follow-up settles. Progress records the wait, check timestamps and
  settlement across restarts. MCP `instructions` are read once when Codex starts;
  they are not refreshed, even when the later section is delivered as a message.
- **Restart** (`drain.go`). Only a latest turn that `completed` or was `interrupted` (or none) is
  positively over; `running`, `unverifiable` and any other state may still run. Reports the Bridge
  that ran before sent may still be executing on the Session, whatever turn they belong to and
  whether or not Orca's latest turn was read, so the relay fails closed: only a latest turn this
  relay tracks (it keeps running across a restart of its loops), or a latest turn never read (or
  none) with nothing acknowledged in the epoch and nothing stored, starts at once. Otherwise it
  records a draining record: `admitted`, the acknowledged sequence then (activation's `reported`; a
  report acknowledged before the record is already gone from the store), `slot`, the last stored
  report slot then (every report stored before the restart), and, when the latest turn's events
  were already read (it is in the remembered turns) but this relay does not track it, its turn ID
  and `completionStored` (true at once when the event loop already stored that turn's completion:
  each remembered turn records `completed` in the write that moves the event cursor past its
  completion, so a completion checkpointed before the record is never awaited again). A named turn
  is cancelled (`Orca.Cancel`, also an unverifiable one) unless Orca shows it positively over; it is
  cancelled and awaited only while it is still the latest turn, since a later turn proves it over and
  is never stopped, also when the drain resumes after another restart. A latest turn never read is
  new (the owner started it in Orca, or the Bridge's own delivery opened it just before the crash,
  and every older turn is over): the
  record names no turn, stops nothing and awaits no completion, but that turn's MCP calls are
  refused like any new work until the drain ends. The relay is not ready, so it admits no new MCP
  call (each is answered with an error) and delivers nothing, until the old work settled:
  1. Drained tool calls (stored through `slot`, or of the named turn) that were never sent (no
     sequence) are removed and their MCP calls answered interrupted, so none is sent after the
     restart. Reports with a sequence may have been admitted and are sent as usual; each
     acknowledgement of a drained report (an admission or a `duplicate`) raises `admitted` to its
     sequence in the write that records it, also when the acknowledgement was saved before a
     restart and the report is only removed after it. Only a refusal that proves non-admission
     (Reports, below) releases a sequence, so a report admitted before a Stop stays in `admitted`.
  2. A named turn's completion report comes from Orca's event once the turn is positively over,
     after the turn's public text, and is stored once: the report and the draining record's
     `completionStored` commit in one write, so a restart during the drain resumes it and never
     stores a second completion. An unverifiable turn never yields a completion.
  3. When a named turn's completion is stored and no drained report remains in the store (every one
     acknowledged or refused), the answer to the next sync that starts after that must show
     `settledThrough` at least `admitted` with every entry read (`examinedThrough` =
     `lastSequence`, so every result of the turn was read: it reached its MCP call, is queued as a
     late-result message, or, read only by a full-queue scan, still lies after the durable cursor,
     which the rewind after a settled delivery reads again; the drain does not wait for the queue,
     which settles nothing before the relay is ready), and Orca must show a named turn positively
     over (a drain that names no turn does not wait for the new turn, but still requires the bound
     provider thread; a page that names no thread leaves the drain open until a later sync, and only
     another named thread stops the relay). Retention can drop such a scanned entry before the rewind, and Codex then gets the
     dropped-range notice instead of that result. An
     acknowledgement proves only admission; `settledThrough` proves the Session recorded each
     outcome. An answer without `settledThrough` (an older server) reads 0 and never ends a drain;
     Orca idle alone never ends one.
  Then the record is cleared and the relay is ready. A record without `slot` (written before it
  existed) reads 0 and drains only its turn. No generation changes: the completion follows
  the turn's reports in the one report stream, which the Session consumes in order, so the old
  turn ends before any report of a new one; Stop and `SetExecutor` stay the only generation
  changes. The Builder sees: a running Codex turn is stopped; tool calls it had not yet sent are
  reported as interrupted; calls already sent (or possibly sent) may still run and finish, their
  results arrive as messages, and the Bridge accepts new work only after they settle. Send a new
  message to continue. Remembered turns stored before `completed` existed read as not completed, so
  after upgrading, the first restart of every existing binding whose latest turn was read and is
  over waits for a completion that never comes again: reconnect existing bindings from Studio after
  upgrading.
- **Generations.** Adopting a generation cancels the Codex turn (`Orca.Cancel`) and records the
  event position as the boundary. A turn is bound to a generation when it is first seen: a turn
  whose opening message the Bridge delivered gets that delivery's generation, recorded with the
  message ID `orca:<operation ID>` before the first submission; any other gets the current
  generation when it started after the boundary, else 0. Reports of a generation that is not current are not sent (a call is answered
  interrupted). Stop and takeover both increase the generation, so a new generation in `sync` is how
  the Bridge learns of a Stop.
- **Reading** (`sync.go`). `sync` long-polls after an in-memory read cursor, with the last state
  digest (the pending question, requirements and todo list are replaced only when the digest
  changed; the brief asks with an empty digest), and moves to each answer's `examinedThrough`.
  Every tool result, and the answer to a waiting question, reaches its MCP call while the call's
  Codex turn is open, with a delivery obligation: the entry is queued as a delivery that settles only once `mcp`
  acknowledged writing that MCP response to Codex's standard input (mcpserver README), so the
  durable cursor never passes a result whose response is unwritten. A response that was not
  written (the MCP call or its connection ended, the write failed, no acknowledgement within 30 s,
  or the wait ended as the result arrived and the call was abandoned) settles by sending the result
  as a late-result message (an answer: the answer message itself), with the same key; a crash
  before that leaves the cursor before the entry, so after the restart no call waits and the
  result is delivered as a late-result message. A notice may repeat when the acknowledgement was
  lost after the response was written; it is never lost, and the tool never runs again. A result of one of this connection's calls (`remote-…`) that no MCP call waits for any more
  (the wait expired, the MCP call ended, its Codex turn completed, or the Bridge restarted) is queued in order as an ordinary
  delivery, key `<connectionId>/<entry sequence>`, with the text "Result of Superverse call
  `<call ID>` (it finished after the call was answered): …" (content truncated to 64 KiB), so Codex
  learns the outcome; it settles like any delivery and the durable cursor passes it only then. Input for Codex (messages, notices, notifications not reported by this connection, and a
  dropped range told once) is queued in order, at most 64 deliveries or 8 MiB, and `queuedThrough`
  marks how far every input is queued.
  - When the queue cannot take the next input (64 deliveries, or the 8 MiB byte bound), reading
    switches to a scan: the read cursor keeps moving to `examinedThrough` and tool results and
    answers still reach their calls, but nothing is queued beyond `queuedThrough`. An answer
    or tool result that reached its call is marked on its entry with its delivery obligation,
    which the entry's delivery takes over once it is queued, and the mark lasts through every
    rewind until `queuedThrough` passes the entry or retention drops it (at most 1,024 marked
    entries; past that the oldest goes, so a repeat as a message is possible, a loss is not). Marks
    live in memory, so after a restart of the binding's loops or of the Bridge an answer or result
    its live call already received may arrive once more as a message. Only a settled delivery ends a scan: it
    interrupts the scan's long poll and reading rewinds to `queuedThrough`, queues the inputs again
    in order, skipping the marked answers and results. An input refused only by
    the byte bound therefore waits for a delivery to settle instead of being re-read in a loop.
  - The durable delivery cursor (`Progress.Cursor`) moves only past settled deliveries, in order,
    and joins `queuedThrough` whenever the queue is empty, so it never passes an unsettled or
    unqueued input; a restart reads again from it.
  - So a delivery that Orca holds `pending` while Codex waits on a Superverse tool never blocks that
    tool's result, even with a full queue.
- **Delivery** (`delivery.go`). One delivery at a time, with the sending record written before
  submission. A journal provider is asked again with the same operation ID while `pending`; a
  one-write provider never repeats an ambiguous attempted write and only an exact marker in
  canonical history can settle it. A permanent `rejected` reports `delivery_failed`; `unknown`
  reports `delivery_uncertain`. A retryable rejection never advances the cursor: the sending
  record durably receives a replacement operation ID (and a new marker for a one-write provider)
  plus any queued-copy withdrawal before retrying, reports `delivery_delayed` once after two
  minutes, and finally reports `delivery_failed` after ten minutes. The final report is stored in
  the write that advances the cursor. A new tool set digest refetches the tools by repeating
  `activate`.
- **Tool calls** (`reports.go`). An MCP call becomes a report of the running turn (it waits up to
  5 s for the turn's start event) and waits for its `tool_result` entry, call ID
  `remote-<epoch>-<sequence>`, for at most `toolWaitBudget` (1,770 s), so the Bridge answers
  before Codex's MCP timeout of 1,800 s. `submit_connector_contribution`, whose Sandbox gate may run
  for 70 minutes, answers as soon as its report is durably accepted that the checks started; its
  result follows through the same late-result message path. For every other tool, expiry leaves the
  outcome pending, never failed: the
  answer is a non-error result telling Codex that the result will arrive as a message naming the
  call ID (once the call was sent), and not to
  run the call again; a question still open returns "the question is still open; the answers will
  arrive as a message". The Bridge checks that the originating turn still runs before admitting
  a call. A turn completion retires its unresolved MCP calls, even when their transport stays
  open, so later results take the ordinary delivery path instead of completing an orphaned call.
- **Questions.** `request_user_input`'s `tool_result` is a placeholder with `status`
  `waiting_for_user`: the MCP call stays open through it and is resolved by the entry whose
  `answeredInputCallId` is the call (the answers), whether both arrive in one sync page or in
  separate ones; it is never resolved by the placeholder. With no live call (the wait expired, its
  Codex turn completed, or the Bridge restarted) the placeholder is a late-result message and the answer an ordinary message.
  The wait's end keeps its answer ("the question is still open"). A question a Stop or takeover
  removes never gets an answer entry, so its call waits until Codex cancels it or the wait ends.
  `confirm_requirements` resolves at once with its ordinary result (no status): the Session records
  the card as the current version and the turn goes on; a change to it arrives as a new message.
- **Reports.** One sender sends the stored reports in order, one at a time. The head gets its
  sequence (acknowledged + 1) when first sent and keeps it across resends and restarts; a sent
  report is always resent, never dropped, because the Session may have admitted it. `duplicate` is
  an acknowledgement; a 429 (`reports_pending`, the Session's inbox of 8 reports or 2 MiB is full)
  is retried by `platform` with the same report and sequence after a backoff, never renumbered;
  `stale_generation` drops the report and frees its sequence: the server answers a sequence it
  admitted in this epoch `duplicate` whatever its generation, so this refusal proves the report was
  never admitted (a retry of a report admitted before a Stop is a `duplicate`, an acknowledgement
  whose call waits for its result: the real `tool_result` if the call ran before the Stop, else the
  Session's interrupted result). A sent sequence is released only by an
  answer that proves non-admission: `stale_generation`, `sequence_gap` (above `reported` + 1), or
  `malformed` and `too_large` (refused before admission on every attempt, which assumes validation
  does not tighten between an admission whose answer was lost and its resend: see Known gaps);
  `sequence_gap` adopts the expected sequence; `malformed` and `too_large` drop it with an error to
  Codex. A report that fails its local validation or size limit (`platform.ErrInvalidRequest`) was
  never sent and would fail again: it is dropped with an error to Codex and a log line without its
  content, and its sequence is free for the next report, so it never blocks the reports behind it.
- **Events** (`events.go`). Orca events after the saved cursor become events of the turn's
  generation: assistant messages (final, in the page of their turn's completion and before it),
  user messages typed in Orca (not echoes of delivered messages, recognized by `orca:<operation ID>`
  and the accepted submission's provider item), activity. Each page of events becomes reports in
  its order (at most 32 events and about 496 KiB of encoded, JSON-escaped, events per report; a
  turn completion is its own report, after its turn's events, so a turn's public text is always
  reported before its completion), and those reports and the cursor after the page commit in one atomic write (`StoreReports`), so a
  crash loses no event and never reports one twice. A failure, such as a full store, keeps the saved
  cursor: the subscription ends and resumes from it after 5 s. The history import commits its
  events with `HistoryImported` the same way, and a delivery's failure report with the cursor that
  passes it.
- **Detach.** `executor_changed` (409), `unauthorized` (401) and `forbidden` (403) end the binding:
  the Codex turn is cancelled, a replaced connection revokes its own row, open calls never sent are
  answered interrupted, sent ones (which may have run) with an unknown outcome, and the binding is
  removed. `disconnect` does the same after revoking the row.

## Binding

`connect` chooses the conversation first: `--session` names it, `--new --worktree` asks for a new
one, and otherwise the Builder picks on the terminal from `OpenConversations`, where only a
structured conversation that no binding holds can be picked and a new conversation is offered only
with `--worktree`. `Bind` checks Orca and the binding limit, then, before it asks for the pairing
code, the choice (an existing session must be an open structured Codex conversation no binding
holds; a new conversation's worktree must resolve) and the Codex configuration (the entry below is
computed against the current file without writing it), so a choice or configuration error never
spends the one-time code. It then pairs, writes the Codex MCP server entry
(`[mcp_servers.superverse]` with `command`, `args = ["mcp"]` (plus `--state-dir` when it is not the
default), `env_vars = ["ORCA_AGENT_SESSION_ID"]` and `tool_timeout_sec = 1800`), resumes the chosen
session or creates a new one, reloads it (`Orca.Reload`: Codex stops, the chat is shown in Orca, and
the brief starts Codex again on the same thread, now with the superverse tools) and stores the
binding. The configuration is parsed as TOML; the
earlier `superverse` table and its sub-tables, however their headers are spelled, are replaced, and
the result is parsed again and must equal the old file without that server plus the new entry. A
file that is not TOML, or one the Bridge cannot edit exactly (the server defined with dotted keys or
inline, a multiline string holding a table header), is left unchanged with an error. A timestamped
backup is written just before the file is replaced, and a lock file next to it
(`.config.toml.superverse-lock`) serializes concurrent connects. A failure after pairing revokes the unused
connection.

## Known gaps

- **Tools and MCP instructions fixed when Codex starts.** Codex 0.162.1 reads the MCP tool list and
  `instructions` once, when it starts the superverse server. Connect restarts Codex after binding
  (`Orca.Reload`), but Codex started by an Orca restart or an idle respawn while serve is down or the
  relay is not yet activated keeps zero tools and no instructions until it restarts again.

- **Validation that tightens across a lost answer.** The server validates a report before it checks
  for a duplicate. If report N was admitted, its answer was lost, and an upgrade between then and the
  resend makes validation stricter, the resend is refused `malformed` or `too_large`, the Bridge frees
  N, and the next report reuses N and is answered `duplicate`, so it is dropped. Closing this needs the
  server to answer a same-connection, same-epoch `Sequence <= Reported` as `duplicate` before body
  validation. The window needs a deploy inside a lost-answer retry, so it is accepted before launch.
- **Conversation cleared or rebound in Orca (incarnation fence).** The relay compares the session's
  provider thread with the bound one at start, on every event page, just before it stores each MCP
  call's report, just before and after every send write, and before and while it cancels; a
  different thread stops the relay as at start (it serves and changes nothing until the Builder
  reconnects or disconnects), and an unconfirmed thread refuses the mutation and is retried; while
  a cancel waits for the turn to stop, a page that names no thread does not prove it stopped. These
  reads detect a change, they do not prevent one: the gap below remains unmet. A
  cancel names the latest turn's `turnId`, which Orca 1.4.224 and 1.4.225 accept and enforce themselves (Codex
  `turn/interrupt` of that turn on the session's current thread; a turn that does not run is
  refused with `cancelled: false`), so a cancel never stops another turn, a replacement
  conversation's included. A send has no such precondition. The installed host's strict request
  schemas are exactly: `agentSession.send` `{envelope: {sessionId, clientOperationId,
  expectedRuntimeFence, payloadFingerprint}, body: {kind: "message", role: "user", blocks},
  delivery?: "queue-if-active", retryUnknown?: true}`; `agentSession.cancel` `{envelope, turnId?,
  scope?: "background-tasks", taskId?, prompt?: {itemId, expectedRevision}}`. The envelope's
  `expectedRuntimeFence` fences the runtime, not the provider thread. So a clear or rebind between
  the read and the write can still deliver a message to, or admit a tool call (whose effects run
  against the bound Project) for, the replacement conversation: that change-after-read race remains
  open. The minimum fix is a host precondition, `expectedProviderThreadId` and `expectedTurnId` on
  `send` and `cancel`, which the host refuses on mismatch; Bridge acceptance requires it, from
  upstream Orca or an Orca plugin, as the owner decides. Whether a cleared conversation should
  detach the binding or adopt the next provider thread is also open; the relay only stops.
- **Unverifiable turn state.** An unverifiable turn never yields a completion and is drained like a
  running one, so a turn that stays unverifiable keeps the platform turn open (until a Stop) and a
  drain unfinished; real-Orca evidence is pending, as it is for the whole drain (that a cancelled
  turn's revision reaches the event cursor as a completed or interrupted turn).

## Scenarios to run against real Orca

None of these runs in CI; each needs Orca 1.4.x, Codex 0.162.x and the Native lane's endpoint.

1. **Full queue, pending send, later tool result.** Codex waits on a slow Superverse tool (a long
   `bash`); Builders send more than 64 Studio messages; Orca holds the first steer `pending`. The
   tool's result, appended after the queue filled, still returns to Codex before its MCP timeout;
   after the turn, the messages arrive in order and none twice.
2. **Pending steer, room in the queue.** One Studio message during a running tool call: the result
   returns at once and the steer is accepted afterwards.
3. **Restart with a sending record.** Kill `serve` while a delivery is `pending`; the restart asks
   with the same operation ID, and Orca's journal holds the message once.
4. **Orca restart during a delivery.** The runtime ID changes; a delivery that does not settle as
   accepted or rejected is reported `delivery_uncertain`.
5. **Rejected with a queued copy.** A rejection with `keptAsQueuedMessageId` withdraws the copy, so
   resuming Orca's queue does not deliver it later.
6. **Stop and takeover.** Stop in Studio during a Codex turn cancels it (`latestTurn` running) and
   interrupts the old turn's calls; a second Bridge activating removes the first binding with 409.
7. **The owner types in Orca.** The message is reported as `user_message` and its turn belongs to
   the current generation; echoes of delivered messages are not reported.
8. **Questions.** The `waiting_for_user` placeholder never resolves the call, with the answer in
   the same sync page or a later one; `confirm_requirements` resolves at once with its recorded result.
   An answer within 30 minutes is the MCP result; an unanswered question returns
   "still open" and its answer arrives later as a message; an answer read during a full-queue scan
   is the result and is not delivered again after one rewind or several.
9. **Dropped range.** After a disconnect longer than retention, Codex is told once.
10. **Binding.** `env_vars` passes `ORCA_AGENT_SESSION_ID` to `mcp`; an unbound session lists zero
    tools; the reload after binding makes Codex load the `superverse` server.
11. **Byte bound and partial drains.** With the queue near 8 MiB and a next input that does not
    fit, later tool results still reach their calls and the refused input is queued after the next
    settled delivery; repeated partial drains deliver every input once and in order.
12. **Restart during a Codex turn.** Also with the turn already over in Orca and its completion
    already stored before the restart. Kill `serve` while Codex runs a slow Superverse tool and has an
    unsent call queued; the restart cancels the turn, answers the unsent call interrupted, sends the
    turn's completion once (also after a second restart during the drain), refuses new MCP calls
    and delivers nothing until `settledThrough` covers the sent reports, and the slow tool's result arrives as a message.
    Again with the owner starting a new turn in Orca after the slow tool's turn ended and `serve`
    killed before reading it: the new turn is not stopped, but its MCP calls are refused until the
    slow tool settles; and with a report admitted, its answer lost and a Stop before the resend,
    which is answered `duplicate` and stays in the drain.
13. **Conversation cleared.** Clear the bound conversation in Orca while `serve` runs, and again
    before a restart; the relay stops at the next event page, MCP call, send or cancel, a cancel
    never stops the replacement conversation's turn, the binding serves nothing and `status` asks
    to reconnect from Studio.
