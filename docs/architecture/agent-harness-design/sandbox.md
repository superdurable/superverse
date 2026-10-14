# Sandbox

The Sandbox is where a Project's Agent runs commands, edits the repository and runs
the Preview. Each Project has one workspace: an isolation boundary (a Kubernetes
namespace; in Local Native a Docker network) with a persistent volume (a PVC; in Local
Native named Docker volumes) that holds the repository checkout and the Preview engine
state. A Pod (in Local Native a container) is started on that volume when the Agent needs
it and reclaimed when it is not used. One Pod lifetime is one **instance**.

Four Flows share the lifecycle. They coordinate through a few RPCs, two ChannelMaps and
one command Channel, never through polling callbacks
(the current contract):

- `SessionAgentFlow` holds the request side: it asks for an instance, holds the accepted
  lease and the Sandbox deadline, lends the instance to the Preview and decides when it is
  idle.
- `SessionWorkspaceFlow`, one execution per Project, owns the instances. It runs the
  Session's `provision` and `reclaim` commands one at a time, so a reclamation and a new
  provisioning never interleave and one volume carries one Pod; it starts each instance's
  provisioning, reclaims Pods, keeps the volume and reports every outcome to the Session.
- `SandboxProvisionFlow` provisions one instance. One execution creates the Pod on the
  Project's volume, checks the repository out, verifies the instance, reports its outcome
  once to the workspace Flow and completes. It has no command, timer or reclamation, and it
  deletes nothing.
- `PreviewFlow` serves the Project's Preview on the instance the Session lends it. One
  execution lives from the first Preview command until the Project is deleted.

