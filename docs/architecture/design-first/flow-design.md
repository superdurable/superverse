# Flow design: the artifact, its storage and API

The Flow design is what the agent proposes before code: the Flows it adds or changes, as a
graph a person can read, and a short summary. It is part of the requirements card
([README](README.md#the-pieces)).

## When the agent writes one

The prompt and the dex-app-builder skill state the rule. A card for a new application, or a
large change that adds, removes or changes a Flow, carries a design. A change counts when it
alters a Flow's Steps, what a Step waits for, its transitions, Channels, Actions, or which
other Flows it starts. A card whose change is only UI, an endpoint or a connection carries
no design. A small change has no card, as today.

Before the card, the agent reads the current Flow source, so that the design of a changed
Flow is complete. This is the same discovery it already does. No Sandbox call is needed to
show the card.

When it finishes the build of a recorded design, the agent compares the Flows it built with
the design and names every difference in its reply, or says that it built the design as
recorded ([differences](studio.md#differences-between-the-build-and-the-design)). Studio
draws no comparison.

## The artifact

`confirm_requirements` gains an optional `design` object (strict schema in
`internal/dexai/agents/session/toolinput/tool_inputs.json`):

| Field | Type | Rules |
|---|---|---|
| `summary` | string | The design in business language, at most 600 characters: which processes run, who acts where, what waits for whom |
| `flows[]` | at most 6 | Each Flow the card adds or changes, listed in full |
| `flows[].flow_type` | string | The Go type name to implement: an exported identifier ending in `Flow`, unique in the design |
| `flows[].purpose` | string | One sentence, at most 200 characters |
| `flows[].start_step` | string | One of its Steps |
| `flows[].steps[]` | 1 to 30 | `step_type` (exported identifier, unique in the Flow), `group` (the stage label, at most 40 characters), `explanation` (at most 200 characters), `waits_for`, `next[]` and `features[]` |
| `steps[].waits_for` | object | `kind`: `none`, `action` (a person answers through an Action of this Flow, whose `rpc` is `name`), `channel` (another sender publishes to a Channel of this Flow, named in `name`) or `timer` (`name` says the business time, for example "24 hours after the request") |
| `steps[].next[]` | at most 8 | `to`: a Step of this Flow, or `complete` or `fail`; `when`: the condition in words (at most 80 characters, empty for the only way out); `is_failure_route`: true only when the Step's failure leads there, for breakage such as retries that ran out or a lost response, never for a business rejection or a failed check ([main story and failure handling](#main-story-and-failure-handling)) |
| `steps[].features[]` | IDs | The card's features this Step serves, for example `R2` |
| `flows[].channels[]` | at most 16 | `name` and `payload` (words) |
| `flows[].actions[]` | at most 8 | `label` (what Studio shows, for example "Approve summary"), `rpc` (exported identifier), `publishes` (a Channel of this Flow, or empty) and `permission` (the Action permission, for example `summary.approve`) |
| `flows[].attributes[]` | at most 24 | `name`, `value` (words), `is_map` (an AttributeMap), `key` (the instance key format, required when `is_map`) |
| `flows[].starts[]` | Flow types | Flows this Flow starts: another Flow of the design, or one of the current source |
| `removed_flows[]` | Flow types | Flows of the current source that the change deletes |

Example: the `summary-approval` scenario's design, in YAML for brevity. Its Steps, groups,
explanations and transitions are those of the template's `TextSummaryFlow` as the analyzer
renders it (`web/studio/lib/generated/template-design/app-template.json`), plus the
approval. Its canonical JSON is 2.1 KB.

```yaml
design:
  summary: "The summary run waits after generation until a person approves it in Studio; then it is saved."
  flows:
    - flow_type: TextSummaryFlow
      purpose: "Summarizes a text with a model and saves it once a person approves."
      start_step: PrepareSummary
      steps:
        - {step_type: PrepareSummary, group: Text intake, explanation: "Validate and keep the text before any paid model request.",
           waits_for: {kind: none}, next: [{to: GenerateSummary, when: "the text is valid"}, {to: complete, when: "the text is empty, too long or not UTF-8"}]}
        - {step_type: GenerateSummary, group: Summary, explanation: "Ask the configured model for a short factual summary.", waits_for: {kind: none},
           next: [{to: AwaitSummaryApproval, when: "generated"}, {to: SaveSummaryResult, when: "truncated"},
                  {to: RecordSummaryFailure, when: "blocked, rejected, invalid or a defect"},
                  {to: RecordSummaryUncertainty, when: "the model response was lost", is_failure_route: true}]}
        - {step_type: AwaitSummaryApproval, group: Result, explanation: "Wait for a person to approve the generated summary before it is saved.",
           waits_for: {kind: action, name: ApproveSummary}, next: [{to: SaveSummaryResult}], features: [R1, R2]}
        - {step_type: SaveSummaryResult, group: Result, explanation: "Save the summary, the model and the token usage.", waits_for: {kind: none}, next: [{to: complete}]}
        - {step_type: RecordSummaryFailure, group: Result, explanation: "Record why the model returned no usable summary.", waits_for: {kind: none}, next: [{to: complete}]}
        - {step_type: RecordSummaryUncertainty, group: Result, explanation: "Record a lost model response without sending the paid request again.",
           waits_for: {kind: none}, next: [{to: complete}]}
      channels: [{name: SummaryApprovals, payload: "the approval"}]
      actions: [{label: "Approve summary", rpc: ApproveSummary, publishes: SummaryApprovals, permission: summary.approve}]
      attributes: [{name: SummaryStatus, value: "pending, awaiting-approval, approved, completed or failed"}, {name: SummaryText, value: "the saved summary"}]
```

Features are attached to Steps only. R2, the Approve summary Action, is served by the Step
that the Action releases, so `AwaitSummaryApproval` lists both R1 and R2.

### Main story and failure handling

A design is read with the split that Dex's analyzer and the Flow canvas apply to implemented
Flows (`dex-skills/dex-app-builder/references/fdg-authoring.md`, and "Failure handling" in
`web/studio/components/flow/canvas/layout/README.md`):

- **Failure handling** is every Step that only `is_failure_route` transitions reach, directly
  or through other such Steps. In the example, `RecordSummaryUncertainty`.
- **The main story** is every other Step, however rarely it runs, including business
  rejections, failed checks and people's decisions. In the example, `RecordSummaryFailure`
  is main story: a blocked or rejected response is an ordinary `next`.
- **Groups** are judged on the main story. A group of two or more main-story Steps is one
  run, where ordinary transitions inside it join all of its Steps, or one choice, where one
  Step outside it leads into every part. A failure-handling Step's group is not checked.

The outline, the Flow tab's design view and the comparator all use this split, so a design
hides and folds the same Steps as the Flow that implements it.

### Validation

`confirm_requirements` validates the design in the `RouteTool` commit, with the card. A
refusal is the existing `invalid_user_input` tool failure, whose message names the field and
the rule, and no version is recorded:

- identifiers are exported Go identifiers; Flow types end in `Flow`; names are unique where
  the table says so;
- every `next.to` is a Step of the same Flow, `complete` or `fail`;
- every Step is reachable from `start_step` through `next`, failure routes included;
- a `waits_for` of kind `action` names an Action of the Flow. A wait of kind `channel` names
  one of its Channels. An Action's `publishes` names one of its Channels;
- `features` name features of this card;
- `removed_flows` and `flows` do not share a type; `starts` names a Flow of the design or any
  type (the current source is not known to the Session, so it is not checked);
- the design is within the [limits](#limits).

Group shape is not validated. A group that is neither one run nor one choice is admitted:
Dex's analyzer owns that rule and only warns (`v2_step_group_shape`), and the Flow tab's design
view shows such a group's Steps in its band instead of folding them, as it does for an
implemented Flow.

The server does not check the design against the current source. The Session does not read
the workspace for a card, and Studio shows the comparison to the person.

### Limits

The design's canonical JSON is at most 24 KiB, with the counts in the table above. The
template's `TextSummaryFlow` needs about 2 KiB. The caps leave room for six such Flows, and
are raised only with evidence of a real design that needs more.

### Digest and outline

- **Digest**: `sha256:` plus the lowercase hex SHA-256 of the design's canonical JSON (keys
  in schema order, no insignificant whitespace). It identifies a design across the card, the
  recorded version and the read.
- **Outline**: text the server renders from the design, at most 2 KiB, truncated by whole
  Flows with "and N more Flows". One line per Flow gives its type, its main-story Steps in
  reading order with waits in brackets, and the number of its failure-handling Steps
  ([main story and failure handling](#main-story-and-failure-handling)). Reading order is
  the canvas's: breadth-first from `start_step` along ordinary transitions, in `next[]`
  order. For example
  `TextSummaryFlow: PrepareSummary, GenerateSummary, AwaitSummaryApproval [Approve summary], SaveSummaryResult, RecordSummaryFailure; +1 failure-handling Step`,
  and then `Removed: ...`. The card's text form and `segment_start` carry it. It counts
  toward the 16 KiB requirements limit.

## Storage

| State | Owner | Where |
|---|---|---|
| The current design | `SessionAgentFlow` | `FlowDesigns` instance `confirmed`; its reference in `ConfirmedRequirements.Spec.Design`, the current version of the requirements |
| The implemented Flows | The Project's source | The prepared source's definition bundle (`projects/<projectID>/preview-source/<commit>/definitions.json`), as today |
| Whether the current design is built, and by which commit | `SessionAgentFlow` | `BuiltFlowDesign`, the digest of the current design that a push built, and `BuiltFlowDesignCommit`, the commit that push verified ([build completion](#build-completion)) |
| Which designed Steps the agent's writes declared | `SessionAgentFlow` | `WrittenDesignSteps`, one entry per Step of the current design ([written Steps](#written-steps)) |

The reference is `flowdesign.Reference{Digest, Summary, Outline}`, so the card shows the design's summary without reading the body. A card without a design has none.
A card is recorded as the next version in the `RouteTool` commit that answers it
([session](../agent-harness-design/session.md#requirements-cards)), so its design goes
straight to `confirmed`. Older designs are not kept. A card with a design replaces
`confirmed`. A card without a design (a change that touches no Flow) carries the design
forward: `Spec.Design` keeps the previous reference and `confirmed` stays, so the last design
stays current, and built if it was. Only a card that sets `replaces_requirements` without a
design clears it: the reference is dropped and `confirmed` is deleted in the same commit. The
instance `pending` held the design of a card that waited for a confirmation; it is no longer
written, and the commit that records a card deletes what an open Session left there.

## Build completion

The build of a recorded design is complete when a `commit_and_push` call pushes without
`blocked`: the agent calls it only when the requested application, its tests and its
requirement trace are complete ([commit gate](../agent-harness-design/commit-gate-and-app-tests.md#the-commit_and_push-tool)).
The commit of that result writes the Attribute `BuiltFlowDesign`, the digest of the design
that `ConfirmedRequirements` then carries, and `BuiltFlowDesignCommit`, the commit the push
verified (the result's `HeadCommit`). The design is built while `BuiltFlowDesign` equals that
digest:

- a new recorded design has a new digest, so it is unbuilt until the next such push;
- a card that carries the design forward keeps its digest, so a built design stays
  built;
- a blocked push, a failed one and one while no design is recorded write nothing.

No lock is added. Only `RouteTool` writes `ConfirmedRequirements`, and the conversation routes
one call at a time: a card is recorded before or after a `commit_and_push`, never while it runs,
so the result's commit reads a version nothing else writes meanwhile. A push that started
before a revised card builds the earlier version; the next push builds the revision.
Studio stops drawing the design once it is built and either the Preview's processes are built
from the commit that built it or the Session HEAD has moved past that commit
([the Flow tab](studio.md#the-flow-tab)). The commit is what makes the second case exact: a later
push, such as a small change without a card, leaves the design built, and without the commit
Studio could not tell the push that built the design from the later one and would draw the old
design again until the later push's processes were built.

## Written Steps

Before the push builds a design, Studio shows how far the agent wrote its code, Step by Step, with no
agent or prompt change: the design already names the Go type of every Step (`step_type`, unique within
its Flow), and the Session holds the full arguments of every `write` and `edit` it records.

- **Declaration.** Source declares a Step two ways: an ordinary Step by its struct type
  (`type PrepareSummary struct`) and a connector Step only by the string of its options
  (`StepType: "GenerateSummary"`). A succeeded `write` (its `content`) or `edit` (its `new_string`) of a
  Go file that is not a test file declares the designed Steps whose type names one of the two forms
  spells. `flowdesign.WrittenSteps.Record` finds them.
- **Same name in two Flows.** A Step type designed in several Flows is marked in the Flow the file's
  name names: the Flow's snake-case name without `_flow`, the longest one the file name starts with,
  such as `text_summary_flow.go` or `text_summary_steps.go` for `TextSummaryFlow`. When the file names
  none of them, each is marked.
- **State.** The Attribute `WrittenDesignSteps` (`flowdesign.WrittenSteps{Digest, Steps}`) lists every
  Step of the current design with `Written`. The commit that records a card with a design starts it for
  that design, keeping `Written` on a Step the new design keeps in the same Flow (its code stays in the
  workspace), and a card that clears the design deletes it. The commit of each recorded `write` or `edit`
  result marks the Steps it declares, so the tool-result Steps never load `FlowDesigns`.
- **Not proof.** A mark says the agent wrote the declaration, not that the Step works, compiles or is
  wired as designed; it never comes back off: a later edit that deletes the struct leaves it marked. A
  `bash` command, a subagent's merged change or a remote executor's change that writes a file marks
  nothing. The push that builds the design is the completion ([build completion](#build-completion)).
- **Studio.** While the design is built, Studio draws a Step solid when it is written or when the
  compared current source already holds it (unchanged or changed), and dashed, with the planned
  emphasis, otherwise; the status chip on the canvas's corner counts "N of M steps written" and the
  legend names the dashed shape. Once the design is built every Step is solid ([the Flow tab](studio.md#the-flow-tab)).

## API

- **Snapshot and Watch**: `AgentRequirements` gains `AgentFlowDesignReference design = 7`
  (`digest`, `summary`, `outline`). It reaches Studio in `confirmed_requirements`, the
  current version, like the rest of the card (and in the pending input of a card an old Session
  still waits at). `AgentRequirements` also gains
  `bool design_built = 8`, which only `confirmed_requirements` sets: whether
  `BuiltFlowDesign` equals the current design's digest, and `string design_built_commit = 9`,
  set only while `design_built`: `BuiltFlowDesignCommit`. `repeated string design_written_steps = 10`,
  set only in `confirmed_requirements`, lists the [written Steps](#written-steps) of the current design,
  each `<flow_type>.<step_type>`, in design order.
- **Draft**: while the agent writes a card, the Session Watch sends its drafts as
  `SessionProgress.requirements_draft` ([streaming and Studio](../agent-harness-design/streaming-and-studio.md#requirements-drafts)),
  with the designed Flows' names and Step types as far as they arrived; Studio lists them and draws no
  design until the card is recorded.
- **`AgentService.GetFlowDesign`** (the ninth RPC of the service): request `project_id`,
  `slot` (`AGENT_FLOW_DESIGN_SLOT_CONFIRMED`, the only slot),
  `digest`, and `CallerContext caller = 100`. Response: `AgentFlowDesign`, the typed design
  (Flows, Steps, waits as an enum `AgentFlowDesignWait.Kind`, transitions with an enum
  `AgentFlowDesignTransition.End` for `complete` and `fail`, Channels, Actions, Attributes,
  starts, removed Flows).
  - Authorization: whoever may read the Session's Snapshot.
  - Errors: NOT_FOUND when the slot holds no design. ABORTED with
    `REVISION_CHANGED` (as the other revision fences in the resource API) when the slot holds another digest: a newer card
    replaced it, and Studio reads the Snapshot again.
- The browser reaches it as `/api/rpc/GetFlowDesign`. No REST transport is added
  ([the current contract](../../api/resource-grpc.md)).
- Changed: `confirm_requirements`'s tool input, and the agent-facing `AgentRequirements`
  rendering. The tool result is `{status: recorded, version, reason}`, and the turn goes on.

## Package placement

- `internal/dexai/agents/session/flowdesign` (new; the Session's tenth sub-package, which reaches
  the limit of 10): the design
  types, validation, canonical JSON with digest, and outline. `agentloop` already has its 10
  files, so `RequestedRequirements` calls into this package and does not grow beyond one
  call. `Requirements` gains `Design *flowdesign.Reference`.
- `internal/dexai/agents/session`: the `FlowDesigns` declaration and the `GetFlowDesign` RPC in
  `session_agent_flow.go`, below the 1600-line cap. The write of a recorded card's design
  (`recordFlowDesign`) goes beside `RouteTool`'s handling of `confirm_requirements`.
- `internal/dexai/api`: the boundary mapping, with the closed sets mapped by `boundary.Enum`.

## Per-Flow contract: SessionAgentFlow

Only the differences are listed. Everything else is unchanged and stays in
[session.md](../agent-harness-design/session.md).

### Identity and execution lifecycle

Unchanged: the Session's FlowID, start, reuse policy, archival and retention are as in
session.md. The design creates no Flow, Run or FlowID. A rerun or recovery of `RouteTool`
writes the same instance with the same digest, because the next version is derived from the
stored one inside the same commit.

### Attributes and AttributeMaps

| Primitive | Type | Key | Notes |
|---|---|---|---|
| `FlowDesigns` | AttributeMap of `flowdesign.Design` | `confirmed`, a fixed key declared as a typed constant; `pending`, the key a waiting card used, is only deleted | Owner `SessionAgentFlow`. One live instance of at most 24 KiB for the Session's lifetime (an open Session may also hold an old `pending` until its next card). Not indexed, holds no secret. Loaded only by `GetFlowDesign` (instance `confirmed`), through the RPC option `LoadAttributeMapInstances` of the pinned SDK (`sdk-go` v1.10.1), which takes the fixed key at registration; `RouteTool` writes and deletes instances without loading them. Removed with the Session's state when it ends. It is an AttributeMap with a fixed key, not a plain Attribute, because of exact-key loading: plain Attributes load with every Step, RPC and Snapshot, while only `GetFlowDesign` loads this instance, so up to 24 KiB is never read with every Session Step or Snapshot. |
| `PendingUserInput` | struct | — | Holds no new design; a card an old Session still waits at keeps the reference of its `pending` instance in `Requirements.Design` |
| `ConfirmedRequirements` | struct | — | The current version: `Spec.Design` holds the reference of the `confirmed` instance; the version number counts the recorded cards |
| `BuiltFlowDesign` | string | — | The digest of the current design that a pushed, unblocked `commit_and_push` built ([build completion](#build-completion)). Owner `SessionAgentFlow`; written only in that result's commit. At most 71 bytes for the Session's lifetime, not indexed, no secret; the Snapshot reads it. |
| `BuiltFlowDesignCommit` | string | — | The commit that the push which built the current design verified, written beside `BuiltFlowDesign` in that result's commit. Owner `SessionAgentFlow`. A commit SHA of 40 characters for the Session's lifetime, not indexed, no secret; read by the Snapshot only while the design is built |
| `WrittenDesignSteps` | `flowdesign.WrittenSteps{Digest, Steps[{FlowType, StepType, Written}]}` | — | The [written Steps](#written-steps) of the current design. Owner `SessionAgentFlow`; set by the commit that records a card with a design (deleted with the design), updated by the commit of a recorded `write` or `edit` result. One entry per designed Step, at most 6 Flows of 30 Steps (about 30 KB at worst), replaced with each design; not indexed, no secret; an ordinary Attribute, so the tool-result Steps read it without loading `FlowDesigns`. The Snapshot reads it and reports its written Steps only while its digest is the current design's. |

Locking and CAS:

- `RouteTool` writes `confirmed` (or deletes it for a replacing card without a design) and
  `ConfirmedRequirements` in one commit under the `MessageWindowLock` it already holds, and
  deletes `pending` in the same commit. It is their only writer while a turn runs; the one other
  writer, `SendMessage`'s reply to an old waiting card, runs only while `AwaitUser` waits at that
  card, under the `ConfirmedRequirements` lock. `AnswerQuestions` no longer touches the
  requirements or the designs.
- A message that answers a card an old Session still waits at records that card in the same
  `SendMessage` commit: its `pending` design moves to `confirmed` and `pending` is deleted.
- `GetFlowDesign` is an unlocked, nontransactional read. It compares the stored digest with
  the request's.

### Channels and ChannelMaps

None added. A change to the card is a chat message on `QueuedUserMessages` or
`SteeredUserMessages`, unchanged.

### Streams

`RequirementsDraft` carries the drafts of a card while the model writes it, the designed Flows'
names and Step types among them; it is owned by
[streaming and Studio](../agent-harness-design/streaming-and-studio.md#requirements-drafts). The
recorded design reaches Studio only through the Snapshot: the Session Watch carries the reference
inside the Snapshot, a lost Watch frame is recovered by the next Snapshot read, and the body is
always read through `GetFlowDesign`.

### Steps, RPCs, and recovery

| Step or RPC | Change | Primitives |
|---|---|---|
| `RouteTool` (`confirm_requirements`) | Validates the card and the design, records the next version with the design's reference and writes the design | writes `ConfirmedRequirements`, `FlowDesigns[confirmed]`; deletes `FlowDesigns[pending]` |
| `GetFlowDesign` RPC | Returns the `confirmed` instance if its digest matches | loads the fixed instance `confirmed` (at most 24 KiB); read timeout as `GetSnapshot` |
| The tool batch's result commit (`commit_and_push`) | A pushed, unblocked result writes the current design's digest and the commit it verified, beside the Session HEAD and the Preview handoff it already writes | reads `ConfirmedRequirements`; writes `BuiltFlowDesign`, `BuiltFlowDesignCommit` |
| The tool batch's result commit (`write`, `edit`) | A succeeded result marks the designed Steps it declares written (`recordDesignProgress`, beside the built design) | reads and writes `WrittenDesignSteps` |
| `RouteTool` (`confirm_requirements`) and an old card's `SendMessage` | The commit that records a design starts its written Steps; one that clears the design deletes them | writes `WrittenDesignSteps` |
| `CallModel` | Writes the drafts of a card the model writes | writes the `RequirementsDraft` Stream |
| `GetSnapshot` and the Watch | `confirmed_requirements.design_built` compares `BuiltFlowDesign` with the current digest; `design_built_commit` carries `BuiltFlowDesignCommit` while it is built; `design_written_steps` lists the written Steps of `WrittenDesignSteps` when its digest is current | reads `BuiltFlowDesign`, `BuiltFlowDesignCommit`, `WrittenDesignSteps` |
| Segment start (compaction, new segment) | Renders the outline from `ConfirmedRequirements`; no map load | reads `ConfirmedRequirements` |

The design has no external effects, so nothing has to be reconciled after an uncertain
outcome. Retries of the commits are idempotent: the same key gets the same value, and marking a
written Step again changes nothing. Open executions: the fields are optional, so a Session that
started before designs existed decodes with no design, and one whose design was recorded before
`WrittenDesignSteps` existed reports no written Steps until its next design. A Session that waits at a card from before cards stopped waiting is answered by
a message, and its next card replaces `confirmed` and deletes `pending`
([session](../agent-harness-design/session.md#recovery-external-effects-and-compatibility)). The
regenerated `docs/flows/sv2-session-agent-flow.json` shows the map and RPC.

### Tests

See the [README](README.md#tests): identity and recovery (reference across a compaction),
admission (refusals), map keys and exact loads (`GetFlowDesign` of `confirmed`, stale digest),
and the no-design compatibility case.
