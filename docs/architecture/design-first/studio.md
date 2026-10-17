# Studio: the design on the card and the Flow tab

Studio shows the real flow whenever one exists. It draws a card's design on the Flow tab, with the
canvas that draws implemented Flows, only before the first build of a Project that started from the
default template, whose processes are the template's text summary until then, and only until the
build of the recorded design is complete. Then the implemented Flows replace it, and no design is
drawn again: a change to a built Project, and any Project that started from an example, keeps the
real flow ([after the first build](#after-the-first-build)). The canvas
leads: while the design is drafted or built, the card's **Process Brief** is only the process
title and a status chip on the canvas's corner and, under the canvas, the card's one-line summary
with one **More details**; once the design is built they go. The card does not wait for an answer
([session](../agent-harness-design/session.md#requirements-cards)): the agent builds from it at
once, and a person changes it only with a chat message. The chat shows each recorded version once,
as one line where the agent recorded it ([README](README.md#the-pieces)).

## Where the code lives

- The shared Agent UI (`components/agent`) may import neither `components/flow` nor the
  transport (`web/studio/eslint.config.mjs`). The chat's line for a recorded version reads only
  the conversation and the Snapshot's current requirements, which its host passes as props, and
  shows the card as text. It gains no graph.
- `components/flow/design` (the ninth directory of `components/flow`) holds:
  - the design expansion (`design-graph.ts`);
  - the comparator and the card's change lines (`design-compare.ts`);
  - the design view (`DesignFlowView.tsx`), its heading on the canvas's corner
    (`DesignHeading.tsx`) and its Step panel (`DesignStepPanel.tsx`);
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
  (`DesignSurface.tsx`, beside `FlowSurface`) for the Flow tab and
  `components/project/ProjectBuilder.tsx` for the Flow tab's marker and the picker.
- The current definitions come from `ProcessDefinitionsProvider`
  (`web/studio/lib/project/ProcessDefinitionsProvider.tsx`), which `ProjectBuilder` mounts
  around the chat column and the surfaces. It reads the prepared source's full definitions and
  now keeps their graphs by Flow type as `currentSource`, with the commit they are of; for a
  setup commit without a prepared source, the template's initial design
  (`initialTemplateSource`). The dock gets the same comparison without mounting a Flow scope.
  A design is compared only when that commit is the Session HEAD.
- Which Projects draw a design at all is `ProjectStageProvider`'s `designFirst`
  (`web/studio/lib/project/stage/ProjectStageProvider.tsx`): a Project without
  `ProjectSnapshot.example` that has no processes built from the agent's own push, now, once
  before in this browser, or in one of the Preview's newest starts. `FlowSurface` and
  `ProjectBuilder` read `useFlowDesign` only while it holds, so neither the design, its bar,
  its phase nor its marker shows otherwise.
- After the first build ([below](#after-the-first-build)), `ProcessDefinitionsProvider` keeps the
  newest prepared source it read and the last built one before it (`lastBuilt`), which
  `SessionFlowScope` draws read-only while the head's push builds or failed to build, else the
  running Preview's definitions, and compares a new build with the one before it by Step
  (`buildChanges`); `FlowSurface` passes a Process's added and changed Steps to the canvas
  (`FlowCanvas`'s `changes`) for a moment, which draws them as pulsing **new** and **changed**
  marks.
- An example start draws no design ([below](#the-flow-tab)): `ProjectStageProvider`'s
  `drawsExample` holds while a Project with `ProjectSnapshot.example` has no processes built and
  its Session's head is still the setup commit (`ProjectSnapshot.setup.source_commit`),
  `useExampleDefinitions` (`lib/project/initial-template-design.ts`) imports that example's
  generated definitions when the tree matches, and `ExampleFlowScope`
  (`components/flow/scope/FlowScope.tsx`) puts them under the canvas as a read-only catalog; the
  design is not read meanwhile, so neither its marker nor its bar shows.

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

## The card in the chat

The chat shows each recorded version once, in the turn whose `confirm_requirements` call
recorded it, as one plain line, "Recorded version N: <title>", that scrolls away with the
conversation; nothing about the card stays pinned. The current version's line opens on click
to the card's full text: its summary, features, design summary with the outline behind **More
details**, connections, what to prepare and acceptance. The history carries each call's target
(the card's title) and outcome but never its arguments or version, so the newest call a result
answers without failure is the current version and each earlier one the version before it; an
older version's line has its title alone, because only the current card is in the Snapshot.

A person changes the card only with a chat message, queued while a turn runs. A card a Session
still waits at from before cards stopped waiting shows read only in the chat, with "Reply in the
chat to build or change it", which the Flow tab repeats in a small line under the canvas; the
reply is an ordinary message.

## The Process Brief

The Brief (`components/flow/design/brief`) reads the card and its design together, in the
words a business person would use, while the design is drafted or built. The canvas leads and
fills the Flow tab; everything else is small and goes once the design is built.

- **On the canvas's corner**: the card's title and one status chip: **Drafting** while the card
  streams, then **Building · N of M steps written** while the design is built
  ([written Steps](flow-design.md#written-steps)), for the Flow the tab shows. There is no strip
  above the canvas.
- **One line under the canvas**: the agent's own one-line summary of the card (`summary`), never
  a sentence Studio puts together from inferred parts.
- **More details** (`brief.more`), one panel with, in this order and each heading shown only when
  it has content:
  - **What you'll set up**: each connection that says what the person sets up, or that is new,
    under the app's name people know, then the card's own list;
  - **Who decides**: each Action's label and where a person chooses it;
  - **Starts when**: per process, the wait of its start Step, or that Step with its
    explanation; a process another one starts says so;
  - **Rules and exceptions**: one short line per ordinary way out with a condition,
    "<condition> → <Step>" in the canvas's titles ("Done" or "Stops as failed" for an end),
    and "+N failure-handling steps" for the Steps the canvas folds away;
  - **What it must do**: the features (R1, R2, ...);
  - **How it's accepted**: the acceptance;
  - a nested **More details** (`brief.code`, "code names"): the outline in Go names and what
    the design changes against the current version when the current source is prepared at the
    Session's HEAD or is the template's initial design
    (`web/studio/lib/project/initial-template-design.ts`), one line per Flow in the names the
    canvas shows, such as "Text summary · changed: adds Await summary approval, Approve summary;
    changes Generate summary", "Release newsletter · new: 5 steps, Approve newsletter", "Text
    summary · unchanged", and "Removes Old" for the Flows the design removes.
- **The drawing**: the design of the Flow the tab shows, drawn left to right with its stages
  closed, framed on its whole main story as Fit shows it, at any size. A new drawing (the recorded
  card's design, a revision, the implemented processes) frames again, and so does a change of the
  room under the summary, such as More details opening or the overview going, while the view is
  still the canvas's own; a reader who panned or zoomed keeps their place. Its **View** button beside the zoom opens a small card with the stages (Collapsed |
  Expanded) and the direction (Top-down | Left-right). A new drawing traces in from its start to
  its finish. The summary under it keeps to the lower part and scrolls on its own, so the drawing
  always shows. A card without a design shows the same corner, line and More details over an
  empty canvas that says the agent is building it, or that its last turn ended short of a build.
- **Transient.** Once the current design is built, the corner, the line and More details go and
  the Flow tab shows only the flow: the drawn design until the Preview's processes are built from
  the commit that built it, then the implemented Flows. A revised card with a design before that
  first build brings them back for that version until it is built; after it, no card brings them
  back.
- **An example start shows no design.** A Project that starts from an example, the one the person
  opened or the one the server matched to the first message (`ProjectSnapshot.example`), holds the
  example's finished source in its first commit, so while its Session's head is that commit and the
  Preview has built no processes the Flow tab draws the example's real Flow definitions for the tree
  the Project started from, with no card design, drafting overview or design phase, and the
  Builder's process list names its Flows. Those definitions are of that commit only: once the head
  moves past it before processes are built, the Flow tab waits for the push's own build as for any
  Project, and the Builder keeps the Building stage while the Preview is to build the head
  (`buildsOwnHead`: an opened example's setup commit, or the agent's push after a matched or
  default-template start); the overview of a card without a design then says it is being built.
  `make generate-app-template` renders every example's definitions with the example's own FDG
  script into `web/studio/lib/generated/template-design/` (the default's `app-template.json`, each
  other's `examples/<id>.json`, and `example-designs.ts` with each example's tree and the import of
  its file); the Builder of such a Project imports only that example's file, on first use, and
  draws nothing of it when this Studio build holds the example at another tree. Once the Preview
  has built processes, they take over as for any Project. An opened example's Session records its
  card as version 1, built at the setup commit, and its chat shows "Recorded version 1" beside the
  greeting; a matched one shows no label, title or greeting of the example, and its Builder keeps
  the Building stage while the agent's first turn writes and pushes version 1, which carries no
  design, and until the Preview has built that push. A Project created from a description that
  starts from the default example keeps the design-first flow above for its first build only. Every
  card of a Project that started from an example carries no design, and Studio would draw none if
  one did. The mock opens an example on the same path and builds nothing of its setup commit, so it
  draws the example's Flows from the same files until a turn pushes.

There is no Confirm and no Change something: changes are asked for in the chat.

**While the agent writes the card**, before it is recorded, the Flow tab shows its newest draft
(`DraftBrief`, [requirements drafts](../agent-harness-design/streaming-and-studio.md#requirements-drafts)):
**Drafting** on the corner over the draft's title, the designed Step names as a plain row of
dashed boxes per process, as they arrive, at most once a second, leaving out the Steps only failure
routes reach as the drawing does (`failure_handling_step_types`), the summary line as it fills in,
and the features behind More details. A draft is never drawn; the drawn design replaces the boxes
when the card is recorded. A Project's first conversation switches to the Flow tab at its first
draft, and a revision's draft takes the place of the current version's Brief until the revision
is recorded.

## The Flow tab

The Flow tab shows the real flow whenever one exists, the conventional practice of coding agents,
which change source without drawing a plan first. It draws a design only where no real flow of
the request exists yet: before the first build of a Project that started from the default
template, whose processes are the template's text summary for the minutes that build takes. There
it draws the design in place of the implemented Flows it changes, from the card until the build of
the recorded design is complete. There is no switch between the design and the implementation:
while a design is drawn, it is what the Flow tab shows. Only those who build see it, and only on
Preview; Live always shows what Live runs.

| Project | Flow tab on Preview |
|---|---|
| Default template, no processes built from the agent's own push yet (`designFirst`) | Design-first as below: the drafting overview, the recorded card's design with its Brief, phase, bar and marker, until the design is built and the Preview's processes are built from that push |
| Example start, head at the setup commit, nothing built (`drawsExample`) | The example's own Flow definitions ([example start](#the-process-brief)) |
| Example start, head past the setup commit, nothing built | The overview of a card without a design ("The agent is building it") until the push is built |
| Processes built, the head's push not built yet, or failed to build, while a change runs | The last built flow, read-only, with the Build processes strip saying the push builds; never a design |
| Processes built, the head's push built | The head's processes, with the Steps the build added or changed marked for a moment |

- **When.** For a `designFirst` Project, from the card with a design until the design is
  built ([build completion](flow-design.md#build-completion)) and the Preview's processes are
  built from the commit that built it. Once processes are built, never again.
- **What.** Each Flow the design lists is drawn from the design, with the Brief's corner and
  line while it is built (above). The Steps and Actions the design removes, which have no place
  on the designed graph, are in the change lines behind the nested More details. New Flows of
  the design join the Flow picker, and every designed Flow is tagged **design** there. On All
  processes a bar names the phase ("Design · building", then "Design · built, building
  processes") and links each designed Flow. Flows the design does not list are drawn from the
  source as today.
- **Marks.** The designed Steps of a process the current source holds are marked **new** or
  **changed** against it, when it is the Session HEAD's. A process the project never built
  carries no marks, since every Step would be new; a first design therefore draws none. A closed
  stage carries **new** when all its marked Steps are new and **changed** when any is marked.
- **Written Steps.** While the design is built, a Step is solid when the agent wrote its
  declaration ([written Steps](flow-design.md#written-steps)) or the compared source already holds
  it, and dashed and faded, with the planned emphasis, while its code is not written yet; its
  accessible name says "not written yet", the corner's chip counts the written ones and the legend
  names the dashed shape. Without a comparison only the agent's writes count. Once the design is built every
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
  itself.
- **Replacement.** Once the design is built and the Preview's processes are built from the
  commit that built it, the implemented Flows replace the design for good. The
  Preview builds them for each pushed commit while its automatic process build is on;
  otherwise the person's Build processes does. A later push moves the Session HEAD past that
  commit; the design is not drawn again. A card recorded without a design after the push that
  built it drops it, so a change sent between that push and its build shows the overview of a
  card without a design, never the earlier design as the new version's; a later push without a
  card records itself as the commit that built the current design.
- **Revision.** Before that first build, a revised card's design replaces the drawn one. A stale
  read (`REVISION_CHANGED`) re-reads the Snapshot first.
- **Example start.** None of the above applies to a Project that starts from an example: while its
  Session's head is the setup commit and the Preview has built no processes, the tab draws the
  example's real Flow definitions instead, as it draws implemented Flows, and once processes are
  built it follows [after the first build](#after-the-first-build).

## After the first build

Once a Project has processes built from the agent's own push, and for every Project that started
from an example, a change keeps the real flow:

- **While the change runs.** The Session works on a newer version, or its head has moved past the
  build the Preview holds. The Flow tab keeps drawing the processes of the head's prepared source
  while they are the head's, and, once the push moved past them, the last built source this
  browser read (read-only, as the prepared-source Flow API would serve it) or, after a reload, the
  definitions the running Preview serves. The Build processes strip above says what happens
  ("Building V3 processes…", or the failure with Build processes again); no design, design bar,
  phase, marker or Brief shows, whatever the card carries. Only when no built flow is known, as
  after a reload while the Preview is stopped, the tab says the processes of this version are not
  built yet. The Builder's process list keeps the last built processes meanwhile.
- **When the push is built.** The Preview's processes of the push replace the drawing, and the
  Steps the build added, or whose definition changed, carry a **new** or **changed** mark for 8
  seconds from the first time the canvas draws that build's Process in this page, pulsing softly
  unless the reader prefers reduced motion, when the marks stay still. A closed stage carries its
  Steps' marks as a design's do. Studio computes them in the browser from the two prepared
  sources' definitions it already read (`ProcessDefinitionsProvider`'s `buildChanges`): a Step's
  definition is its node and every node under it, the edges that leave them, the resources it reads
  or waits on and its group, without source positions, so moved lines mark nothing. Nothing is
  stored: a reload, a build whose predecessor this page did not read, and a first build compared
  with the template commit the Project started from (whose processes are not the person's) mark
  nothing, and the flow itself is unchanged.

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

- The Brief is a region whose parts are regions named by their headings, and its status is a
  text chip. Marks (**new**,
  **changed**) are text badges above the Step card and part of its accessible name, never
  color alone and never a dashed, faded or ghosted card. A dashed card in the failure color
  means failure handling; a dashed, faded neutral card is a Step not reached yet: a run's next
  Step, or a designed Step whose code is not written yet, which its accessible name ("not written
  yet"), the chip's count and the legend say in text.
- The View button is a button with `aria-expanded` whose card is a labelled dialog; Escape or a
  press outside closes it and returns focus to the button. A recorded version's line in the chat
  is a disclosure.
- Loading uses `NotReady`'s placeholder, and a failed read uses its error state with retry.
  NOT_FOUND (a slot without a design) draws no design.
- Mock mode has a fixture design for the requirements fixture (`web/studio/mock/`), selected
  only in `app/` route files. Its card streams as a draft for about ten seconds before it is
  recorded, its first two Steps are written and the rest dashed, and `finish build` builds it at
  the Session's head, which shows the drawing alone.
