# Studio: the design on the card and the Flow tab

Studio draws the design on the Flow tab, with the canvas that draws implemented Flows, until
the build of the recorded design is complete. Then the implemented Flows replace it. The canvas
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
  the commit that built it, then the implemented Flows. A new version with a design brings them
  back for that version until it is built.

There is no Confirm and no Change something: changes are asked for in the chat.

**While the agent writes the card**, before it is recorded, the Flow tab shows its newest draft
(`DraftBrief`, [requirements drafts](../agent-harness-design/streaming-and-studio.md#requirements-drafts)):
**Drafting** on the corner over the draft's title, the designed Step names as a plain row of
dashed boxes per process, as they arrive, at most once a second, the summary line as it fills in,
and the features behind More details. A draft is never drawn; the drawn design replaces the boxes
when the card is recorded. A Project's first conversation switches to the Flow tab at its first
draft, and a revision's draft takes the place of the current version's Brief until the revision
is recorded.

## The Flow tab

The Flow tab draws a design in place of the implemented Flows it changes, from the card until
the build of the recorded design is complete. There is no switch between the design and the
implementation: while a design is drawn, it is what the Flow tab shows. Only those who build
see it, and only on Preview; Live always shows what Live runs.

- **When.** From the card with a design until the design is
  built ([build completion](flow-design.md#build-completion)) and the Preview's processes are
  built from the commit that built it.
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
  commit that built it, the implemented Flows replace the design. The
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
