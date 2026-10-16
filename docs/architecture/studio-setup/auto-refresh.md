# Automatic process builds

Every successful Session push schedules a process build immediately.
Preview refresh is asynchronous and latest-wins: the pre-push source gate
already compiles Go, renders strict FDG and runs the application's declared API tests before
publication (`workspace/git/commit_source_checks.go`, `verifyApplicationSource`). Post-push
building must not block the Agent. Direct external/shell pushes are outside this Session event.

## Dex Flow Changes

### Identity and execution lifecycle

The complete unchanged inventories and execution lifecycles are the
[Preview contract](../../../internal/dexai/agents/sandbox/preview/README.md) and
[Session contract](../../../internal/dexai/agents/session/README.md), with
[Session creation](../../../internal/dexai/agents/README.md#session-start) and the
[Setup Flow deltas](flow-changes.md). This document changes only the following paths.

`SessionAgentFlow`, `session-<projectID>` (example `session-a7k2`), is still started once by
Project readiness with the fingerprint of `{ProjectID}` and `IDReuseDisallow`.
`PreviewFlow`, `preview-<projectID>` (example `preview-a7k2`), is the independent Project-owned
execution started by its first command/handoff with the same fingerprint/reuse policy.
No new execution or Run is created for a push, duplicate, retry or manual rebuild. Business
stop/archive remains distinct from engine completion; Project deletion completes Preview and
retains engine snapshots under existing retention. Relationships/ownership are unchanged.

`ExecuteTool` commits a pushed, successful, unblocked result and schedules `DeployPreviewHandoff`
alongside `CheckSteered`, instead of at turn end. Its identity is the result sequence:
`deploy-preview-137` for sequence 137, pinned to the verified commit and accepted workspace.
Repeated pushes of the same accepted commit add no handoff. Off consumes the result without
scheduling it; enabling later applies to future pushes. Stop preserves the scheduled build.
Manual rebuilding uses a fresh request UUID; uncertain acceptance retains that UUID.

### Attributes and AttributeMaps

No new primitives. Session owns `PreviewAutoDeploy` (`bool`, default true) as the sole preference;
its existing RPC `UpdatePreviewAutoDeploy` locks SessionStatus and this Attribute. The existing
ordinary `PreviewHandoff` struct (`Commit string`, `Sequence` and `PublishedSequence` as
`conversation.Sequence`) is submission identity, not build state. It is published in the result's
durable decision. No new indexed field, AttributeMap, instance key, selective load or CAS rule.
All unaffected declarations and map-key/loading rules remain those of the baseline contracts.

Preview owns `PreparedPreviewSource` (the existing struct of status, request/instance/generation,
commit, exact definitions reference/revision, safe failure and `repohost.CommitVersion`). Automatic
admission records PREPARING with its command/commit. ReadPreviewSourceVersion commits the Git-owned version in a short metadata Step
before the compilation Step; sdk-go v1.5.0 stages Attribute.Set until invocation success. Thus an
in-Step save alone cannot expose an intermediate building version; successful generation atomically saves READY and its matching `AppManifest`.
Build failure records FAILED only while this command's preparation remains PREPARING. Later
configuration/refusal failures do not invalidate READY processes. `PreviewState` independently
owns the serving version. READY process rendering is gated by its immutable commit bundle,
not the current workspace identity. Acquiring a replacement rebinds that READY preparation
to the acquired instance before a manual start; the serving-runtime instance fences remain
unchanged, and definitions are not regenerated solely because a Sandbox was replaced. A failed build may retain the prior successful AppManifest; it never
claims that manifest belongs to the failed commit. Immutable per-commit bundles remain exact.

`CommandLock` serializes admission/state completion. Provider Steps hold no lock and write only
while their admitted command is active. Queued RPCs modify only `PreviewDeployment`, excluded
from provider saves, preserving its existing single newest request and sequence fence. The
active/last source and command remain fixed-size; `RecentStarts` retains at most 50. No secrets.

### Channels and ChannelMaps

No added/changed declaration or instance key. `PreviewCommands<Command>` keeps at most one
current-command message; producer RPC/end Steps and `AwaitPreviewCommands` consumer are unchanged.
One active command and one newest queued deployment remain authoritative; a newer queued
sequence replaces an older one, duplicates reuse the existing sequence admission rule. No FIFO
is introduced. `WorkspaceSettled<WorkspaceSettlement>` retains command/request fencing, bounded
wait and stale-message cleanup. All other Channels/ChannelMaps retain the baseline contracts.
Project completion cleans pending messages; Agent Stop does not discard an accepted pushed build.
A queued push following an admitted Preview stop/reclamation builds after that command releases
its resources; it is no longer silently cancelled for naming that earlier workspace.

### Streams

Preview: None. Session declarations, producers/consumers and budgets are unchanged:
`ReasoningSummary<string>`, `AssistantText<string>`, `AgentActivity<conversation.AgentEvent>`,
each 10 MiB **shared across instances of the Flow type and Stream name**, verified against
pinned sdk-go v1.5.0 `dex/stream.go` on 2026-10-12. No added Stream writes. Delivery is best
effort, may truncate, and resumes by token; Snapshot and CurrentMessages/ArchivedMessages
recover after disconnect. Streams are never authoritative process-build records.

### Steps, RPCs and recovery

Push result -> `CheckSteered` and `DeployPreviewHandoff` in one decision. The existing handoff
provider Step has 45-second attempts and a three-minute retry budget; admission repeats the
same identity, with Preview sequence admission reconciling lost answers. Exhaustion ends through
`AbandonPreviewHandoff`; it does not imply successful external effects. Authorization remains
the Project's Session, and the preference command still rejects non-ACTIVE Sessions.

Preview admission -> workspace acquisition -> `ReadPreviewSourceVersion` ->
`PreparePreviewSource` (compile/render the pinned archive) -> READY
save -> `VerifyPreviewSource` -> existing configuration/start path. Acquiring now resumes a
reclaimed Session workspace instead of silently skipping the push. Replacement first releases
an earlier instance, preserving the reclamation handshake and workspace-settlement timeout.
The closed Session/deletion path never resumes it. ReadPreviewSourceVersion uses one-minute
heartbeating attempts, two attempts within three minutes, and read-only Project/Git metadata
operations (ValidateReleaseCommit, DescribeCommit), plus workspace inspection for manual builds.
It changes only the existing PreparedPreviewSource version/commit and workspace observation.
A manual HEAD that changes after its name is committed is refused and offers a new explicit build;
no stale name is paired with another commit. Project completion cancels the added Step.
Source compilation retains six-minute
heartbeating provider attempts, two attempts with bounded retry, stable request/commit identity,
manifest digest checks and typed refusal/failure paths. Cleanup/notification owners remain Preview
and SessionWorkspaceFlow; configuration gaps are independent of the built process catalog.

Existing Go Flow/Step identities and persistence fields remain unchanged; ReadPreviewSourceVersion
is the one added default Go Step identity. A previously scheduled manual PreparePreviewSource
without a named commit moves through the metadata Step; an existing pinned compilation continues
with its admitted identity; future results use immediate dispatch. A pre-upgrade
unpublished turn-end handoff is diagnostic historical state, not automatically replayed on
upgrade. Old status/version fields decode as before; no identity override or data migration.
Tests exercise actual Dex decisions, default/off behavior, Stop, latest-wins admission,
configuration refusal, build failure/retry and workspace reclamation. Pinned CLI history verifies
that the version-bearing PREPARING write commits before the compilation completion, rather than
relying on a transient UI frame. Uncertain manual admission is reconciled through the exact command
ID/kind and preparation ID before cleanup, without resending a command; its RPC code/substatus is logged. Existing real-dependency
integration cases continue covering duplicate/uncertain delivery, maps, races and recovery.

## Database Schema Changes

None. No table, counter, mirrored lifecycle or migration. Existing projections carry the same
status/version/preference contracts; Git remains the only version number owner.

## UI/UX

The step is **Build processes**; the preference is **Build each push automatically**. Setup shows
the step while the latest push is not built (building, failed, or waiting for a manual build) and
whenever automatic builds are off, where it holds the manual build; with automatic builds on, a
built push needs none, so it goes away. Healthy automatic builds hide the manual button. Off exposes **Build processes**; FAILED exposes
**Build V12 processes again** (or **Build processes again** before version metadata resolves).
Status reads **Building V12 processes…** / **Processes of V12 ready**, beside **V12 · subject**.
Unresolved metadata names the latest push, never a hash. Configuration gaps remain separate.
The template design remains readable before its first Agent push; turn off automatic builds to
build that initial root manually. The Preview's transport names stay unchanged.

## Tests

Real-dependency Dex cases use the mock model only; Dex, Sandbox, Docker, local Git and storage
remain real. New cases verify immediate handoff from ExecuteTool, processes READY before missing
configuration, its durable named PREPARING state before compilation, and preservation across Stop. Existing cases verify switch/default, duplicate and
newest-queued admission, failure with serving processes preserved and rebuild, and reclaimed
workspace acquisition. A real replacement/read/start case removes only its owned Sandbox,
reads the retained immutable process/configuration bundle, and starts the same version on a
replacement without another PreparePreviewSource execution. The Studio integration spec verifies the real BFF/server path, default-on
READY, versions, hidden manual control, configuration refusal, off/push/manual, dirty-source refusal/retry and re-enable.
Its four ordered inputs use `process-auto-refresh` in one Project, preserving the composer's rule
that source-changing scenarios run only in a new Project or while that same scenario remains unfinished.
Required validation is `make ci`, affected-package tests/specs and the three Native E2E cases;
Kind/AWS acceptance is outside this patch's lane. Observed evidence belongs to its change receipt.

## Documentation

The owning API/Setup and Session/Preview contracts, generated graphs/README summaries and
Studio spec coverage describe the immediate per-push handoff.

## Resource Scalability

New cloud/Kubernetes/Temporal/Dex/database resources: None. No per-push namespace, bucket,
principal, Flow or Run. One existing Project Preview and Session Sandbox are reused; reacquisition
uses the existing per-Session replacement/deletion path. One active build, one queued request and
50 recent starts bound control state, including concurrent Agent pushes. Instances are deleted
by existing reclamation/Project cleanup; engine retention remains the existing 30-day setting.

Existing isolated exports retain at most the prepared tree and the tree in use (workspace
Capabilities.EnsurePreviewSource); replacement/Project cleanup owns the volume. Immutable `projects/<id>/preview-source/<commit>/definitions.json`
bundles are per distinct built commit, not per retry. They are retained until existing workspace
volume/Project-prefix deletion. This patch adds no new object format or quota-limited tenant
resource. The existing per-Project lifetime source-object growth is not independently capped;
that pre-existing storage-retention limitation remains explicit, rather than claiming a new
cleanup policy. Existing shared object storage scales by keys; no tenant-proportional scan or
IAM/EKS/load-balancer/DNS/certificate call is introduced per push or status poll. One added metadata Step adds a bounded completion event and two Git metadata reads per build
(plus one Project row read), on the existing Flow and its existing history-retention policy; it
creates no new resource or business record. These reads are per build, never per status poll. More frequent
post-push builds add Sandbox CPU work; latest-wins bounds queued work while the pre-push gate
continues verifying each commit. No Actions work or quota increase is added. The 10,000 Project /
100,000 deployment target continues using shared environment resources under existing capacity
contracts; no new hard-quota resource or requested quota uplift is involved.
