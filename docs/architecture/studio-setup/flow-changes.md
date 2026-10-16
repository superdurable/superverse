# Flow changes

These are deltas against the current contracts. The unchanged parts stay as their owning
documents describe them:

- PreviewFlow: [`internal/dexai/agents/sandbox/preview/README.md`](../../../internal/dexai/agents/sandbox/preview/README.md)
  and [sandbox.md](../agent-harness-design/sandbox.md).
- ProjectReleaseFlow: [`internal/dexai/delivery/releases/README.md`](../../../internal/dexai/delivery/releases/README.md).
- ProjectDeployFlow: [`internal/dexai/delivery/deployments/README.md`](../../../internal/dexai/delivery/deployments/README.md).

STUDIOSETUP adds or renames no Flow type, Step identity, Channel, ChannelMap or Stream.
The Live next-publish layer and cancellation add one Step, one Attribute and two RPCs: see [next publish and cancellation](#next-publish-and-cancellation).

## PreviewFlow

### Identity and execution lifecycle

Unchanged:

- FlowID `preview-<projectID>`, for example `preview-a7k2` (`internal/comm/flowid/project.go:137-141`).
- One execution per Project until deletion, with the same start, RequestID and reuse policy.

Changed: a command now carries its actor and whether it applies configuration.

- `Command.ActorAccountID` is the caller of `StartPreview`, `StopPreview` or
  `PreparePreviewSource`, taken by the PreviewService handler from `CallerContext`. It is empty
  for a `deploy-preview` (a push by the Agent), a `reclaim` and a `release`; the timeline shows
  those as "the agent" or "the platform".
- `Command.ApplyConfiguration` and `Command.ConfigurationRevision` are set only on
  `start-preview`, from `StartPreviewRequest.apply_configuration` and `configuration_revision`.
  The revision is the draft revision the user saw (review 1). Both take part in the repeat
  comparison.
- The Agent's own Preview start (`internal/dexai/agents/session_flow_service_preview.go:82`) sends
  `start-preview` with an empty actor and `ApplyConfiguration = false`, so the Agent never
  applies drafts.
- Both fields take part in the exact-repeat check of `admit`
  (`preview_command.go:155-177`, the `original != c` comparison). A retry with the same command
  ID but a different actor or flag is `COMMAND_CONFLICT`, as any other field change is today.
- The Studio request ID scheme is unchanged. Apply and Start use distinct request IDs, because
  they are different user actions.

### Attributes and AttributeMaps

| Attribute | Type | Writers | Notes |
|---|---|---|---|
| `AppliedConfiguration` (new) | `AppliedConfiguration{ObjectKey, ObjectVersion, Digest string; ConfigurationRevision uint64; SourceCommit, CommandID, ActorAccountID string; AppliedAt time.Time}`, zero until the first applying start | `PreparePreviewConfiguration`, only for an applying start, in the same save that pins `PreviewConfigurationRef` | Kept across stops and reclaims. It is the head version that every non-applying start revalidates. Metadata only, no secret. Read by `GetPreview`. |
| `RecentStarts` (new) | `[]StartRecord`, at most 50, oldest dropped first | `accept` appends for `start-preview` and `deploy-preview`; `finishCommand` and `serve` complete the newest entry | `StartRecord{CommandID string; Kind CommandKind; ActorAccountID, Commit string; ConfigurationRevision uint64; Applied bool; AdmittedAt, EndedAt time.Time; Outcome StartOutcome; FailureCode string; Timings []dexflow.PhaseTiming}`. `StartOutcome` is the closed set `RUNNING`, `READY`, `FAILED`, `REFUSED`, `CANCELLED`, declared once as typed constants. |
| `Command` fields (`ActiveCommand`, `LastCommand`) | adds `ActorAccountID string`, `ApplyConfiguration bool` and `ConfigurationRevision uint64`, all `json:",omitempty"` | as today | An open execution's persisted command decodes with zero values: no actor and not applying. |

- Both new Attributes are ordinary struct values, not AttributeMaps, and are not indexed. Each is
  read whole by `GetPreview` and by the Steps through `readSnapshot`, and written whole by `save`.
  `save` gains two bindings in `mutableAttributes` (`preview_snapshot.go:280-300`).
- The writers already hold `CommandLock` (`stateStepOptions`, `commandEndStepOptions`), or are
  the single writer of what they save while a command is active (`PreparePreviewConfiguration`,
  per the comment at `preview_flow.go` above `GetPreview`). No new locks are needed.
- **Growth.** `RecentStarts` holds 50 entries of at most 5 timings each, well under 64 KiB.
  `AppliedConfiguration` has a fixed size. Both are retained for the Flow's lifetime and go with
  the execution when the Project is deleted.
- **Secrets.** There are none. `FailureCode` is a code name; the failure message stays in
  `Failure`.
- **Open executions.** Both Attributes are absent until first written. A Preview whose
  `AppliedConfiguration` is zero treats its next start as applying, which is today's behavior.
  That next start may be the Agent's first `deploy-preview`. With no applied version yet, that
  push applies the current draft, its first Apply in effect, and the timeline records it as
  applied by "the agent".
  `RecentStarts` starts empty, so a commit that ran before the change has no recorded validation
  and needs one Preview start, or a written skip reason, before publishing.

### Channels and ChannelMaps

None changed. `PreviewCommands` carries the extended `Command`. Its admission, ordering and
duplicate handling are unchanged apart from the two fields in the repeat comparison.
`WorkspaceSettled` is unchanged.

### Streams

None. The Preview has no Stream. Studio reads status from `GetPreview` and the Session Watch, as
it does today.

### Steps, RPCs and recovery

- **`PreparePreviewConfiguration`.** The Step graph and options are unchanged
  (`preview_flow.go`, the Step after `VerifyPreviewSource`). The Step now branches:
  - **Applying start**: `ActiveCommand.ApplyConfiguration`, or `AppliedConfiguration` is zero.
    The Step validates and freezes the command's `ConfigurationRevision`, not the revision it
    reads at Step time. `ValidationRequest.ConfigurationRevision` already rejects a mismatch
    (`configuration_validation.go:98-101`). A mismatch is a refusal with the new recoverable code
    `PREVIEW_CONFIGURATION_CHANGED` (owner configuration, remedy apply again): "Setup changed
    after you pressed Apply: review it and Apply again". It is not a gap report. For a
    zero-applied start without a revision (the Agent's first deploy), the revision read at Step
    time is used, as today. The Step then sets `AppliedConfiguration` from the result in the same
    save as `startsPreview`.
  - **Any other start** (`deploy-preview`, a non-applying `start-preview`): the Step calls the new
    `projectconfig.Configuration.ValidateAppliedPreview(scope, manifest, applied)`. It posts
    `ValidationRequest{Snapshot: &applied}` for the commit's prepared source, which freezes
    nothing (`configuration_validation.go:92-123`). On success the pin is the applied version,
    and the grant is reused while the revision is unchanged (`grantFor`).
  - **Refusal of a non-applying start**: `ErrNotReady` or `ErrConflict` asks for the gaps with the
    new `RequirementsRequest.Snapshot` field. The configuration host computes the gaps of that
    exact version instead of the head. The start is refused as `PREVIEW_CONFIGURATION_INCOMPATIBLE`
    with those gaps, and the running Preview is left untouched, as today.
  - **Shared settings and usages.** A non-applying start does not refresh shared settings or
    replace usages (`configuration_validation.go:136-168`). The running Preview keeps exactly the
    settings and usages of its last Apply. Credentials are read at use, as today.
- **`accept` and `finishCommand`** (`preview_command.go:205-222`, `preview_deployment.go:254-300`)
  maintain `RecentStarts`. Every update finds its entry by `CommandID`, never as "the newest".
  `serve` (`preview_snapshot.go:175-181`) marks the entry of the active command `READY` with
  `EndedAt`. A failure or refusal marks it `FAILED` or `REFUSED` with the code and the timings.
  A stop or reclaim of a running entry marks it `CANCELLED`. An entry already evicted from the
  50 is not recreated.
- **RPCs.** `GetPreview` returns the two new Attributes. `RequestPreviewCommand` stores the
  extended command. No RPC is added.
- **Recovery.** Validation of an exact version is read-only and retry safe. An applying freeze
  keeps its current recovery: each head write is a new version and the Step pins what it got. A
  retried `PreparePreviewConfiguration` attempt after a lost response of an applying freeze
  freezes again and pins the newer version of the same revision. That is today's behavior, and
  `AppliedConfiguration` follows the pin.
- **Errors.** The new failure definitions carry `remedy` and `owner`
  ([error contract](observability.md#error-contract)). `PREVIEW_CONFIGURATION_FAILED`'s message
  loses its stale "Open Build Configuration".
- **Tests** (Dex integration, real Dex and MinIO, under `internal/dexai/agents`). Each is a scenario
  with these steps:
  - **Apply, push, verify.** Apply, change a draft field, push a commit (deploy-preview). The
    Preview runs the *applied* revision, `RecentStarts` holds both starts, and the diff counts
    one unapplied change.
  - **Apply after stop.** Apply after a stop. `AppliedConfiguration` survives the stop.
  - **Incompatible commit.** A commit that declares a new required connection is refused
    `INCOMPATIBLE` with the applied version's gaps, while the old Preview keeps serving.
  - **Restart mid-start.** The Worker restarts during an applying start. The record completes
    once, with no duplicate entry.
  - **Command conflict.** The same command ID with a different `ApplyConfiguration` is refused
    `COMMAND_CONFLICT`.
  - **Bound.** The 51st start drops the oldest record.

## ProjectReleaseFlow

### Identity and execution lifecycle

Unchanged: FlowID `release-<projectID>-<commit>`, for example
`release-a7k2-3f9c…` (`internal/dexai/delivery/releases/project_release_flow_client.go:33-36`), and its
start, reuse and build revision identity.

Also unchanged:

- Business IDs: Project ID and commit.
- Start RequestID and `IDReuseDisallow`.
- The build revision ID `build-<uuid>`, chosen by the caller.
- Duplicate admission returns the original acceptance.
- Business completion (READY build), the Release's removal and engine completion, with their
  retention.
- Child deployment ownership through automatic deployment.

### Attributes and AttributeMaps

`ReleaseBuildRevisions` (an AttributeMap keyed by `build-<uuid>`, unchanged) gains a field on
its value `ReleaseBuildRevision` (`project_release_flow_state.go:156-180`):

```go
// PreviewValidation is how a build met the publishing rule: a READY Preview start of the commit,
// or the builder's written reason for skipping it. Set at admission; never changed.
type PreviewValidation struct {
    Kind           PreviewValidationKind // VALIDATED, SKIPPED or LIVE_CODE, typed constants
    StartCommandID string                // VALIDATED: the RecentStarts entry
    StartedAt      time.Time             // VALIDATED: its EndedAt
    SkipReason     string                // SKIPPED: 1..500 code points, trimmed
    ActorAccountID string
}
```

- It syncs to `release_build_revisions.preview_validation`. It holds no secret.
- `BuildRevisionRequest` (`project_release_flow_state.go:325-334`) carries the value, decided at
  admission.

- **Compatibility and retention.** Existing build revisions, and builds in flight when this
  ships, decode with a zero `PreviewValidation`, and their `preview_validation` column stays
  null; Studio shows "validation not recorded". The value is written once at admission, and is
  retained with its map entry until the Release is removed, like the rest of the entry.

### Channels and ChannelMaps

None changed. `ReleaseBuildCommands` carries the same command; the admitted request gains the
validation value only.

### Streams

None changed. `BuildProgress` is unchanged and stays best effort; the build revision is the
durable record.

### Steps, RPCs and recovery

No Step is added or renamed.

- **Admission** happens in the client's build path (`project_release_flow_client_build.go`)
  before the RPC that admits the build. It reads the Preview through the Flow's dependency port.
  `ProjectReleaseFlowDependency` gains `PreviewValidation(ctx, projectID, commit)`, which returns
  the newest `READY` `RecentStarts` entry of that commit. `internal/dexai/bootstrap`'s adapter
  implements it through `PreviewFlowClient` (`PreviewValidationReader`; see
  [cross-scope reads](#cross-scope-reads-go-through-ports)). `delivery/releases` never imports
  the Preview package.
  - **Replay comes first** (review 4). The client reads the existing `ReleaseBuildRevisions`
    entry of the requested build revision ID before any check. If the entry exists, admission is
    the existing replay: same fingerprint, the original acceptance; a changed input is refused.
    No validation, staleness or conflict check is recomputed, so a retry after a lost response
    never gets `STALE` while its build runs.
  - **Fingerprint.** `SkipReason` enters `buildFingerprint` (`project_release_flow.go:257`), so
    a changed reason under the same key is changed input. The computed results do not enter it:
    the validation kind, its start record, and the staleness and conflict checks. They are stored
    on the record.
  - A build of the Release that Live serves (`ProjectSnapshot.live.release_id`, the same
    commit) is `LIVE_CODE` and needs no Preview start. This is "Change the current Live
    configuration" ([studio](studio.md#change-the-current-live-configuration)).
  - With no such entry and an empty `preview_skip_reason`, the request is refused with
    `FAILED_PRECONDITION`, reason `PREVIEW_VALIDATION_REQUIRED`.
  - For a new key, admission refuses `RELEASE_CONFIGURATION_STALE` if current Live, Preview's applied version or the next-publish layer moved since the publish copy was built. The freeze refuses missing required items with an itemized gap report.
- **Recovery.** A retried admission with the same request ID returns the same build revision, as
  today. The validation is part of the admitted request, so a retry cannot change it.
- **Rollback is exempt.** A rollback is `StartDeployment` of an existing READY build, so this
  admission never runs for it, and the original build keeps its validation.
- **Tests** (Dex integration in `internal/dexai/delivery/releases`, real Dex, Postgres and MinIO,
  local builder):
  - A build without a Preview start and without a reason is refused.
  - With a reason, it is admitted, and the reason is in the projection.
  - After a `READY` Preview start of the commit, it is admitted as `VALIDATED`.
  - A configuration-only build of the Live Release is admitted as `LIVE_CODE`. It deploys, and
    a rollback to the previous build restores the previous configuration.
  - A publish copy with a required gap fails at the freeze with the itemized gap.
  - A retried request after a lost response returns the original build even after Live changed.
  - A publish copy whose Live, Preview applied version or layer moved is refused `STALE`.

## ProjectDeployFlow

### Identity and execution lifecycle

Unchanged: FlowID `deploy-<projectID>-<deploymentID>` (`project_deploy_flow_client.go:42-45`),
for example `deploy-a7k2-b51c0b8e`, and its start, reuse and fencing.

### Attributes and AttributeMaps

The only change is that `StartIdentity` (`project_deploy_flow.go:22`) is declared with
`dex.SyncToAttributeStore()`, so the projection `deployments.start_identity` names the actor.
It holds the actor, the request ID and the fingerprint, and no secret. Its value and owner are
unchanged.

### Channels and ChannelMaps

None.

### Streams

None changed. `DeploymentProgress` is unchanged.

### Steps, RPCs and recovery

- Open executions sync it on their next attribute write.
- A deployment that never writes again keeps a null column, which the timeline shows as
  "unknown".
- No Step, RPC or recovery changes.
- **Tests**: the existing `read_model_integration_test.go` asserts the column.

## Configuration host (no Flow)

The configuration host is not a Flow. It resolves the next-publish layer, computes item differences, reads exact versions through its ports and freezes a copy for each build. The [configuration host README](../../../internal/dexai/studio/confighost/README.md) owns its implementation.

### Cross-scope reads go through ports

`projects/config` reads the referenced objects itself through `Storage.ReadConfiguration` at the
exact version and digest, and hands the host the decoded document with secret references
stripped ([configuration-drafts](configuration-drafts.md#reading-previews-applied-object-from-a-live-request)).

The host serves one scope. `internal/dexai/projects/config` cannot import PreviewFlow, because
`preview` already imports `projects/config` (`preview_configuration.go:14`). It cannot read
deployments either: `projects/config` imports `delivery/releases` (`prepared_configuration.go:12`),
and `deployments` imports `projects/config`. So every cross-scope read is a port that its
consumer declares and `internal/dexai/bootstrap` implements by reading PreviewFlow and
ProjectDeployFlow through their clients:

| Port (declared by) | Answers | Used for |
|---|---|---|
| `config.PreviewReader` (existing, `internal/dexai/projects/config/configuration.go:50-55`), gaining a method | A Project's Preview `AppliedConfiguration` ref, and the pinned configuration object of its READY Live deployment | Live items that follow Preview resolve from its applied snapshot; other Projects' bindings support `CHANNEL_SHARED` |
| `PreviewValidationReader` (`internal/dexai/delivery/releases`, a method of `ProjectReleaseFlowDependency`) | The newest READY `RecentStarts` entry of a commit | Build admission (PUBGATE) |

`projects/config` passes the resolved snapshot refs into the host request. The host stays one
scope and reads only the objects it is handed. The `CHANNEL_SHARED` lookup lives in
`projects/config`: it lists the Projects that use the same TeamSpace connection
(`team_space_connection_usages`), reads their applied refs through the port, and reads their
bindings through each Project's own host.

## Next publish and cancellation

The current contracts for ProjectReleaseFlow and ProjectDeployFlow are in their package READMEs. This section describes cancellation and the next-publish admission behavior they expose.

### A running Step does not see a new cancellation

The plan had each polling round check the cancellation. The pinned SDK reads an Attribute from the
invocation's own writes or from the values the invocation was started with
(`getAttributeValue`, `sdk-go@v1.10.1/dex/invocation.go:493-533`), so a polling attempt never sees
a cancellation that an RPC committed after the attempt began. Reading the Flow's own snapshot
through its client from the round would be a self-RPC. So the RPC that admits a cancellation ends
the Steps that may be running: its result cancels them (`RPCResult.CancelSteps`,
`dex/rpc.go:119-137`, resolved after the RPC's writes commit and before its `NextSteps` are
queued) and schedules the Step that settles the cancellation. This is the pattern of PreviewFlow's
`EndPreview` (`preview_flow.go:242`) and SessionAgentFlow's interrupt (`session_agent_flow.go:305`).
The cancelled set always contains every Step the Flow's one chain can be in at that moment, so
exactly one chain continues. Where the RPC does not preempt, the next Step boundary routes the
cancellation: a Step attempt that starts later reads the committed value.

### ProjectReleaseFlow: build cancellation

#### Identity and execution lifecycle

- Unchanged: Flow type `ProjectReleaseFlow`, FlowID `release-<projectID>-<commit>`, for example
  `release-a7k2-083ed9fdc1e8f7f039e2f03e092e6e8894f3ce00`; the business IDs (Project ID and main
  commit), the Start RequestID, `IDReuseDisallow`, and the build revision ID `build-<UUIDv4>`
  chosen by the caller of `StartReleaseBuild`.
- New business end of a build revision: `CANCELLED`. The Release stays open: a CANCELLED build is
  not engine completion, and only Project deletion completes the Release, with the retention of
  today. The Release's `ReleaseStatus` mirrors its last build, as it does for FAILED.
- Duplicate and retried requests: a cancellation is admitted once per build revision. Every later
  request, with the same RequestID or another, is answered with success and changes nothing; the
  first RequestID and actor stay. No cancellation creates a FlowID or Run.
- A new build after a cancellation is a new build revision ID in the same Flow, admitted as after
  a failure: `PreviousBuildRevisionID` names the cancelled build.
- Child deployment: a build starts its automatic deployment only after its artifacts commit, which
  an admitted cancellation precedes, so a cancelled build never has one. A build whose automatic
  deployment may be admitted is refused instead, naming the deployment to cancel
  (`deploy-<projectID>-<build UUID>`, the UUID without `build-`).

#### Attributes and AttributeMaps

`ReleaseBuildRevisions` stays the one AttributeMap, keyed by the build revision ID (the caller's
`build-<UUIDv4>`, `build.RevisionIDPattern`), for example
`build-550e8400-e29b-41d4-a716-446655440000`. The cancellation is a field of that instance's value
`ReleaseBuildRevision` (`project_release_flow_state.go:163-194`), not an Attribute or map of its
own:

```go
// BuildCancellation is a person's admitted request to cancel the build; it is set once.
type BuildCancellation struct {
    RequestID, ActorAccountID string
    RequestedAt               time.Time
    // Phase is Operation.Phase when the request was admitted.
    Phase string
    // SettledAt is when the build ended CANCELLED; zero while it is being cancelled.
    SettledAt time.Time `json:",omitzero"`
}

// ReleaseBuildRevision gains:
Cancellation *BuildCancellation `json:",omitempty"`
```

- **Owner and locks.** `RequestReleaseBuildCancellation` writes it once, and
  `CommitReleaseBuildRevision` sets `SettledAt`; both hold the Release `Revision` lock and the
  instance's lock. The RPC loads and locks only the requested instance, through
  `InvokeRPCWithOptions` as `RecoverBuildRevision` does; no read hydrates the whole map.
- **Status and phase.** `Cancelled Status = "CANCELLED"` joins the one status set of a Release, its
  source and its builds (`project_release_flow_state.go:27-36`). `phaseCancel =
  "CancelReleaseBuildRevision"` joins the persisted build phases (`:58-65`); the RPC sets it, so a
  resumed build resumes the cancellation.
- **Read model.** The field syncs to `release_build_revisions.cancellation` (migration
  `0009_publish_cancellation.sql`). It is not indexed and no query filters on it. It holds no
  secret.
- **Growth and retention.** Fixed size per entry, retained with the entry until the Release is
  removed. A cancelled build counts toward the map's cardinality as a failed build does.
- **Not to be confused with** `CancellationRequested`, which stays the Release's Project cleanup
  fence.
- **`PreparedSource`** loses its seed fields ([below](#configuration-host-no-flow)).

#### Channels and ChannelMaps

None changed. `ReleaseBuildCommands` gets no new kind, because a cancellation preempts instead of
sending a command. A `BUILD` command still pending for a cancelled build is consumed once the
cancellation has settled, because the RPC cancels the waiting `AwaitReleaseBuildCommands` and the
settling commit waits there again, and `ResumeReleaseBuildRevision` then ignores it because the
build is no longer PENDING or RECOVERING.
ChannelMaps: None.

#### Streams

None added. `BuildProgress` gains frames whose Phase is `CancelReleaseBuildRevision`: one when the
Step starts, one when its pending stage changes and the commit's. The stream stays best effort with
its shared 10 MiB budget; the build revision (status, `cancellation`, `provider_settled`) is the
durable record that WatchRelease's snapshot recovers.

#### Steps, RPCs and recovery

**RPC `RequestReleaseBuildCancellation(BuildRevisionCancellation{BuildRevisionID, RequestID,
ActorAccountID})`** returns `buildRevisionResult`. New. Options `dexflow.ShortRPC(dex.LockAttribute(Revision))`;
the client loads and locks the instance. Its caller is the new
`ProjectReleaseFlowClient.CancelBuildRevision`, behind `ReleaseService.CancelReleaseBuild`, which
validates the build ID and authorizes the actor with the build capability on the READY Project
(`readyProject`, ProjectFlow's current state). Admission, in this order:

| Condition | Answer |
|---|---|
| No entry | `NOT_FOUND` |
| `Cancellation` already set | accepted, unchanged |
| `CancellationRequested` (Project deletion) | `FAILED_PRECONDITION`: its cleanup cancels the build |
| READY with `AutoDeploy`, or in phase `DeployReleaseBuildRevision` | `FAILED_PRECONDITION`, reason `RELEASE_BUILD_DEPLOYING`, naming the deployment to cancel |
| Not the active build, or it ended: FAILED, or READY without automatic deployment | `FAILED_PRECONDITION`, reason `RELEASE_BUILD_NOT_CANCELLABLE` |
| PENDING, BUILDING or RECOVERING in phase Freeze, Inspect, Start, Await or Publish | admitted |

An admitted request sets `Cancellation`, `Operation.Phase = phaseCancel` and the entry's
`Revision`, bumps the Release `Revision`, and returns `NextSteps` with `CancelReleaseBuildRevision`
for the build (with `releaseBuildOptions`: the entry loaded, `RecordReleaseBuildFailure` as the
failure route) and `CancelSteps` of `ResumeReleaseBuildRevision`,
`FreezeReleaseBuildConfiguration`, `InspectReleaseBuildRevision`, `StartReleaseBuildRevision`,
`AwaitReleaseBuildRevision`, `PublishReleaseBuildRevision`, `CommitReleaseBuildRevision`,
`RecordReleaseBuildFailure`, `NotifyReleaseFailure`, `AbandonReleaseFailureNotification` and
`AwaitReleaseBuildCommands`. These are all the Steps the chain can be in while the build is
admissible; Deploy cannot run before the artifacts commit, and the source and cleanup Steps cannot
run while a build is active and the Project is not deleting. A cancelled `NotifyReleaseFailure` is
an earlier failure's best-effort notice, which the Release keeps. The build's status stays until it
settles; Studio shows "Cancelling…" from `cancellation` without status CANCELLED.

**The race with publication.** The RPC and every commit of a build phase hold the Release
`Revision` lock and the instance's lock. A cancellation committed first cancels the queued
`CommitReleaseBuildRevision`, also the one that would record the artifacts, so a cancelled build
never becomes READY or the Release's `ReadyBuildRevisionID`. A commit first moves the phase or
status, and the RPC answers by the table. the current contract lets a build be cancelled "until its
deployment is admitted". `DeployReleaseBuildRevision` admits it right after the artifacts commit,
and a call in flight there cannot be observed from the Release's RPC without reading another Flow,
so the build's window ends at the artifact commit; from then the refusal names the deployment,
which `CancelDeployment` cancels as soon as it exists, a few seconds later.

**Step `CancelReleaseBuildRevision`** (new, group publishing): stops the provider build and waits
until it settles. `dexflow.PollingStepOptions(buildCancelWaitLimit, ...)` with
`buildCancelWaitLimit = 15 * time.Minute` (new), failure route `RecordReleaseBuildFailure` with
`releaseBuildCommitOptions`; it holds no lock.

- With `CancellationRequested` it moves to `CleanupReleasePublishing`, which cancels the build in
  its rounds (`cleanupRelease`, `project_release_flow_phases.go:317-345`).
- A build without `Configuration` never reached the provider, as `fail` already reasons, and
  settles without a provider call.
- Otherwise each round, every 5 seconds within 40, calls `Builder.Cancel` for the build's
  `revisionRequest`. Both builders read their record before they stop anything: the hosted one
  reads the admission head and the CodeBuild build, writes a cancelled head when nothing was
  admitted, so that a late Start of the same attempt is refused, and calls `StopBuild` only for a
  running build (`hosted/builder.go:644-688`); the local fixture inspects under its lock and
  cancels only a live job (`local/builder.go:51-64`). `build.ErrPending` keeps polling; nil ends
  the wait, and one `Inspect`, under `dexflow.KeepAlive`, reads the provider's final phases.
- A new typed error in the parent package, `build.ErrCancelUnconfirmed`, which both builders wrap
  when the provider cannot confirm that the stopped build ended (hosted: `cancelBuild` finding an
  UNKNOWN build, `hosted/builder.go:677-679`, not a failed provider call), is a decision: the build settles CANCELLED with
  `ProviderSettled = false` and the failure `BUILD_CANCEL_UNCONFIRMED` (owner platform, remedy
  contact the operator). Any other error returns through `dex.ErrorWithStack` for the retry policy.
- The result goes to `CommitReleaseBuildRevision` as `releaseBuildPhaseResult{Phase: phaseCancel,
  Observation, Unconfirmed}`.

**Changed Steps.**

- `CommitReleaseBuildRevision`: `advance` handles `phaseCancel`: status CANCELLED,
  `ProviderSettled` true unless unconfirmed, the provider's phases as `Timings`,
  `Cancellation.SettledAt`, next phase none. Outside Project deletion it sets `ReleaseStatus`
  CANCELLED and `Failure` to nil or the unconfirmed failure, then waits in
  `AwaitReleaseBuildCommands`. `ActiveBuildRevisionID` keeps the cancelled build. The Session is
  told nothing: a person asked for it, and Tools ▸ Publishing shows the outcome.
- `RecordReleaseBuildFailure`: when the failed Step is `CancelReleaseBuildRevision` (its attempts
  were exhausted or its wait limit passed) it settles the build CANCELLED unconfirmed with the
  cause, "The build provider did not confirm within 15 minutes that the build stopped", and tells
  nobody.
- Start checks: `ResumeReleaseBuildRevision` resumes `phaseCancel` at `CancelReleaseBuildRevision`;
  Freeze, Inspect, Start, Await, Publish and Deploy move a build with `Cancellation` there before
  any provider call, beside their `CancellationRequested` check (`project_release_flow.go:628`,
  `:658`, `:684`, `:723`, `:767`, `:793`). With preemption, only a stale wakeup, a recovery or an
  open execution reaches these checks.
- Build admission (`RequestReleaseBuildRevision`, `:306-310`): the active build may be superseded
  when it is FAILED or CANCELLED and `ProviderSettled`. An unconfirmed cancellation stays fenced,
  as a `BUILD_UNKNOWN` build is.
- Recovery: `recoverable` also admits CANCELLED with `ProviderSettled = false`, and
  `admitRecovery` sets `phaseCancel` with the attempt unchanged, so `RecoverReleaseBuild` runs the
  cancellation again for a provider that answers later.

**Error, cancellation and cleanup paths.** The Release owns the cancellation until the build is
CANCELLED. Project deletion stays admissible meanwhile: the next boundary moves to
`CleanupReleasePublishing`, which cancels again (idempotent) and removes every object of the
Release. Work a provider keeps running after an unconfirmed cancellation is bounded by the
provider's own limit (CodeBuild's 45-minute build timeout) and fenced by Release cleanup. Objects
written by a preempted Freeze or Publish (a frozen copy, part of the build's artifacts) are
immutable, keyed by the build, and removed with the Release or the Project's Live prefix.

**Recovery after a Worker restart.** The cancellation and the phase are durable. A restarted
`CancelReleaseBuildRevision` attempt calls `Builder.Cancel` again, which is idempotent and
answers from the provider's record, and the commit runs once under the locks.

#### Draft seed removal and build admission



- `PreparedReleaseSource` loses `SeedDeploymentID` and `SeedBuildRevisionID`
  (`project_release_flow_state.go:128`), and its comment loses "one-time draft seed".
- `ProjectReleaseFlowDependency.RegisterReleaseSource` (`project_release_flow.go:67-69`) keeps its
  signature but no longer seeds. `PublishingFlowService.RegisterReleaseSource`
  (`publishing_flow_service.go:72-97`) stops reading the Live deployment, so source preparation no
  longer fails when that deployment is not READY at that moment.
  `projectconfig.PreparedSourceSeed`, the seed parameter of `RegisterPreparedSource` and
  `seedCommitDraft` go; the publish copy replaces the seed
  ([publishing freezes a copy](configuration-drafts.md#publishing-freezes-a-copy)).
- `CheckReleaseDraft` (`:74-76`) keeps its signature: `ErrConfigurationStale` when current Live,
  Preview's applied version or the layer moved since the copy was built; `ErrConfigurationConflict`
  goes. Admission keeps its order: replay first, then the Preview validation, then this check. The
  freeze is unchanged.
- Open executions: Dex decodes Attribute values with `encoding/json`, which ignores the old keys
  (`sdk-go@v1.10.1/dex/value.go:320-328`); the synced `releases.prepared_source` keeps them until the
  Release's next write rewrites the row.

#### Compatibility for open executions

The RPC and the Step are additive, and no Step or RPC is renamed. An entry without `Cancellation`
decodes nil and its column stays null. A build in flight when  shipped can be cancelled; its
entry gains the field on that write. No open execution holds `CANCELLED`, which nothing has
produced since the current contract. `docs/flows` is regenerated.

#### Tests

Real-dependency Dex integration tests in `internal/dexai/agents`, next to
`live_retirement_integration_test.go` (real Dex, Postgres, MinIO, the local build service and the
lane's Live runtime, the mock model only), and in `internal/dexai/delivery/releases` for the builder:

- **Cancel before the provider.** Cancel right after `StartReleaseBuild`: CANCELLED with
  `provider_settled`, the provider holds nothing for the build, and a new build with
  `PreviousBuildRevisionID` set to it is admitted.
- **Cancel during the build.** Cancel while `AwaitReleaseBuildRevision` polls: the provider job
  stops and the build ends CANCELLED once, without artifacts or `ReadyBuildRevisionID`.
- **At publication.** A cancellation racing the artifact commit gives exactly one outcome: CANCELLED
  without artifacts recorded, or READY with the cancellation refused.
- **Replay and concurrent admission.** The same and a different RequestID after admission leave one
  `Cancellation` with the first RequestID. `StartReleaseBuild` while cancelling is refused, and
  admitted once CANCELLED.
- **Refusals.** After the automatic deployment is admitted, `RELEASE_BUILD_DEPLOYING` names it, and
  `CancelDeployment` of that ID succeeds; READY builds without automatic deployment and FAILED
  builds are `RELEASE_BUILD_NOT_CANCELLABLE`, and a repeated request for a CANCELLED build is
  accepted unchanged.
- **Restart.** The Worker restarts while `CancelReleaseBuildRevision` polls: one CANCELLED commit.
- **Project deletion** during a cancellation ends the Release REMOVED.
- **Retention and sync.** The entry and its `cancellation` survive until removal and reach
  `release_build_revisions.cancellation`.
- **Native confirms.** A build cancelled while it runs on Native settles CANCELLED with
  `ProviderSettled` true, and a new build of that commit is admitted: the local builder runs each
  job's processes under the launcher's group, stops them and records CANCELLED once every process
  it observed for the job is gone (see the settled points below).

### ProjectDeployFlow: deployment cancellation

#### Identity and execution lifecycle

- Unchanged: Flow type `ProjectDeployFlow`, FlowID `deploy-<projectID>-<deploymentID>`, for example
  `deploy-a7k2-b51c0b8e`; the business IDs, the Start RequestID, `IDReuseDisallow`, the lease and
  its generation.
- New business end: `CANCELLED`, after `CANCELLING`, which now means that the Project's deletion or
  a person cancels the deployment. CANCELLED is a business completion: the Flow completes
  gracefully with the CANCELLED snapshot, as at RETIRED, and engine retention starts there.
  `GetProjectDeployment` stays a retained read.
- Duplicate and retried requests: a repeated request after admission is answered with success. A
  new RequestID for a deployment whose cancellation cleanup FAILED admits that cleanup again, as
  `RequestProjectCleanup` admits a failed Project cleanup again. `RequestLiveDeployment` keeps
  answering an already accepted receipt first, and refuses a new one for a cancelled deployment;
  the client's resume refuses CANCELLED as it refuses RETIRED. Another attempt uses a new
  deployment ID; no cancellation creates a FlowID or Run.
- Ownership: the cancelled deployment owns its generation's objects and grant until its cleanup
  removes them, and releases its Project lease with the existing `ReleaseProjectDeployment`.
  `RemoveForProjectCleanup` answers CANCELLED as cleaned up without an RPC, as it answers RETIRED.

#### Attributes and AttributeMaps

| Attribute | Value type | Writers | Notes |
|---|---|---|---|
| `Cancellation` (new) | `*DeploymentCancellation{RequestID, ActorAccountID string; RequestedAt time.Time; Phase string}`, nil until a person cancels | `RequestDeploymentCancellation`, under the `Revision` lock | `Phase` is `ProviderRefs.Phase` at the first admission; `RequestID` is the latest admitted request. `dex.SyncToAttributeStore()` to `deployments.cancellation`; not indexed; no secret |
| `DeploymentStatus` | adds `Cancelled Status = "CANCELLED"` | `CommitDeploymentPhase` | keyword3 index, so `ListDeployments` filters it; `Cancelling` also covers a person's cancellation |

- An ordinary Attribute, not an AttributeMap; AttributeMaps: None. It is read whole by
  `GetProjectDeployment` with `dexflow.Optional`, because open executions lack it, and is
  registered in `GetPersistenceSchema` and `initialAttributes` (nil).
- Fixed size, retained for the deployment's lifetime. `CleanupRequested` stays the Project cleanup
  fence and is never set by a person's cancellation.

#### Channels and ChannelMaps

- `DeploymentRecoveryDecisions` is unchanged: no new producer, since the RPC preempts a FAILED
  deployment's wait instead of waking it. A recovery wakeup pending when a RECOVERING deployment
  is cancelled is consumed by `AwaitDeploymentRecovery`, which then routes to cleanup. A wakeup
  still pending at completion goes with the execution, as for Project cleanup today.
- `LivePreparationResults` is unchanged. A preparation round is never preempted, so its join still
  consumes both results under `prepare-<generation>-<round>`, and no stale result exists.

#### Streams

None changed. `DeploymentProgress` carries `CleanupCancelledDeployment` and
`ReleaseProjectDeployment` frames for a cancelled deployment, best effort; the snapshot (status,
`cancellation`, phase timings) recovers them.

#### Steps, RPCs and recovery

**RPC `RequestDeploymentCancellation(cancellationRequest{RequestID, ActorAccountID})`** returns
`mutationResult`. New; options `dexflow.ShortRPC(dex.LockAttribute(Revision))`. Its caller is the
new `ProjectDeployFlowClient.Cancel`, behind `DeploymentService.CancelDeployment`, which validates
the deployment ID and authorizes the actor with the build capability on the READY Project, as
`admit` does for a start. Admission, in this order:

| Condition | Answer |
|---|---|
| `Cancellation` set, and the same RequestID or status CANCELLING or CANCELLED | accepted, unchanged |
| `Cancellation` set and FAILED (its cleanup failed), new RequestID | admitted again |
| `CleanupRequested` | `FAILED_PRECONDITION`: the Project's deletion removes it |
| READY, RETIRING, RETIRED or REMOVED, or in any status in phase `CommitCurrentLive`, `RequestReplacedRetirement`, `ReleaseProjectDeployment` or `RetireReplacedGeneration` | `FAILED_PRECONDITION`, reason `DEPLOYMENT_NOT_CANCELLABLE` |
| PENDING, DEPLOYING, RECOVERING or FAILED in a phase from `AdmitDeployment` to `VerifyReadiness` | admitted |

An admitted request sets `Cancellation`, status CANCELLING and bumps `Revision`. Then:

- In phase `StartLiveDex`, `ApplyApplication` or `VerifyReadiness`, or when FAILED, the result
  returns `CancelSteps` of `StartLiveDex`, `ApplyApplication`, `VerifyReadiness`,
  `RecordDeploymentFailure`, `CommitDeploymentPhase`, `NotifyDeploymentFailure`,
  `AbandonDeploymentFailureNotification` and `AwaitDeploymentRecovery`, every Step the chain can be
  in there, and `NextSteps` with `CleanupCancelledDeployment`. The polling of these three Steps
  stops at once.
- In an earlier phase, PENDING, DEPLOYING or RECOVERING, it does nothing more. The running short
  Step or the preparation round finishes, and the next boundary routes to cleanup.

**The race with the commit.** `CommitDeploymentPhase` holds the `Revision` lock when it commits
`VerifyReadiness`'s result and chooses `CommitCurrentLive`, and so does the RPC. A cancellation
committed first cancels that queued commit; a commit first sets phase `CommitCurrentLive`, and the
RPC refuses. Studio disables Cancel once `VerifyReadiness` has a timing, which that same commit
records.

**Why preparation waits.** The preparation branches mint external identities, hosted the Temporal
Cloud namespace and its API key, which each round hands to the Project before it ends, and the join
consumes exactly two results per round. A cancellation during preparation waits for the round, at
most the branches' 20 and 15 minute wait limits; `PrepareLiveExecution` starts no round for a
cancelled deployment. The three preempted Steps are safe to interrupt: `ApplyApplication` and
`VerifyReadiness` act only on the generation, which cleanup removes by its deterministic
`app-<deploymentID>-<generation>` name, deleting only the UID it reads; `StartLiveDex` acts on the
Project's Live Dex, whose deterministic identity the next `StartLiveDex` reads and adopts again,
so an interrupted round leaves nothing without an owner.

**Changed Steps.**

- Start checks: `VerifyReleaseAndConfiguration`, `PublishDefinitions`, `PrepareLiveExecution`,
  `StartLiveDex`, `ApplyApplication` and `VerifyReadiness` treat `Cancellation` as they treat
  `CleanupRequested`, returning `resultCleanupRequested` (`CancellationBoundary`) without their
  effect. `AcquireProjectDeployment` does so only on its first attempt (`ctx.Attempt() == 1`): a
  later attempt may follow an acquire whose answer was lost, so it completes the idempotent
  acquire (same DeploymentID and ExpectedLiveRevision, `project_flow.go:322-325`), and its commit
  records the lease that cleanup releases. `AdmitDeployment` registers the deployment regardless,
  because Project deletion must find every deployment that may hold a lease.
- `CommitDeploymentPhase`: `ProviderRefs.advance` gets `CleanupRequested || Cancellation != nil`,
  so every committed phase continues with cleanup; the status stays CANCELLING instead of
  DEPLOYING. A failure of a forward phase after a cancellation is moot: the commit keeps what the
  phase recorded and its timing, records no Failure and continues with cleanup. A failure of
  `CleanupCancelledDeployment` or `ReleaseProjectDeployment` commits FAILED, tells nobody, and
  waits in `AwaitDeploymentRecovery` for a new cancellation request or the Project's deletion. The
  commit of `ReleaseProjectDeployment` ends REMOVED for Project cleanup, CANCELLED with Failure nil
  and `GracefulComplete` for a cancellation, and READY otherwise.
- `AwaitDeploymentRecovery`: after the `CleanupRequested` case, a deployment with `Cancellation`
  and status CANCELLING moves to `CleanupCancelledDeployment`.
- `CleanupCancelledDeployment`: the before-provider-effects branch (`project_deploy_flow_phases.go:459-462`)
  admits the deleting Project's fence, as today, or a deployment with `Cancellation`, whose
  Project may be READY. Either way it reconciles only the exact lease tuple of this deployment,
  recovering from `Project.ActiveDeployment` a lease whose acquire answer was lost. After provider
  effects it is unchanged: `RevokeWorkload`, then `Serving.CleanupDeployment` of this generation.
  The check that the deployment is not the current Live (`:456-458`) stays, and always holds,
  because cancellation is refused from `CommitCurrentLive` on. Its explanation reads "of a deleting
  Project or a cancelled deployment".
- Trigger registrations: `PrepareLiveAccess` calls `RetainTriggerRegistrations` with the new
  version's selections (`project_deploy_flow_phases.go:263`), which deletes the Live scope's Slack
  trigger registrations whose name no longer selects the registered connection
  (`connectiondao/slack_relay.go:189-200`). An application registers only at its start, so after
  a cancelled or failed deployment the running Live would stop receiving those events. Now
  `PrepareLiveAccess` keeps the registrations that the new version or the current Live's pinned
  configuration selects, and `CommitCurrentLive`'s round, once the Live pointer names this
  generation, keeps only the new version's (idempotent; a retried round repeats it).
- `ProjectDeployFlowClient.start`: an active lease held by a CANCELLED deployment, which only an
  acquire admitted after its cleanup read can leave, is released with its exact tuple before
  the new deployment is admitted. That deployment ran no provider effect after the lease, so
  nothing else remains.

**What cancellation never touches.** The current Live: its generation, grant, route, Live Worker
endpoint and Flows. The Project's Live execution environment: the Temporal namespace and
credential and the Live Dex, which preparation and `StartLiveDex` may have prepared and handed to
the Project, and which the current Live shares. The Live connection usages: `EnsureConnectionUsages`
only adds, so usages a cancelled deployment added stay until "release unused connections". The
Flow Definitions it published under its build's key in project storage, immutable and removed with
the Project.

**Interactions with the current contract.**

- `Activate`, inside `CommitCurrentLive`, moves the stable Live Worker endpoint and the public
  route to the new generation before the Live pointer commits, and `CleanupDeployment` never moves
  them back; a candidate that failed in that phase keeps the lease until it resumes or the Project
  is deleted. So cancellation is refused from `CommitCurrentLive` on, a FAILED deployment in that
  phase included.
- A cancelled deployment never sends `RequestLiveRetirement`, which follows the commit, so the
  generation it would have replaced stays READY. The `ReplacedLive` it recorded stays as data.
- RETIRING and RETIRED generations refuse cancellation: their lifecycle is retirement.
- A cancelled deployment never activated, so its `DeploymentResources.Worker` is empty and a
  retirement request would answer it unchanged; it needs no drain window, because no Live traffic
  or Live Flow ever reached it. Retirements in progress go on independently.

**Recovery after a Worker restart.** The cancellation is durable. A restarted cleanup attempt
repeats its reads and finds what is already gone; the commits run once under the `Revision` lock.

#### Compatibility for open executions

The RPC is additive and no Step is added or renamed. `Cancellation` is absent in open executions
and reads nil, so their column stays null. No open execution holds `CANCELLED`, which nothing has
produced since the current contract. `docs/flows` is regenerated.

#### Tests

Real-dependency Dex integration tests in `internal/dexai/agents` on the lane's real Live runtime:

- **Before the lease.** Cancel right after `StartDeployment`: CANCELLED, no lease, no generation
  object, and Project deletion acknowledges the deployment.
- **During `VerifyReadiness` with a current Live** (a generation that never becomes ready): the
  previous Live serves its URL throughout, stays READY and not RETIRING, keeps its grant, its
  Worker endpoint and its Slack trigger registrations; the cancelled generation's objects and
  grant are gone, the lease is released and a new deployment is admitted.
- **During preparation.** Cleanup starts only after both branch results; the Project records the
  execution references.
- **The race.** Concurrent `CancelDeployment` calls against the commit end either CANCELLED with
  Live unchanged, or READY with every cancellation refused `DEPLOYMENT_NOT_CANCELLABLE`.
- **FAILED.** A deployment failed in `ApplyApplication` is cancelled and releases its lease; one
  failed in `CommitCurrentLive` is refused.
- **Replay.** The same RequestID twice; `StartDeployment` of a cancelled deployment ID is refused.
- **Restart.** The Worker restarts during `CleanupCancelledDeployment`: one CANCELLED completion.
- **Project deletion** during CANCELLING ends REMOVED; after CANCELLED it is acknowledged without an
  RPC.
- **Sync.** `deployments.cancellation` and `deployment_status` reach the read model.

### API additions

These are business API RPCs, not Flow RPCs; each reaches a Flow only through its client.

| RPC | Flow use | Authorization |
|---|---|---|
| `ReleaseService.CancelReleaseBuild(project_id, release_id, build_revision_id, request_id)` returns `{}` | `ProjectReleaseFlowClient.CancelBuildRevision` invokes `RequestReleaseBuildCancellation` | build capability on the READY Project |
| `ReleaseService.GetReleaseBuildLog(project_id, release_id, build_revision_id, cursor)` returns `{lines, next_cursor, complete}` | retained reads `GetProjectRelease` and `GetReleaseBuildRevision` for the build's provider identity; no Flow write | build capability (read model) |
| `DeploymentService.CancelDeployment(project_id, deployment_id, request_id)` returns `{}` | `ProjectDeployFlowClient.Cancel` invokes `RequestDeploymentCancellation` | build capability on the READY Project |

- **Counts.** ReleaseService had 9 RPCs;  removed `ConfirmLiveConfigurationItems` (8, ), and
   adds two, reaching 10, the cap: a further ReleaseService RPC needs a split. DeploymentService
  goes from 4 to 5. The names are unique across services.
- **Statuses and fields.** `RELEASE_STATUS_CANCELLED = 9` and `DEPLOYMENT_STATUS_CANCELLED = 10`
  (`make check-proto-enums`); the comment of `DEPLOYMENT_STATUS_CANCELLING` covers a person's
  cancellation. A new message `Cancellation{actor_account_id, requested_at}` is the field
  `cancellation` of `ReleaseBuildRevisionSnapshot` (17) and `DeploymentSnapshot` (14), mapped from
  the read model's columns. `EnvironmentEvent.Kind` gains `KIND_PUBLISH_CANCELLED = 6`, derived from
  CANCELLED build and deployment rows.
- **Reasons.** `RELEASE_BUILD_NOT_CANCELLABLE`, `RELEASE_BUILD_DEPLOYING` and
  `DEPLOYMENT_NOT_CANCELLABLE` join `internal/dexai/api/boundary/error_reasons.go`, each with its remedy,
  and `RELEASE_CONFIGURATION_CONFLICT` is gone.
- **The log read.** `Builder.ReadLog(ctx, request, cursor)` returns whole lines, at most 64 KiB: the
  hosted builder reads CloudWatch `GetLogEvents` from the head with `nextForwardToken` as the
  cursor, the local one a byte offset. The cursor also names the build's attempt, so a recovered
  build restarts at its new log. The log group and stream always come from the build's admission
  record, never from the cursor, so a forged cursor can only fail. `LogTail` becomes a read from
  the end. Each page is redacted on the server with the build's `ConfiguredValues` and the
  Project's infrastructure names; the redactor moves from `internal/dexai/agents/diagnostics`
  (`project_diagnostics.go:120-200`) into `internal/dexai/projects/config`'s existing
  `configured_values.go` as `OutputRedaction`, so the package keeps nine files. Nothing is stored and secret values are held only for one redaction, so each read costs two
  retained Flow reads, the frozen configuration's values and one provider log read. The local
  fixture keeps only the last 256 KiB of a job log (`MAX_JOB_LOG`), so its offsets count from the
  log's start and a cursor before the kept window resumes at its start.
- **Quota.** CloudWatch Logs `GetLogEvents` allows 10 transactions per second per account in
  us-west-2, the hosted region (`infra-as-code/terraform/cloud/variables.tf:11`), and is not
  adjustable (AWS "CloudWatch Logs quotas", checked 2026-10-13). At one read every 2 seconds per
  open log view, about 20 views at once exhaust an environment's account, Agent log tails included.
  The log is best effort and the build snapshot is authoritative, so reads slow down instead of
  failing: each replica makes one read per build and cursor that all its viewers share, holds a
  token bucket for log reads (`builds.logReadsPerSecond`, two per second per replica by default,
  0.1 to 10, burst 4), backs off from 4 to 60 seconds on throttling, and answers with
  `retry_after_ms`, the time of the next useful read, which Studio waits for. With R replicas and
  V builds viewed at once, spread over the replicas, a view refreshes about every
  max(2, V / (2 · R)) seconds: 40 viewed builds on three replicas refresh about every 7 seconds.
  The account then sees about 2R reads per second, 4R in a burst, so the default holds up to five
  replicas (two in a burst); with more, `builds.logReadsPerSecond` drops to at most 10 / (2R).
  100 builds viewed at once would want 50 reads per second, five times the quota, and refresh
  every 25 seconds on three replicas; another log transport is the owner's call before hosted
  rollout at that scale.

### Settled points

- **Native confirms a cancellation.** The Native build fixture used to report a cancelled running
  build UNKNOWN, because it could not prove that no reparented descendant survived
  (`server.py:310-316`). It now stops the job's processes and records CANCELLED once every process it
  observed for the job is gone, so `local.Builder.Cancel` is confirmed as the hosted one is. The jobs
  stay in the launcher's process group, because `sv2 native stop` and `recover` prove shutdown by an
  empty launcher group (`infra-as-code/scripts/native/cli.py:330-358`), which a job in a group of its
  own would escape. `build.ErrCancelUnconfirmed` remains for a provider that genuinely cannot say.
- **A preempted Apply.** A deployment cancelled while `ApplyApplication` runs records
  `Cancellation.Preempted`: the Step stops only at its next heartbeat, so its round in progress may
  still create the generation's objects or reissue its grant. Cleanup repeats the revocation and
  removal in every round until the heartbeat timeout plus one interval and one round (1m43s) has
  passed since the request.
- **The remedy of `RELEASE_BUILD_DEPLOYING`** is a new `FailureRemedy` value, cancel the
  deployment, with the deployment ID in the ErrorInfo.
- **The log quota** is bounded as above.
