# Orca client

Purpose: Implements the relay's Orca interface over the Orca runtime's local socket with the owner token and shared minimum-version policy.

Callers: `cmd/dexai-connect`.

Dependencies: `cmd/dexai-connect/relay`, `cmd/dexai-connect/state` and `internal/comm`; it calls the Orca runtime's local socket and runs `codex --version`.

The Orca runtime protocol is confined by this adapter. Orca 1.4.0 and Codex
0.162.0 are minimum versions; 1.4.225 and 0.163.0 are newest verified versions.
Newer versions warn and may connect.

- **Transport** (`transport.go`). `orca-runtime.json` in `$ORCA_USER_DATA_PATH` or Orca's default
  directory gives the runtime ID, the unix socket and the owner token; all three change on every
  Orca restart, so the file is re-read after a failed dial, an `unauthorized` answer or a changed
  `_meta.runtimeId`. Each call is one NDJSON request on its own connection, bounded by 90 s, with
  keepalive frames skipped and answers capped at 4 MiB, Orca 1.4.224 and 1.4.225's response frame bound. A
  connection that closes without an answer, or an answer from another runtime, is uncertain
  (`errNoAnswer`); a longer answer is `errAnswerTooLarge`, returned as is and never resent as
  uncertain. The token is only in the request
  line, which never reaches an error or a log.
- **Method allow-list.** `status.get`, `session.tabs.listAll`, `agentSession.create`, `send`, `cancel`,
  `history`, `queuedMessageDelete`, `close` and `reveal`; any other method panics. Version checks use
  `status.get` `appVersion` and `codex --version` through the login shell, as Orca
  resolves it, cached for 5 minutes.
- **Fingerprint** (`canonical.go`). Every mutation carries `payloadFingerprint`, the SHA-256 of
  Orca's canonical JSON of `{method, sessionId, fields}` (UTF-16 key order, `JSON.stringify`
  escaping). The body is built once and hashed as sent.
