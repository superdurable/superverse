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

When it finishes the build of a confirmed design, the agent compares the Flows it built with
the design and names every difference in its reply, or says that it built the design as
confirmed ([differences](studio.md#differences-between-the-build-and-the-design)). Studio
draws no comparison.

## The artifact

`confirm_requirements` gains an optional `design` object (strict schema in
`internal/agents/session/toolinput/tool_inputs.json`):

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
the rule, and no pending input is written:

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
  confirmation and the read.
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
| The design of the waiting card | `SessionAgentFlow` | `FlowDesigns` instance `pending`; its reference in `PendingUserInput.Requirements.Design` |
| The confirmed design | `SessionAgentFlow` | `FlowDesigns` instance `confirmed`; its reference in `ConfirmedRequirements.Spec.Design` |
| The implemented Flows | The Project's source | The prepared source's definition bundle (`projects/<projectID>/preview-source/<commit>/definitions.json`), as today |
| Whether the confirmed design is built, and by which commit | `SessionAgentFlow` | `BuiltFlowDesign`, the digest of the confirmed design that a push built, and `BuiltFlowDesignCommit`, the commit that push verified ([build completion](#build-completion)) |

The reference is `flowdesign.Reference{Digest, Summary, Outline}`, so the card shows the design's summary without reading the body. A card without a design has none.
Older confirmed designs are not kept. A confirmation of a card with a design replaces
`confirmed`. A confirmation of a card without a design (a change that touches no Flow)
carries the confirmed design forward: `Spec.Design` keeps the previous reference and
`confirmed` stays, so the last design stays confirmed, and built if it was. Only a card that sets
`replaces_requirements` without a design clears it: the reference is dropped and
`confirmed` is deleted in the same commit.

## Build completion

The build of a confirmed design is complete when a `commit_and_push` call pushes without
`blocked`: the agent calls it only when the requested application, its tests and its
requirement trace are complete ([commit gate](../agent-harness-design/commit-gate-and-app-tests.md#the-commit_and_push-tool)).
The commit of that result writes the Attribute `BuiltFlowDesign`, the digest of the design
that `ConfirmedRequirements` then carries, and `BuiltFlowDesignCommit`, the commit the push
verified (the result's `HeadCommit`). The design is built while `BuiltFlowDesign` equals that
digest:

- a new confirmed design has a new digest, so it is unbuilt until the next such push;
- a confirmation that carries the design forward keeps its digest, so a built design stays
  built;
- a blocked push, a failed one and one while no design is confirmed write nothing.

No lock is added. A card ends the turn, so no confirmation runs while the agent's tools do,
and the result's commit reads `ConfirmedRequirements` that nothing else writes meanwhile.
Studio stops drawing the design once it is built and either the Preview's processes are built
from the commit that built it or the Session HEAD has moved past that commit
([the Flow tab](studio.md#the-flow-tab)). The commit is what makes the second case exact: a later
push, such as a small change without a card, leaves the design built, and without the commit
Studio could not tell the push that built the design from the later one and would draw the old
design again until the later push's processes were built.

## API

- **Snapshot and Watch**: `AgentRequirements` gains `AgentFlowDesignReference design = 7`
  (`digest`, `summary`, `outline`). It reaches Studio in the pending input and in
  `confirmed_requirements`, like the rest of the card. `AgentRequirements` also gains
  `bool design_built = 8`, which only `confirmed_requirements` sets: whether
  `BuiltFlowDesign` equals the confirmed design's digest, and `string design_built_commit = 9`,
  set only while `design_built`: `BuiltFlowDesignCommit`.
- **`AgentService.GetFlowDesign`** (the ninth RPC of the service): request `project_id`,
  `slot` (`AGENT_FLOW_DESIGN_SLOT_PENDING` or `AGENT_FLOW_DESIGN_SLOT_CONFIRMED`),
  `digest`, and `CallerContext caller = 100`. Response: `AgentFlowDesign`, the typed design
  (Flows, Steps, waits as an enum `AgentFlowDesignWait.Kind`, transitions with an enum
  `AgentFlowDesignTransition.End` for `complete` and `fail`, Channels, Actions, Attributes,
  starts, removed Flows).
  - Authorization: whoever may read the Session's Snapshot.
  - Errors: NOT_FOUND when the slot holds no design. ABORTED with
    `REVISION_CHANGED` (as the other revision fences in the resource API) when the slot holds another digest: a newer card or confirmation
    replaced it, and Studio reads the Snapshot again.
- The browser reaches it as `/api/rpc/GetFlowDesign`. No REST transport is added
  ([the current contract](../../api/resource-grpc.md)).
- Changed: `confirm_requirements`'s tool input, and the agent-facing `AgentRequirements`
  rendering. The tool result stays `{status: waiting_for_user, version}`.

## Package placement

- `internal/agents/session/flowdesign` (new; the Session's tenth sub-package, which reaches
  the limit of 10): the design
  types, validation, canonical JSON with digest, and outline. `agentloop` already has its 10
  files, so `RequestedRequirements` calls into this package and does not grow beyond one
  call. `Requirements` gains `Design *flowdesign.Reference`.
- `internal/agents/session`: the `FlowDesigns` declaration and the `GetFlowDesign` RPC in
  `session_agent_flow.go` (1479 lines today, below the 1600-line cap). The copy on
  confirmation goes beside `AnswerQuestions`' existing effect.
- `internal/api`: the boundary mapping, with the closed sets mapped by `boundary.Enum`.

## Per-Flow contract: SessionAgentFlow

Only the differences are listed. Everything else is unchanged and stays in
[session.md](../agent-harness-design/session.md).

### Identity and execution lifecycle

Unchanged: the Session's FlowID, start, reuse policy, archival and retention are as in
session.md. The design creates no Flow, Run or FlowID. A rerun or recovery of `RouteTool` or
`AnswerQuestions` writes the same instance with the same digest.

### Attributes and AttributeMaps

| Primitive | Type | Key | Notes |
|---|---|---|---|
| `FlowDesigns` (new) | AttributeMap of `flowdesign.Design` | `pending` or `confirmed`: two fixed keys declared as typed constants | Owner `SessionAgentFlow`. At most two instances of at most 24 KiB each, for the Session's lifetime. Not indexed, holds no secret. Loaded only by `AnswerQuestions` (instance `pending`) and `GetFlowDesign` (both instances), through the RPC option `LoadAttributeMapInstances` of the pinned SDK (`sdk-go` v1.5.0), which takes the fixed keys at registration. Removed with the Session's state when it ends. It is an AttributeMap with two fixed keys, not two plain Attributes, because of exact-key loading: plain Attributes load with every Step, RPC and Snapshot, while only `AnswerQuestions` and `GetFlowDesign` load these instances, so up to 2 × 24 KiB is never read with every Session Step or Snapshot. |
| `PendingUserInput` (changed) | struct | — | `Requirements.Design` holds the reference of the `pending` instance |
| `ConfirmedRequirements` (changed) | struct | — | `Spec.Design` holds the reference of the `confirmed` instance; the version number counts both |
| `BuiltFlowDesign` (new) | string | — | The digest of the confirmed design that a pushed, unblocked `commit_and_push` built ([build completion](#build-completion)). Owner `SessionAgentFlow`; written only in that result's commit. At most 71 bytes for the Session's lifetime, not indexed, no secret; the Snapshot reads it. |
| `BuiltFlowDesignCommit` (new) | string | — | The commit that the push which built the confirmed design verified, written beside `BuiltFlowDesign` in that result's commit. Owner `SessionAgentFlow`. A commit SHA of 40 characters for the Session's lifetime, not indexed, no secret; read by the Snapshot only while the design is built |

Locking and CAS:

- `RouteTool` writes `pending` under the `MessageWindowLock` it already holds. A card is
  refused while another input is pending (`user_input_already_pending`), so no confirmation
  can run against a `pending` that is being replaced.
- `AnswerQuestions` keeps its locks (`PendingUserInput`, `ConfirmedRequirements`). On a
  confirmation it loads `pending`, compares its digest with the pending reference, and writes
  `confirmed` in the commit that writes `ConfirmedRequirements`. A digest mismatch is a
  defect (only `RouteTool` writes `pending`, and only while no input is pending), so it
  panics; it is not a business rejection.
- A request for changes, or steering that deletes the pending input, leaves `pending` as it
  is. It is unreachable without a pending reference, and the next card overwrites it. The
  bound stays two instances with no cleanup step.
- `GetFlowDesign` is an unlocked, nontransactional read. It compares the stored digest with
  the request's.

### Channels and ChannelMaps

None added. The answer still travels on `AnsweredUserInputs`, unchanged.

### Streams

None added. The Session Watch carries the reference inside the Snapshot. A lost Watch frame
is recovered by the next Snapshot read, and the body is always read through
`GetFlowDesign`.

### Steps, RPCs, and recovery

| Step or RPC | Change | Primitives |
|---|---|---|
| `RouteTool` (`confirm_requirements`) | Validates the design, writes `FlowDesigns[pending]` and puts the reference in `PendingUserInput` | writes `FlowDesigns[pending]`, `PendingUserInput` |
| `AnswerQuestions` RPC | On confirmation, copies `pending` to `confirmed` after the digest check | loads `FlowDesigns[pending]`; writes `FlowDesigns[confirmed]`, `ConfirmedRequirements` |
| `GetFlowDesign` RPC (new) | Returns the requested instance if its digest matches | loads both fixed instances (at most 48 KiB); read timeout as `GetSnapshot` |
| The tool batch's result commit (`commit_and_push`) | A pushed, unblocked result writes the confirmed design's digest and the commit it verified, beside the Session HEAD and the Preview handoff it already writes | reads `ConfirmedRequirements`; writes `BuiltFlowDesign`, `BuiltFlowDesignCommit` |
| `GetSnapshot` and the Watch | `confirmed_requirements.design_built` compares `BuiltFlowDesign` with the confirmed digest; `design_built_commit` carries `BuiltFlowDesignCommit` while it is built | reads `BuiltFlowDesign`, `BuiltFlowDesignCommit` |
| Segment start (compaction, new segment) | Renders the outline from `ConfirmedRequirements`; no map load | reads `ConfirmedRequirements` |

The design has no external effects, so nothing has to be reconciled after an uncertain
outcome. Retries of the commits are idempotent: the same key gets the same value. Open
executions: the new fields are optional and the AttributeMap is additive. A Session that
started before the change decodes with no design and behaves as today. The regenerated
`docs/flows/sv2-session-agent-flow.json` shows the new map and RPC.

### Tests

See the [README](README.md#tests): identity and recovery (reference across a compaction),
admission (refusals), map keys and exact loads (`GetFlowDesign` per slot, stale digest),
and the no-design compatibility case.
