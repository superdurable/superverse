# Subagents

A subagent is a background agent that works on one bounded task in its own context and its
own git worktree, then reports back. It keeps wide investigation and independent changes
out of the main agent's context and lets them run in parallel with the conversation. Each
subagent is a Dex Flow, `SubagentFlow`, with its own message log, model loop, tools and
limits. The main agent starts it, keeps talking to the user, and merges the result.

| Property | Rule |
|---|---|
| Always in the background | `start_agent` returns at once with a task ID. Completion arrives as a notification ([notifications](notifications-and-feedback.md)); the main agent never waits |
| Own context | A subagent sees none of the conversation. Its prompt and the repository are everything it knows. Only its report returns to the main agent |
| Own worktree | It works in a git worktree under `/workspace/.git`, so it cannot disturb the main workspace. Its changes reach the workspace only through `merge_agent_changes` |
| Bounded | Per-kind duration limits; at most 3 run at once; at most 6 start without user input |
| Frozen | Model, effort, system prompt and tools are fixed at start. The model is the Session's model at that moment |
| Stoppable | The user's Stop stops every subagent; the main agent can stop one with `stop_task` |
| Not a second Session | It cannot ask the user, set timers, commit, push, start agents or run background commands |

## When the main agent delegates

The main agent's prompt contains a delegation section with these rules:

- Delegate reading and changes that would otherwise fill the main context with files,
  search results and command output: broad investigation of a code path or a dependency,
  and bounded changes in files that do not depend on what the main agent is doing.
- Do not delegate what a few tool calls or a verification can answer, and do not delegate
  the next step the main agent itself is waiting for.
- Brief each subagent once, completely: the goal, the relevant paths, the constraints and
  what the report must contain. There is no way to message a running subagent.
- Start independent subagents in the same reply.

| Kind | Use | Effort setting | Duration limit | Workspace |
|---|---|---|---|---|
| `investigate` | Answer a question about the code or its dependencies | `agent.subagentInvestigateEffort`, default `low` | 20 minutes | A worktree of the application |
| `change` | Make a bounded change and test it | `agent.subagentChangeEffort`, default `medium` | 45 minutes | A worktree of the application |
| `connector_select` | Choose connectors for the requirements | `medium`, fixed | 10 minutes | A worktree of the application, read only |
| `connector_develop` | Build a missing connector or operation, or fix a connector, for a draft pull request | `agent.subagentConnectorDevelopEffort`, default `high` | 45 minutes | A worktree of the connectors library on the connector's branch |

