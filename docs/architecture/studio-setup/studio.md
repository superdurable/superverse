# Studio

The Studio frame shows Preview and Live status, Setup and publishing. The [configuration contract](configuration-drafts.md) owns values, and [observability](observability.md) owns status and events. Component READMEs describe the implementation.

Every version is shown as "V*N* · subject", never as a hash
([version numbers](version-numbers.md)).

## Bar



- **Environment tabs.** `EnvironmentSwitch` (`components/frame/bar/EnvironmentSwitch.tsx`) with
  `EnvironmentStatusTab` (`components/frame/status/`) shows two tabs, Preview | Live. Each carries
  one dot for its [status](observability.md#environment-status), and a word only when something
  is abnormal, such as "failed", "needs setup", "starting", "stopped" and, on Live, "publishing" and
  "publish failed". The word is a link to the fix (the Interface, Setup at the first missing item,
  also for a Preview failure whose fix is Setup, or Tools ▸ Publishing; an operator sees the word without a link): a click on the selected tab
  opens the fix, and a click on the other tab selects it. Setup gaps and unapplied changes also
  put an amber badge on the Tools tab and on Setup. The full status stays in the tab's accessible
  name and hover hint, with accessible labels. On phone widths, the Project and environment controls wrap above the
  surface navigation, keeping its scrolling tabs separate from the controls and actions beside
  them.
- **Colours (E3).** Preview is grey and only Live is green: the selected tab of the switch is
  filled with its colour (solid grey or solid green, its words and healthy dot in the panel's
  colour), and the process picker's pill (`ProcessPicker`) and the active tab's 3px underline
  (`TabStrip`) take the selected environment's colour. No line runs along the bar. The bar's Publish is green whenever a version is ahead of Live or settings
  wait for a publish, on either tab.
- **No chevron menus.** `SegmentMenu` is deleted, and the Preview menu (`build/PreviewMenu.tsx`)
  and `LiveTimelineMenu` leave the bar. What they held moves to the
  [surfaces below](#moved-surfaces).
- **What the tabs set.** Runs, Inbox, Interface and the Flow follow the selected tab, as they
  do today through `?v=live`.
- **Operators** see only the Live tab, as today.
- **Publish button** (`components/publishing/publish/PublishButton`). "Publish" with the Publish ↑
  icon, at the top right, opens the [Publish popover](#publish-popover). It is the one primary
  action, in green, whenever a version is ahead of Live or settings wait for a publish, on either
  tab. On the Preview tab it counts the versions ahead of Live ("2 new"); on the Live tab, and on
  the Preview tab when no version is ahead, it counts the waiting settings ("1
  waiting") and opens on Live's own version, so a change in the Next publish column publishes from
  either tab. While a publish runs it reads "Publishing…" and is disabled.
- **Unsaved writes.** A Setup write that is still pending makes the bar show "Saving…".

## Tools ▸ Setup

Setup replaces Tools ▸ Connectors and Tools ▸ Configuration, as
STUDIOSETUP shipped:

- `TOOLS` in `components/frame/surface-registry.ts` is `publishing`, `setup` and `permissions`.
- `?tool=connectors` and `?tool=configuration` redirect to `?tool=setup`.
- Setup follows the environment tab.

### The table

One table, generated from the item list and the gap report of `GetProjectConfiguration`, for
Preview's prepared commit on the Preview tab:

- **Columns (L1).** Connection | Preview | Next publish. The headers carry no icon and the Next
  publish header never says "Live". The Preview column is what Preview runs once applied. The
  Next publish column shows what Live gets when this version is published and edits the
  [next-publish layer](configuration-drafts.md#live-next-publish-layer). The Live tab edits the
  same layer for the version Live runs, so the two show the same settings only when Preview and
  Live run the same version. Nothing in either reaches Live before a publish.
- **Hints, not prose.** Setup, Publishing and Permissions have no intro line under their heading,
  and Setup uses the panel's width (up to 1400px, as Publishing): the Preview and Next publish
  columns grow from 232px with it, the Connection column takes the rest, and under the table only
  the link to the TeamSpace's connections remains. Build processes shows while the latest push is
  not built and whenever automatic builds are off, where it holds the manual build, and Project
  details is reached from the Project's menu. What each column means is the hover hint of its header
  (`SETUP_HINTS` in `components/flow/connections/model/connection-words.ts`), which names it without
  an icon: Preview, "What Preview runs with. Changes save as you make them and reach Preview when
  you apply them."; Next publish, "What Live gets when you publish this version. Changes reach Live
  only when you publish. The Live tab shows the same settings when Live runs this version."; and the
  Live tab's Live column, the same from Live's side with the blue dot.
- **A row per connection name**, with its connector's logo and name and the Steps that use it,
  and a connection dropdown in each column. A row opens to its body. On the Preview tab a row
  starts folded unless something in it is missing or worth a look; a row that opened for that
  stays open once it is set up, so the control just used does not vanish. On the Live tab rows
  are open:
  - **Connection settings**, with one Edit for each side, which opens that TeamSpace connection's
    editor inline. Reauthorize is reached through it on `AUTHORIZATION_REQUIRED`.
  - Each trigger, in the connector manifest's own words ("Starts when a new message arrives",
    the raw `trigger/binding` name in a hover hint), and each Step that has settings, each setting
    with a Preview control and a Next publish control. A trigger or Step without settings has no
    line of its own.
- **Variables** at the bottom: the required ones first, the optional ones folded ("3 optional").
  A secret shows only whether it is set and is never copied to Live ("Enter its own value").
- **Footer.** "A new key or sign-in on a connection reaches every project that uses it at once",
  with a link to the TeamSpace's connections.

### Controls

- Every choice is the [one dropdown component](#one-dropdown-component); text and secrets are
  inputs.
- **"Same as Preview".** A Next publish dropdown starts with "Same as Preview", whose sub-label
  names Preview's applied value, then the choices. A Next publish text field reads "Same as
  Preview" with "Different on Live…"; once it has its own value, a ✕ goes back to the same as
  Preview. Choosing it removes Live's own value.
- **Choices from the provider** (channels, models, members, spreadsheets) wait for a working
  connection on their side: the control reads "After connecting", disabled, with the reason in
  its tooltip.
- **Reasons live in the menu**, in the note at the top of the open menu or in an option's
  tooltip, never in a line of text under the control.
- **A glyph inside the control** marks a red control (a gap: nothing works yet) or an amber one
  (worth a look: a new sign-in is needed, the app is not in the channel). A channel that Preview or
  another Project also listens in is a choice, not a fault: the control keeps its normal colour, its
  menu note says that both answer each message, and the Publish popover lists it once under Check.
- **A blue dot inside the control** means that its value differs from what Live runs now: it is
  waiting. Its tooltip names Live's value, "Live V12 runs #support now. This goes out with the
  next publish."
- **An item that cannot follow**, a secret or an address-bound connection, has no "Same as
  Preview"; the menu note says why, for example that the connector signs its events for one
  address and Live needs a connection for its own address.

### New connection (N1)

"New connection…" ends every connection dropdown. It opens inline in the row and reuses the
TeamSpace connection form (`components/flow/connector/team-space`); for an address-bound connector
it shows that environment's address to copy. OAuth creates the connection when the sign-in
completes, and the new connection is selected for the side that asked. It is saved to the
TeamSpace, so any Project there can pick it.

### Narrow panel (R1)

Container queries on the tool panel decide the layout, never viewport breakpoints:

- **Wide:** the dropdowns are in the row.
- **Below 780px:** the row shows a Preview chip and a Publish chip instead, each ✓ or a warning
  glyph, with a tooltip such as "Next publish: same as Preview, Support Slack"; the dropdowns move
  into the open row.
- **Below 520px:** the cells stack, each captioned Preview or Next publish.

### Each column's state

Setup has no bar of its own and no Apply or Publish button. The table's head names its columns
only: Connection, Preview and Next publish, or Live on the Live tab; once the controls carry their
own captions (below 780px, or 520px for Live's one column), Connection alone. Each column's state
is in its colours, not in words: a row's red "Not connected" control and status chips say what a
column lacks, the bar's Preview status leads to the first missing item, and the Publish popover
lists what the next publish needs, with a link to each. A Preview problem shows above the table
unless its fix is Open Setup, which the table's colours already show. The bar's "Saving…" and "Saved" say what
the autosave does; the bar's Publish publishes. Before the latest push is built, the table is
replaced by "Building the latest push… Setup opens when it is ready.", that it did not build, or
that it is not built yet (`project/setup/SetupStatus.tsx`).

- **Applying is automatic, and the only way.** Changes save as they are made, and Setup applies
  them on its own (`project/setup/auto-apply.ts`): three seconds after the last saved change, when
  the configuration is READY, while Preview runs and has unapplied changes, or after its automatic
  update to the latest push was refused for missing settings. One apply covers a burst of edits,
  because each apply rebuilds the Preview application of the same commit before it swaps it in;
  the running Preview serves until the swap, and edits made meanwhile go out together with the next
  apply. An apply is never repeated for the same configuration and Preview revisions, so a refused
  one waits for the next change, a new Preview revision or the problem's Apply again. A stopped
  Preview is not started: Start takes the saved changes (`StartPreview` with `apply_configuration`).
- **A push's update goes first.** While a push's automatic update runs
  (`automaticDeployment` RUNNING), apply and Start are not allowed (`use-preview-commands.ts`): the
  update moves the Preview's revision, so either would be refused with `REVISION_CHANGED`. Setup
  applies once the update finishes.

### Start Preview

Nothing starts a stopped Preview but the agent's next push (with automatic builds on) and Start.
A Preview stops when someone stops it, when its Sandbox is reclaimed, or when an update fails; it
stays stopped until then. Start is offered where its absence shows, never as a standing control:

- **The bar.** A stopped Preview's word, "· stopped" or "· not started", opens a small popover
  (`build/PreviewStart.tsx`): what Start does, how long it takes, why it waits (the latest push
  not built, Setup missing something, or a push's update running), and **Start Preview**. It works
  from every tab, Flow and Runs included.
- **Empty surfaces.** Runs, Inbox and the Interface, which are empty while Preview is stopped,
  offer Start Preview in place. The Interface's address bar keeps only Stop, while Preview runs.

## The Interface

The Interface follows the prototype (`build/InterfaceSurface`): a 680px column with an address bar
over the app's page, and its details folded underneath. The prototype's list of the app's pages is
not built, because pages are not declared in `dex-app.yaml` yet.

- **Address bar** (`build/InterfaceBar.tsx`): a chip with the status dot and the version, "Preview
  V12" or "Live V12" (the state and commit in its hint), the address, reload and open in a new
  window; on Preview, Stop while it runs; on Live, copy the Live link.
- **The page.** Each environment's app in its frame: Preview's in `build/PreviewPanel`, and
  Live's in `build/LiveInterface`, where what people do is real. `scripts/check-consumed-pins.mjs`
  allows exactly these two application frames; the owner approved the Live frame on 2026-10-14.
- **Preview's states**, as the prototype draws them: starting, a card with its phases; failed, the
  problem with Start Preview; missing settings, the way to Setup; not built, the way to Setup;
  stopped, "Start it to see the page. Live is not affected." with Start Preview.
- **Details**, folded and read once opened: on Preview the deployed commit, where it stands beside
  the latest push, the last start's phases and Preview's history; on Live the version and commit
  Live runs and Live's history.

### The Live tab

Setup on the Live tab shows the same Next publish controls in one column, for current Live's
version, under a dark banner (`project/setup/LiveBanner.tsx`):

- **Banner.** "You're changing Live · V12". What waits for a publish, "1 change waiting for a
  publish" or "Live runs these settings", is the state under Live's name; the bar's Publish, which
  counts it ("1 waiting"), opens the Publish popover on Live's own version.
- **Intro.** These are the settings of the Next publish column on Preview; a new key or sign-in on
  a connection reaches Live at once, anything else goes out with the next publish; a blue dot
  marks a setting that differs from what Live V12 runs now.
- **Arrive with the next publish.** Connections that a newer version adds, each a link "set up
  from Preview ›" to its row on the Preview tab.
- **No longer used after the next publish.** The items the next version drops, read-only.
- Before the first publish there is no Live tab: the Next publish column sets up the first
  publish.

### Two writers in one table

`flow/scope/SetupChecklistSurface.tsx` mounts the Preview provider and binds the Next publish
cells to the layer's path: `/dex/projects/<projectID>/configuration/live/next` on the Preview tab,
checked against the Preview's source, and `.../live/next/releases/<commit>` on the Live tab,
checked against current Live's Release. The autosave queues are keyed by path, and the bar's
"Saving…" merges both ([autosave](configuration-drafts.md#autosave-and-the-revision-fence)). The
native connector units gain "Same as Preview" as their first option and the blue dot.

## One dropdown component

`components/ui/menu/` (because `components/ui` is at its ten files)
holds `Dropdown.tsx`, `ActionMenu.tsx`, `use-listbox-keys.ts` and its README, on `usePopover` and
`PopoverCard` of `ui/Popover.tsx`.

- **Dropdown** is a listbox popover with keyboard support, a tone (red or amber, with the glyph),
  the blue waiting dot, a sub-label per option, a disabled option's tooltip with its reason, and a
  menu note at the top.
- **ActionMenu** is the same popover as a menu of actions.
- They replace the native selects of `flow/connections/ConnectionRows.tsx`, where the
  `"\u0000new"` sentinel becomes a real "New connection…" option,
  `flow/connector/native/CompactConnectorUnit.tsx` and `ApplicationEnvironmentEditor.tsx`. The
  reasons `picker-options` shows beside a select move into option tooltips and the menu note.
- Every other choice in Studio uses them too; no second dropdown is built.

## Publish popover

The bar's Publish button is the one way to publish. It opens one small popover,
`components/publishing/publish/PublishPopover`, a `PopoverCard` rather than a dialog; a failed
publish's "Publish again" opens it too (`publish-opener.ts`). Setup, its Live banner and
Tools ▸ Publishing have no Publish of their own. On the Preview tab it shows:

1. **Version.** The newest version ahead of Live, with the others folded behind "Another version
   (N more ahead of Live)". Each names its subject and its Preview validation: "ran on Preview at
   10:02", or "not in the Preview record" when no READY start of the commit is among the bounded
   50 `RecentStarts`; an evicted start is not reported as "never ran". "All versions" opens
   Tools ▸ Publishing.
2. **Set up for Live first · N.** Each gap of the next publish for that version, with "Set up ›",
   which opens its Setup row with the Next publish control focused. Publish is disabled while any
   remains, or while the configuration is not READY.
3. **Summary.** One line first: "Replaces V12 · 2 setting changes · about 3 min", "V12 again · 1
   setting change", or "First publish to Live", the time from the previous publish's durations.
   **Details**, folded under it, lists the rest from the item list and the Live pointer: "Replaces
   V12. Runs in flight finish on V12; new runs start on this version", the processes that start on
   Live or change, each waiting setting, an address-bound connection sending events to Live's own
   address, the items no longer used, the connections Live uses with their channels, and the build
   and deploy times. It reuses `ConsequenceSummary`.
4. **Check.** What deserves a look and does not block, in a few words with why in its hint, one
   line per concern, so a connection's triggers that listen where Preview listens read as one
   ("support: Preview listens here too"), each with "Pick ›" to its row.
5. **Reason.** A version never started on Preview needs a short reason, at most 500 code points,
   sent as `preview_skip_reason`; Publish stays disabled until it is filled, as the build admission
   already requires. The prototype's checkbox is not adopted.
6. **Publish V13**, the popover's one button. There is no "Compare with Live": the version
   choice already names the versions ahead of Live.

The popover prepares a version's Release when it has none (`use-publish-check.ts`), never again
for one whose preparation failed. `publish/publish-commands.ts` runs Publish: it writes what Setup
still holds for the layer, then `DeriveLiveConfiguration` with the layer revision and Preview's
applied revision the popover showed, then `StartReleaseBuild` with `auto_deploy` fenced on the
Live revision shown, the copy's revision and the reason. A revision that moved
(`REVISION_CHANGED`, `RELEASE_CONFIGURATION_STALE`) is read again and the popover says what
changed; once the build is admitted, the popover closes and Tools ▸ Publishing opens on it
(`?release=&build=`) with its stage bar.

### Change the current Live configuration

The current entry is the Publish popover.

**Entry and rules.** Waiting settings are published from the bar's Publish: on the Live tab, or on
the Preview tab while no version is ahead of Live, it counts them ("1 waiting") and opens on Live's
own version. The rules follow the existing freeze model:

- **Immediate.** TeamSpace connection credentials take effect at once without publishing: a
  reauthorization or a replaced key is read at the next credential read
  ([the current contract](../teamspace/connections.md); `confighost/README.md`, "A
  freeze copies the selected connections' current settings").
- **Frozen with the version.** Everything else: connection selections, trigger bindings, Step
  settings, environment variables and connection settings.
- **Changing them** means publishing the same code with a new configuration: the same Release, a
  new copy of the layer, a new build revision, then a deployment.
- **Rollback** restores the older build with its frozen configuration and leaves the layer alone.
  There is no in-place change while Live runs.

**The popover in this mode** says "Publish V12 again with 1 setting change. The code stays the
same; it rebuilds, deploys and runs a health check", or, with nothing waiting, that publishing it
again rebuilds the same code with the same settings; it lists the waiting settings and offers the
newer versions on Preview with "Switch ›". Its Publish runs the same commands on Live's own commit. Build admission records `PreviewValidation{Kind: LIVE_CODE}`
([flow-changes](flow-changes.md#projectreleaseflow)), because the code is what Live already runs,
so no reason is asked for.

**Progress** shows the build honestly. A build revision always runs the builder: there is no
image reuse for an unchanged commit today (`project_release_flow_phases.go:131-140` and `:257`,
where each build revision records its own provider operation and image). The builder has no
skip-when-image-exists path; only its BuildKit cache mounts speed up a rebuild
(`internal/dexai/delivery/releases/build/README.md`, the cache sections). Reusing a READY image of the
same commit and build profile is a possible later optimization, outside the current design.

## Tools ▸ Publishing

 `PublishingPanel.tsx` shows, top to
bottom:

- **The publish in progress, or the last one**, from `components/publishing/progress/`: its header
  ("Publishing V13 · Started … by Maya", Cancel), the stage bar **Check → Lock settings → Build →
  Deploy → Go live** (Deploy and Go live for a rollback) with each stage's state and duration
  ([stages](observability.md#publishing-stages)), and "View logs" / "Hide logs". Each stage is as wide
  as its words; a panel narrower than 46rem drops the durations and one narrower than 34rem keeps
  only the running, failed or cancelled stage's name, all of which stay in each stage's hover hint. Cancel confirms
  briefly that the running Live is untouched and is disabled once the health check
  (`VerifyReadiness`) has passed; a later refusal reads "Too late to cancel". The log opens inline
  under the bar, grouped by stage and monospace: the build's output, redacted, read only while it is
  open ([build log](observability.md#build-log)), and the publish's recorded phases, cancellation
  and failure, each with its time and level. New lines keep it scrolled to the end unless the
  reader scrolled up. A failure shows the problem panel under the bar.
- **What is Live:** the version and Stop Live…. The Interface opens Live's address; Publishing
  does not.
- **The versions**, newest first, Preview's and Live's marked. There is no version card, Publish
  or "Compare with Live": the list names and marks each version, and the bar publishes. The
  selected version's **Builds** follow, headed "Builds · V10", with "Roll Live back to V10…"
  beside the heading for a version older than Live.
- **Builds** and **Live history** (from `ListEnvironmentEvents` for Live: publishes, rollbacks and
  cancelled publishes, with who and when; [timeline](observability.md#environment-timeline)) share
  one treatment (`publishing/HistoryRows.tsx`): a heading over a box of one-line rows, newest
  first, each with its age (the date and time in its hint). A row with more opens underneath it:
  a build's Preview validation or skip reason and its commit, build and configuration revision;
  an event's durations, the stage a cancel stopped in, or why it failed.

Stop Live (`RemoveLive`) and rollback (`StartDeployment` of an older build) each confirm first
with their consequences: which version, connections and channels will run, that runs in flight
finish on the version they started on, and for a rollback that Preview stays on its version, so
the next publish moves Live forward again and the layer's differences show as waiting; or that
Live stops serving and its triggers stop; both wait while a publish runs. `lib/project/publishing/`
holds `publish-stages.ts`, `use-publish-progress.ts` (the Release and Deployment Watches),
`use-build-log.ts` and `use-publish-check.ts`.

## Moved surfaces



- **Recent events.** The Runs column (`flow/run/RunListColumn.tsx`) puts what the environment's
  triggers received around its runs: one line above them while recent events did not arrive, with
  its fix (Start, Ask the agent, or Copy details for the operator), and the log folded at its foot
  as "Events received". `project/activity/RecentEventsProvider` reads them once for both, through
  `flow/scope`, for Preview and Live, only where a process relays a trigger or events arrived
  ([recent events](observability.md#recent-events-received)).
- **The Interface pane.** Stop, the start's phases and its problem move from the Preview menu into
  `build/PreviewPanel` and `InterfaceSurface` ([the Interface](#the-interface)).
- **Project details** (`/p/<id>`, `project/ProjectPage.tsx`) holds the Preview's "Build each push
  automatically" switch (`PreviewAutoDeploy`), the workspace's time with "Keep running for 7 days"
  and its startup phases (`WorkspaceDetails` of `session/SessionStatusIndicators.tsx`), and Delete.
  Setup no longer shows the Project's facts or a delete link, and the chat's header holds only a dot
  while live updates reconnect.
- **Live's timeline** moves from `LiveTimelineMenu` to the Live history of Tools ▸ Publishing,
  and the Live Interface's Details keep it open to operators.
- **Share.** The bar's right side is Share, then Publish, as the prototype's Invite: a popover
  (`teamspace/ProjectShare.tsx`) to add a member of the TeamSpace with a role, for owners, as
  Tools ▸ Permissions does, and to copy the link to the page shown, with "Who has access ›". The
  bar's copy-Live-link icon moves to the Live Interface's address bar, and the bar's "Live V12"
  chip goes: the Live tab and the Interface's chip name the version.

## Timeline and recent events

- `components/project/activity/` renders the per-environment timeline (`ListEnvironmentEvents`)
  and the recent events panel (`ListRecentTriggerEvents`).
- Each recent event row shows its kind in words, time and fate; its raw fields (type, channel and
  team IDs, connection, HTTP status, duration, event ID) wait behind its More details. The fate
  links to the run once the external item reports a FlowID.

## Mock development

The mock serves the new RPCs from `mock/store/setup/` with typed fixtures:

- a Preview with one unapplied change;
- a Preview with one gap;
- a failed start with remedy `OPEN_SETUP`;
- recent events covering every fate.

The mock uses
`mock/store/setup/next-publish.ts` and the `?setupFixture=` values `next-publish`, `waiting`,
`endpoint-bound` and `narrow`. The mock runs a publish on a clock with a growing
redacted log and cancels it at the build or the deployment, and `?publishFixture=` holds one
publish in each state ([mock README](../../../web/studio/mock/README.md)). The mock supports local inspection at common viewport widths.

It is selected only in `app/` route files, as today.

## Preview's timeline

The Preview menu's "Timeline" (`EnvironmentTimeline` for Preview), which the prototype does not
draw, moves to the Interface's folded Details, with the deployed commit and the last start's phases. Live's
timeline moves to Tools ▸ Publishing.
