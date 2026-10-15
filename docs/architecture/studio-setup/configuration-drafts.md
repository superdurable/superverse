# Configuration drafts

## The existing mechanism

- A project scope's configuration is one SDK `projectconfig.Configuration` document kept by the
  configuration host. Every write is a compare-and-swap on the document revision: a write carries
  the revision its form was read at (`confighost/project_configuration.go:249-260`), and a stale
  write is refused (`REVISION_CHANGED` at the API).
- **Preview** edits the scope's canonical head directly (`project_configuration.go:37-51`). A
  Preview start validates the head and freezes it: each write is a new object version, and the
  application stays pinned to the version it accepted (`configuration_validation.go:146-168`).
  PreviewFlow keeps that pin as `PreviewConfigurationRef`
  (`internal/agents/sandbox/preview/preview_snapshot.go:122-140`).
- **Live** edits one next-publish layer per Project. Publishing resolves it against Preview's applied configuration and freezes an immutable copy for the build. See [Live next-publish layer](#live-next-publish-layer).
- Requirement gaps are computed for an exact prepared source against the configuration the
  request edits (`configuration_validation.go:177-294`). The checklist is therefore always the
  chosen version's declarations: its AppManifest connectors and environment, and its FDG trigger
  bindings and Step uses.

## Preview: drafts and Apply

This needs no new table or copy.

- **Draft.** The draft is the canonical head. Every Setup edit autosaves to it through the
  existing RPCs:
  - `SelectTeamSpaceConnection`
  - `SaveTriggerBinding`
  - `SaveOperationConfiguration`
  - `SaveApplicationEnvironment`

  The edit survives navigation and refresh because it is stored.