This document owns the design of the Sandbox: the workspace lease and its instance
identity, the instance lifecycle (cold path, Sandbox deadline, reclamation and release),
sandboxd and its confinement, the coding image and its helpers, the command ledger and
process limits, the Sandbox-local Preview, cold start, resources and phase timings. The
complete per-Flow contracts of the three instance Flows are in their package READMEs
([below](#dex-flow-changes)). Other topics own the callers: the Session
([session](session.md)) owns the conversation and the Attributes of the request side,
tools reach the Sandbox as described in [tool orchestration](tool-orchestration.md),
subagents use worktrees in the workspace ([subagents](subagents.md)), and Preview outcomes
reach the Session through [notifications](notifications-and-feedback.md).

## Who owns what

| Fact | Owner |
|---|---|
| The request side: the request in flight and how it settled, the accepted lease and its WorkspaceRequestID, the Preview's hold of the instance | `SessionAgentFlow`, Attribute `Workspace` |
| When the Pod is reclaimed | `SessionAgentFlow`, `Workspace.SandboxDeadline` |
| The current instance: its state (`none`, `provisioning`, `ready`, `reclaiming`, `reclaimed`, `failed`), its resources, what its volume holds, the phases of its provisioning, its lease once ready and its last failure | `SessionWorkspaceFlow`, Attribute `Instance` |
| Boundary and volume references that outlive an instance | `SessionWorkspaceFlow`, `Instance.Resources.Retained()`; the runtime's deterministic names, owner labels and UIDs are the authority for what exists |
| Which workspace commands were admitted, and the outcome of the last one with the Session's answer | `SessionWorkspaceFlow`, Attributes `Admission` and `Outcome` |
| How one provisioning went: the resources it found, its checkout, its outcome and how the workspace Flow answered its report | One `SandboxProvisionFlow` execution |
| The repository HEAD the Agent works on | `SessionAgentFlow`, `HeadCommit` |
| Preview state, Preview engine status, the prepared source, configuration pins, grants, the instance the Preview last acquired, the automatic deployment and the outcome report it owes the Session | The Project's `PreviewFlow` execution; the engine's own state lives on the volume under the supervisor |
| What outlives every Flow: the Pod and volume, the Preview route objects, grants and data | The deleting Project, which releases them by label |
| The state of a command that runs in the Pod | The command ledger inside the Pod, a reference only: platform gates never trust it. What the model learns comes from tool results |
| Git work of a subagent | Its worktree on the volume, with refs and receipts ([subagents](subagents.md#worktrees)) |

## Instance model

| Object | Created | Deleted |
|---|---|---|
| Isolation boundary of the Project's workspace: a namespace, or in Local Native a network | First instance | Project deletion |
| Volume `workspace` (10 GiB PVC): subPath `repository` mounted at `/workspace`, subPath `preview-state` at `/var/lib/superverse/preview`; in Local Native the named volumes `<network>-workspace-repository` and `<network>-workspace-preview-state`, which share one workspace-UID label | First instance | Project deletion |
| Sandbox object, Pod and the controller's headless Service; in Local Native the container | Each instance | Reclamation, or Project deletion |
| Preview route objects: Ingress and Service `preview-app`, NetworkPolicy `allow-preview-app`; in Local Native a route file of the gateway | Each Preview start, when the Preview is exposed | A Preview stop, the release before a reclamation, or Project deletion ([Route and URL](#route-and-url)) |
| The Project's Preview data in Live storage: configuration versions, grants, prepared sources | First preparation or start | Project deletion |
| Command ledger `/tmp/superverse-exec` and every process | Each instance | End of the Pod |

The coding profile `coding-v4` (an immutable ConfigMap content-addressed by the chart)
defines the Pod: one container from the coding image; `tini` as PID 1 with `sandboxd`
as its child; UID/GID 1000; requests 500m CPU and 1 GiB, limits 2 CPU and 4 GiB, 8 GiB
ephemeral storage; a 2 GiB `/tmp` volume; a 45-second termination grace period with a
preStop hook that stops the Preview engine; `containerPort` 8801 beside sandboxd's 8080
and 9090. The namespace is bounded by a quota of one Sandbox, two Pods, three Services,
one PVC, four Secrets and 4 CPU and 8 GiB. Local Native runs the same profile: the server
reads the chart's rendering of it and starts the Pod template's container with the Pod's
hardening (UID 1000, a read-only root, every capability dropped, `no-new-privileges`, the
CPU and memory limits), the workspace subPaths as two named volumes, `/tmp` as a volume on
disk (tmpfs would count against the memory limit) and the preStop hook before
`docker stop` with the same 45-second grace period. The runtimes differ only in the
infrastructure underneath:

| | Local Kind | Local Native (Docker) | Hosted (AWS) |
|---|---|---|---|
| Runtime | runc; flock works across Pods on one node | A container with the Pod's hardening and no restart policy: the Flow decides whether an instance runs | gVisor (`runsc`) on Karpenter capacity; flock is assumed only within one Pod |
| Boundary | A namespace | A network; the container also joins the Project's network, where its Preview reaches the Live MinIO as `storage` | A namespace |
| Volume | `standard`, ReadWriteOnce, which does not enforce single use | Named volumes that share one workspace-UID label | gp3 EBS, ReadWriteOncePod; the platform also waits for the old Pod to be gone |
| Egress | Open | Open; no NetworkPolicy | Public HTTPS only; no private ranges, no metadata endpoint |
| Storage grant for Preview | MinIO service account in the Live MinIO | A MinIO service account in the Project's MinIO; its key pair is a private file (mode 0600) that the server created exclusively | IAM role bound by Pod Identity |
| Platform to sandboxd and Preview Dex | The Sandbox Service's cluster DNS name | A port the container publishes on 127.0.0.1, looked up for each use and never kept as an address | Headless Service DNS admitted by NetworkPolicy |

The baseline ingress policy admits sandboxd (8080, 9090) and Preview Dex (8801) only from
the platform server; the application port 3000 is reached only through the Preview
route's own Service. The exact Pod, quota and policy values are in the chart
(`_coding-profile.tpl`); this document states the contract the Flows rely on. Local Native
accepts the deviations that [the current contract](../../runbooks/local-native.md)
lists: a published port is reachable from other containers through
`host.docker.internal`, `docker inspect` shows a container's environment, and there is no
NetworkPolicy, ephemeral-storage limit or `/tmp` size limit.

Every identity a Flow checks has a counterpart in each runtime, and an object whose labels
or UID differ is never adopted:

| Reference (`sandbox.Resources`) | Kubernetes | Docker |
|---|---|---|
| Isolation boundary | The workspace's namespace, by UID | The workspace's network, by network ID |
| Workspace volume and the principal of Preview grants | The PVC and the service account, by UID | The two workspace volumes, by their shared workspace-UID label, together with the network ID |
| Instance | The Sandbox object, by UID | The container, by its instance-UID label, which is also `SUPERVERSE_POD_UID` and survives `docker restart` |
| Sandbox Service name and UID | The headless Service | The alias `coding` and the container ID |

A cold start attaches the next instance to the same boundary and volumes, so the principal
that a Preview grant binds survives the instance in both runtimes. the current contract holds the
complete mapping, including the Project scope and the Live generations.

## Workspace lease

### Lease and instance identity

The Session accepts one capability per instance, the **lease**:

| Field | Meaning |
|---|---|
| `RequestID` | The WorkspaceRequestID: the instance identity |
| `Binding` | Provider identities only: Project, the Session's FlowID (the root of the execution IDs), Generation, the isolation boundary, Service name, Sandbox UID and the workspace (volume) UID. The runtime resolves the sandboxd addresses from these identities (`SandboxAddresses`: Kubernetes derives the cluster DNS name of the Service; Docker reads the ports that the container carrying the instance UID publishes on 127.0.0.1 now), so no caller can route a tool to an arbitrary endpoint |
| `Repository` | Repository identity and `ExpectedHeadSHA`, the HEAD the Sandbox observed at checkout |

The lease has no expiry. It admits new tools while the Session's `Workspace.State` is READY,
and the Session ends it by revoking it before the instance is reclaimed
([Sandbox deadline](#sandbox-deadline)).

- The Session persists the lease in `Workspace.Lease` and replays it to the pooled
  transport. No Flow is consulted while the lease is usable.
- **WorkspaceRequestID** is `ws-<n>` (for example `ws-42`), where `n` is the numeric
  suffix of the StepExecutionID of the `RequestSessionWorkspace` execution that opened
  the request. The counter never repeats within the Flow, so the value is unique.
- The WorkspaceRequestID is carried by the workspace commands (`ws-<n>/provision`,
  `ws-<n>/reclaim`), the provisioning's FlowID and Start input, the lease, both outcome
  reports, the Preview's acquisition and release, and the label
  `superdurable.io/workspace-request-id` on the Sandbox object and its Pod. Every cross-Flow
  payload compares it. RunID is never compared, stored or returned. Generation is the
  constant `1`.
- sandboxd verifies no request-level identity. The Binding is validated before the
  transport dials; a liveness probe shows that the Service answers now, not which Pod is
  behind it, and an error that proves the instance unreachable guarantees that no request
  was sent ([tool orchestration](tool-orchestration.md#process-local-transport) describes the pooled transport).
  Identity is fenced by the boundary and Service names, by the Sandbox UID checked when the
  provisioning verifies the instance, by the WorkspaceRequestID in every payload, and
  by `--not-before` in the command ledger ([below](#command-ledger)).

### Gates and the cold path

On the normal path `SessionAgentFlow` runs Sandbox tools and commands without any Flow
RPC, StartFlow or Flow read: the Step reads the accepted lease from its own Attributes
and talks only to sandboxd (and, for Git, to the repository host). Flow RPCs are limited to
on-demand workspace initialization, recovery after the transport proves the instance
unreachable, Preview commands, the one Preview deployment request a turn's end makes for a
pushed commit, the reclamation, which asks the Preview to release the instance only while
the Preview holds it, and Project deletion, which calls `CompleteProjectSession` once and
ends the workspace Flow and the Preview. A cold start makes four cross-Flow calls (the
Session's `provision` command, the provisioning's start, its report to the workspace Flow
and the workspace Flow's report to the Session); a reclamation at the deadline without a
running Preview makes two (the `reclaim` command and the `reclaimed` report). Read-only
platform tools are a separate category: they run server-side, read only the Session's own
Project and never touch the lease or sandboxd.

Three gates are the only entries from the conversation into the cold path. They are
described with the Steps that contain them in [session](session.md#steps); in short:

- **G1, first segment.** The model call that finds no context segment yet and no recorded
  `Workspace.RepositoryContext` goes through `RequestSessionWorkspace` first, and the cold
  path returns to the model continuation through `CheckSteered`. The cold path opens the
  Session's first segment itself, with the repository context if the lease provided it, and
  does not ask G1 again, so a workspace that cannot be prepared delays the model but never
  blocks it; that segment then carries no repository context.
- **G2, dispatch.** A wave or a serial call with workspace access requires
  `Workspace.State == READY` and a usable lease, then sets `WorkspaceUse` under the
  `Workspace` lock; otherwise it requests a workspace.
- **G3, lost transport.** An error that proves the call was not dispatched
  (`ErrWorkspaceUnavailable`) re-enters the cold path as a decision: no Dex retry is
  consumed and the call stays pending. More than two losses of one call finish it as
  `workspace_unavailable`. A call that may already have run is never repeated silently: it
  records `command_interrupted` with effect unknown ([tool orchestration](tool-orchestration.md)).

```text
G1 | G2 | G3 | RPC RequestSessionWorkspace
  -> RequestSessionWorkspace
       -> SendWorkspaceCommand(provision) -> AwaitSessionWorkspace -> ApplySessionWorkspace   (opens the request)
       -> ApplySessionWorkspace{PromptOnly}      (READY lease without the repository context)
       -> join: JoinedResumeAt = continuation, DeadEnd   (a request in flight)
     SendWorkspaceCommand -(refused)-> RecordWorkspaceFailure
     SendWorkspaceCommand | AwaitSessionWorkspace | ApplySessionWorkspace -(retries exhausted)-> RecordWorkspaceFailure
  -> finish(ok | failed): CheckSteered(continuation) | DeadEnd

ApplySessionWorkspace(READY) -> AwaitSandboxDeadline -> SendWorkspaceCommand(reclaim)

SessionWorkspaceFlow, one command at a time:
  AwaitWorkspaceCommand -> [ReclaimInstance ->] ProvisionInstance -> AwaitProvisioningOutcome -> ReportToSession
  AwaitWorkspaceCommand(reclaim) -> ReclaimInstance -> ReportToSession
```

| Session Step | Policy | Behavior |
|---|---|---|
| `RequestSessionWorkspace` | ASYNC; one-minute method timeout; locks `Workspace`; loads `CurrentMessages` and `Subagents` | Not ACTIVE: DeadEnd. A reported loss of the current READY or REVOKED request counts per call (`LostCallID`, `Losses`) and revokes the lease; more than two losses of one call finish as failed without a new request. READY with a usable lease: finish ok, or `ApplySessionWorkspace{PromptOnly}` for a model continuation whose repository context was never read. A request in flight is joined: an RPC-triggered branch ends, the conversation records its continuation as `JoinedResumeAt` and ends, and the Step that settles the request resumes it. Otherwise it opens a request (`State = REQUESTED`, a new WorkspaceRequestID, the previous failure cleared), cancels the previous request's `AwaitSandboxDeadline` and moves to `SendWorkspaceCommand`; the request is durable before its command |
| `SendWorkspaceCommand` | `dexflow.ProviderStepOptions()` (SYNC, 45-second attempts, retried for three minutes); no lock; failure to `RecordWorkspaceFailure` | For a request that settled or was replaced meanwhile: `ApplySessionWorkspace`. Otherwise `ProvisionWorkspace(identity, requestID, PreviewHolds)` through the Session Service, which requires a READY Project, starts the `SessionWorkspaceFlow` on the first command and submits `ws-<n>/provision`; a repeat is accepted without a second command. A refusal (`ErrWorkspaceCommandRejected`, including an ended workspace Flow) or the refusal of a replaced request goes to `RecordWorkspaceFailure` with the reason; an accepted command goes to `AwaitSessionWorkspace`. For the reclamation at the deadline it submits `ws-<n>/reclaim` (`ReclaimWorkspace`) and ends; a refusal or exhausted retries record only the detail |
| `AwaitSessionWorkspace` | Waits for the request's `WorkspaceOutcomes` instance or a timer of `provision.Budget` plus 30 minutes, which outlasts the workspace Flow's reclamation of an earlier instance, its own wait and its report; `ProviderStepOptions()`; no lock; failure to `RecordWorkspaceFailure` | Takes the grant of the reported outcome, then `ApplySessionWorkspace`. At the timer without a report it reads the request's outcome from the workspace Flow (`ObserveWorkspace`, its `GetSessionWorkspace`), and without one goes to `RecordWorkspaceFailure` with `WORKSPACE_TIMEOUT`; the workspace Flow reclaims that instance before the next one |
| `ApplySessionWorkspace` | SYNC; 30 s; retried for 2 minutes; locks `Workspace`; loads `CurrentMessages` and `Subagents`; failure to `RecordWorkspaceFailure` | Another request replaced this branch's: an RPC-triggered branch ends and the conversation goes back to `RequestSessionWorkspace`. A request still REQUESTED settles from the observed outcome: a READY one is accepted (its lease recorded, the Sandbox deadline set one hold ahead, and a Preview's hold of an earlier instance cleared), a failed one is recorded with its code (the provisioning's own, such as `CHECKOUT_FAILED`, or `WORKSPACE_RECLAIM_FAILED` when the earlier instance could not be reclaimed). The owner of an RPC-triggered request takes `JoinedResumeAt`, and a waiting Preview is told the settlement. On READY the idempotent post-grant effects commit with this Step: `HeadCommit` from the lease and the repository context (`Workspace.RepositoryContext`, at most 8 KiB). READY finishes ok and the owner arms `AwaitSandboxDeadline`; FAILED, REVOKED or an empty state finishes failed |
| `RecordWorkspaceFailure` | ASYNC; one-minute method timeout; locks `Workspace`; loads `CurrentMessages` and `Subagents` | Reached from a refused command, from a Step that exhausted its retries or from `AwaitSessionWorkspace` at its timer. The owner's request, if still REQUESTED, becomes `FAILED("WORKSPACE_TIMEOUT")` after that timer and `FAILED("WORKSPACE_START_FAILED")` otherwise, with `FailureDetail` from `ctx.RecoveryError().Detail` or the refusal (at most 2 KiB); a waiting Preview is told the failure. A prompt-only branch owns no request and records only the detail. The owner of an RPC-triggered request takes `JoinedResumeAt`. A READY request whose lease effects failed still arms its Sandbox deadline |

`finish(ok)` continues the caller: an RPC-triggered branch ends with DeadEnd, and any other
branch goes through `CheckSteered` with the continuation it waited at (a model continuation
or `route_tool`). `finish(failed)` does the same, except that `route_tool` first appends the
known-failure tool result `workspace_unavailable` for the current pending call, ends
workspace use and continues after the tool, and a model continuation opens the Session's
first segment without the repository context if it has none. A Step that settles a request
owned by an RPC-triggered branch also finishes the joined conversation continuation, in the
same commit. A pending `start_agent` call that the settling Step answers this way started no
subagent, so the Step deletes its STARTING record.

The workspace RPCs of `SessionAgentFlow` (short RPCs with a ten-second timeout; internal
and reached only through the Session Service; locked handlers may run up to three times and
are idempotent; the typed client retries `*dex.RPCLockConflictError` for at most ten
seconds):

| RPC | Locks | Contract |
|---|---|---|
| `ReportWorkspaceOutcome(WorkspaceReport{WorkspaceRequestID, Grant, Reclaimed}) -> bool` | `SessionStatus`, `NotificationReceipts`, `Workspace` | Called by the workspace Flow's `ReportToSession` through `SessionFlowService`. A `reclaimed` report is always admitted and clears the Preview's hold when it names that instance. A ready or failed outcome is admitted only while an ACTIVE Session waits for exactly that request (`State == REQUESTED` and `RequestID` equal to the report's) and is published, with its grant, to that request's `WorkspaceOutcomes` instance. A repeated report is admitted again by its receipt (`NotificationReceipts`, key `workspace/<WorkspaceRequestID>`) without a second message. Anything else, such as a late outcome after a replacement or the Project's deletion, answers false, which the workspace Flow records as `declined` and answers by reclaiming the instance. A missing or closed Session is `ErrSessionInactive`, which the Service answers with false |
| `RequestSessionWorkspace(WorkspaceDemandRequest{PreviewCommand, ReadyOnly, ForAgentID, Lost}) -> {Summary, Lent, Lease, Closed}` | `SessionStatus`, `Workspace`; the asking subagent's record is loaded | A Session that is not ACTIVE answers `Closed` and does nothing; the typed client reports it as `ErrSessionNotActive`. A usable instance is returned in the summary and the caller talks to the Sandbox directly; for the Preview (`Preview: true`) it is lent (`Lent`), the Session records the Preview as its holder (`PreviewHolds`), which every later workspace command names so that the reclamation asks the Preview to release it, and the lend extends the Sandbox deadline by one `sandbox.hold` under the same `Workspace` lock, as the main agent's workspace use does, so the command runs on an instance that the deadline does not reclaim meanwhile. A ready-only demand (`ReadyOnly: true`, a Preview deployment's) ends there without an instance: it opens no request and records no wait, so a reclaimed Sandbox is never woken for a deployment ([notifications](notifications-and-feedback.md#the-deploy-preview-chain)). Otherwise, unless a request is in flight, it schedules the Step `RequestSessionWorkspace` with an empty continuation, and a Preview demand is recorded as waiting (`PreviewWaits`), which the Step that settles the request announces. `ForAgentID` names a subagent: the RPC answers only while that agent's record is RUNNING (otherwise `Closed`). A usable instance's lease is returned in `Lease`; `Lost` names the request whose lease the subagent's transport proved unreachable, and when that is still the current request the lease is revoked, so the next request reclaims and replaces the instance ([subagents](subagents.md#lease-hand-over-and-refresh)) |
| `RenewSessionWorkspace() -> bool` | `SessionStatus`, `Workspace` | The "Keep running" command: extends the Sandbox deadline to at least now + 7 days; true only while the Session is ACTIVE and the workspace READY |

Recovery and ownership:

- A workspace command is idempotent by its ID `ws-<n>/<kind>`: `SubmitWorkspaceCommand`
  accepts a repeat without publishing it again and refuses a command of an older request,
  so `SendWorkspaceCommand` repeats it with its retries. The workspace Flow's start
  (`dex.IDReuseDisallow`, a RequestID fingerprinting `{SessionFlowID}`) joins its one
  execution; an ended execution refuses every command, which the Session records as a
  refused request. `ObserveWorkspace` reads the outcome from the workspace Flow before the
  owner settles a state when the report did not arrive in time.
- Every instance has its own provisioning FlowID under `dex.IDReuseDisallow`, so a
  repeated start of a settled request joins its completed execution and provisions
  nothing again. No admission Step asks whether the request is still wanted
  ([StartFlow admission](../start-flow-admission.md)); the workspace Flow admits the
  provisioning's report only for its current instance, and the Session admits the workspace
  Flow's report only for the request it waits for.
- Ownership stays with the workspace Flow. The provisioning's outcome hands it the
  instance's `Resources`, and what outlives a Pod (`Resources.Retained()`: the boundary and
  the volume with its coding profile) goes to the next provisioning in `Request.Retained`.
  The Session receives only the lease of a ready instance.

`Workspace` and `WorkspaceUse` are ordinary Attributes holding struct values, not
AttributeMaps. Their fields and locks are declared in [session](session.md#attributes); the
rules above are their contract.

## Sandbox deadline

The Session decides when a Pod is reclaimed; the workspace Flow reclaims it at the Session's
command. No other Flow holds a timer for an instance or is asked whether the tools stopped.
The rule and its single writer, `Workspace.SandboxDeadline`, are specified in
[session](session.md#sandbox-deadline). In short, the deadline is a point in time that only
moves later: one hour (`sandbox.hold`) after the main agent uses the Sandbox, the Session
lends the instance to a Preview command, or a task completes, seven days
(`sandbox.previewHold`) after the Preview reports a deployment or start READY, seven days
after the user's "Keep running", never reclaimed while a subagent or background command is
unfinished. It is computed in the Session's own Steps, so the normal tool path needs no Flow
RPC. Public traffic to the Preview URL, Studio reads, Preview configuration work and Runs are
not use.

**The decision.** `AwaitSandboxDeadline` waits on a business timer until the deadline and
decides under the `Workspace` lock, atomically with tool dispatch. A later deadline, a wave
still marked in `WorkspaceUse`, a running background command or an unfinished subagent holds
the instance for another hold, and the Step waits again. Otherwise it revokes the lease in
the same commit (`State` REVOKED), before `SendWorkspaceCommand` sends the `reclaim`
command. A wave admitted earlier has already moved the deadline, and a later wave sees
REVOKED and takes the cold path. No Flow is asked whether the tools stopped, and no callback
exists.

**What reclamation does.** The workspace Flow's `ReclaimInstance` is one polling Step
without a lock (`dexflow.PollingStepOptions(15m)`, a round every 2 seconds within a
40-second round timeout). A new attempt starts over, because every stage queries before it
changes anything and repeats only what the attempt did not finish:

1. **Wait for a running provisioning.** An instance still `provisioning`, for example after
   the Session's own wait for its outcome passed, may still create objects: the Step first
   waits until its provisioning reports an end and takes the resources it recorded.
2. **Release from the Preview.** Only when the command names the instance as the Preview's
   (`PreviewHolds`), the Session Service sends the Preview the command `reclaim` (ID
   `reclaim-<workspaceRequestID>`); the Preview removes the route, stops the application,
   revokes the grants and lets go of the instance. A Preview busy with another command
   answers `BUSY` and is asked again in the next round; one that does not hold the instance
   answers `NOT_HELD`. A command that waits for the Session's next instance holds none,
   because it gave up the instance it held before it waited
   ([Preparing and starting](#preparing-and-starting)), so a reclamation never waits for a
   command that waits for the Session. A Session without a running Preview makes no call
   here.
3. **Discover** the references the instance lacks from the deterministic names, checking the
   owner labels and every recorded UID. An object of another owner is a refusal.
4. **Stop the engine.** The Sandbox-local Preview engine, which the Agent may have started
   without a Preview, is asked to stop. An unreachable supervisor, or one still stopping
   after three minutes, leaves the stop to the Pod deletion, whose preStop hook asks again.
5. **Inspect** the workspace and record CLEAN, DIRTY or UNKNOWN. The volume is kept either
   way, so reclamation never fails on a dirty workspace.
6. **Delete the Pod** and wait until every Pod the instance controlled is gone, so the next
   instance never mounts the volume beside a terminating Pod.

The reclamation keeps the isolation boundary, the volume (repository and Preview engine
state) and the Project's Preview data. The workspace Flow records the instance `reclaimed`
with its resources without the Pod, then provisions a waiting `provision` command's instance,
or reports `reclaimed` to the Session. The Session stays ACTIVE with `Workspace` REVOKED. The
next demand opens a new request, `ws-<m>` with a later number; the new instance adopts the
checkout as it is, uncommitted changes included, and the Preview engine resumes from its
state on the volume. The Preview application does not restart by itself: a Preview that was
running stays STOPPED, and the user starts it again or the next push redeploys it.

**One volume, one Pod.** The workspace Flow runs one command at a time, and a `provision`
command first reclaims an instance whose Pod may still run (`provisioning`, `ready` or
`failed`); the provisioning's check that no other Pod mounts the volume stays the hard guarantee.

**A reclamation that cannot finish** (a definite refusal such as an owner conflict, exhausted
retries or the passed wait limit) goes to `RecordReclaimFailure`, which keeps the instance's
resources and records why. A `provision` command that waited for the volume fails
`WORKSPACE_RECLAIM_FAILED`, and the Session's waiting branch resumes without a workspace. The
Pod then stays until the next command reclaims it or the Project is deleted.

**No second timer.** The Session's deadline is the only timer of an instance: neither the
workspace Flow, the provisioning nor the Preview has a TTL, the Session has no drain to poll,
and no Flow calls back into the Session for a reclamation except the workspace Flow's one
outcome report.

**Release by Project deletion.** Nothing else releases what outlives a Pod. The deleting
Project completes the Session once (`CompleteProjectSession`), stops the Session's subagents
and waits for their ends, ends the workspace Flow (`CompleteSessionWorkspace`, which cancels a
provisioning or reclamation in progress) and the PreviewFlow, and then releases by label and
owner, through the same `sandbox.InstanceProvider`, the Session's Pod, the Preview's route
objects, grants and data, the volume and the isolation boundary. Each part queries its exact
object first and deletes it with its UID as a precondition, and the pass answers true only
when everything is gone ([composition](composition.md#reads-preview-configuration-and-deletion)).
No dirty workspace is protected: deleting the Project discards it.

The hold settings `sandbox.hold` (1 hour) and `sandbox.previewHold` (7 days) belong to the
Session's rule ([session](session.md#configuration)). `SessionWorkspaceSnapshot.expires_at` shows
`SandboxDeadline`.

## sandboxd and confinement

`sandboxd` (Agent Sandbox v1.0.1, built from the pinned upstream source by the image) is
the only entry the platform has into a Pod. It serves REST on 8080 and gRPC on 9090 with
`--root-dir=/workspace`, as UID 1000. Its protocol is small, and the platform treats its
limits as facts:

- The **file API** reads bytes, lists a directory and writes atomically, confined to
  `/workspace`. It cannot read by line, search, or reach outside the root.
- The **process API** starts a command in a process group and streams its events. It
  limits only the working directory. It has no process listing, no durable request
  identity and no reattachment: the platform keeps no recoverable handle to a running
  command through sandboxd itself.
- sandboxd kills the whole process group of a command it started when the stream is
  cancelled or closed. A command that must outlive the call therefore starts in its own
  session (`Setsid`), which the supervisors of the image do.
- Every process gets stdin EOF right after Init. Combined output beyond its budget
  (16 MiB at most) is killed by the platform client.
- A start refused before Init with `InvalidArgument`, `NotFound` or `PermissionDenied`
  never ran (`ErrProcessNotStarted`), and a missing working directory and a missing
  command look the same.

Because sandboxd offers neither line reads, search, a ledger nor confinement, the image
carries two helpers (below), and the Agent's tools reach the Sandbox as sandboxd
processes that run these helpers. The confinement
(`sandbox/coding/internal/confinement`) is an isolation and correctness boundary, not a
security boundary: the helpers and the commands they run share UID 1000 and the Pod.
Isolation against hostile code rests on the Pod: gVisor, the namespace quota, the network
policies and the absence of credentials in the process environment (in Local Native, on the
container's hardening and the absence of credentials). Tool processes run
with `HOME=/tmp`, so nothing a tool writes under HOME lands in the repository; Git
credentials are minted per call by `*git.HostCredentials` from the GitHub App and reach
only the platform's own Git processes, and storage credentials for the Preview arrive as
a private file under `.git`, never as arguments.

**Path rules.** The agent root (the workspace for the main agent, the worktree for a
subagent), the read-only roots and the refusal of `.git` paths are specified with the file
tools in [tool orchestration](tool-orchestration.md#files-and-confinement). The
confinement package implements them once for every helper that takes a path, resolving
symlinks before the check.

## Coding image

The image `superverse-coding-sandbox` (build: `sandbox/coding/Dockerfile`) implements
**sandbox image contract revision 8** under the profile name `coding-v4` (metadata revision 9, which
matches the platform Service selector for Sandbox credential reads).
Revision 6 added the application test engine, revision 7 the `dex-connector-contributor`
skill and revision 8 the file helper's `path_permission_denied`. Raising the revision
increments the profile's `metadataRevision`, which is what gives the changed profile bytes a
new immutable ConfigMap name.

| Component | Role |
|---|---|
| `tini` (PID 1) | Reaps the orphans of detached processes and forwards termination to sandboxd. The entrypoint is `tini -- sandboxd` |
| `sandboxd` | The platform's transport (above) |
| `superverse-exec` | The command ledger and supervisor ([below](#command-ledger)) |
| `superverse-files` | `read`, `edit`, `write`, `grep` and `glob` with the path rules above. `grep` and `glob` use `rg --no-config`, which honors `.gitignore` and skips `.git` |
| `superverse-previewctl` | The Preview supervisor ([Preview](#preview)) |
| Shims of `dexcli` and `temporal` on PATH | `dexcli` passes only the read subcommands (`flow search`, `summary`, `state`, `history`, `inspect`, `watch`, `health`, `version`, `visualize`) unless `--allow-mutation` is given, which the Dex skills reserve for an explicit user request; `dexcli dev` and `temporal server start-dev` are refused always. The real binaries live in `/usr/local/libexec/superverse` for `superverse-previewctl`. The shims guard against mistakes, not against a hostile Agent of the same UID |
| Toolchain | Git, Go (with a prewarmed proxy of the `app-template/` module graph at `/opt/superverse/go-module-proxy`), Node.js and npm, Python 3, `ripgrep`, `jq`, `make`, Chromium Headless Shell, Dex CLI v1.6.2 and Temporal CLI 1.8.1 |
| Skills | The repository's Dex skills (`dex-app-builder`, `dex-sdk` and `dex-connector-contributor`), copied read-only to `/opt/superverse/dex-skills` |

The three commands are built from the repository's own Go module, so they share
`sandbox/coding/internal/supervision` (flock, process identity read from procfs,
atomic JSON files, `Setsid` start, process-group termination) and
`sandbox/coding/internal/confinement` (the path rules). The helpers use only the standard
library and these packages.

The image's environment fixes the toolchain's cache paths under `/tmp` (module cache,
build cache, npm cache, XDG directories), `GOPROXY` to the local proxy then the public
proxy, `GOTOOLCHAIN=local` and `CGO_ENABLED=0`. Applications may select newer module
releases; they download through the public fallback. Browser downloads are disabled; the
system Chromium is used with `--no-sandbox`.

Fixed ports of the Pod (the contract for every agent, subagent and test run):

| Port | Bind | Owner |
|---|---|---|
| 8080, 9090 | Pod address | sandboxd REST and gRPC; platform server only |
| 8801 | Pod address, relayed to Dex on loopback | Preview Dex; the only Sandbox Service port besides sandboxd |
| 8802 | 127.0.0.1 | Dex Web, never exposed |
| 7233, 7243, 7244 | 127.0.0.1 | The Preview engine's Temporal |
| 3000 | 0.0.0.0 | The Preview application, behind the Preview route |
| 18811 | 127.0.0.1 | The Preview application's Worker, dialed only by the local Dex |
| 18812 | 127.0.0.1 | Reserved for a local run of the application, which the template does not offer: the application reads its configuration only from a Preview start |
| 7235, 7245, 7246, 8805, 8806, 18813 | 127.0.0.1 | The application test engine ([commit gate](commit-gate-and-app-tests.md)) |

## Command ledger

`superverse-exec` is the only way the platform starts a command that must be recoverable.
It keeps a ledger of runs inside the Pod and runs each command under its own supervisor,
so a command survives the sandboxd call that started it, a Worker restart and a Step
retry, and a retried call can ask what happened instead of guessing. The ledger lives in
the Pod's `/tmp`, so a replaced Pod has no ledger: that case is explicit
(`instance_replaced`). The ledger is a reference for the Session, never a gate.

Every subcommand prints one JSON object on stdout. Exit code 0 is a definite answer; exit
code 1 is a helper, transport or usage error that may be retried and says nothing about
the command.

| Subcommand | Behavior |
|---|---|
| `begin --id ID --cwd DIR --lifetime DURATION [--not-before TIME] [--background] [--wait DURATION] -- ARGV...` | Decides under the run's flock. Same ID and same digest (of argv, cwd and the background flag): returns the current status without starting again. Same ID, different digest: `execution_conflict`. No record and `/proc/1` started after `--not-before`: `instance_replaced`, nothing started. A missing working directory, or one that is not a directory: `working_directory_unavailable` before any process starts, which the server records as `command_not_started`. The Sandbox already holds four background commands: `background_limit`. Otherwise a `Setsid` supervisor starts the command. `--wait` (at most 25 s) holds the answer until the command ends or the time passes |
| `status --id ID [--wait DURATION] [--from OFFSET --max BYTES]` | `not_found`, `running`, `exited` or `lost`, with the end reason `exit`, `lifetime_limit`, `output_limit` or `stopped` and the exit code, and optionally a window of the merged output from an offset |
| `background --id ID --lifetime DURATION` | Turns a running foreground command into a background command. `background_limit` when four already run |
| `stop --id ID` | TERM to the process group, KILL after 10 seconds. Idempotent |
| garbage collection | Runs on every `begin`: finished runs older than two hours are removed, and the total is kept within 256 MiB, oldest first. A running run is never removed |

- **Files.** `/tmp/superverse-exec/runs/<id>/{admission.json, lock, output.log, exit.json}`.
  `admission.json` records the request digest and the supervisor's process identity (PID,
  start time, command line); `exit.json` is written once, atomically, when the command
  ends. stdout and stderr go to one merged `output.log`, so there is one offset space and
  the order matches a terminal. `lost` means the supervisor is gone and no `exit.json`
  exists, for example after it was killed.
- **Idempotency by execution ID.** The ID is derived from the Flow ID and the tool call
  ID, so it is stable across Step retries and Worker replacement. Two calls with one ID
  start one command. `--not-before` carries the time of the first attempt and is sent
  only from the second attempt on: an absent record then means either that the first
  attempt never reached this Pod, or that the Pod is a new instance. PID 1 (`tini`)
  started after the first attempt means the latter: `instance_replaced`, and the caller
  records the outcome as unknown instead of running the command again.
- **Output.** The platform reads output only through `status --from --max`. Large output
  stays in `output.log`; the model receives a bounded projection and, when truncated, the
  path of the full file (a read-only root of the path rules).
- The foreground tool flow (`begin --wait 5s`, then a polling Step that asks
  `status --wait 20s`, and the settle step that decides after a timeout or exhausted
  retries) is specified in [tool orchestration](tool-orchestration.md#commands).

### Process limits

The supervisor enforces these itself; nothing depends on a Flow, a user or an agent
killing a process.

| Limit | Value | Result |
|---|---|---|
| Lifetime | `--lifetime` is required. Foreground: the tool timeout plus 30 s. Background: 3300 s by default. No command exceeds 6900 s | The process group is killed; end reason `lifetime_limit` |
| Output | 16 MiB per command (merged) | The process group is killed; end reason `output_limit` |
| Background commands | At most 4 per Sandbox, all agents together | `begin --background` and `background` answer `background_limit` |
| Ledger size | Finished runs removed after 2 hours; at most 256 MiB in total | Garbage collection |
| Pod end | Everything dies with the Pod | A later `status` for the ID is `not_found` on the new Pod |

### Background commands in the Sandbox

A background command is an ordinary ledger run whose supervisor outlives the call that
started it: started with `begin --background`, or converted from a foreground run with
`background` when the foreground timeout passes. From the Sandbox's side it differs from a
foreground run only in its lifetime and in the four-command limit, which is shared by
every agent of the Sandbox (subagents start none). The Session
reads the output with `status --from --max` and stops a command with `stop`, both
resolved through the ledger without a Flow read. Its watch is a `BackgroundCommandFlow`
SubFlow that polls the ledger, so the Session only waits while the command runs. The
Session's records, the watch, the completion notification and the stop semantics are
specified in [tool orchestration](tool-orchestration.md#background-commands) and
[notifications](notifications-and-feedback.md). A command ends at its lifetime, at its
output limit, at `stop`, or with the Pod; a reclaimed Pod therefore ends every background
command, which is why the deadline rule never reclaims while a task is running.

### Verification

The ledger is verified in a container of the coding image with the real binaries and the
real sandboxd, as the helper container tests ([Tests](#tests)): one start per ID, offset
reads, lifetime and output limits, `stop`, `lost` and `instance_replaced`. The `Setsid`,
flock and `/proc/1` start time behavior under gVisor is proven only by an AWS receipt.

## Preview

The Preview is the user's running application inside the Sandbox, reachable at a stable
URL. `PreviewFlow` owns its state and commands, and the instance it runs on belongs to the
Session's workspace Flow; the Session lends it. Build owns Preview source preparation, configuration and
deployment; Publishing owns Live only
([the current contract](../superverse-v2.md)). The engine is
described by the current contract; the exact
command contract of the supervisor is the
[previewctl README](../../../sandbox/coding/cmd/superverse-previewctl/README.md).

### Engine

- **One engine per Pod**, owned by `superverse-previewctl supervise`, a detached daemon
  that holds an exclusive flock on its state directory for its lifetime. It runs a local
  Temporal development server (loopback 7233, SQLite on the volume) and `dexcli dev` with
  `--external-temporal-address`, so the daemon owns Temporal, its store and its
  retention. Dex binds loopback 8801 and the daemon relays the Pod's own addresses on 8801
  to it, because dexcli binds Dex and Dex Web to one loopback address. Dex Web (8802) is
  never exposed.
- **Retention is 30 days**, set and read back as `2592000s`. A closed Preview execution's
  history and Dex blobs stay readable for 30 days, which bounds the volume's growth. The
  daemon refuses to start (`inconsistent`) when the store is missing while blobs or its
  record remain, because a new store would make Dex delete those blobs, and refuses
  (`full`) below 512 MiB of free space.
- **Lazy start.** The engine never starts when the workspace becomes READY. The first
  `environment ensure`, from the Preview start or from an Agent that reads Preview Flows,
  launches it. A taken fixed port fails the start; a Temporal or Dex that exits is
  restarted up to three times within ten minutes. Stop order is application, Dex,
  Temporal (SIGTERM, SIGKILL after ten seconds). The workspace Flow's reclamation stops it
  through `environment stop`, after the Preview stopped the application, and the preStop hook
  `shutdown --timeout 30s` is the backstop inside the 45-second grace period.
- **No second environment.** The shims, the state lock, the fixed ports and the state
  directory outside sandboxd's root prevent a second engine by mistake; the Dex skills
  point to `127.0.0.1:8801`, namespace `default`.
- **State** (`/var/lib/superverse/preview`, the `preview-state` subPath): the lock, the
  owner record (Pod UID and the identities of the daemon and its process groups), engine
  metadata, Temporal's SQLite store, Dex blobs and connector config, the source trees,
  and `logs/` (supervisor, Temporal, dexcli, build and application logs, copy-truncated
  above 100 MiB). A daemon of a new Pod takes over the state after the platform has
  confirmed the old Pod is gone.

### Preparing and starting

A Preview deploys a **prepared commit**. It never inspects the workspace, so edits made
after the preparation cannot reach the build. A command that needs the instance
(`prepare-preview-source`, `start-preview` and `deploy-preview`) first acquires it
(`AcquireSessionWorkspace`): the Session lends its READY instance, records that the Preview
holds it and holds the Sandbox for another `sandbox.hold`, or requests one and announces the
settlement to the waiting command, which the Preview admits only while that command is active
([PreviewFlow](#previewflow)). A new
instance starts with its engine `NOT_STARTED`. A `deploy-preview` asks ready-only: without a
READY instance it ends `SKIPPED` instead of waiting, so a reclaimed Sandbox is never woken for a
deployment.

The Session lends no instance while it replaces the one the Preview holds, because it asks the
Preview to release that instance before it reclaims it. A `start-preview` or
`prepare-preview-source` that is lent nothing therefore gives up the instance it holds before
it waits, through the stop Steps as a `reclaim` does: it stops the application, revokes the
grants, stops the engine of an instance that published its Service and drops the Pod from its
resources, so a `reclaim` of that instance answers `NOT_HELD`. It then asks the Session again
and waits without an instance. A start stays `STARTING` through this, and a source preparation
leaves a Preview that served on the instance `STOPPED`. Without it the reclamation would answer
`BUSY` for as long as the command waits, and each would wait for the other until the
reclamation's limit failed them both.

1. **Prepare the source** (`prepare-preview-source`, and the first part of
   `deploy-preview`). `PreparePreviewSource` registers the commit's AppManifest and
   generates strict FDG 2.0 from an isolated `git archive` export of the commit. The
   manual command requires a clean, pushed HEAD that is also the exact readable commit on
   GitHub. The `deploy-preview` command is pinned to its commit: it asks
   `superverse-previewctl source ensure --commit SHA` to export the tree into
   `$S/source/SHA` (a commit the repository lacks is `PREVIEW_SOURCE_NOT_PREPARED`), runs
   the definitions program in its pinned mode on that tree, and requires the manifest
   digest to equal the tree's `dex-app.yaml` digest. Definitions, AppManifest and the
   build all come from the same tree, never from `/workspace`. The AppManifest is recorded
   only together with the READY preparation of its commit, so a refused or failed
   preparation keeps the previous preparation and that preparation's AppManifest. The
   bundle `projects/<p>/preview-source/<commit>/definitions.json` holds the definitions and
   is written before the configuration host registers the prepared source; the READY
   preparation, recorded after both, marks a complete one.
2. **Verify** (`VerifyPreviewSource`, a state Step that calls no provider). The preparation
   must be READY for this instance (its WorkspaceRequestID and Generation), else
   `PREVIEW_SOURCE_NOT_PREPARED`. The Project's one Preview takes no route lease.
3. **Configure** (`PreparePreviewConfiguration`). Validates the Project's Preview
   configuration against the prepared commit's AppManifest and pins it together with the
   Live storage (namespace UID, bucket), the Sandbox principal and the grant generation.
   Its save marks the engine `STARTING` and, for a `deploy-preview`, the Preview too: a
   `start-preview` is `STARTING` from its admission, but a deployment changes the Preview's
   status only here, so one refused earlier leaves a READY Preview serving its commit.
4. **Ensure the engine** (`EnsurePreviewEnvironment`). Runs `environment ensure` in every
   round; once it answers `ready`, `infra.Connections.ProbeSandboxDex` verifies the recorded
   boundary, Sandbox UID and Service UID and the Dex health through the Sandbox Service
   on 8801 (in Local Native the port the container publishes for it). `full`, `inconsistent`
   and `failed` are refusals and mark the engine FAILED.
5. **Start the application** (`StartPreview`). Issues or reuses the storage grant, then
   `begin --commit SHA` in every round. `begin` builds in `$S/source/SHA` (web
   dependencies, `make generate`, the web build and `go build ./cmd/server`) and only
   after a successful build stops the previous application and starts the new binary. The
   application identity is (commit, configuration digest, grant generation); a running
   application of the same identity answers `running` without a rebuild. A failed build
   leaves the previous application serving. If the new application exits or misses its
   health probe (`GET /api/health` on 127.0.0.1:3000) within five minutes, the supervisor
   stops it and answers `failed` with the redacted log tail; the Step refuses
   `PREVIEW_FAILED`, which is application-owned. At most two trees exist: the running one
   and the candidate.
6. **Expose** (`ProbeAndExposePreview`). Reconciles the Project's route to this instance and
   probes the real gateway ([Route and URL](#route-and-url)). A running Preview holds its
   accepted SourceCommit, DefinitionRevision and definition ObjectRef; preparing another
   commit does not replace them. `serving_revision` changes only when the Preview is exposed
   or stopped.

`stop-preview` removes the route, stops the application and revokes the grant but keeps the
engine. The Preview application receives `PORT=3000`, `HOST=0.0.0.0`,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801`, its Worker address 127.0.0.1:18811 and the
Project's configuration grant; the caller cannot override these.

### Configuration and grants

- **Scope.** The Preview configuration of a Project lives in the Live bucket under the
  prefix `project-configuration/projects/<p>/preview/`. Preview needs only Live storage,
  never the Live execution environment. Every Project-keyed name (the configuration
  prefix, the grant workload ID `preview`, the cleanup prefixes `projects/<p>/preview/`
  and `projects/<p>/preview-source/`, the hosted cloud-name prefix and the owner fields
  of the Sandbox Dex target) is derived by one helper, `infra.PreviewKeysOf(projectID)`;
  no other code joins a Project ID into these names.
- **Grant.** The application reads its configuration through a storage grant bound to the
  Sandbox's principal by UID: the namespace and service account (in Local Native the
  network and the workspace volumes). Kind: a MinIO service account per grant
  generation in the Live MinIO, with a policy limited to the exact configuration object
  and its head's current version, its referenced app secrets including the credential
  broker's scope token, and `projects/<p>/preview/trigger-inboxes/*`; the `allow-storage`
  policy is updated in place to admit the Project's Sandbox Pods. Local Native: the same
  MinIO service account in the Project's MinIO, whose key pair is a private file created
  exclusively by the server. Hosted: one IAM role per Sandbox principal under the
  Project's Preview cloud-name prefix, tagged with scope, Live namespace UID and the
  principal's namespace and service-account UIDs, trusted by the principal and bound by
  one Pod Identity association that never moves to another role. A grant generation is a
  set of statements of the role's `scope` policy whose Sid names the generation, written
  in place, because the EKS Pod Identity agent caches issued credentials: a moved
  association left a Pod with the revoked role's credentials.
- **Generations.** A start keeps the unrevoked grant while the configuration
  revision and the principal are unchanged. Otherwise it allocates
  `max(PreviewState.Revision, Grant.Generation) + 1` (`PreviewState.Revision` is seeded from
  the execution's start time in Unix seconds and only grows, and one execution serves every
  instance, so a generation never repeats)
  and records the replaced grant as `SupersededGrant` until the new application runs;
  then the superseded grant is revoked. Hosted, the principal role's policy holds the
  union of both generations' statements in between, only the current generation's after
  the revocation, and deny-all once the last generation is revoked; IAM evaluates it per
  request, so cached credentials follow it a few seconds after the write, and an issuance
  stays pending for that propagation delay after each write. The role's
  `grant-generation` and `revoked-generation` tags, written before the policy, refuse an
  issuance of an older or revoked generation. This counter is separate from every other
  revision.
- **Cleanup.** `stop-preview` and `reclaim` revoke the grants and keep the Project's
  configuration; the hosted principal role and its association stay, deny-all, for the
  next start. A hosted issuance or revocation deletes the Preview's other roles, of an
  earlier principal or of per-generation grants of earlier releases. The deleting
  Project ends the PreviewFlow and then deletes the Project's Preview data, grants (the
  association and the role included) and prepared sources by label.

### Configuration without a workspace

Reading, editing and validating the Preview configuration never starts a Sandbox.
`ReadPreviewTarget(project)` reads only stored state: the Session's `HeadCommit` from the
retained summary, the one object
`projects/<p>/preview-source/<HeadCommit>/definitions.json` through
`PreviewFlowClient.ReadPreparedSource` (which makes no Flow call and needs no Preview
execution), and the Live storage from `ProjectFlow`. Session liveness does not gate it, because the configuration is
Project storage. A HEAD without a preparation reads NOT_READY with "Prepare the current
source"; only that explicit preparation, or a Preview start, reaches a workspace. The
configuration is fenced by the configuration revision and the source commit, with no
workspace request ID or generation; only Studio Runs keep the instance fence of a READY
Preview.

### Runs

Studio Runs reach the Preview engine through the Flow API: the Flow API target resolves to
the Project's Preview, `servingTarget` dials the Sandbox's Dex with
`infra.Connections.DialSandboxDex` (boundary and owner labels, Sandbox UID and
WorkspaceRequestID label, Ready, Service UID owned by that Sandbox, TCP 8801, then Dex
health), and `CheckPreviewWorker` runs `superverse-previewctl status` to require
the exact application identity running with its Worker listening on 127.0.0.1:18811.
Runs require the Session's READY instance, the Preview READY on that same instance and its
engine READY behind the instance's recorded Sandbox and Service. Application API tests run on their
own ephemeral engine and never appear in Runs.

### Route and URL

The Preview has one URL per Project, `preview-<projectID>.<env>.dexai.dev` (locally
`preview-<projectID>.localhost:23080`), which never changes. The route exists only while
the Preview serves. `ProbeAndExposePreview` reconciles the route objects (HTTPRoute and
Service `preview-app`, NetworkPolicy `allow-preview-app`, labeled with the Project; the
Service selects the instance's Pod by Project and profile; in Local Native the gateway's
route file) to the instance it serves on: it creates an absent object, keeps one that
matches the platform's current spec and replaces one whose spec changed by deletion and
creation, because the Sandbox namespace admits no update. `StopPreview` removes the route
before it stops the application, for an explicit stop, the `release` of a deployment and the
release before a reclamation, so a stopped Preview admits nothing to the application's port
and the project Gateway holds routes only for the Previews that serve. The next start
waits only for the gateway to load the route again: the platform ALB's one wildcard rule and
DNS record serve every Project host.
The deleting Project's release (`ReleasePreviewRoute`) removes a route that no stop removed.

While the application rebuilds behind a serving route the gateway keeps routing to the
previous application, which serves until the new one is healthy. Without a route the
Project's host answers as the gateway answers an unknown host (404 from the local gateway).
Nothing starts on demand: public traffic neither starts compute nor extends the Sandbox
deadline. Like Live, the Preview is public HTTPS without platform authentication, with the
hosted policy admitting any source and the local one admitting only the platform namespace
where the gateway runs.

- **Known risk.** The Preview runs unreviewed agent code with the Project's real connector
  credentials at a guessable host. A later gate (ALB OIDC or a signed cookie) is the planned
  mitigation; it is not scheduled.
- **Why the Preview URL does not replace the Flow API.** The URL serves the application's own
  HTTP service to its end users. Studio's Runs, Inbox, diagnostics, connectors and environment
  configuration go through the Flow API, which owns login, Project ownership, CSRF, target
  binding, the method allowlist and redaction.

### Preview auto-deploy on PreviewFlow

After a push that passed the gate, the Session requests a Preview deployment at the end of
its turn. [Notifications](notifications-and-feedback.md#preview-auto-deploy-after-a-push)
owns the handoff, the setting, the request, the `deploy-preview` chain, the outcome and
the concurrency rules; this document owns the primitives they use on `PreviewFlow`, which
the contract below lists:

- the RPC `RequestPreviewDeployment{WorkspaceRequestID, Commit, Sequence}` under
  `CommandLock`, the only admission of the internal command kind `deploy-preview` (ID
  `deploy-preview-<sequence>`); it answers `SKIPPED` for an instance the Preview released or
  is releasing, and otherwise `DUPLICATE`, `QUEUED` or `ACCEPTED`
  ([the request](notifications-and-feedback.md#the-request)); a commit is deployed once
  (`HandledSequence`, which lasts as long as the execution and so holds across instances), and
  a request that arrives while a command is active is queued, the newest winning;
- the Attribute `PreviewDeployment` and the gap list `PreviewState.ConfigurationGaps`
  (names only), filled by `PreparePreviewConfiguration` when
  [the gap report](notifications-and-feedback.md#configuration-gap-report) finds gaps,
  which refuses `PREVIEW_CONFIGURATION_INCOMPATIBLE` and leaves the running Preview
  untouched; an unresolvable connector release is not a gap and fails the Step with
  `PREVIEW_CONFIGURATION_FAILED`;
- `PreparePreviewSource` in its pinned mode (`source ensure --commit SHA`, then the
  definitions program on that tree and the manifest digest check, described under
  [Preparing and starting](#preparing-and-starting));
- the Attribute `OutcomeReport`, the `delivery` notification of the last reportable outcome,
  and the Step `NotifyPreviewOutcome`, which reads it and makes one Service call
  (`ReportSessionNotification`, kind `delivery`, source `preview`) with the notification
  ID `preview/<WorkspaceRequestID>/<CommandID>/<outcome>` (for example
  `preview/ws-42/deploy-preview-137/ready`), saves nothing and goes to
  `AwaitPreviewCommands` on success and, once its retries are exhausted, through its failure
  route. `CompletePreviewCommand`, `RecordPreviewRefusal` and `RecordPreviewFailure` record
  `PreviewDeployment.Last` with its owner, keep the report, admit a queued request and send
  the reportable outcome to `NotifyPreviewOutcome`. Every start or deployment that ends READY
  reports `ready`, and the Session RPC that receives it holds the instance the Preview serves
  on for `sandbox.previewHold`, which is what extends the Sandbox deadline to seven days; the
  notification itself opens no turn;
- the internal command kind `release` (ID `release-<deploy command ID>`), which the Steps that
  end a deployment admit themselves and `RequestPreviewCommand` refuses `INVALID_COMMAND`. A
  deployment that ends after it started the Preview without serving it (the Preview is still
  `STARTING`: it was refused or failed before READY, or was skipped behind one that was) is
  followed at once by the release, because nobody sent the deployment and nobody would stop
  it, and a Preview left `STARTING` offers no Start. A deployment refused before
  `PreparePreviewConfiguration` pinned its configuration started nothing and needs no release.
  The release runs the stop chain as a `stop-preview` does (`BeginPreviewStop`,
  `StopPreview`): it removes the route, stops the application and revokes the grants, and
  keeps the instance, its engine, the configuration gaps and the deployment's failure, so the
  Preview ends `STOPPED` with the failure and a person starts it again once its source and
  configuration are ready. A request queued meanwhile runs after the release. The release
  reports nothing, and the end of a deleting Project admits none. A release that fails records
  its own failure in the phase `release`, which Studio retries as a Preview cleanup
  ([Preview panel](streaming-and-studio.md#preview-panel)).

A `stop-preview` or `reclaim` that ends with a request waiting, and the Project's end, cancel
that request: the user's stop wins until the next push. A `release` cancels nothing.

### Owner of a Preview failure

The owner of a failure follows from the failure-record Step together with its typed cause,
never from the code alone. The deciding Step sets `previewRefusal.Owner`, `RecordPreviewRefusal`
records it in `PreviewDeployment.Last.Owner` and in the report `NotifyPreviewOutcome` delivers;
`RecordPreviewFailure` always records `platform`, because every exhausted retry and every
wait limit is a platform symptom. The cases (application: the supervisor's `failed`
answer, `PREVIEW_FDG_INVALID`, manifest content; configuration: the gaps; platform:
everything else) and what each does to the conversation are in
[failure owners](notifications-and-feedback.md#failure-owners).

## Dex Flow Changes

The three instance Flows each have one owner of their complete per-Flow review contract, the
package README next to the Flow: identity and execution lifecycle, every Attribute, Channel
and ChannelMap with its instance keys, the Steps, RPCs, wait limits, recovery and
compatibility, and the tests that run them. This section states their role in the lifecycle
and the identities a reviewer traces; the README is authoritative for everything else. What
the lifecycle adds to `SessionAgentFlow` is in [session](session.md#sessionagentflow-contract)
and in [Gates and the cold path](#gates-and-the-cold-path) above. No other Flow is introduced
by this topic.

### SessionWorkspaceFlow

Contract: [the Session workspace README](../../../internal/agents/sandbox/sessionworkspace/README.md).

- **Identity.** FlowID `session-workspace-<projectID>`, for example `session-workspace-a7k2`
  (`flowid.SessionWorkspaceFlowID`), one execution per Project. The Session's first workspace
  command starts it through the Session Service (`dex.IDReuseDisallow`, a RequestID
  fingerprinting `{SessionFlowID}`, same-request IgnoreError), and every command is confirmed
  by the RPC `SubmitWorkspaceCommand`, which an ended execution refuses. Only the Project's
  deletion ends it (`CompleteSessionWorkspace`, which cancels a provisioning or reclamation in
  progress and completes with `dex.GracefulComplete(Receipt{ProjectID})`). An independent
  top-level execution, neither a SubFlow nor a Project child.
- **Attributes.** `ProjectID`; `Instance` (state, WorkspaceRequestID, InstanceID, resources,
  volume state, provisioning phase timings, the lease of a ready instance, failure); `Outcome`
  of the last command with how the Session answered it (`admitted`, `declined`,
  `undelivered`); `Admission`, the last admitted command and whether the Flow closes. No
  AttributeMap, no index, no secret, fixed size.
- **Channels.** `WorkspaceCommands` (`WorkspaceCommand{ID ws-<n>/<kind>, Kind provision |
  reclaim, WorkspaceRequestID, HeadCommit, PreviewHolds}`), consumed one at a time; ChannelMap
  `ProvisionOutcomes`, key `ws-<n>`, the provisioning's report. No Stream.
- **Steps.** `AwaitWorkspaceCommand`, `ProvisionInstance`, `AwaitProvisioningOutcome`
  (bounded by `provision.Budget` plus ten minutes), `ReclaimInstance`
  (`dexflow.PollingStepOptions(15m)`), `RecordProvisionFailure`, `RecordReclaimFailure`,
  `ReportToSession` with `RecordReportFailure`, and `CompleteSessionWorkspace`. RPCs
  `SubmitWorkspaceCommand`, `ReportProvisionOutcome`, `GetSessionWorkspace` (the retained read
  that Studio, the Preview's lend, the Session's bounded wait and the cleanup use),
  `CompleteSessionWorkspace`, `GetDexSummary` and `GetDexDisplay`.

### SandboxProvisionFlow

Contract: [the provisioning README](../../../internal/agents/sandbox/provision/README.md).

- **Identity.** FlowID `sandbox-provision-<projectID>-<workspaceRequestID>`, for example
  `sandbox-provision-a7k2-ws-41` (`flowid.SandboxProvisionFlowID`); one execution is one
  instance. The workspace Flow's `ProvisionInstance` starts it through the Session Service
  with `Request{Identity with repository and HEAD, WorkspaceRequestID, Retained}`,
  `dex.IDReuseDisallow`, a RequestID fingerprinting `{SessionFlowID, WorkspaceRequestID,
  retained volume reference, profile}` and same-request IgnoreError: a retry joins the
  execution, also a completed one, so no admission Step is needed.
- **Lifecycle.** The outcome, `ready` or `failed`, is the business result; reporting it to
  the workspace Flow (`ReportProvisionOutcome`) completes the execution with
  `dex.GracefulComplete(Receipt{ProjectID, WorkspaceRequestID, Status})`. The workspace Flow
  copies what it keeps into `Instance`, so nothing depends on the retained execution.
- **Attributes.** `Request`, `Resources`, `Checkout`, `WorkspaceTimings` and `Outcome`; no
  AttributeMap, Channel, ChannelMap or Stream; no lock, because the Steps form one chain.
- **Steps.** `ProvisionInstance` (`PollingStepOptions(30m)`), `CheckoutWorkspace` (15 minutes),
  `VerifyInstanceHealthy` (5 minutes), `ReportProvisionOutcome` and the failure route
  `RecordProvisionFailure` (`PROVISION_FAILED`, `PROVISION_DEADLINE`, `CHECKOUT_FAILED`,
  `INSTANCE_UNHEALTHY`, `WORKSPACE_PROFILE_MISMATCH`, `WORKSPACE_VOLUME_IN_USE`); the whole
  provisioning is bounded by `provision.Budget` (55 minutes). RPCs `GetProvision`,
  `GetDexSummary` and `GetDexDisplay`; the client reads a provisioning without an outcome
  whose engine execution stopped as failed `WORKSPACE_EXECUTION_STOPPED`.

### PreviewFlow

Contract: [the Preview README](../../../internal/agents/sandbox/preview/README.md).

- **Identity.** FlowID `preview-<projectID>`, for example `preview-a7k2`
  (`flowid.PreviewFlowID`), the Project's one Preview, started on its first Preview command in
  the first-demand form or on its first automatic deployment, with `dex.IDReuseDisallow`, a
  RequestID fingerprinting `{ProjectID}` and same-request IgnoreError. `STOPPED` is a business
  state of the running execution; only `EndPreview`, which the deleting Project calls, leads to
  `CompletePreview`.
- **Attributes.** `ProjectID` (indexed `keyword2`), `Identity`, `Generation` (constant 1) and
  `Revision` (the command fence, seeded from the start time so grant generations never repeat),
  `WorkspaceRequestID` and `Resources` of the acquired instance, `WorkspaceState` and
  `HeadCommit` of the last source preparation, `PreviewState` (with `Timings` and
  `ConfigurationGaps`), `PreviewEnvironment`, `AppManifest`, `PreparedPreviewSource`,
  `PreviewConfigurationRef`, `ActiveCommand`, `LastCommand`, `Failure`, `PreviewDeployment`,
  `OutcomeReport`, `CreatedAt`, `UpdatedAt` and `CommandLock`. No AttributeMap.
- **Channels.** `PreviewCommands` (kinds `prepare-preview-source`, `start-preview`,
  `stop-preview`, `reclaim`, `deploy-preview` and `release`) and `WorkspaceSettled`. No
  ChannelMap or Stream.
- **Steps and RPCs.** `AwaitPreviewCommands`, `RoutePreviewCommand`, `AcquireSessionWorkspace`,
  `AwaitWorkspaceSettled` (90 minutes), `PreparePreviewSource`, `VerifyPreviewSource`,
  `PreparePreviewConfiguration`, `EnsurePreviewEnvironment`, `StartPreview`,
  `ProbeAndExposePreview`, `BeginPreviewStop`, `StopPreview`, `StopPreviewEnvironment`,
  `RecordForcedEnvironmentStop`, `CompletePreviewCommand`, `RecordPreviewRefusal`,
  `RecordPreviewFailure`, `NotifyPreviewOutcome` and `CompletePreview`; RPCs `GetPreview`,
  `RequestPreviewCommand`, `RequestPreviewDeployment`, `NotifyWorkspaceSettled`, `EndPreview`,
  `GetDexSummary` and `GetDexDisplay`. The automatic deployment and its outcome reports are
  described in [notifications](notifications-and-feedback.md#preview-auto-deploy-after-a-push).

### Compatibility

`SandboxProvisionFlow` and `PreviewFlow` replaced `SandboxFlow` before launch, and
`SessionWorkspaceFlow` took the instance lifecycle over from `SessionAgentFlow` the same way:
the Session's instance fields and the Steps `ProvisionSessionSandbox`,
`AwaitSandboxProvisioning`, `ReclaimSessionSandbox`, `CompleteSandboxReclamation` and
`RecordSandboxReclaimFailure` are gone, and the Session's close tail, the SandboxFlow callbacks
and the Session status `CLOSING` were removed earlier. No alias is registered and no open
execution is migrated: before a rollout every Project is deleted through the business API
(rollout reconciliation).
The generated graphs `docs/flows/sv2-session-workspace-flow.json`,
`sv2-sandbox-provision-flow.json` and `sv2-preview-flow.json` are regenerated with every
change to a graph.

## Cold start

A cold start is the time from the demand for a workspace to a usable Sandbox. Two paths
matter, and with a one-hour hold the second is the common one:

1. **First instance of a Project.** The isolation boundary and the volume do not exist yet:
   the provisioning creates them, starts the Pod and checks the repository out.
2. **Resume.** The boundary and the volume exist (the previous Pod was reclaimed at the
   Sandbox deadline). A new provisioning starts a Pod on the same volume; the checkout adopts
   the existing tree, uncommitted work included, and the Preview engine resumes from its state
   on the volume when the Preview starts.

The durable `WorkspaceTimings` (`provision`, `checkout`) of each provisioning, which the
workspace Flow keeps in `Instance.Timings` and Studio shows, and `PreviewState.Timings` of
each start, record both paths. No placeholder Pod, image pre-pull or warm pool is part of the current design.

## Resources

| Component | Budget | Lives in |
|---|---|---|
| Coding container | 500m / 1 GiB requests; 2 CPU / 4 GiB limits; 10 GiB PVC; 8 GiB ephemeral; 2 GiB `/tmp` | `_coding-profile.tpl`; Terraform `coding_profile.workspace_size` |
| Platform Dex | 250m / 512 MiB to 2 CPU / 2 GiB | chart `dex.resources` |
| Server | 250m / 256 MiB to 2 CPU / 2 GiB | `server.resources` |
| Studio | 100m / 192 MiB to 1 CPU / 1 GiB | `studio.resources` |
| Sandbox controller | 25m / 64 MiB to 500m / 256 MiB | `sandboxController.resources` |
| Live Project Dex | 100m / 256 MiB to 2 CPU / 2 GiB | `internal/projects/infra/kube/resources.go` |
| Deployed application default | 100m / 128 MiB to 2 CPU / 1 GiB | `live_serving_application.go` |
| Platform nodes | t4g.large on demand (test and staging); gVisor Sandbox nodes and, apart from them, gVisor Live application nodes from Karpenter, Spot backed by On-Demand; ceilings per environment in `node_pool_limits`  | `environments/{test,staging}/cloud.tfvars`, `terraform/cloud/karpenter.tf` and the chart's node pools |

Limits are concurrency limits, not larger budgets. The namespace quota stays
4 CPU / 8 GiB. A Sandbox runs the engine (Temporal and Dex), the Preview application, the
builds of the main agent, up to three subagents' builds and at most one application test
engine (about 300 MiB), all in a 4 GiB container. That is why subagent commands run with
`nice -n 10`, `GOMAXPROCS=2` and a bounded Node heap, why at most three subagents run at
once, and why test engines queue ([subagents](subagents.md#resource-rules),
[commit gate](commit-gate-and-app-tests.md)).

## Phase timings

Studio shows where time goes. The model is the same everywhere:
`dexflow.PhaseTiming{Phase, StartedAt, Duration}`, where a zero `Duration` marks the phase
in progress, phase names are code constants and never provider text, and a repeated
attempt keeps the phase's first start. The save that moves a command into a phase starts
it, the save that leaves it ends it, and both failure-record Steps end the phase in
progress. A timing is a measurement, not a deadline.

| Recorded by | Attribute | Phases |
|---|---|---|
| `SandboxProvisionFlow` | `WorkspaceTimings`, at most 2, never replaced; the workspace Flow copies them into `Instance.Timings` with the outcome | `provision`, `checkout` |
| `PreviewFlow` | `PreviewState.Timings`, at most 5, replaced when a start is admitted | `configuration`, `environment`, `grant`, `application`, `route` |
| `ProjectDeployFlow` | `PhaseTimings`, one per phase, at most 16, a rerun phase replaces its entry | The deployment phases, including the two parallel preparation branches |
| `ProjectReleaseFlow` | `ReleaseBuildRevision.Timings` and the prepared source's `Timings`, at most 16 | `QUEUE`, `HOST_PREPARATION`, `DEPENDENCY_PREPARATION`, `ANALYSIS`, `COMPILATION`, `IMAGE_PUBLICATION`, and the Flow-side `CONFIGURATION_FREEZE`, `ARTIFACT_PUBLICATION` |

Release provider phases come from the builder: hosted (CodeBuild phases plus the runner's
monotonic segments in its result) and local (the local builder's state), both mapped to the
same codes. A build or preparation that reaches its wait limit records no provider
timings, because the observation is committed only after the poll ends. Progress frames
carry only the Step name as `Phase`; pending reasons and build states move to a
`Detail` field that is never mapped to the API.

The API carries a message `PhaseTiming{string phase, Timestamp started_at, int64
duration_ms}` whose `phase` is an open string set, `ResourceProgress.created_at`, and
`repeated PhaseTiming phase_timings` on the Deployment, Release build revision and
prepared-source snapshots, on `PreviewSnapshot` (the current or last start) and on
`SessionWorkspaceSnapshot` (the provisioning that ended with the current instance; empty
while it provisions). Studio labels the known phases and shows others verbatim.

## Database Schema Changes

None. No database, projection, counter or durable mirror is added for the Sandbox. The
lease and the deadline live only in the Session's `Workspace` Attribute; the instance, its
retained references and its phase timings in the workspace Flow's `Instance` Attribute; a
provisioning's state is the Attributes of its own Flow and the Preview's state the Attributes
of `PreviewFlow`; the command ledger, the intent records and the worktree plans and receipts
are files in the Sandbox that nothing trusts; the runtime owns object existence. In Local
Native the labels on Docker objects, the gateway's route files and the private credential
files are identities and configuration of external resources, not state. The Sandbox-local
Preview engine has its own SQLite store (Temporal's schema, no migration), which lives and
dies with the volume.

## UI/UX

- **Workspace.** The Project page shows the workspace status (`PROVISIONING`, `READY`,
  `CLOSING` while a reclamation deletes the Pod, `CLOSED` once it is gone, `FAILED`), the
  phases of its provisioning, and its deadline (`expires_at`, the Sandbox deadline) with a
  "Keep running for 7 days" button, enabled while the workspace is READY. After a reclaim it
  shows that the Sandbox paused to save resources and that the files are kept. "Preparing
  workspace" covers both a first start and a resume.
- **Preview.** The Preview panel always shows the stable URL and its status, `NOT_STARTED`
  before the first Preview command. While no application runs it offers "Start Preview";
  nothing serves the URL meanwhile. Starting shows the phases as they happen ("Starting
  engine", "Authorizing", "Building app", "Exposing route"; zero duration means in progress).
  A start on a Session without a READY instance is accepted and waits while the Session
  brings one up. After a turn that pushed, the Preview deploys the pushed commit by itself on
  a READY instance, with no click
  ([notifications](notifications-and-feedback.md#preview-auto-deploy-after-a-push)). After a
  deployment that ended before READY, "Start Preview" and "Retry Preview cleanup" follow the
  state its `release` left ([Preview panel](streaming-and-studio.md#preview-panel)). The panel
  names the commit it serves, since a start deploys the prepared commit even when the
  workspace has moved on. Opening the Preview configuration never starts a Sandbox;
  "Prepare current source" is an explicit action.
- **Timings.** Deployment, Release, Preview and workspace views show the duration of each
  phase.
- **Failures.** A failed cold path appears as a model turn without repository context or
  as a failed tool row; an engine or Preview failure appears as a delivery notification
  ([notifications](notifications-and-feedback.md)). The lease, the binding, retained
  references, internal endpoints and credentials never reach the browser.

## Tests

Real dependencies only: actual Dex, Kubernetes or Docker, sandboxd, the coding image, the
repository host and MinIO/S3. The Agent paths use the mock model; the provisioning and
Preview tests need no model. A missing service is a failure, never a skip. Local Native,
Local Kind and MinIO establish local API coverage, not AWS acceptance, and the lanes a
change needs are named in [composition](composition.md#local-lanes). Assertions cover
retained state and single external effects, never an unchanged Run.

The tests that run each Flow, and the scenarios no real-dependency test runs yet, are listed
in the package READMEs of [the Session workspace](../../../internal/agents/sandbox/sessionworkspace/README.md#tests),
[the provisioning](../../../internal/agents/sandbox/provision/README.md#tests),
[the Preview](../../../internal/agents/sandbox/preview/README.md#tests),
[the Kubernetes instances](../../../internal/agents/sandbox/kube/README.md) and
[the Docker instances](../../../internal/agents/sandbox/docker/README.md). The coverage they
keep, by area:

- **Identity and admission.** A Session start creates no workspace Flow and no provisioning;
  the first demand starts `session-workspace-<projectID>` and
  `sandbox-provision-<projectID>-ws-<n>`; a repeated command joins, an older one is refused;
  a later demand after a reclamation starts a new provisioning with a new WorkspaceRequestID
  on the kept volume; a repeated start of the same request joins its one execution; a Worker
  replaced during a provisioning or a reclamation leaves one execution. A read of a Project
  that never ran a Preview command reports absence and starts nothing.
- **Commands and races.** A provision command waits behind a reclamation, so one volume
  carries one Pod; a replacement reclaims the lost instance first; a late outcome after a
  replacement is declined and the instance reclaimed. A Preview command that arrives while
  another is active is `BUSY`; `reclaim` of an instance the Preview does not hold is
  `NOT_HELD`; `deploy-preview` requests coalesce to the newest and a retried sequence is
  `DUPLICATE`; a Preview whose first command was refused before it acquired an instance admits
  the next command.
- **Zero Flow RPC.** A turn that runs file tools, commands and `commit_and_push` on a usable
  lease produces no cold-path log line and no new event in the workspace, provisioning or
  Preview histories. The pinned SDK client has no interceptor, so these observations are the
  evidence.
- **Deadline and reclamation.** With a shortened hold the deadline reclamation is exactly the
  `reclaim` command and the `reclaimed` report, with no PreviewFlow; a background command holds
  the instance past its deadline; a reclamation of the instance a READY Preview serves on stops
  the Preview first, whose engine its supervisor stops gracefully before the Pod deletion, and
  removes the route; an uncommitted file survives reclaim and resume; Preview configuration
  reads with a reclaimed Pod start nothing.
- **Project deletion.** One `CompleteProjectSession` call; the workspace Flow and the Preview
  end; a deletion during a provisioning, during a reclamation and after an abnormal Session end
  leaves no Pod, volume, boundary, route object or Preview data; a release failure keeps the
  Project DELETING until a retried deletion.
- **Preview.** Pinned deployments, refusals that keep the preparation, incompatible
  configuration with its gaps, the `release` after a failed deployment, a deployment on a
  reclaimed instance that ends `SKIPPED`, and a stopped Preview execution brought back by time
  travel; the route lifecycle on both runtimes.
- **Helper container tests** (the real coding image, real sandboxd, fast): the ledger starts
  one command per ID, reads output by offset, enforces the lifetime and the output limit,
  stops a command, and reports `lost` and `instance_replaced`; the file helper's basic
  behavior and its path rules; timeouts. The supervisor
  ([previewctl README](../../../sandbox/coding/cmd/superverse-previewctl/README.md)) keeps
  its own container verification.
- **Kind only.** Admission policies, NetworkPolicy, quotas, the Sandbox controller, PVC
  behavior and the Pod's restrictions of the coding image (read-only root, permissions,
  volumes) are shown only by Local Kind, which is why a change to the coding image or profile
  always runs there.
- **Unverified until AWS.** `Setsid`, flock and the `/proc/1` start time under gVisor;
  SQLite WAL and mmap on the gVisor volume; ReadWriteOncePod; Pod Identity tags and the
  denial of another Project's prefix; the in-place Preview grant policy and the denial of a
  revoked generation's cached credentials; admission policies.

## Documentation

This document owns the Sandbox design; the package READMEs of `internal/agents/sandbox` and
its `sessionworkspace`, `provision`, `preview`, `kube`, `docker` and `workspace` packages own
the per-Flow contracts and the file tables. The commands keep their own READMEs, which carry
exact flags and JSON shapes: the
[previewctl README](../../../sandbox/coding/cmd/superverse-previewctl/README.md), the
[superverse-exec](../../../sandbox/coding/cmd/superverse-exec/README.md) and
[superverse-files](../../../sandbox/coding/cmd/superverse-files/README.md) READMEs and the
[coding image README](../../../sandbox/coding/README.md). [StartFlow admission](../start-flow-admission.md)
keeps the start and FlowID reuse rules, [deployment names](../superverse-v2.md) the
resource names, and the rollout reconciliation runbook
the cleanup of executions of earlier graphs. [the current contract](../../runbooks/local-native.md)
records the Docker identity mapping, the accepted deviations and the lane rules,
the current contract the lifecycle decisions,
their move into `SessionWorkspaceFlow` and the amendments to decisions 0006 and 0013, and
the current contract the READY
Preview hold.