- **Send** (`send.go`). One text block; `delivery` and `retryUnknown` are never sent. Orca's send
  has no provider-thread precondition, so each write is preceded and followed by a history read:
  another thread fails it with `relay.ErrConversationChanged`, none refuses it before the write
  (the change between read and write is the relay's known gap). A send is
  fenced to the runtime ID of the delivery's first submission: the transport writes it only to that
  runtime, and when the metadata it reads or re-reads on a retry names another runtime it refuses
  before writing and the send is `unknown`. A repeat with
  the same operation ID is answered from Orca's durable ledger. Outcomes follow the spec's mapping:
  `accepted` (with the provider item for echo suppression), `pending`, `rejected` (a rejection that
  kept a paused queued copy withdraws it with `queuedMessageDelete`; a failed withdrawal is
  `unknown`, a copy already dispatched is `unknown`, since it was sent under its own message ID and asking
  with the original operation only repeats the rejection; it is never resent under a new identity), `unknown`; `operation_expired` and
  `operation_unknown` are unknown; a refused admission is rejected, after up to 2 minutes of
  resends while Orca's Codex owner recovers; any other failure is returned for the relay to ask
  again.
- **Conversations.** `OpenConversations` reads `session.tabs.listAll`: an `agent-session` tab of
  agent `codex` is a structured conversation (its `sessionId`, `title`, the snapshot's `worktree`
  ID `<repoId>::<path>`, and its newest journal row's `observedAt` as the last activity, one `tail`
  read each). A fresh desktop Codex chat on Orca 1.4.225 had a session ID but no `providerSession.id`
  in its tail before its first message; the picker shows it unavailable and `connect --session`
  rejects it before pairing. A Bridge-created chat had a provider thread before its first message.
  A session whose journal cannot be read is also unavailable while other sessions stay available.
  A `terminal` tab whose `launchAgent` or `agentStatus.agentType` is `codex` is a Codex TUI, listed as
  unavailable (its `agentStatus.updatedAt` as the last activity). `OpenWorktrees` lists every distinct
  worktree from the tabs snapshot. `ResolveWorktree` accepts `id:<worktreeId>` or `path:<dir>` only
  when it names one of those worktrees, and returns it as `id:<worktreeId>`.
- **Create and cancel** resend identical params when unanswered (Orca's ledger replays them).
  `CreateSession` mints the session ID (`sv-` and 24 hex digits) in the worktree `ResolveWorktree`
  returned. The Bridge sends `{envelope, worktree, agent}` to Orca 1.4.x.
  The inspected 1.4.224 and 1.4.225 releases also accept optional `resumeFrom` and `tabId` (`tabId` is the desktop window's own
  provisional chat tab); 1.4.225 additionally accepts `firstMessage` and `options`, which the
  Bridge does not send. Orca publishes the chat's tab to the
  runtime's session-tab inventory (`session.tabs.listAll`, paired clients) with `activate: true`,
  but sends the desktop window no tab: the window shows the chat only once it reloads that
  inventory, as opening the chat from Agent Session History in the right sidebar does (a history
  entry whose Codex thread a structured chat holds carries that chat's `structuredSession`, so the
  same Orca session opens), so `connect` says so after creating one. New Codex agents open as
  structured chats instead of terminal sessions when Settings, Experimental, Chat UI is on
  (`experimentalNativeChat`) or the app already holds a structured chat
  (`app.holdsStructuredAgentSessions`). These Orca behaviours were read from the Orca 1.4.224 and 1.4.225
  application bundle and are not yet verified live. `Cancel` reads the history first, fails with `relay.ErrConversationChanged`
  when the session holds another provider thread than the binding's (and refuses when it holds
  none), and cancels unless the latest turn is
  positively over (`completed` or `interrupted`, or no turn), because a cancel would start Codex for
  an idle session; an `unverifiable` turn is cancelled. The cancel names the turn's own `turnId`
  (from `latestTurn.turn.turnId`, else the newest root turn item), so Orca cancels that turn and
  no other; without one it is refused. It then waits up to 2 minutes until a page that names the
  bound provider thread shows the latest turn positively over; another thread fails with
  `relay.ErrConversationChanged`, and a page that names no provider thread (Orca omits
  `providerSession` without a provider handle) proves nothing, so the wait goes on. An accepted
  cancel whose turn stays `unverifiable` (Orca may answer `ok` with `cancelled: false`), in any
  other state, or without a named thread is an error at that deadline, never a stopped boundary
  (generation adoption and the drain never record a boundary from it). This check does not close
  the change between read and write (the relay's known gap). `Conversation` reads the provider thread and the latest turn (`latestTurn`, else the newest
  root turn item) as idle, running or unknown.
- **Events** (`events.go`). The socket has no subscription, so `Subscribe` polls
  `agentSession.history` `after` the saved `{epoch, sequence}` about once a second and loops at once
  while `hasNewer`. A history page over the 4 MiB bound is read again with half the item limit, down
  to one item, and the limit grows back after each page; a single item over the bound fails the read
  with that reason. Each item is reported once per subscription whatever revisions arrive: user
  messages on sight, a turn's start (its item ID is the turn ID, its `userItemId` the opening
  message) and end (only `completed` or `interrupted`; an unverifiable turn has not ended), other
  MCP servers' and local tool calls and pending approvals as one-line activity. A page lists items
  in the order of their first write, each at its latest revision, and a Codex assistant message
  carries no `state` in any revision (observed with Orca 1.4.224 and Codex 0.162.1, where ORCACHECK
  assumed `running` while it streams), so a message seen while its turn runs may be a fragment.
  Assistant messages are therefore reported only with their turn's end: when a page shows a turn
  over, the subscription reads the history `after` the turn item's own sequence to the journal's
  end (paged like any read, bounded by the turn's items and what followed them) and reports the
  turn's root assistant messages in their first-write order at their last revision, then the
  completion, in that page's events, so the text and the completion commit together and a restart
  repeats both or neither. Orca writes a turn's completion after its messages' last revisions, so
  the text read then is final; a turn's text reaches the Session after that turn's activity and
  tool calls, and a turn that stays `unverifiable` (not ended) reports none of its text. A reset during that read fails the subscription, which resumes from the saved
  cursor. An assistant message outside any turn is reported once its `state` is `completed`.
  The history import leaves out the assistant messages of a latest turn that is not over; the
  events report them with its completion. A page whose `providerSession` names another thread than the
  binding's ends the subscription with `relay.ErrConversationChanged`. Reasoning (by role), tool output and diffs are never passed on, nor any row a
  subagent produced in the shared journal (one with an `agentId`): the history import, the events
  and the turn state read only the conversation's own rows, as Orca's root predicate does. A root
  assistant message's `subagent-group` block is read separately for activity: each agent ID produces
  one short started line with its label and one finished line when Orca records `settledAt` (or
  marks a recovered agent `unverifiable` without that timestamp). The block's
  message text and every subagent row stay out of imported history. A reset
  restarts from the journal's current end.
- **Resume.** `ResumeSession` accepts only an open `agent-session` tab of agent `codex`, waits up to
  2 minutes until its latest turn is positively over (an unverifiable one fails the connect) and
  confirms its Codex thread.
- **Reload** (`Reload`, after a resume or a create and before the binding is stored). Codex starts
  its MCP servers when its process starts and ignores `notifications/tools/list_changed`, so a
  process that started before the binding existed lists no superverse tools. `Reload` reads the
  history (the bound thread, idle, else it fails), calls `agentSession.close {sessionId}` (answer
  `{ok: true}`: Orca stops the session's Codex process as a user close, records a Stop row, hides
  the chat tab and keeps the session record and its provider thread), then
  `agentSession.reveal {sessionId}` (answer `{ok, sessionId, workspaceId, agent, readable}` or
  `{ok: false, refusal}`: Orca opens the conversation's journal and publishes its tab, visible and
  active, without starting Codex unless a submission waits) and confirms the bound thread again.
  The next send, the brief, makes Orca start Codex for the session, resuming the head of its
  provider handle chain (the same thread) at a new runtime fence, and that process reads
  `config.toml` and starts `dexai-connect mcp` with the binding already stored. It is safe
  because the session is idle (no turn to cut), the session ID and thread do not change, every
  later send checks the thread before and after, and a changed thread stops the relay
  (`relay.ErrConversationChanged`); nothing creates a replacement conversation. A message the
  Builder types in Orca between the reload and the stored binding starts Codex too early; connect
  again then. These semantics were read from the Orca 1.4.224 and 1.4.225 bundles and await a live run.