- **Applied.** The applied configuration is the head version that the last *applying* start froze.
  It is recorded in the new PreviewFlow Attribute `AppliedConfiguration`
  ([flow-changes](flow-changes.md#previewflow)), which survives a stop. `PreviewConfigurationRef`
  is cleared by a stop (`preview_flow.go` `StopPreview`, line 768) and cannot
  serve.
- **Apply.** This is `StartPreview` with `apply_configuration = true`. It restarts the Preview on
  its prepared commit and freezes the head, as every start does today. Setup sends it on its own
  once edits settle while Preview runs, never while a push's update runs, and Start from Studio
  sends it while saved changes are not applied; there is no Apply button
  ([each column's state](studio.md#each-columns-state)).
- **No other path applies drafts.** These starts revalidate the applied version against their
  commit through the existing exact-snapshot validation (`ValidationRequest.Snapshot`,
  `configuration_validation.go:92-101`):
  - the automatic deployment of a pushed commit (`deploy-preview`);
  - a Start from Studio while every saved change is applied;
  - the wake of a stopped Preview.

  If that version lacks what the new commit declares, the start is refused as
  `PREVIEW_CONFIGURATION_INCOMPATIBLE`, with the gaps of the applied version, and the remedy
  "Complete Setup, then Apply". The first start of a Preview has no applied version, so it
  applies.
- **"N changes not applied."** This is a new read-only confighost diff between the head and the
  applied version. It compares the head as a freeze would write it, so a TeamSpace connection
  whose settings changed since the Apply counts as a change, because the freeze copies settings
  (`configuration_validation.go:136-143`). The items are compared by key:
  - connection selection, by connection name;
  - trigger binding, by connection name, trigger and binding;
  - Step setting, by connection name, operation, Flow type and Step type;
  - environment variable, by name.

  A secret is compared by its reference only.

### Reading Preview's applied object from a Live request

A Live host cannot read Preview objects: `ConfigurationObjects` confines a host to its scope
prefix (`internal/projects/infra/docker/storage.go:300-318`), and sdkgo `ReadSnapshot` requires
the store's own key. So `projects/config` reads Preview's applied object server-side, through
`Storage.ReadConfiguration` at the exact version and digest. It strips secret references and
passes the decoded non-secret document into the Live host request. The `CHANNEL_SHARED` lookup
reads other Projects' pinned objects the same way. The applied ref comes from the existing
`config.PreviewReader` port (`internal/projects/config/configuration.go:50-55`), which gains a
method for it; there is no separate `AppliedConfigurationReader`. Under the current contract the same
read feeds the [resolution](#resolution) of every Live read and every frozen copy.

`internal/studio/confighost/configuration_items.go`, owned by LIVEDRAFT, declared the item key and
the kind, state and warning sets once, and APPLY added the Preview diff to it. The package was at
its file limit and the file at 916 lines, so  moved the pure item model (the item key,
document items, the comparisons, anchors and the Preview's item diff) to
[`internal/studio/confighost/configitems/`](../../../internal/studio/confighost/configitems/README.md),
and the layer's routes, storage and admission to `confighost/next_publish.go`.

### Autosave and the revision fence

Each edit is a CAS write, so Studio serializes the writes of one configuration object:

- One queue per configuration path and tab (`lib/api/flow/draft/configuration-draft.ts`):
  the Preview head, and under 0027 the next-publish layer. The Preview tab's table writes both,
  so it has two queues; "Saving…" covers both. A field change is written 500 ms after the last
  keystroke, or at once for a selection.
- Each write carries the revision the previous write returned.
- **Apply flushes first.** An apply first drains the queue, then sends the revision the
  last write returned as `StartPreviewRequest.configuration_revision`. The Preview freezes
  exactly that revision. A later revision, from another tab or the Agent, refuses the start
  with `PREVIEW_CONFIGURATION_CHANGED` ("Setup changed after you pressed Apply: review it and
  Apply again"); nothing the user did not see is applied (review 1).
- On `REVISION_CHANGED`, Studio rereads. If the server's value of that item is unchanged since
  this tab read it, the pending change is written again on the new revision. Otherwise the item
  shows "Changed elsewhere: yours | theirs", and nothing is overwritten silently.
- The bar shows "Saving…", "Saved" or "Not saved: retry". Leaving the page while a write is
  pending triggers the browser's leave prompt.
- The per-field Save buttons go away. Connector UI units already write through the unit host's
  writer (`components/flow/connector/native/NativeConnectorUnitHost.tsx:49-50`); they now call it
  on change.

## Live next-publish layer

The [configuration host](../../../internal/studio/confighost/README.md#live-next-publish-layer) owns one sparse layer per Project. The [project configuration](../../../internal/projects/config/README.md#lives-next-publish) resolves absent items from Preview's applied configuration, subject to the exclusions below. Studio edits the layer without selecting a release version.

### Storage

- **One object per Project:** `projects/<projectID>/live/next-publish/configuration/head`, an SDK
  `projectconfig.Configuration` document in Live's own prefix. Every write is a compare-and-swap on
  its revision through the SDK configuration store over a key-mapping object store
  (`redirectedHeadObjects`, as a version's copy is); a stale write is `REVISION_CHANGED`.
- **Sparse.** It holds only the items Live has chosen for itself, *own* items. An item that is
  absent *follows Preview*.
- **Secrets** in it are `SecretRef`s into Live's own secret area, written through the Live scope's
  credential broker. No secret value is ever copied from Preview.
- **Address.** The layer is a Live target with `next_publish`: the Flow API routes
  `/dex/projects/<projectID>/configuration/live/next` (checked against the Preview's source) and
  `.../live/next/releases/<commit>` (checked against that Release's) for the configuration RPCs,
  while `GetProjectConfiguration` with scope `LIVE` returns the items it resolves for a version
  ([item states](#item-states)). The Next publish column on
  the Preview tab and Setup on the Live tab both use it, so a change made in either place is the
  same change.
- **Cleanup.** It lives under the Project's Live prefix and goes with it when the Project's Live
  configuration is destroyed. There is exactly one, whatever the number of prepared versions.

### Resolution

For each item a version declares (connection selection, trigger binding, Step setting,
environment variable), the next publish uses the first of these:

1. **Live's own value** from the layer.
2. **Preview's applied value**, when Preview's applied version declares the item: what Preview
   runs (PreviewFlow `AppliedConfiguration`), never the head's unapplied edits. `projects/config`
   reads it server-side and passes the non-secret document into the Live host request
   ([above](#reading-previews-applied-object-from-a-live-request)); the Live host never reads
   outside its own prefix (`scopedObjects` refuses any other key). A followed connection is
   selected again for the Live scope when the copy is built, with Live's credential broker, so
   Live records its own usage when it deploys.
3. **Current Live's value**, when Preview's applied version does not declare the item, or Preview
   has applied nothing yet.
4. Otherwise the item is a **gap** and blocks publishing.

An edit on Preview is therefore followed only once it is applied. Until then "Same as Preview"
names Preview's applied value, not the edit on screen.

### What never follows

- **A secret environment value** is never copied across scopes. Live needs its own; until then the
  item is a gap.
- **A connection tied to one address**, an inbound webhook signed with a secret of one endpoint,
  cannot serve both environments, because Preview and Live have different addresses. Its
  connection selection is a gap until Live selects its own. The platform keeps the list of these
  connectors, Stripe, Typeform, Help Scout and Calendly, each with its reason, in
  `internal/studio/confighost/connectormanifest/endpoint_bound.go` until the connectors library
  declares it in the manifest. Letting such a connection be chosen by only one environment
  ("tied to Project · Live") is deferred.

Both carry `follow_refusal` (`SECRET`, `ENDPOINT_BOUND`) on their item, and Studio says why in the
dropdown's menu note.

### Item states

`GetProjectConfiguration` for the Live scope resolves the items of one version and classifies each:

| State | Meaning | Blocks Publish |
|---|---|---|
| `FOLLOWS_PREVIEW` | Absent from the layer; resolved by step 2 or 3 | only if it is a gap |
| `OWN` | Live's own value from the layer, or an item that cannot follow | only if it is a gap |
| `NO_LONGER_USED` | On current Live, not declared by the version; read-only, "no longer used after the next publish" | no |

Each item also carries:

- `differs_from_live`: the resolved value differs from what current Live runs. Studio shows such
  an item as **waiting**.
- `differs_from_preview`: the resolved value differs from Preview's applied value.
- `preview_value_json`: Preview's applied non-secret value, which "Same as Preview" names.
- `live_value_json`: current Live's non-secret value, which the waiting marker names ("Live V12
  runs #support now").
- `follow_refusal`, for an item that cannot follow.
- `warnings`, for example `CHANNEL_SHARED`; see [pitfalls](#pitfalls-caught-at-selection).

**Which version.** A target addresses the layer explicitly (`next_publish`), and names the version
whose declarations resolve and check the request: with a `release_id`, that Release (current
Live's on the Live tab, the chosen one when publishing); without one, Preview's prepared source
(the Preview tab's Next publish column). A Live target without `next_publish` names a version's
frozen copy, which is read only, and a Live target without a Release is invalid. There is no Live
tab before the first publish; the Next publish column sets the first publish up.

**Anchors.** The configuration format requires a trigger binding or Step setting to sit under its
connection's entry. When Live owns a binding or setting under a connection that follows Preview,
the layer keeps an anchor: that connection's entry without a selected TeamSpace connection, which
never counts as Live's own value.

**Declarations from outside Live's scope.** The named source may not be prepared in Live's scope,
and a host reads only its own prefix. When the target names the Preview's prepared source,
`projects/config` reads it through the Preview's host (`ConfigurationHost.PreparedSource`) and
passes it into the Live host request, as it passes Preview's applied document. Studio never
prepares a Release to edit the layer.

### Writing the layer

- The existing configuration RPCs write the layer when their target is the Live scope with
  `next_publish`: `SelectTeamSpaceConnection`, `SaveTriggerBinding`, `SaveOperationConfiguration`
  and `SaveApplicationEnvironment`. A write sets Live's own value.
- **"Same as Preview"** removes Live's own value with the same RPC and `follow_preview` set (for
  environment variables, the names in `follow_preview`). The item follows again.
- `follow_preview` is refused with HTTP 409 and the code `CONFIGURATION_ITEM_CANNOT_FOLLOW` in the
  Flow API document on the Preview scope and for an item that cannot follow. A Release target's
  frozen copy is read only (InvalidArgument).
- Writes autosave through the layer's own queue ([autosave](#autosave-and-the-revision-fence)).
- There is no confirm step and no proposal: nothing waits to be accepted, and no item can
  conflict, because the layer has one value per item.

### Waiting

A setting is **waiting** while its resolved value differs from what current Live runs. It reaches
Live only with a publish, together with that version's code. Waiting includes a value Preview
applied that Live follows. A new key or sign-in on a TeamSpace connection is not waiting: it
reaches Live at its next credential read ([the current contract](../teamspace/connections.md)).
Right after a publish nothing is waiting, so editing Next publish on the Preview tab has the same
outcome as editing on the Live tab.

### Publishing freezes a copy

- **The copy.** `DeriveLiveConfiguration(project_id, release_id, expected_revision,
  expected_preview_configuration_revision)` builds version R's full configuration from the
  resolution into `release-drafts/<R>/configuration/head` and returns it. `expected_revision` is
  the layer revision and `expected_preview_configuration_revision` Preview's applied revision that
  the Publish popover showed; when either moved, it refuses with `REVISION_CHANGED` and the popover
  reads again and says what changed. The copy is written only by `DeriveLiveConfiguration`; Setup
  never edits it.
- **What it was built from.** `release-drafts/<R>/derivation` stays a small metadata object,
  written by compare-and-swap on its ETag after the copy, and now records the copy's inputs:
  `liveDeploymentId`, `layerRevision`, `previewRevision`, `copyRevision` and `derivedAt`. It holds
  no value and no conflicts. A record of an earlier per-version draft reads as stale.
- **Staleness.** `StartReleaseBuild` sends the copy's `configuration_revision`. Build admission
  (`CheckReleaseDraft`, `deployments/publishing_flow_service.go:127-143`) refuses
  `RELEASE_CONFIGURATION_STALE` when the copy is absent or current Live, Preview's applied version,
  the layer or the copy itself changed since it was built, and the conflict check is removed.
- **The freeze** is unchanged: the build freezes the copy at `release-drafts/<R>/freezes/<buildID>`,
  and Release and Deployment pin that version.
- **No seed.** `seedCommitDraft` and the seed passed through `RegisterReleaseSource` are removed;
  `PreparedReleaseSource` loses its seed fields
  ([flow changes](flow-changes.md#next-publish-and-cancellation)).
- **Live's own version again.** Publishing current Live's version with waiting settings, from the
  Live tab's banner, builds the copy of Live's commit the same way; admission records
  `PreviewValidation{Kind: LIVE_CODE}`.

### Rollback

A rollback restores the older build with its frozen configuration and leaves the layer alone. The
Live tab then shows every next-publish value that differs from what the restored Live runs as
waiting.

### Migration

- **Lazy seed.** While a Project has no layer, a read computes one from what it runs: every item
  whose current Live value differs from Preview's applied value, every secret and every
  address-bound connection selection count as Live's own, with current Live's value; the rest
  follow. No running Live changes.
- **Stored on first need.** The first write or publish stores that seed create-if-absent as the
  layer's first revision, then applies its change by compare-and-swap on it. Concurrent first
  writes store one seed; a change raced by another is refused by revision (`REVISION_CHANGED`), as
  any raced write is, and Studio rereads.
- **Before the first publish** the seed is empty: everything follows, and secrets and
  address-bound connections are gaps.
- **Dropped.** Unpublished edits in today's per-version drafts are dropped: the next
  `DeriveLiveConfiguration` overwrites the copy from the layer, and an old `derivation` object's
  `conflicts` and `fromCommit` are ignored. The current layer is authoritative.

### Enforcement

Studio disables Publish, and the server also refuses. Build admission
([flow-changes](flow-changes.md#projectreleaseflow)) runs the requirement gap report and the
staleness check on the copy revision it is asked to freeze. Gaps have one owner (review 5). There
is no client-side gap refusal. The Flow's freeze already refuses a configuration with gaps and
records them itemized (`FreezeReleaseBuildConfiguration` and `RecordReleaseBuildFailure` in
`project_release_flow.go`). The latter's copy still says "in Setup · Live", the Live tab's Setup,
which shows the same next-publish settings as the Next publish column. An item that cannot follow
and has no Live value is such a gap. Admission refuses only what the freeze cannot see:

- `RELEASE_CONFIGURATION_STALE`: the copy's inputs moved.

It runs only for a new build revision key; replay comes first
([flow-changes](flow-changes.md#projectreleaseflow), review 4). `RELEASE_CONFIGURATION_CONFLICT`
went away with conflicts.


## Defaults set in Studio

- **Reply trigger channel.** When a connection declares a thread reply binding and a start
  binding (`threadReplyCreated` with `channelThreadCreated`) and the reply binding has no
  channel, Studio fills it from the start binding's channel and saves it. The item shows
  "same channel as 'Starts when a new message arrives'".
- **Replier.** This is a `memberPicker` unit. Studio defaults it to the Slack member who
  authorized the connection, when that authorization's account is the current user. Otherwise it stays empty and is a gap if
  required.
- **First Live configuration.** It follows Preview: a first publish has an empty layer, so
  every item takes Preview's applied value, except secrets and address-bound connections, which
  Live sets up itself ([what never follows](#what-never-follows)).

## Pitfalls caught at selection

- **Channel the bot is not in.** The channel picker lists every channel `listChannels` returns:
  - joined channels first;
  - a channel with `is_member: false` shown with "The app is not in #name: type /invite @app in
    Slack, then refresh";
  - a stored channel ID that no page returns as joined is flagged the same way.

  The parse change is in `components/flow/connector/native/slack-resources.ts`.
- **Channel already listened to by another Project.** For every Slack trigger binding of the
  draft, the diff reads the bindings of the other Projects' *applied* configurations in the same
  TeamSpace and environment that use the same TeamSpace connection. Those Projects come from
  `team_space_connection_usages`. Each Project's applied version is its Preview
  `AppliedConfiguration`, or its Live deployment's pinned configuration object. A match on the
  channel ID adds the item warning `CHANNEL_SHARED`, with the other Project's name if the caller
  holds a role in it and "another Project" otherwise. Binding configuration holds channel IDs,
  never message content. This is a warning only.
- **Next publish listening where Preview listens.** A trigger binding whose next-publish value
  equals Preview's applied value through the same TeamSpace connection makes Preview and Live
  both answer each event, for example both replying in one Slack channel. Studio finds it from
  the item list (`preview_value_json` against the resolved value). The control keeps its normal
  colour, since the person may want both in one channel: its menu note says "Preview listens here
  too: while Preview runs, Preview and Live both answer each message", and the Publish popover
  lists it once under Check. It never blocks a publish.
- **Connection needs reauthorization.** An `AUTHORIZATION_REQUIRED` gap shows on the item with
  "Reauthorize". The button starts `StartTeamSpaceConnectionOAuth` in a dialog and returns to
  the item. The TeamSpace rule that refuses reauthorizing a connection Live uses still applies,
  and its refusal uses the [error format](observability.md#error-contract).

## Configuration API

The [resource API](../../api/resource-grpc.md#configuration) owns the transport fields.

### Next-publish API

Described in the [resource API](../../api/resource-grpc.md#configuration).
Pre-launch, removed fields and values are deleted without reservations, and the Go and Studio
bindings are regenerated.

| Change | Where |
|---|---|
| `follow_preview` (bool) on `SelectTeamSpaceConnectionRequest`, `SaveTriggerBindingRequest` and `SaveOperationConfigurationRequest`; `repeated string follow_preview` (variable names) on `SaveApplicationEnvironmentRequest`. Refused with 409 `CONFIGURATION_ITEM_CANNOT_FOLLOW` in the Flow API document on the Preview scope and for an item that cannot follow | ConnectorService, ApplicationEnvironmentService |
| `ConfigurationItem.State` Live values `FOLLOWS_PREVIEW`, `OWN`, `NO_LONGER_USED`; `KEPT`, `NEW_PROPOSED`, `NEW_CONFIRMED` and `CONFLICT` removed | `GetProjectConfiguration` |
| `ConfigurationItem.preview_value_json` (string) and `follow_refusal` (enum `SECRET`, `ENDPOINT_BOUND`); `proposal_json` removed | `GetProjectConfiguration` |
| `ProjectConfigurationTarget` and the configuration `FlowApiTarget` gain `next_publish`: a Live target that addresses the layer, resolved for and checked against the source its `release_id` names or, empty, the Preview's prepared source; `ProjectConfigurationSnapshot` gains `next_publish_revision` and `preview_configuration_revision` | ProjectService, the configuration RPCs |
| `DeriveLiveConfigurationRequest`: `from_release_id` removed; `expected_preview_configuration_revision` (uint64) added; `expected_revision` is the layer's | ReleaseService |
| `ConfirmLiveConfigurationItems` and `ConflictResolution` removed | ReleaseService (9 to 8 RPCs) |
| `ConnectorCatalogEntry.endpoint_bound` (bool) and `endpoint_bound_reason` | TeamSpaceConnectionService `ListConnectorCatalog` |
| `RELEASE_CONFIGURATION_CONFLICT` removed; `CONFIGURATION_ITEM_CANNOT_FOLLOW` is a configuration host code in the Flow API document, not an ErrorInfo reason | shared |
| The BFF routes `/dex/projects/<projectID>/configuration/live/next` and `.../live/next/releases/<commit>` | Studio BFF |

The cancellation and build log RPCs are in
[observability](observability.md#api-changes-of).
