# Studio: the design on the card and the Flow tab

Studio shows the design on the requirements card and draws it on the Flow tab, with the canvas
that draws implemented Flows, until the build of the confirmed design is complete. Then the
implemented Flows replace it. The card keeps its two actions
([README](README.md#the-pieces)).

## Where the code lives

- The shared Agent UI (`components/agent`) may import neither `components/flow` nor the
  transport (`web/studio/eslint.config.mjs`). So the card shows the design as text: the
  summary, the outline and the change lines that its host computes, with the Open design
  link its host passes (`DesignChanges`), as it gets its other callbacks. It gains no graph.
- `components/flow/design` (the ninth directory of `components/flow`) holds:
  - the design expansion (`design-graph.ts`);
  - the comparator and the card's change lines (`design-compare.ts`);
  - the design view (`DesignFlowView.tsx`) and its Step panel (`DesignStepPanel.tsx`).

  `components/flow/scope` exports the view lazily (`LazyDesignFlowView`), as it exports the
  canvas, and the change lines and the design's type through `flow-design.ts`.
  `components/flow/canvas/model` already holds 10 files, so nothing is added there.
- `FlowCanvas` finds its graph in the catalog and needs the Flow API for run overlays. Its body
  below the decoded `FlowModel` is `FlowGraphCanvas`, which takes the model, its groups, an
  optional run overlay, optional per-Step marks and an optional Step panel. `FlowCanvas` keeps
  the catalog lookup and runs, and draws through `FlowGraphCanvas`. `DesignFlowView` draws
  through it without any catalog or Flow API. A mark is a field of the Scene's box, so the
  layout and `Stage` are unchanged.
- `lib/session/design` reads the design: `use-flow-design.ts` decides from the Snapshot which
  design the Flow tab draws and reads its body through `GetFlowDesign` (via
  `/api/rpc/GetFlowDesign`), cached and shared by digest, so a Watch frame with the same
  reference reads nothing; `use-design-shown.ts` remembers the card version the Flow tab
  showed. `lib/session` itself is at its 10-file limit. The phase and its label are declared
  once, in `lib/shared/design-phase.ts`.
- Flow internals may not import `lib/session` or `lib/project`, so the hosts pass the design
  and the current definitions to `components/flow/design` as props: `components/build`
  (`DesignSurface.tsx`, beside `FlowSurface`) for the Flow tab, `components/session`
  (`use-card-design.ts`) for the card, and `components/project/ProjectBuilder.tsx` for the
  Flow tab's marker and the picker.
- The current definitions come from `ProcessDefinitionsProvider`
  (`web/studio/lib/project/ProcessDefinitionsProvider.tsx`), which `ProjectBuilder` mounts
  around the chat column and the surfaces. It reads the prepared source's full definitions and
  now keeps their graphs by Flow type as `currentSource`, with the commit they are of; for a
  setup commit without a prepared source, the template's initial design
  (`initialTemplateSource`). The dock gets the same comparison without mounting a Flow scope.
  A design is compared only when that commit is the Session HEAD.

## Expansion into an FDG-shaped graph

`design-graph.ts` turns one designed Flow into the subset of FDG 2.0 that the canvas decoder
reads (`components/flow/canvas/model/decode-flow-definition.ts`):

- a `step` node per Step, with `metadata.explanation` and `start` on the start Step;
- groups from the Step groups;
- for each `next`, a `decision` node under its Step, with `condition` set to `when`, and a
  `transition` edge to the target Step, or a `failure_transition` edge when
  `is_failure_route` is set;
- `complete` and `fail` as `gracefulComplete` and `forceFail` decisions;
- a `wait` node for a wait on a Channel, Action or timer, with the Channel resource and,
  for an Action, an `rpc` node with its `resource_publish` edge;
- `attribute` nodes for the Attributes;
- a `v2` block: `v2.actions` with one entry per designed Action, and `v2.start` naming the
  start Step. The decoder joins an `rpc` node with `v2.actions` to know that it is an Action
  (the header of `decode-flow-definition.ts`). Without it, a designed Action would decode as
  a plain RPC and differ from the implementation.

A failure route is a `failure_transition` edge from the Step itself, as the analyzer emits an
Execute failure route; a failure route without a Step is the default and draws nothing. Groups
take their id from their label. The graph carries `source.language: "design"` and
`valid: false`. It never reaches `buildDefinitionDexCatalog`, `DesignCatalogProvider`, the
project map or any Runs, Start or Action control. The decoder's `dropped` list comes back empty
for every expanded design; the DFVIEW receipt records that for the `summary-approval` design,
with its comparison against the template's `TextSummaryFlow`.

The decoded proposal goes through the canvas's own split (`failureHandling` and `mainStory`
in `components/flow/canvas/model/flow-model.ts`). Its failure handling is a ⚠ count on the
failing Step's card and a dashed detour when that Step is selected, and a group folds into a
stage only when it is one run or one choice on the main story. A proposal and its
implementation therefore draw the same picture.

## The comparator

`design-compare.ts` takes two decoded `FlowModel` sets and answers, per Flow type:

- whether the Flow is new, removed, unchanged or changed;
- for a changed Flow:
  - Steps added and removed;
  - Steps whose wait kind or wait name differs;
  - Step-to-Step transitions added and removed (after the decoder's remap of decisions to
    their Steps; guards are ignored);
  - transitions whose kind differs: ordinary on one side and a failure route on the other.
    A changed kind moves a Step between the main story and failure handling, so it is a
    difference even when the same Steps are joined;
  - Channels added and removed;
  - Actions added and removed, matched by RPC name.

A wait is normalized on both sides: a wait on a Channel that an Action publishes to is
"action <rpc>", any other Channel wait is "channel <name>", and a timer is "timer". A
connector factory branch without a guard is still a Step-to-Step transition.

Not compared: Attributes , guards, timeout handlers,
SubFlow nodes, Summary and Display RPCs, and Start input. The design cannot express them, or
they are implementation detail. The Steps a connector
factory generates (for example `GenerateSummary`, from `llm.NewGenerateTextStep`) are
compared by their Step type, like any other Step.

## The card

The card keeps its four parts and its two actions
([streaming and Studio](../agent-harness-design/streaming-and-studio.md#requirements-card)),
and adds a fifth, **Design**, when the card carries a design:

- the design summary and the outline, one line per Flow with its main story;
- what the design changes, when the current source is prepared at the Session's HEAD or is
  the template's initial design (`web/studio/lib/project/initial-template-design.ts`), one
  line per Flow:
  - "TextSummaryFlow · changed: adds AwaitSummaryApproval, Approve summary; changes
    GenerateSummary" (Steps and Actions added, Steps changed, Steps and Actions removed);
  - "ReleaseNewsletterFlow · new: 4 Steps, Approve newsletter";
  - "TextSummaryFlow · unchanged";
  - and "Removes OldFlow" for the Flows the design removes;
- otherwise one line, "Not compared: build the processes of the current version to see what
  changes." (Build processes is the existing control);
- **Open design**, a link to the Flow tab at the design's first Flow.

Confirm and Request changes stay on the card only, so there is one place to decide.
Requesting changes works as today: the person's text is the answer, the agent revises, and
the revised card carries the revised design. **Confirmed requirements** show the confirmed
design's summary and outline as text.

## The Flow tab

The Flow tab draws a design in place of the implemented Flows it changes, from the card until
the build of the confirmed design is complete. There is no switch between the design and the
implementation: while a design is drawn, it is what the Flow tab shows. Only those who build
see it, and only on Preview; Live always shows what Live runs.

- **When.** While a card with a design waits, and after its confirmation until the design is
  built ([build completion](flow-design.md#build-completion)) and the Preview's processes are
  built from the commit that built it.
- **What.** Each Flow the design lists is drawn from the design under a label, in a strip
  above the canvas that never covers it: "Design · waiting for your confirmation" while the
  card waits, "Design · building" after the confirmation, and "Design · built, building
  processes" once the design is built and until those processes are built. The strip also
  gives the Flow's purpose, the Steps and Actions the design removes, which have no place on
  the designed graph, and "Not compared" when there is nothing to compare with. New Flows of
  the design join the Flow picker, and every designed Flow is tagged **design** there. On All
  processes a bar names the label and links each designed Flow. Flows the design does not
  list are drawn from the source as today.
- **Marks.** The designed Steps are marked **new** or **changed** against the current
  source, when it is the Session HEAD's. A closed stage carries **new** when all its marked
  Steps are new and **changed** when any is marked.
- **Step panel.** The explanation, the wait in words, the ways out with their conditions
  (failure routes named as failure handling), and the features the Step serves with their
  text from the card.
- **Controls.** A designed Flow has no Runs, Start or Actions; the design view offers none.
  The Build processes control stays above it.
- **Arrival.** When a card with a design arrives while the Flow tab is open, the tab shows the
  design at once, at its first Flow. This happens once per card version, recorded in the
  browser, so a reload does not move the person again. On any other tab the Flow tab's entry
  carries a **Design** marker until the Flow tab shows that version, which it then does by
  itself. **Open design** goes to the Flow tab.
- **Replacement.** Once the design is built and the Preview's processes are built from the
  commit that built it, the implemented Flows replace the design and the label goes. The
  Preview builds them for each pushed commit while its automatic process build is on;
  otherwise the person's Build processes does. A later push moves the Session HEAD past that
  commit, and the design is no longer drawn: it was built.
- **Revision.** A revised card's design replaces the drawn one. A stale read
  (`REVISION_CHANGED`) re-reads the Snapshot first.

## Differences between the build and the design

Studio draws no comparison between the implementation and the confirmed design: no drift
line, no marks and no note on the project map. The canvas shows what runs, and the agent
explains the rest in the chat.

When the agent finishes a build that follows a confirmed design, it compares the Flows it
built with the design and names every difference in its reply, for example:

- "NotifyApprover is in the design but I did not build it, because ...";
- "I added SendReminder, which the design did not have";
- "GenerateSummary's route to RecordSummaryFailure is an ordinary decision, not a failure
  route".

It says that it built the design as confirmed when there is no difference. The outline of the
confirmed design is in every segment (`segment_start`), and the commit gate renders every
Flow the commit holds, so the agent has both sides. A difference that changes the confirmed
requirements still gets a revised card, as today. One that changes only the design is told
and needs no card.

## Accessibility and states

- The Design part of the card is a list with headings, and the label is text. Marks (**new**,
  **changed**) are text badges above the Step card and part of its accessible name, never
  color alone and never a dashed, faded or ghosted card: on every Flow canvas a dashed card
  means failure handling.
- Open design is a link, so the browser's history works.
- Loading uses `NotReady`'s placeholder, and a failed read uses its error state with retry.
  NOT_FOUND (a slot without a design) draws no design.
- Mock mode has a fixture design for the requirements fixture (`web/studio/mock/`), selected
  only in `app/` route files. The mock never builds, so a confirmed fixture design stays
  "Design · building".