The two connector kinds, their prompts, tools, reports and the `ConnectorContributionFlow` that
publishes a contribution are specified in [connectors](connectors.md#connector-subagents). The
effort settings are keys of
the server configuration ([context management](context-management.md#configuration)); a
value is clamped to the ceiling of the model (Haiku 4.5 supports up to `high`) and can be
tuned from measurements. `change` and `connector_develop` subagents may raise their own
effort with `set_effort`; the main agent and the rules for it are in
[context management](context-management.md).

## The main agent's tools

### `start_agent`

Input (non-strict schema until the live strict-tool test measures it,
[strict schemas](tool-orchestration.md#strict-schemas)): `description` (1 to 80 characters,
shown to the user), `prompt` (at most 32 KiB) and `kind`.

1. **Admission** (`RouteTool`). At most 6 subagents are started without user input
   (`AgentState.SubagentsStartedWithoutUser`, reset when a user-initiated turn begins) and
   at most 3 run at once. The budget is checked first, because once it is spent, waiting
   for a running subagent does not help. An admitted call creates the Session record
   `Subagents/<id>` with status STARTING and counts against the budget; a second route of
   the same call, after the cold path for example, finds the record and counts nothing. A
   rejected call is a failure of the call with no effect, and its hint tells the model what
   to do instead: wait for a notification, stop one with `stop_task`, continue with the
   subagents it has or ask the user.
   When the 50 finished records are full, admission evicts only the oldest finished record
   and stores its agent ID in `PendingSubagentWorktreeCleanup` in the same commit. A pending
   ID defers further eviction. An unfinished record is never selected.
2. **Execution** (`ExecuteTool`, serial and SYNC: 2 minutes per attempt, retried after a
   transient fault, and not interruptible by Stop mid-call). It creates the worktree
   ([below](#worktrees)) and starts the `SubagentFlow` through the Service (the
   `AgentTaskLauncher` port) with the call, the snapshot commit, the Session's model and
   HEAD and its accepted lease. Both effects are idempotent, so a retry repeats neither.
   Before creation, a separate committed `ExecuteTool` cleans the pending expired agent ID
   through `CleanupAgentWorktree` with `removeCheckout: true`, then clears the ID and resumes
   the same call. This flag is exclusive to platform expiry; ordinary remote task cleanup
   refuses a registered checkout. Cleanup can remove one left registered after a failed save.
   A lost answer repeats the same ID. If the Sandbox instance is unreachable, the call
   takes `RequestSessionWorkspace` on every attempt and resumes after a new instance is ready;
   this cleanup consumes no Dex retry, even when the checkout and refs are already absent.
   A failed cleanup leaves it pending, answers the start
   as unavailable after its normal retries, and removes the new unadmitted STARTING record;
   the next start tries it again. No new worktree or SubagentFlow starts until cleanup is
   confirmed.
   - A worktree that the Sandbox refuses (`snapshot_too_large`, `workspace_disk_low`) is a
     failure of the call before any subagent starts: the record is deleted and the start no
     longer counts against the budget. Only such a refusal deletes the record, because the
     helper refuses only a creation that found no snapshot of the agent, which proves that no
     attempt of the call started the subagent. Any other failure of the creation, such as a
     helper answer the platform cannot read, is an error that the Step retries: an earlier
     attempt may have created the worktree and started the Flow.
   - A Sandbox lost during a retry is retried, not rerouted to the cold path. A start whose
     retries run out ends in `RecordToolFailure`, which marks the STARTING record FAILED,
     so a late admission is denied and no subagent runs behind a start that the model reads
     as failed. The model may start a new subagent, which is a new call with a new agent ID.
   - A pending `start_agent` call that a Step answers without its `ExecuteTool` started no
     subagent, because `ExecuteTool` reroutes to the cold path only from its first attempt,
     before it starts one. Steering supersedes such a call (`CheckSteered`), and so does a
     workspace the call waited for that could not be prepared (the cold-path Step that
     settles it answers `workspace_unavailable`). The Step that answers the call forgets its
     STARTING record and gives the start back, so no orphan holds a running place, extends the
     Sandbox deadline or reads `starting` in the tasks panel.
3. **Result.** Immediately `{task_id: "agent-k3n8q5r1", base_commit}`. For a record that a
   Stop ended before the start, the result is `{task_id, status}` with that status, and no
   subagent starts.

The agent ID `k3n8q5r1` is `flowid.AgentTaskID(sessionFlowID, callID)`: eight lowercase
letters and digits derived from the Session's FlowID and the tool call ID, so it is stable
across Step retries and Worker replacement and distinct for every call.

### `stop_task`

`stop_task{task_id}` stops one task. An `agent-...` ID goes to the `SubagentFlow`'s
`StopSubagent` through the Service; a `cmd-...` ID goes to the command ledger
([sandbox](sandbox.md#command-ledger)). A stopped subagent saves its worktree changes and
delivers a STOPPED report as a notification; one that is still initializing records the
stop and ends STOPPED right after its admission. The answer says that the subagent is
stopping (its report follows) or has ended; for a subagent that had already ended it names
its status, for a subagent the Session counts as lost ([below](#stop-and-project-deletion)) it
says so instead of promising a report, and an ID that no subagent of the Session has is
`task_not_found`. The user has no per-task stop button; the model stops one task when the
user says, for example, "stop doing X".

### `merge_agent_changes`

`merge_agent_changes{agent_id, action: apply | discard, paths?}` is a Sandbox tool that the
Session's `ExecuteTool` runs (SYNC, 3 minutes per attempt, not interruptible by Stop). It
performs no Flow RPC: the Session checks its own record, the helper in the Sandbox merges,
and the commit that records the result closes the record when the call decides all of the
subagent's changes. Only a finished subagent can be merged, and a subagent the Session counts
as lost ([below](#stop-and-project-deletion)) counts as finished, because a record without a
report may still hold changes. A subagent that left nothing to merge, with no saved changes
and none left unsaved, is answered without touching the Sandbox. A subagent whose end could
not save its worktree ([Worktrees](#worktrees)) is merged from that worktree: the helper saves
it first, as the finish would, and a worktree lost before it was saved holds nothing to merge
(`nothing_to_merge`).

- **`apply`** merges the subagent's result into the main workspace ([merge](#merge)) and
  returns `merged`, `merged_with_conflicts` or `nothing_to_merge`, with the lists of
  applied, conflicting (with the conflict kind) and unchanged files. `paths` (workspace
  relative, at most 200) restricts the merge to the changed files they name, files or
  directories; a path that names no changed file, such as a typo, is listed under `unknown`
  and never as unchanged. A conflict of kind `content` is left in the file with conflict
  markers; every other kind keeps the workspace's version and is reported for the main agent
  to resolve. When there are conflicts the result's `next` line says how to read the
  subagent's version of a file from its result ref.
- **`discard`** drops the result, returns `discarded` and leaves the workspace untouched.
  `paths` with `discard` is an invalid call.
- A discard, or an apply of every file (without `paths`), sets the Session record to MERGED or
  DISCARDED, in the commit of the tool result and without the record's instance lock: once a
  subagent has ended, the merge is the only writer of its record. That decision is final:
  every later call of either action returns its receipt. An apply with `paths` is not final.
  It has its own plan and receipt, keyed by the sorted paths, so a repeat of it answers its
  receipt, a call for other paths merges them, and the files it left out stay mergeable (the
  files it merged read as unchanged) and readable in the result ref. The record stays open
  until a discard or an apply of every file closes it.

The merge writes uncommitted changes into the main workspace. A merge that applied files
(`merged` or `merged_with_conflicts`) counts as a source change like `edit` and `write`: a
turn that ends before a successful `commit_and_push` after it gets the unfinished-source
reminder, up to three times
([context management](context-management.md#what-the-design-removes)). The main agent then
runs the checks and commits through `commit_and_push` as for any change of its own.

### Failure codes of the three tools

These use the owner and effect model of [tool orchestration](tool-orchestration.md#codes).
A conflict is not a failure: `merged_with_conflicts` is a normal result that the model
acts on.

| Code | Owner and effect | Raised when |
|---|---|---|
| `subagent_budget_reached` | call, none | Six subagents were started since the user's last message (checked before the running limit) |
| `subagent_limit_reached` | call, none | Three subagents are already running |
| `snapshot_too_large` | call, none | The uncommitted and untracked files a worktree would snapshot exceed 100 MiB; commit, delete or ignore files first |
| `workspace_disk_low` | platform, none | The volume has less than 1 GiB free, so no worktree is created |
| `agent_not_finished` | call, none | `merge_agent_changes` names a subagent that is still running (one the Session counts as lost is not) |
| `task_not_found` | call, none | `stop_task` or `merge_agent_changes` names an unknown agent ID |

`stop_task` on a finished subagent returns its status and is not an error. After exhausted
retries a `start_agent` or `merge_agent_changes` call becomes `tool_unavailable` with effect
unknown. A start that failed this way is final (its record is FAILED); the model starts a
new subagent if it still wants one. The model repeats `merge_agent_changes`, which is
idempotent through its receipt, and reads `git status` to reconcile.

## What a subagent can do

| | |
|---|---|
| Available | Every kind: `bash` in the foreground, `read`, `grep`, `glob` and `connector_catalog`, rooted in its own worktree. `edit` and `write` for the kinds that edit files (`investigate`, `change`, `connector_develop`); the platform's read-only delivery, platform-flow and Live-run readers for `investigate` and `change`; `set_effort` for `change` and `connector_develop` on a model that takes effort (`agentloop.SubagentTools`) |
| Not available | `write_todos`, `request_user_input`, `start_timer`, `confirm_requirements`, `commit_and_push`, `start_agent`, `merge_agent_changes`, `stop_task`, `read_task_output`, background commands |
| Failures | A failed tool call returns to the subagent's model with its owner and effect like any tool failure ([tool orchestration](tool-orchestration.md)); there is no manual recovery |
| Platform tools | They read only the Project of the owning Session and refuse the subagent Flows themselves |

The subagent's prompt rules:

- Work only inside your root. The root is your worktree; do not read or write the main
  workspace.
- Do not start servers, do not hold ports, and do not use the Preview, run the application or
  run its test engine.
- Run one command at a time, in the foreground; a command past its timeout is stopped.
- Do not commit, push, rebase, stash or change branches.
- Finish with a report: what you found or changed, the files involved and anything the
  main agent must decide. The report is your last message; it is capped at 24 KiB.

The path rules of the confinement package apply with the worktree as the agent root
([sandbox](sandbox.md#sandboxd-and-confinement)). The isolation is a soft one: a command
can `cd` out of the worktree. A per-agent mount namespace is a later hardening, not part
of this design.

## Worktrees

A subagent's worktree is a git worktree of the workspace repository, created, finished and
merged by one helper process per operation (`internal/dexai/agents/sandbox/workspace/git/agent_worktree.go`
runs the helper program that no model input selects). Every operation queries before it
writes. No helper Git command takes an optional lock (`GIT_OPTIONAL_LOCKS=0`), so the status
read of a snapshot never rewrites the workspace's index or holds its lock against a Git command
of the main agent.

- **Location.** `/workspace/.git/superverse/worktrees/<id>`. Git accepts a worktree inside
  its own directory, and there `git add --all`, `git clean -fdx` and the main agent's file
  tools cannot touch it.
- **Refs and records.** `refs/superverse/agents/<id>/base` and `.../result`. The plan
  (`plan-<key>.json`) and the receipt (`receipt-<key>.json`) of each merge request live in
  `.git/superverse/agents/<id>/`; the key is `apply` for every file, `discard`, or
  `apply-<digest>` for the sorted selected paths.
- **Creation.**
  1. An existing base ref is reused, which makes creation idempotent: a repeated creation
     creates nothing, and a subagent that already finished gets no new checkout.
  2. A first creation first saves and removes the worktrees of earlier subagents that the
     caller does not name as unfinished (orphans of earlier failures; a worktree that was
     not saved yet is saved into its result ref first). It then refuses the request when
     the volume has less than 1 GiB free (`workspace_disk_low`) or the dirty snapshot would
     exceed 100 MiB (`snapshot_too_large`).
  3. The snapshot uses a temporary index: `read-tree HEAD`, `add -A`, `write-tree`,
     `commit-tree -p HEAD`. It captures uncommitted and untracked (but not ignored) files
     and never touches the main index, HEAD or working tree. Ignored files such as
     dependencies and build output are not copied; a subagent that needs them restores them
     in its worktree.
  4. `worktree add --detach` checks the snapshot out.
- **Finish.** `add -A`, `commit-tree -p base`, the result ref is created, `diff --numstat`
  is recorded, and `worktree remove --force` deletes the checkout. The refs remain, so the
  result outlives the worktree and stays available to `merge_agent_changes`. A saved
  result is reused, so a retry saves once. A worktree that holds no change leaves nothing to
  merge. A worktree that the end could not save (the Sandbox was unreachable, it refused the
  save, or the worktree was gone before it was saved) is not known to be empty: the outcome
  records why its changes are unsaved, they stay in it on the volume, and `merge_agent_changes`
  saves what it still holds first; a worktree that was lost holds nothing, which that merge
  answers `nothing_to_merge`.

### Merge

The merge is a per-file three-way merge where the **base** is the snapshot, **ours** is the
current main workspace (snapshotted the same way, uncommitted and untracked files included)
and **theirs** is the subagent's result.

| Case | Result |
|---|---|
| Only one side changed the file | That side's version is used |
| Both sides made the same change | The file is reported unchanged |
| Both sides changed a text file | `git merge-file --diff3`; on conflict the file holds the conflict markers (`workspace`, `snapshot`, `agent-<id>`) and is reported as `content` |
| Binary file, delete against modify, or both sides added | The main workspace's version is kept and the conflict is reported as `binary`, `delete_modify` or `add_add` |
| A file the workspace changed between the plan and its write | The workspace's version is kept and the conflict is reported as `changed_during_merge` |

It runs in three steps: write the **plan**, write only the files that still equal their
expected pre-image, write the **receipt**. Each request has its own plan and receipt, keyed
by the files it selects (every file, a discard, or the sorted selected paths), so a retry after
a lost response, a Worker restart or a Sandbox replacement applies each file once and returns
that request's receipt. A discard, or a merge of every file, is the decision: every later
call, of either action, returns its receipt. A merge of selected paths leaves the other
changes mergeable. The outcome is `merged_with_conflicts` when any file is a conflict,
`merged` when files were applied and none conflicts, `nothing_to_merge` otherwise, and
`discarded` for a discard.

## Lease hand-over and refresh

The subagent runs commands and file tools through the same sandboxd transport as the main
agent, so it needs the workspace lease, and the normal path needs no Flow RPC:

- The start installs a copy of the lease the Session accepted as the subagent's
  `Workspace` Attribute, together with its identity and start, atomically with the
  StartFlow. A retry that carries a fresher lease joins the same execution; Dex keeps the
  first accepted start.
- While the lease is usable the subagent talks only to sandboxd: no Flow RPC and no Flow
  read is involved. It does not tell the Session about each tool call: the Session sees its
  own `Subagents` records and knows that a task is running, which keeps the Sandbox
  deadline from expiring ([sandbox](sandbox.md#sandbox-deadline)).
- When the transport proves the instance unreachable (a reclaimed or replaced Pod), the
  call was not dispatched, and the Step `RefreshSubagentWorkspace` asks the Session every
  10 seconds, through the Service, for the lease of its instance with
  `RequestSessionWorkspace{ForAgentID, Lost}`, where `Lost` names the request whose lease
  proved unreachable. The Session answers only while that agent's record is RUNNING
  (otherwise `Closed`, which fails the subagent). If `Lost` is still the Session's current
  request, it revokes that instance, so the next request replaces it; it returns the lease
  of a usable instance and otherwise starts a new instance, and the subagent asks again
  until it holds a lease of another request or the 30-minute wait limit fails it. The
  worktree lives on the volume under `.git`, so the new instance finds it intact. A call
  may lose its lease twice; a third loss fails the call `workspace_unavailable`. A mutating
  call that an earlier attempt may have dispatched, and a command that was running when the
  instance went away, are reported to the model as interrupted with an unknown effect.
- `FinishSubagent` saves the worktree with the same lease copy, and the instance may have been
  replaced or removed while the subagent was outside a tool call, for example when a Stop or
  the deadline cancelled its lease refresh, the main agent reported the lease lost and a
  reclamation deleted the Pod, or an eviction. The worktree is intact on the volume, and the
  Session's record lends the lease until the report, so on a lease copy that proves
  unreachable `FinishSubagent` goes to `RefreshFinishingWorkspace`, which asks the Session for
  the lease of its current instance exactly as the loop's refresh does (revoking a lost
  current instance, whose replacement the Session provisions on the same volume) and saves
  the worktree again on it, at most twice. A Session that no longer lends the workspace, or
  the 30-minute wait limit, records the end with its changes unsaved.

## Resource rules

A Sandbox container is limited to 2 CPUs and 4 GiB, and the main agent, the Preview and
the application test engine run in it too. The platform applies these rules to every
subagent command:

- `nice -n 10`, `GOFLAGS=-p=2`, `GOMAXPROCS=2` and `NODE_OPTIONS=--max-old-space-size=1024`.
- One foreground command per agent at a time, and no background commands.
- No use of the fixed ports of the Pod, the Preview's and the application test engine's
  among them (the table in [sandbox](sandbox.md#coding-image)); the prompt lists them and
  the subagent starts no servers.
- At most 3 subagents at once. The application test engine takes an exclusive queueing
  lock, so concurrent tests wait instead of killing each other
  ([commit gate](commit-gate-and-app-tests.md)).
- The budgets are in [sandbox](sandbox.md#resources).

## Reports and the Session record

A finished subagent delivers one task notification to the Session: `TaskID`
`agent-<id>`, `TaskStatus` and the report (at most 24 KiB), with the notification ID
`task/agent-<id>/<status>`. The Session adds it to the conversation as a `notification`
entry at the next model-call boundary of a running turn, or opens an automatic turn when
it is idle ([notifications](notifications-and-feedback.md#task-notifications)). A subagent
whose end could not save its worktree says so: the report leads with why, the notification
carries `changes_unsaved`, and its summary says that the changes could not be saved and stay in
the worktree, where `merge_agent_changes` saves them. The notification also extends the Sandbox
deadline.

The Session keeps one `SubagentRecord` per subagent in its AttributeMap `Subagents`,
declared in [session](session.md#attributemaps) (key: the agent ID, at most 2 KiB, no
prompt text, at most 3 unfinished and 50 finished). Its status moves through these writers:
`RouteTool` creates the record STARTING when `start_agent` is admitted; `AdmitSubagent`
writes RUNNING; the task notification writes the terminal status (COMPLETED, FAILED,
STOPPED or TIMED_OUT), also when the notification is not admitted and also in a Session
that is no longer ACTIVE, and a record that ended keeps its end; `merge_agent_changes`
writes MERGED or DISCARDED for a discard or a merge of every file. Besides, a Stop marks a
STARTING record STOPPED, and a `start_agent` call whose retries ran out marks it FAILED. A
STARTING record whose subagent cannot have started is deleted instead, in the commit that
answers its call: by `ExecuteTool` when the Sandbox refused the worktree, and by the Step that
answers a pending call without `ExecuteTool`. The record's locks are in
[session](session.md#attributes). The statuses are persisted, and named in the notification
ID, in lower case (`starting`, `running`, `completed`, `failed`, `stopped`, `timed_out`,
`merged`, `discarded`). The record holds the kind, description, call fingerprint, base commit,
start, admission and end times, the one-line summary of the end (at most 400 bytes) and, once
finished, the changes (result commit and diffstat) or the mark that they stayed unsaved, and
what the merge did (its outcome and the numbers of applied, conflicting and unchanged files).
The Snapshot lists tasks from the records for Studio; the conversation never carries one.

## Stop and Project deletion

- **A subagent that never reports.** If a record is still unfinished one hour after the
  subagent's wall-clock limit, the Session treats it as lost: it no longer counts toward the
  concurrency limit, no longer keeps the Sandbox alive and is no longer stopped by the Stop
  button. The Snapshot marks it (`AgentSubagent.lost`), so every view leaves it out of the
  running work as well: Studio counts it as ended and shows "Lost, no report", the Session
  Watch relays none of its Stream, `stop_task` answers that it is lost instead of promising a
  report, and `merge_agent_changes` treats it as ended and asks the Sandbox, because a record
  without a report may still hold changes. A late report still ends the record.
- **Stop.** The user's Stop runs the Session's `InterruptAgent` Step. It stops the commands
  first (the foreground command of a stopped `bash` call and every background command) and
  then calls `StopSubagent` through the Service for every RUNNING record. It tries every one
  of these stops before it reports a failed one, so a subagent whose stop failed, for example
  because a lifecycle Step held its status lock past the client's wait, leaves no command
  running, and the retry repeats all of them, each idempotent. Each running subagent saves
  its changes and reports STOPPED; the report returns to the main agent, and a "continue"
  lets it carry on from the saved work. A model call in flight notices the cancellation at
  its next heartbeat, up to about 48 seconds, and its output is discarded.
- **Stop while a subagent is starting.** A record that is still STARTING has no subagent to
  stop. The Step that commits the Stop, `FinishInterrupt`, or the turn end that the Stop
  leaves to `CheckSteered`, marks it STOPPED under the AgentInterrupt lock, so the Flow's
  admission answers that a Stop stopped it. `FinishInterrupt` does so only while the Stop of
  its own turn is still the pending one: once the turn's next boundary applied the Stop and
  a later turn admitted a new start, that start is none of this Stop's. A start the Stop
  overtook before the Flow existed starts nothing, and a retry of the `start_agent` call that
  finds its record ended answers with that status. But `start_agent` cannot be interrupted,
  so the Stop can commit while the call's `ExecuteTool` already started the Flow and answers
  `{task_id, base_commit}`: the model then waits for a report, so the subagent ends STOPPED
  without an effect and reports it.
- **A Session that no longer lends the workspace.** A subagent runs on its lease copy and
  learns that its Session ended, or no longer runs its record, only when it asks: at a lease
  refresh, which fails it, and when it reports, which then goes undelivered. Nothing else
  stops it before its duration limit.
- **Project deletion.** `CleanupProjectSession` completes the Session, ends its
  `SessionWorkspaceFlow` and waits for a provisioning that may still run. It then finds the
  subagent executions that have not ended by FlowType, `ProjectID` and status, because they are
  not registered as Project children, stops them and waits for their ends, closes the Session's
  connector contributions, and only then ends the Preview and releases the resources. The
  search is eventually consistent: a subagent it misses is denied by the closed Session at its
  admission or fails at its next lease refresh, and the next pass asks again. The worktrees,
  refs and receipts are files on the volume that Project deletion deletes.

## Dex Flow Changes

The complete per-Flow review contract of `SubagentFlow` (identity, every Attribute and
AttributeMap, the ChannelMap, the Streams, the Steps, RPCs, external-effect identities and
recovery) is owned by the [subagent package README](../../../internal/dexai/agents/session/subagent/README.md),
and the records, kinds and bounds by [agenttasks](../../../internal/dexai/agents/session/agenttasks/README.md).
The Session's primitives for subagents (the `Subagents` map, `AdmitSubagent`, `InterruptAgent`)
are declared in [session](session.md); their semantics are above. In outline:

- **Identity.** FlowID `subagent-<projectID>-<agentID>` (`flowid.SubagentFlowID`), for example
  `subagent-a7k2-k3n8q5r1`; the task ID is `agent-<agentID>`. Only the Session's `start_agent`
  `ExecuteTool` starts it, through the Service (`AgentTaskLauncher`), with
  `dex.IDReuseDisallow`, same-request IgnoreError, no Flow timeout and the Start RequestID
  `flowid.Fingerprint{call fingerprint, BaseCommit}`, where the call fingerprint is
  `flowid.Fingerprint{SessionFlowID, AgentID, Kind, Description, PromptSHA256}`. A duplicate
  start joins the execution, also a completed one, and a finished subagent is never started
  again. The start installs `Identity`, `Start`, `ProjectID`, `SubagentStatus` (INITIALIZING)
  and `Workspace` (the lease copy).
- **Admission and lifecycle.** The first Step asks the Session (`AdmitSubagent`) before any
  effect ([Stop and Project deletion](#stop-and-project-deletion) describes the answers).
  Business completion is the terminal `Outcome` (COMPLETED, FAILED, STOPPED, TIMED_OUT or
  DENIED); every end but a denial reports to the Session, and engine completion is
  `dex.GracefulComplete(Receipt)` once the report was delivered or its two-hour wait passed.
  An independent top-level Flow owned by the Session, neither a SubFlow nor a Project child.
- **Primitives.** Attributes `Identity`, `Start`, `ProjectID` (indexed `keyword2`),
  `SubagentStatus` (indexed `keyword3`, the status lock), `Config`, `StopRequest`,
  `ConnectorCheckFailures` (integer, absent means zero), `Workspace`,
  `Outcome` (with `Contribution` and `Refused` for `connector_develop`), `AgentState`,
  `MessageWindowLock` and `ContextSegment`; AttributeMaps `CurrentMessages`, `ArchivedMessages`
  and `ContextSegmentPrompts`; ChannelMap `ParallelToolResults` (key `tool-batch-<seq>-<first>`);
  Streams `AgentActivity`, `AssistantText` and `ReasoningSummary`, 10 MiB each, shared by every
  execution of the Flow type, which the Session's Watch relays as a live status line while the
  Session's record stays canonical.
  `AgentState` no longer counts model calls. A persisted prelaunch
  `turn_model_calls` field and `Config.model_calls` field are ignored on decode.
  Existing FlowIDs, Step identities, message keys and deadlines are unchanged;
  an open execution continues at its next model-call boundary without a cap.
- **Steps and RPCs.** `InitializeSubagent`, `RecordSubagentAdmissionFailure`,
  `ApplySubagentAdmission`, `AwaitSubagentDeadline`, `CallSubagentModel`,
  `CompactSubagentContext`, `RecordSubagentFailure`, `RouteSubagentTools`,
  `ExecuteSubagentTool`, `SettleSubagentCommand`, `RecordSubagentToolFailure`,
  `ExecuteSubagentParallelTool`, `RecoverSubagentParallelToolExecution`,
  `AwaitSubagentParallelToolResults`, `RefreshSubagentWorkspace`, `BeginSubagentFinish`,
  `FinishSubagent`, `ResumeConnectorAfterCheckFailure`, `RefreshFinishingWorkspace`, `RecordSubagentOutcome`,
  `ReportSubagentOutcome` and `RecordUndeliveredReport`; RPCs `StopSubagent`,
  `GetSubagentSnapshot`, `GetDexSummary` and `GetDexDisplay`. The generated graph is
  `docs/flows/sv2-subagent-flow.json`.
- **Packages.** `internal/dexai/agents/session/subagent` holds the Flow, its client, bootstrap and
  dependency; `SubagentFlowClient` fronts exactly this Flow (the start, `StopSubagent`, the
  retained read, the search by FlowType and `ProjectID` that Project cleanup uses and the
  activity read the Session's Watch relays). `SubagentFlowDependency` is what the Flow consumes
  from the Session Service: the admission, the lease refresh and the report.
  `internal/dexai/agents/session/agenttasks` declares `AgentTaskLauncher`, which the Session consumes.
  The Session Service implements both in `internal/dexai/agents/session_flow_service_tasks.go`
  ([composition](composition.md)); the Flow reaches the Session only through that Service.

## Database Schema Changes

None. Subagent state is owned by `SubagentFlow` and the Session's `Subagents` map; the
worktree, plans, receipts and refs are files and git objects on the volume, never trusted
by a gate.

## UI/UX

- **Tasks panel** (`AgentTasksPanel`): one row per subagent and background command with
  its description, status, a live one-line status for a RUNNING subagent (from its
  `AgentActivity`), and, once finished, the diffstat line `N files changed (+x -y)` when it
  saved changes. It only shows progress; there is no per-task stop button. The single Stop
  button of the conversation stops everything. The panel is specified in
  [streaming and Studio](streaming-and-studio.md#tasks-panel).
- **Notifications.** A finished subagent appears as a task notification in the
  conversation, never as a user bubble.
- **Merge.** The main agent's own message tells the user, in business language, what the
  subagent found or changed and what was merged.
- Accessibility, keyboard behavior, cancellation and stale-response protection follow the
  shared rules ([streaming and Studio](streaming-and-studio.md)).

## Tests

Real dependencies only: real Dex, a real coding container with sandboxd, and the mock
model, which starts subagents through a scenario marker in the `start_agent` prompt. The
three end-to-end cases stay the only E2E; subagent paths are Dex integration tests.

- **Identity.** A repeated `start_agent` call with the same call ID joins the same
  execution and the same worktree; a start after Project deletion or after a Stop marked the
  record is denied by `AdmitSubagent` with no effect; while a subagent runs, the Session
  admits only its own call again, unchanged, and denies another; the FlowID derives from
  the Session FlowID.
- **Concurrent admission.** A fourth concurrent `start_agent` and a seventh start without
  user input are rejected without an effect, the budget before the running limit; a
  worktree the Sandbox refuses gives the start back; a duplicate `AdmitSubagent` is
  idempotent; a wave of `start_agent` calls in one reply admits at most the limit; a
  `start_agent` call that steering supersedes, or whose workspace could not be prepared,
  leaves no record and starts no Flow.
- **Map keys and selective loads.** `Subagents/<id>` exact loads; message map keys
  (`37`, `11`) and exact archive loads.
- **Channel races.** A parallel wave joins exactly once; a Stop during a wave drains the
  instance and leaves no pending message.
- **Stream loss.** Dropping `AgentActivity` frames changes nothing: the Snapshot and
  `GetSubagentSnapshot` remain correct.
- **Restart and recovery.** A Worker restart during a tool call runs the command once
  (ledger) and the worktree is saved once; a restart during `FinishSubagent` finishes the
  worktree once; a Pod replacement makes the subagent refresh its lease through the Session
  and the worktree is intact; an instance removed while a change subagent's check runs makes
  the finish, not the loop, refresh the lease, and the merge applies the change saved on the
  next instance of the same volume.
- **Limits.** A subagent runs until it reports, is stopped or reaches its duration
  deadline; `timed_out` and `stopped` save their changes. No model-call cap or
  forced final model call remains.
- **Stop.** `stop_task` stops a running subagent, which saves its worktree and reports
  STOPPED, and its report opens an automatic turn; the Stop button stops one while no turn
  runs, and the paused automatic turns record its report without opening a turn; a Stop that
  overtakes a starting subagent marks its record STOPPED, and the subagent that the racing
  `start_agent` still starts is answered `Stopped` at its admission and reports STOPPED.
- **Merge.** Snapshot of a dirty workspace, per-file three-way merge, conflicts of every
  kind, `discard`, repeated apply, and a restart between the plan and the receipt (container
  tests on the real coding image, which also verify that Git accepts a worktree inside its
  own git directory and that a snapshot leaves the workspace's index unwritten even when
  it is stale, plus Dex integration tests of a merge and of a content conflict). A merge of
  paths that no changed file matches merges nothing, lists them as `unknown` and leaves the
  change mergeable by the following merge of every file; a repeated selection answers its
  receipt; a worktree that the end could not save is saved by the merge, and one lost before
  it was saved merges as `nothing_to_merge`. A merge that applied files counts as a source
  change.
- **Retention and cleanup.** Project deletion finds a running subagent by FlowType and
  `ProjectID`, stops it and waits before it releases the Sandbox's resources, and the
  release of the Pod ends their commands; no subagent execution is registered in
  `ChildFlows`. A finished record evicted at the 50-record limit is no longer addressable by
  `merge_agent_changes`; its base/result refs, checkout and helper plans/receipts are cleaned
  on the next platform start. If its save had failed, eviction deliberately discards the
  unmerged checkout when 50 finished records already exist. A Session switched to
  remote mode cannot start a platform subagent; it retains at most one pending cleanup ID
  until it returns to platform mode or Project deletion removes its workspace volume.
- **Expiry cleanup recovery.** A real Git ref lock fails cleanup, keeps the one pending ID
  and leaves no new STARTING record after exhausted retries; removing the lock lets the next
  start finish the same cleanup before creating its own worktree. The default helper request
  still refuses a registered checkout; only expiry requests checkout removal.
- **Unverified until the final acceptance.** Quality of real subagent work; the real
  model's use of the delegation rules; whether `start_agent` and `merge_agent_changes` fit
  the strict grammar (the live strict-tool test measures it); the refusal below 1 GiB of
  free volume, which no test provokes.

## Documentation

This document owns the subagent design: when the main agent delegates, its three tools, the
worktrees and the merge, the lease, the resource rules, the reports and the stop.
[session](session.md) lists the Session primitives and links here;
[start-flow-admission](../start-flow-admission.md) lists the `SubagentFlow` StartFlow call
site; the README of [`session/subagent`](../../../internal/dexai/agents/session/subagent/README.md)
owns the per-Flow contract and the README of `session/agenttasks` the records and bounds; the
SubagentFlow section of [the specification](../superverse-v2.md#subagentflow) summarizes them.
The Git helpers' README (`internal/dexai/agents/sandbox/workspace/git`) describes the worktree
operations and the merge receipt.

## Resource Scalability

No new cloud, Kubernetes, Temporal, Dex or database resource is created. One
admitted subagent still owns one Dex Flow and one worktree; the existing Session
limits of three running subagents and six starts between user messages cap
simultaneous work, while each kind's 10-, 20- or 45-minute deadline ends its
execution. Project deletion stops running subagents and removes worktrees;
Dex retains closed execution state for its configured retention period.

The Session's existing Sandbox volume now keeps refs and merge receipts for at most
50 finished platform subagents plus one expired ID awaiting confirmed cleanup;
at most three unfinished subagents may still own worktrees. Cleanup occurs only
when `start_agent` evicts a finished record, once per such start, with no per-request
cloud control-plane call or tenant scan. Failure does not accumulate pending IDs:
the one ID survives and a cleanup failure removes that call's unadmitted record.
Deleting the refs makes their Git objects eligible for Git garbage collection;
physical bytes may remain until Git maintenance or Project deletion. Remote mode
adds no platform subagent resource and postpones this one cleanup until platform
mode resumes or Project deletion removes the volume.

Removing the call cap can grow `ArchivedMessages` further within one subagent
execution. `CurrentMessages` stays below 20 entries and archives its oldest ten;
`ArchivedMessages` uses exact sequence keys and is retained with that Flow.
There is no per-execution model-call count maximum now, so the number of archive
chunks is limited by the deadline and actual model/tool latency, not a fixed
quota. The pinned Flow contract and SDK do not declare a separate archive-chunk
quota (checked 2026-10-13); capacity must be measured under sustained subagent
work. The three 10 MiB Streams retain their existing shared Flow-type budget
and are best effort. Each call uses the existing model and Sandbox data paths:
no cloud control-plane call or tenant-wide scan occurs per call, poll or event.


## Project credential execution copy

All existing Flow identity, business task identity, RequestID, duplicate start,
reuse, admission, deadline, Steps, locks, message maps, Channels and shared
best-effort Streams remain unchanged. `SubagentStart.CredentialRevision` is a
non-secret frozen source revision, installed as `ModelCredentialRevision<uint64>`.
The new ordinary `ModelKey<Envelope{Revision uint64, Key string}>` Attribute uses
`HardDeleteOnNull()`; it is not an AttributeMap and has no instance key.
`ModelCredentialChanged<bool>` records a credential-driven Stop reason. None of
these Attributes is indexed or SQL-synced. The Session remains the credential
owner; the child holds only a bounded execution copy to avoid a cross-Flow read
on every model call.

The Session ExecuteTool serializes Start+copy through its exact provider metadata lock,
starts without a key, checks the child's frozen revision/lifecycle, and copies
through the typed Subagent Flow client's direct hard-delete write. It checks the
child lifecycle again and clears copies of terminal/finishing children. A repeated
Start never writes key bytes through initial Attributes or RPC input. A mismatched
old execution is stopped, and a terminal execution receives no new copy. If a replay
finds the admitted copy absent or mismatched, it stops that child with the credential-change
reason and exact-clears its copy. The uncertain copy is not repeated; parent metadata
remains Active throughout, while the child fails closed. A missing or superseded
prepared holder is a typed tool rejection before any child starts, without
retry exhaustion. A child stopped during revision/copy reconciliation also
answers `model_credential_unavailable`, rather than claiming a working task;
its retained record delivers the credential-change task notification. A metadata-only holder is
committed by the existing ExecuteTool before the copy; its CallID makes admission
idempotent. This adds no Step type and passes no key through a Step payload or RPC.
`CallSubagentModel` and compaction fail closed with the typed retryable
`UnavailableError` until the envelope matches the frozen revision. Existing bounded
model Step retries apply; exhaustion reports `model_credential_not_available`. An
existing child without the new frozen revision has revision zero and fails closed
through this same bounded typed failure; it never adopts a platform or Project key.

No credential Channel, confirmation RPC, or waiting Step is added. The original
model Step's retry and explicit failure outcome are sufficient for the short
Start+copy interval, avoiding a second admission/notification lifecycle. Replacement/deletion also exact-clears finished, timed-out and abnormally ended
children retained in holder metadata or the bounded task records. An engine-ended
child cannot be assumed to have run its terminal hard-delete Step. On
replacement or deletion the existing Stop RPC accepts a non-secret reason flag,
cancels the model/tool Steps and records the closed typed `ChangedReason` in
`Outcome.CredentialChangeReason`; summary rendering uses that value rather than
branching on arbitrary failure text.
The Service exact-deletes the copy before reporting the mutation successful.
Terminal `RecordSubagentOutcome` explicitly deletes the copy as well; Flow
completion is not automatic hard deletion. Abnormal termination retains the
usual operator/retention limitation. The [Session credential contract](session.md#project-model-credentials)
owns mutation recovery, secret boundaries, authorization and resource bounds.


Credential holders are scoped to the frozen provider. Session admission records
the child model before the copy movement so changing the Session selection
cannot copy a different provider's key. Replacement/deletion stops and clears
only children of that provider, while Project deletion clears every provider.
Old retained records lacking a model field belong to the former Anthropic-only
credential boundary; source and child physical secret keys remain unchanged.
