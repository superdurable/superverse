# Studio: the design on the card and the Flow tab

Studio draws the design on the Flow tab, with the canvas that draws implemented Flows, below
the summary of the card's **Process Brief**, until the build of the recorded design is
complete. Then the implemented Flows replace it. The card does not wait for an answer
([session](../agent-harness-design/session.md#requirements-cards)): the agent builds
from it at once, the Brief says which version it builds, and a person changes it with a message,
which the Brief's **Change something…** sends and steers. The card in the chat points to the
Brief ([README](README.md#the-pieces)).

## Where the code lives

- The shared Agent UI (`components/agent`) may import neither `components/flow` nor the
  transport (`web/studio/eslint.config.mjs`). So the card learns from its host
  (`DesignChanges`) where the Brief is, or that its design is still loading, as it gets its
  other callbacks, and then only points there. A card without a design, or whose design did
  not load, keeps its own controls and shows the design as text. It gains no graph.
- `components/flow/design` (the ninth directory of `components/flow`) holds:
  - the design expansion (`design-graph.ts`);
  - the comparator and the card's change lines (`design-compare.ts`);
  - the design view (`DesignFlowView.tsx`) and its Step panel (`DesignStepPanel.tsx`);
  - the Process Brief (`brief/`).

  `components/flow/scope` exports the view lazily (`LazyDesignFlowView`), as it exports the
  canvas, and the change lines and the Brief through `flow-design.ts`.
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

A card without a design keeps its four parts and has no actions
([streaming and Studio](../agent-harness-design/streaming-and-studio.md#requirements-card)).
A card with a design reads on its Process Brief: in the chat it shows only its title,
version and summary and **Open the brief**, a link to the Flow tab at the
design's first Flow ("Preparing the brief…" while the design loads). The chat leaves this
pointer out while the Flow tab shows the Brief (`web/studio/lib/session/design/use-brief-on-screen.ts`).
When the design does not load, the card adds a fifth part, **Design**: the design summary and,
behind **More details**, the outline.

The chat's composer stays open: a message changes the card, queued while a turn runs, as the
Brief's **Change something…** does (which also steers it). A card a Session still waits at
from before cards stopped waiting shows read only, with "Reply in the chat to build or change
it"; the reply is an ordinary message.

## The Process Brief

The Brief (`components/flow/design/brief`) reads the card and its design together, in the
words a business person would use. It is one column: a header, a summary grid of short parts,
**More details**, the drawn design below the grid and the actions at its foot.

- **Header**: the card's title and one badge, **Building from this brief · version N**, the
  version the Session recorded. A card whose agent stopped short of building carries no badge;
  the Flow tab says so above it.
- **Summary grid**, three columns when the Flow area is wide, then two, then one as it
  narrows (container queries, never the viewport). A part shows only when it has content:
  - **Starts when**: per process, the wait of its start Step, or that Step with its
    explanation; a process another one starts says so;
  - **People**: each Action's label and where a person chooses it;
  - **Rules and exceptions**: one short line per ordinary way out with a condition,
    "<condition> → <Step>" in the canvas's titles ("Done" or "Stops as failed" for an end),
    and "+N failure-handling steps" for the Steps the canvas folds away;
  - **Apps**: each connection as a chip with the name people know; whether it is new or
    existing, its need, what the person sets up and its operations behind the part's own
    **More details**.

  "I assumed" and "Needs you · N" take the cells after Apps once the card carries them.
- **More details** (`brief.more`): **What it does** (the card's summary and the design's),
  **You need to prepare**, **Done when** (acceptance), **Features** (R1, R2, ...), what the
  design changes against the current version when the current source is prepared at the
  Session's HEAD or is the template's initial design
  (`web/studio/lib/project/initial-template-design.ts`), one line per Flow in the names the
  canvas shows:
  - "Text summary · changed: adds Await summary approval, Approve summary; changes Generate
    summary" (Steps and Actions added, Steps changed, Steps and Actions removed);
  - "Release newsletter · new: 5 steps, Approve newsletter";
  - "Text summary · unchanged";
  - and "Removes Old" for the Flows the design removes;

  and the outline in Go names.
- **The drawing**: the design of the Flow the tab shows, full width below the grid, drawn left
  to right with its stages closed; the reader may change the direction or open stages. A new
  drawing traces in from its start to its finish. The summary above it keeps to the top part
  and scrolls on its own, so the drawing always shows. There is no Steps list: the drawing is
  the steps. A card without a design shows the same grid with no drawing.
- **Actions**: **Change something…** opens a small box in place with **Send changes**, which
  sends the text as a chat message and steers it, so the agent takes it at its next safe
  boundary, revises the card as the next version with the revised design, and builds the
  difference. There is no Confirm. On the mock model the foot names the mock composer instead,
  which sends every input.

**The current requirements** in the chat show the design's summary and its outline behind
**More details**.

**While the agent writes the card**, before it is recorded, the Flow tab shows its newest draft
(`DraftBrief`, [requirements drafts](../agent-harness-design/streaming-and-studio.md#requirements-drafts))
as the Brief with the badge **Brief · drafting**: the title, the summaries, the features, the apps
and a plain list of each designed process's Step names, as they arrive, at most once a second, and
the note that the drawing appears when the agent records the card. A draft is never drawn. A
Project's first conversation switches to the Flow tab at its first draft, and a revision's draft
takes the place of the current version's Brief until the revision is recorded.

## The Flow tab

The Flow tab draws a design in place of the implemented Flows it changes, from the card until
the build of the recorded design is complete. There is no switch between the design and the
implementation: while a design is drawn, it is what the Flow tab shows. Only those who build
see it, and only on Preview; Live always shows what Live runs.

- **When.** From the card with a design until the design is
  built ([build completion](flow-design.md#build-completion)) and the Preview's processes are
  built from the commit that built it.
- **What.** Each Flow the design lists is drawn from the design under a label, in a strip
  above the canvas that never covers it: "Design · building" from the card on, and "Design ·
  built, building processes" once the design is built and until those processes are built. The strip also
  gives the Flow's purpose, how many of its Steps are written ("4 of 6 steps written", below),
  the Steps and Actions the design removes, which have no place on the designed graph, and
  "Not compared" when there is nothing to compare with. New Flows of
  the design join the Flow picker, and every designed Flow is tagged **design** there. On All
  processes a bar names the label and links each designed Flow. Flows the design does not
  list are drawn from the source as today.
- **Marks.** The designed Steps are marked **new** or **changed** against the current
  source, when it is the Session HEAD's. A closed stage carries **new** when all its marked
  Steps are new and **changed** when any is marked.
- **Written Steps.** While the design is built, a Step is solid when the agent wrote its
  declaration ([written Steps](flow-design.md#written-steps)) or the compared source already holds
  it, and dashed and faded, with the planned emphasis, while its code is not written yet; its
  accessible name says "not written yet", the strip counts the written ones and the legend names the
  dashed shape. Without a comparison only the agent's writes count. Once the design is built every
  Step is solid, and the implemented Flows replace the drawing when the Preview's processes are built.
- **Step panel.** Under the Step's title: the explanation, the wait in words, the ways out
  with their conditions (failure routes named as failure handling), and the features the
  Step serves with their text from the card; its Go names behind **More details**.
- **Controls.** A designed Flow has no Runs, Start or Actions; the design view offers none.
  The Build processes control stays above it.
- **Arrival.** When a card with a design arrives while the Flow tab is open, the tab shows the
  design at once, at its first Flow. This happens once per card version, recorded in the
  browser, so a reload does not move the person again. On any other tab the Flow tab's entry
  carries a **Design** marker until the Flow tab shows that version, which it then does by
  itself. **Open the brief** goes to the Flow tab.
- **Replacement.** Once the design is built and the Preview's processes are built from the
  commit that built it, the implemented Flows replace the design and the label goes. The
  Preview builds them for each pushed commit while its automatic process build is on;
  otherwise the person's Build processes does. A later push moves the Session HEAD past that
  commit, and the design is no longer drawn: it was built.
- **Revision.** A revised card's design replaces the drawn one. A stale read
  (`REVISION_CHANGED`) re-reads the Snapshot first.

## Differences between the build and the design

Studio draws no comparison between the implementation and the recorded design: no drift
line, no marks and no note on the project map. The canvas shows what runs, and the agent
explains the rest in the chat.

When the agent finishes a build that follows a recorded design, it compares the Flows it
built with the design and names every difference in its reply, for example:

- "NotifyApprover is in the design but I did not build it, because ...";
- "I added SendReminder, which the design did not have";
- "GenerateSummary's route to RecordSummaryFailure is an ordinary decision, not a failure
  route".

It says that it built the design as recorded when there is no difference. The outline of the
current design is in every segment (`segment_start`), and the commit gate renders every
Flow the commit holds, so the agent has both sides. A difference that changes the current
requirements still gets a revised card, as today. One that changes only the design is told
and needs no card.

## Accessibility and states

- The Brief is a region whose parts are regions named by their headings, and the label is
  text. Marks (**new**,
  **changed**) are text badges above the Step card and part of its accessible name, never
  color alone and never a dashed, faded or ghosted card. A dashed card in the failure color
  means failure handling; a dashed, faded neutral card is a Step not reached yet: a run's next
  Step, or a designed Step whose code is not written yet, which its accessible name ("not written
  yet"), the strip's count and the legend say in text.
- Open the brief is a link, so the browser's history works.
- Loading uses `NotReady`'s placeholder, and a failed read uses its error state with retry.
  NOT_FOUND (a slot without a design) draws no design.
- Mock mode has a fixture design for the requirements fixture (`web/studio/mock/`), selected
  only in `app/` route files. The mock never builds, so a recorded fixture design stays
  "Design · building", with its first two Steps written and the rest dashed.
