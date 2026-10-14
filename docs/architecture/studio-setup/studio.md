# Studio

The Studio frame shows Preview and Live status, Setup and publishing. The [configuration contract](configuration-drafts.md) owns values, and [observability](observability.md) owns status and events. Component READMEs describe the implementation.

Every version is shown as "V*N* · subject", never as a hash
([version numbers](version-numbers.md)).

## Bar



- **Environment tabs.** `EnvironmentSwitch` (`components/frame/bar/EnvironmentSwitch.tsx`) with
  `EnvironmentStatusTab` (`components/frame/status/`) shows two tabs, Preview | Live. Each carries
  one dot for its [status](observability.md#environment-status), and a word only when something
  is abnormal, such as "failed", "needs setup", "starting", "stopped" and, on Live, "publishing" and
  "publish failed". The word is a link to the fix (the Interface, Setup at the first missing item
  or Tools ▸ Publishing; an operator sees the word without a link): a click on the selected tab
  opens the fix, and a click on the other tab selects it. Setup gaps and unapplied changes also
  put an amber badge on the Tools tab and on Setup. The full status stays in the tab's accessible
  name and hover hint, with accessible labels. On phone widths, the Project and environment controls wrap above the
  surface navigation, keeping its scrolling tabs separate from the controls and actions beside
  them.
- **Colours (E3).** Preview is grey and only Live is green: the tab's dot, the process picker's
  pill (`ProcessPicker`) and the active tab's underline (`TabStrip`) take the selected
  environment's colour.
- **No chevron menus.** `SegmentMenu` is deleted, and the Preview menu (`build/PreviewMenu.tsx`)
  and `LiveTimelineMenu` leave the bar. What they held moves to the
  [surfaces below](#moved-surfaces).
- **What the tabs set.** Runs, Inbox, Interface and the Flow follow the selected tab, as they
  do today through `?v=live`.
- **Operators** see only the Live tab, as today.
- **Publish button** (`components/publishing/publish/PublishButton`). "Publish" with the Publish ↑
  icon, at the top right, opens the [Publish popover](#publish-popover). On the Preview tab it
  counts the versions ahead of Live ("2 new") and is the one primary action when there are any; on
  the Live tab it counts the waiting settings ("1 waiting") and opens on Live's own version. While
  a publish runs it reads "Publishing…" and is disabled.
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

- **Columns (L1).** Connection | Preview | Next publish. The Next publish header carries the
  Publish ↑ icon and never says "Live"; its hint is "Live's settings". The Preview column is what
  Preview runs once applied. The Next publish column shows what the next publish uses and edits
  the [next-publish layer](configuration-drafts.md#live-next-publish-layer), the same settings the
  Live tab shows, so a change made in either place is the same change.
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
  (worth a look: a new sign-in is needed, the app is not in the channel, Preview listens there
  too).
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

### The Setup bar

Above the table, `project/setup/SetupBar.tsx` has two lines:

- **Preview:** its dot, then by state "N missing:" with links that open and focus each item;
  "N changes not on Preview yet" with the note "restarts in about 30 s" (the median of the last
  `recent_starts` timings) and the apply action, `StartPreview` with `apply_configuration`, which
  first flushes the autosave queue and sends the revision it saved (the prototype labels it
  "Restart Preview", and "Start Preview" before a first start); "Restarting with your changes…"
  with the phases; or "Running V12 with these settings". Apply is enabled only when the
  configuration's `status` is `READY`; with no gaps and not READY it shows "Cannot check:
  {reason}". "Saving…" and "Saved" follow.
- **Next publish · Live's settings:** by state "N to set up before publishing:" with links, each in
  plain words such as "Stripe needs its own connection"; "Follows Preview once Preview is set up"
  (with the gaps of its own, if any); "Ready to publish V13 with these settings", with "Publish…",
  when a version is ahead of Live; "N changes waiting for a publish"; or "Live runs these
  settings". Things to check follow as amber links.

### The Live tab

Setup on the Live tab shows the same Next publish controls in one column, for current Live's
version, under a dark banner (`project/setup/LiveBanner.tsx`):

- **Banner.** "You're changing Live · V12 · 1 change waiting · Publish this change…" ("Publish
  these changes…" for several), or "Nothing waiting for a publish · Publish V12 again…". Its action
  opens the Publish popover on Live's own version (`openPublishPopover({ mode: "live" })`).
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

 The Publish button, the Setup bar's
"Publish…", the Live tab's banner and Tools ▸ Publishing's "Publish V13…" open one small popover,
`components/publishing/publish/PublishPopover`, a `PopoverCard` rather than a dialog
(`publish-opener.ts`). On the Preview tab it shows:

1. **Version.** The newest version ahead of Live, with the others folded behind "Another version
   (N more ahead of Live)". Each names its subject and its Preview validation: "ran on Preview at
   10:02", or "not in the Preview record" when no READY start of the commit is among the bounded
   50 `RecentStarts`; an evicted start is not reported as "never ran". "All versions" opens
   Tools ▸ Publishing.
2. **Set up for Live first · N.** Each gap of the next publish for that version, with "Set up ›",
   which opens its Setup row with the Next publish control focused. Publish is disabled while any
   remains, or while the configuration is not READY.
3. **Check.** What deserves a look and does not block, such as a trigger listening where Preview
   listens or a connection that needs a new sign-in, each with "Pick ›" to its row.
4. **What happens.** From the item list and the Live pointer: "Replaces V12. 3 runs in flight
   finish on V12; new runs start on V13", the processes that start on Live or change, each waiting
   setting, an address-bound connection sending events to Live's own address, the items no longer
   used, the connections Live uses with their channels, and "Build about 2 min, then deploy and a
   health check" from previous durations. A first publish reads "Starts Live at its address for
   the first time." It reuses `ConsequenceSummary`.
5. **Reason.** A version never started on Preview needs a short reason, at most 500 code points,
   sent as `preview_skip_reason`; Publish stays disabled until it is filled, as the build admission
   already requires. The prototype's checkbox is not adopted.
6. **Publish V13**, beside "Compare with Live", which lists the versions it adds.

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

**Entry and rules.** Waiting settings are published from the Live tab's banner, "Publish this
change…", or from Live's own version in Tools ▸ Publishing, "Publish its waiting settings…". The
rules follow the existing freeze model:

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
newer versions on Preview with "Switch ›". "Publish this change" runs the same commands on Live's
own commit. Build admission records `PreviewValidation{Kind: LIVE_CODE}`
([flow-changes](flow-changes.md#projectreleaseflow)), because the code is what Live already runs,
so no reason is asked for.

**Progress** shows the build honestly. A build revision always runs the builder: there is no
image reuse for an unchanged commit today (`project_release_flow_phases.go:131-140` and `:257`,
where each build revision records its own provider operation and image). The builder has no
skip-when-image-exists path; only its BuildKit cache mounts speed up a rebuild
(`internal/delivery/releases/build/README.md`, the cache sections). Reusing a READY image of the
same commit and build profile is a possible later optimization, outside the current design.

## Tools ▸ Publishing

 `PublishingPanel.tsx` shows, top to
bottom:

- **The publish in progress, or the last one**, from `components/publishing/progress/`: its header
  ("Publishing V13 · Started … by Maya", Cancel), the stage bar **Check → Lock settings → Build →
  Deploy → Go live** (Deploy and Go live for a rollback) with each stage's state and duration
  ([stages](observability.md#publishing-stages)), and "View logs" / "Hide logs". Cancel confirms
  briefly that the running Live is untouched and is disabled once the health check
  (`VerifyReadiness`) has passed; a later refusal reads "Too late to cancel". The log opens inline
  under the bar, grouped by stage and monospace: the build's output, redacted, read only while it is
  open ([build log](observability.md#build-log)), and the publish's recorded phases, cancellation
  and failure, each with its time and level. New lines keep it scrolled to the end unless the
  reader scrolled up. A failure shows the problem panel under the bar.
- **What is Live:** the version, its address with Open, and Stop Live….
- **The versions**, newest first, each with its builds and Preview validation or skip reason,
  "Publish V13…" for a version ahead of Live, "Publish its waiting settings…" for Live's own, and
  "Roll Live back to V10…" for an older one.
- **Live history**, from `ListEnvironmentEvents` for Live: publishes, rollbacks and cancelled
  publishes, with who and when ([timeline](observability.md#environment-timeline)).

Stop Live (`RemoveLive`) and rollback (`StartDeployment` of an older build) each confirm first
with their consequences: which version, connections and channels will run, that runs in flight
finish on the version they started on, and for a rollback that Preview stays on its version, so
the next publish moves Live forward again and the layer's differences show as waiting; or that
Live stops serving and its triggers stop; both wait while a publish runs. `lib/project/publishing/`
holds `publish-stages.ts`, `use-publish-progress.ts` (the Release and Deployment Watches),
`use-build-log.ts` and `use-publish-check.ts`.

## Moved surfaces



- **Runs | Events.** The Runs column (`flow/run/RunListColumn.tsx`) gains a Runs | Events switch.
  Its Events slot is filled through `flow/scope` with `project/activity/RecentEvents`, for Preview
  and Live ([recent events](observability.md#recent-events-received)).
- **The Interface pane.** Start and Stop, the start's phases and its problem move from the
  Preview menu into `build/PreviewPanel` and `InterfaceSurface`, beside the page's address.
- **The chat header's timer.** The workspace timer of `session/SessionStatusIndicators.tsx`
  becomes a popover with `WorkspaceKeepRunning`, "Keep running for 7 days".
- **Project details** (`/p/<id>`, `project/ProjectPage.tsx`) holds the Preview's "Build each push
  automatically" switch (`PreviewAutoDeploy`) and Delete. Setup no longer shows the
  Project's facts or a delete link.
- **Live's timeline** moves from `LiveTimelineMenu` to the Live history of Tools ▸ Publishing,
  and a folded History on the Live Interface keeps it open to operators.

## Timeline and recent events

- `components/project/activity/` renders the per-environment timeline (`ListEnvironmentEvents`)
  and the recent events panel (`ListRecentTriggerEvents`).
- Each recent event row shows time, type, channel and fate. The fate links to the run once the
  external item reports a FlowID.

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
draw, moves to the Interface pane as a folded "History" section below Preview's phases. Live's
timeline moves to Tools ▸ Publishing.
