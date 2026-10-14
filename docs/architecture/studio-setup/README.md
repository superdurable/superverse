# Studio setup, Preview and publishing

Studio presents one Setup surface for Preview drafts and Live's next-publish layer. The [configuration contract](configuration-drafts.md) owns their values and revision rules; [Flow changes](flow-changes.md) owns publishing and cancellation; [observability](observability.md) owns status, events and build logs; and [Studio](studio.md) owns the controls and navigation. The [resource API](../../api/resource-grpc.md) defines their transport.

## Dex Flow Changes

- **PreviewFlow.** `Command` gains `ActorAccountID`, `ApplyConfiguration` and
  `ConfigurationRevision`. Two new Attributes:
  - `AppliedConfiguration`, kept across stops.
  - `RecentStarts`, a list bounded at 50.

  Only an applying start freezes the configuration, at exactly the revision the user saw. Every
  other start, including the
  automatic deployment of a pushed commit, revalidates the applied version against the new
  commit. The full contract delta is in [flow-changes.md](flow-changes.md#previewflow).
- **ProjectReleaseFlow.** `ReleaseBuildRevision` gains `PreviewValidation`. Replay is checked
  first. For a new build, admission refuses when any of these holds:
  - there is no successful Preview start of the commit, no skip reason, and it is not the Live
    Release;
  - the frozen copy is stale: current Live, Preview's applied version or the layer
    changed since the copy was built.

  The STUDIOSETUP refusal for an unresolved conflict went away with conflicts. Gaps, including
  an item that cannot follow Preview and has no Live value, keep their existing owner: the
  freeze's itemized refusal.

  Details in [flow-changes.md](flow-changes.md#projectreleaseflow).
- **ProjectDeployFlow.** `StartIdentity` is synced to the attribute store, so the timeline can
  name who deployed ([flow-changes.md](flow-changes.md#projectdeployflow)).
- **the current contract** adds build cancellation to ProjectReleaseFlow (an RPC, a `Cancellation`
  field on the build revision and a Step that stops the provider build and settles it as
  CANCELLED) and deployment cancellation to ProjectDeployFlow (a `Cancellation` Attribute and an
  RPC, refused once the Flow has chosen to commit the new Live),
  `PreparedReleaseSource` has no seed fields, because the copy is built from the layer
  at publish time. The contracts are in
  [flow-changes.md](flow-changes.md#next-publish-and-cancellation).
- No new Flow type, Channel, ChannelMap or Stream.

## Database Schema Changes

Migration `0006_studio_setup.sql` adds two columns to read-model projections that their Flows
already own. There is no new table.

- `release_build_revisions.preview_validation JSONB`: projection of
  `ReleaseBuildRevision.PreviewValidation`. It is written by ProjectReleaseFlow's
  attribute-store sync and is null for builds created before the migration.
- `deployments.start_identity JSONB`: projection of ProjectDeployFlow `StartIdentity`
  (`ActorAccountID`, `RequestID`, `Fingerprint`). It is null for deployments that started
  before the change; the timeline then shows "unknown".

Migration `0008_release_build_failure_owner.sql` adds the column that `0006` missed:

- `release_build_revisions.failure_owner JSONB`: projection of
  `ReleaseBuildRevision.FailureOwner`, who must act on the build's failure or its auto-deploy
  failure. It is null for builds without a failure and for builds synced before the migration.

The current contract adds migration `0009_publish_cancellation.sql`:

- `release_build_revisions.cancellation JSONB` and `deployments.cancellation JSONB`, nullable
  projections of the Flows' new `Cancellation` fields, written only by the Dex Attribute Store
  sync and null for every row before the migration. There is no backfill.

There is no new object store table either. The next-publish layer is one configuration
object per Project in Live's prefix, not a table. Each prepared version keeps its copy and its small
`release-drafts/<commit>/derivation` metadata object, which now records what the copy was built
from ([the frozen copy](configuration-drafts.md#publishing-freezes-a-copy)). The recent events
buffer is memory only, and the build log is never stored.

## UI/UX

See [studio.md](studio.md). In summary, as revised by the current contract:

- The bar has Preview | Live tabs with one dot each and a word only when something is abnormal,
  which links to the fix. No chevron menus stay in the bar. Preview is grey and only Live is
  green, on the tab, the process picker's pill and the active tab's underline.
- Tools ▸ Setup is one table: a row per connection with its triggers and Step settings under it,
  then the variables, with a Preview column and a Next publish column. The Live tab shows the
  same controls under a dark banner.
- One dropdown component serves every choice in Studio.
- Publish opens a small popover instead of the three-step wizard.
- Tools ▸ Publishing shows the stage bar, the build log and Cancel, what is Live, the
  versions, Live history, Stop Live and rollback.
- Runs | Events, the Interface pane, the chat header's timer and Project details take over what
  the bar's menus held.
- Every failure uses one four-part problem panel.

## Tests

Real-dependency Dex integration tests and Studio integration specs cover the behavior on the Local Native lane. The three browser journeys cover publishing through the Publish popover. Hosted builder cancellation and log reads require hosted verification.

## Resource Scalability

- **The next-publish layer:** one configuration object per Project, deleted with the Project's
  Live prefix; it replaces editable drafts that grew by one per prepared version.
- **The copy and its `derivation` object:** one each per prepared version, as before, deleted with
  the prefix.
- **The build log:** per replica, one provider read per viewed page, shared by its viewers for two
  seconds, within the token bucket `builds.logReadsPerSecond` (default 2, burst 4); nothing stored
  ([build log](observability.md#build-log)).
- **Cancellation:** creates nothing and frees a generation's resources sooner.
- **Migration 0009:** two nullable columns.
- The recent events ring is bounded per process ([recent events](observability.md#placement-and-bounds)).

## Documentation

- This directory is the design, revised by
  [the current contract](flow-changes.md).
- [api-and-studio.md](../teamspace/api-and-studio.md#studio) links it, and its sentence
  "Selection happens only here" changes to point at Setup.
- Current behavior is documented in the owning topic documents.
- `docs/api/slack-relay.md` gains the recent events read.
- `docs/api/resource-grpc.md` describes the protobuf contracts.
- The package READMEs of `confighost`, `preview`, `releases`, `deployments` and `slackrelay`
  change with their code, and the Studio directory READMEs with theirs.
