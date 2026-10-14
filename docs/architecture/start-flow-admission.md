# StartFlow admission

## Dex Flow Changes

The complete Flow primitive, lifecycle and recovery inventory is in
[the architecture](superverse-v2.md). This document narrows the StartFlow boundary;
it adds no Flow, Attribute, AttributeMap instance, Channel, ChannelMap, Stream,
index, or persistence field. Existing Step/RPC identities, locks and retention
remain authoritative there and in the owning README.

Start first with a stable FlowID, complete safe logical RequestID, an explicit
reuse policy and `AlreadyStarted.IgnoreError=true`. Twelve call sites use
`IDReuseDisallow`, `TeamSpaceFlow`
([contract](teamspace/teamspaces-and-roles.md#identity-and-lifecycle)), the Sandbox
instance Flows and the subagents among them; the platform issue reports use
`dex.IDReuseAllowIfNotRunning`. Accounts start no Flow (they are database rows,
[accounts](teamspace/accounts.md));
a platform issue report after the run of its signature closed is a new run, as
specified in [Platform issue reports](#platform-issue-reports);
the Sandbox instance Flows never reuse a FlowID, as specified in
[Sandbox provisioning and Preview starts](#sandbox-provisioning-and-preview-starts),
and neither do the subagents, as specified in [Subagent starts](#subagent-starts).
Dex accepts exact retries
without replacing accepted input/Attributes. An acceptance-only successful start immediately returns known resource IDs,
without a snapshot RPC or a comparison against newly generated initial values. A different
RequestID for a running execution yields typed `FlowAlreadyStartedError`, mapped
to a resource conflict. AllowIfNotRunning does not deduplicate even the same
RequestID across a closed execution; a platform issue report then starts a new run.
Authorization, credential verification, admission RPCs and explicit recovery
reads still serve their own purposes. Get/Watch owns authoritative snapshots. They are not pre-start deduplication.

There are exactly twelve production StartFlow call sites:

Domain Dex access belongs to the Flow client of the Flow it fronts:
`<FlowType>FlowClient` in `<flow_type>_flow_client.go`, with additional
responsibilities in `<flow_type>_flow_client_<responsibility>.go`. Every Flow has
its own client in its own package, and a client fronts exactly one Flow type; it
reaches another Flow only through that Flow's client. The clients are
`projects.ProjectFlowClient`, `setup.ProjectSetupFlowClient`, `repobinding.RepositoryBindingFlowClient`,
`sessionagent.SessionAgentFlowClient`, `sessionworkspace.SessionWorkspaceFlowClient`,
`provision.SandboxProvisionFlowClient`,
`preview.PreviewFlowClient`, `subagent.SubagentFlowClient`,
`platformissue.PlatformIssueFlowClient`,
`releases.ProjectReleaseFlowClient` and `deployments.ProjectDeployFlowClient`.
`ProjectFlowClient` recognizes the administrator through
`accountdao.AccountDAO.IsPlatformAdmin`.

The other categories follow the same rule set. A Flow bootstrap,
`<FlowType>FlowBootstrap` in `<flow_type>_flow_bootstrap.go`, assembles one Flow
with its process-local resources and closes them:
`sessionagent.SessionAgentFlowBootstrap`, `subagent.SubagentFlowBootstrap`,
`provision.SandboxProvisionFlowBootstrap` and `platformissue.PlatformIssueFlowBootstrap`. A Service is a general component above Dex
Flows that Flows and the API both use, the concrete type `<Domain>FlowService` in
`<domain>_flow_service.go`, in a package other than the Flows that depend on it.
`agents.SessionFlowService` (in `session_flow_service.go`) coordinates the Session,
provisioning, Preview, subagent, platform issue and Project Flows and owns no Flow; it
issues the Session, provisioning, Preview, subagent and platform issue starts through
their Flow clients. A Flow package never
names its own dependencies `XyzService`: the interface through which a Flow
consumes such a Service is `<FlowType>FlowDependency`, here
`sessionagent.SessionAgentFlowDependency`, `provision.SandboxProvisionFlowDependency`,
`preview.PreviewFlowDependency` and `subagent.SubagentFlowDependency`; the Session
starts and stops its subagents through `agenttasks.AgentTaskLauncher`, which the
Service implements, because neither Flow package imports the other. A narrow single-capability port keeps its
responsibility name, such as `projects.SessionLifecycle` or
`sessionagent.repositoryContextReader`. Project's Session child admission lives in
`project_flow_client_session.go`. `projectconfig.Configuration`
owns scope authorization and admits configuration operations to the in-process
configuration host. External
GitHub, Kubernetes and SDK clients keep their existing Client names.
`SessionAgentFlowBootstrap` and workspace Capabilities own disposable resources,
not Flow state.
Setup result/resource handoff, deployment lease admission/release and Live commit,
and Project deletion/Live-removal commands also use the owning `ProjectFlowClient`.
Their RPC locks, revision/generation checks and cleanup ownership belong to the
owning Flow; provider effects execute in the owning durable Steps.
Boundary and file names are source-only: they do not change the Flow types,
Step/RPC/primitive names, payload fields, indexes or external resource identities.
Complete real business acceptance remains required after static
checks, as documented in the owning component test sections.

Pure unlocked read RPCs do not require active execution: retained terminal
executions remain readable when the handler returns no durable effects. The
pinned SDK translates the server's missing-target substatus to
`dex.FlowNotActiveOrNotFoundError` for InvokeRPC even on these reads. Project
Read/ReadChild,
`ProjectSetupFlowClient.Read`, `RepositoryBindingFlowClient.Find`, Agent
GetSnapshot/GetSessionSummary/GetArchivedMessages, provisioning Get, Preview Get,
subagent Snapshot,
Release ReadSnapshot/ReadBuildRevision and Deployment ReadSnapshot normalize that typed
missing-target result to FlowNotFound at their owning Flow client through
`dexflow.InvokeRetainedRead`, which accepts exact AttributeMap loads but no
locks. `RepositoryBindingFlowClient.Find` then returns its established absent
result, nil/nil.
These paths do not perform WaitForFlow existence probes. No provisioning or
Preview FlowID is reused, so a Get of either reaches the one execution of its
FlowID, completed or not, and its callers need not compare an instance identity
to tell executions apart. Worker, transport,
hydration and handler failures remain errors. Mutations, locks and transactions
retain inactive-execution errors and their required receipt/effect reconciliation.
A Dex Server configuration that serves every RPC as an active-execution write is
incompatible with these retained reads; the real dependency checks validate the
supported read-only path.

| Owner | FlowID / example | Accepted RequestID and retained reads |
| --- | --- | --- |
| projects.ProjectFlowClient.Create | `project-a7k2` | Authenticated owner + original CREATE request fields; the managed source is required and fixed. Exclude the server-selected template and provider results. CreateProject returns the known ProjectID, including matching retries. No Project or Setup snapshot read. |
| setup.ProjectSetupFlowClient.StartForProject, called through `projects.SetupLifecycle` from the Project StartProjectSetup Execute | `setup-a7k2-k9m4p7w2` | Exact accepted Project StartIdentity. Read accepted Project/template/repository; never use a newer request/default. Setup independently registers membership before effects. |
| repobinding.RepositoryBindingFlowClient.Claim | `repository-123456789` | Numeric repository ID. After StartFlow invoke the ClaimRepository RPC: generation/claim ownership is its actual serialized business admission. A released binding completed its execution: StartFlow joins it under the same RequestID and the RPC's inactive-execution error is classified as FAILED_PRECONDITION, so no Run starts. |
| sessionagent.SessionAgentFlowClient.StartAccepted, called through `projects.SessionLifecycle` from the Project StartProjectSession Execute | `session-a7k2` | Fingerprint of `{ProjectID}`, once the Project is READY. `IDReuseDisallow`: a retried or replayed start and a repeated setup commit join the one execution; another RequestID conflicts. Server defaults (profile, model, prompt, HEAD) are not logical input. Return only start success or error; the Session starts no Sandbox and registers itself as the Project's child in its first Step. There is no API start. |
| sessionworkspace.SessionWorkspaceFlowClient.Submit, called through `SessionFlowService.ProvisionWorkspace` and `ReclaimWorkspace` from the Session's durable SendWorkspaceCommand Execute | `session-workspace-a7k2` | Fingerprint of `{SessionFlowID}`. `IDReuseDisallow` with same-request `IgnoreError`: every command after the first joins the one execution. After StartFlow invoke the SubmitWorkspaceCommand RPC: the engine joins a same-request start of an ended execution as it answers a new one, so the RPC confirms every command, accepts a repeat of an admitted command without publishing it, refuses an older request's command, and its inactive-execution error is `ErrWorkspaceClosed`. The provision path first requires a READY Project. |
| provision.SandboxProvisionFlowClient.Start | `sandbox-provision-a7k2-ws-41` | Fingerprint of `{SessionFlowID, WorkspaceRequestID, retained volume reference, profile}`. `IDReuseDisallow`: a retry joins the execution, running or completed. The SessionWorkspaceFlow's durable ProvisionInstance Execute starts directly, without a Get used to decide whether Start is necessary. Return only start success or error. The outcome arrives separately through the workspace Flow RPC ReportProvisionOutcome, and the workspace Flow reads it from the provisioning; no first-Step admission is needed. |
| preview.PreviewFlowClient.Start | `preview-a7k2` | Fingerprint of `{ProjectID}`. `IDReuseDisallow`: a repeated start joins the Project's one Preview execution. The Session Service starts it on the Project's first Preview command in the first-demand form, or its first automatic deployment, before the command. Return only start success or error. |
| subagent.SubagentFlowClient.Start | `subagent-a7k2-k3n8q5r1` | Fingerprint of the admitted `start_agent` call (`{SessionFlowID, AgentID, Kind, Description, PromptSHA256}`) and the worktree's snapshot commit, without the Session's model, HEAD, lease or a time. `IDReuseDisallow`: a retry, also one with a fresher lease, joins the execution, running or completed. The Session's durable `start_agent` ExecuteTool starts it through the SessionFlowService after it created the worktree; the subagent's first Step asks the Session to admit it before any effect. Return only start success or error. |
| platformissue.PlatformIssueFlowClient.Report, called through `SessionFlowService.ReportPlatformIssue` from the Session's side-branch ReportPlatformIssue Execute | `platform-issue-test-3f9a1c2b7d4e5f60` | Fingerprint of `{SessionFlowID, CallID, Signature}`. `IDReuseAllowIfNotRunning`: a retry of the same report joins the running run; another report of the signature while it runs is `*dex.FlowAlreadyStartedError`, which the client answers as joined; a report after the run closed starts a new run of the same FlowID, even with the same RequestID. The first Step's label query is the admission check. Return only start success, joined or error. |
| contribution.ConnectorContributionFlowClient.Submit, called from the Session Service once a COMPLETED develop subagent's report is admitted | `connector-contribution-a7k2-mockfeed` | Fingerprint of `{SessionFlowID, slug, branch, head commit}`. `IDReuseDisallow` with same-request `IgnoreError`. After StartFlow, also after `FlowAlreadyStartedError` of a later head, invoke the UpdateConnectorContribution RPC: the engine joins a same-request start of an ended execution as it answers a new one, and the owner requires an ended contribution to refuse every submission, so the RPC is idempotent for the starting submission and its inactive-execution error is `ErrContributionClosed`. The first Step admits the contribution with its Session before any effect. |
| releases.ProjectReleaseFlowClient.Prepare | `release-a7k2-<commit>` | Actor + request ID + the immutable project/commit/branch/repository-binding source fingerprint + the server-selected build profile version and digest. Success returns the Release ID. Only typed AlreadyStarted reads immutable identity/revision and admits an explicit source-preparation retry; builds are admitted later by RPC under their own build IDs. |
| deployments.ProjectDeployFlowClient.Deploy | `deploy-a7k2-d6m4p7w2` | Complete immutable desired deployment + actor + logical command ID. StartDeployment returns the deployment ID. Active/unreleased deployment reads reconcile actual effects, not start existence. Only the explicit different-command/same-immutable-input recovery path compares accepted fingerprint after typed AlreadyStarted. |

Each call supplies these options (the named values are those computed in the
actual call sites above). Release, Deployment and Session additionally pin ASYNC
durability; the Session has no Flow timeout; every call passes a nil input (the
Session's start Step takes `dex.None`), and the platform issue report passes
`dex.IDReuseAllowIfNotRunning`:

```go
_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{
    IDReusePolicy: dex.IDReuseDisallow,
    RequestID: &requestID,
    AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
    Attributes: attributes,
})
```

CreateProject, PrepareRelease, StartReleaseBuild and StartDeployment
return only the resource-specific ID of the resource they started
(`project_id`, `release_id`, `build_revision_id` or `deployment_id`). The
Project's Session has no API start: ProjectFlow starts it
([the Project's Session start](#the-projects-session-start)). Their responses contain no status, revision, timestamps,
server-selected defaults or synthetic snapshot. A matching retry may refer to a
resource that has already progressed or closed; Get/Watch reports its current
state and authorization. Studio navigates/selects those IDs and retains one
resource Watch rather than inserting acceptance into snapshot state. No generic
operation resource or RunID is exposed.

RegisterAccount starts no Flow: it verifies the password and commits the login row in
the database. `RepositoryBindingFlowClient.Claim` invokes the serialized ownership RPC.
These are actual business milestones and cannot be removed as start deduplication. Future APIs
that promise a durable milestone must bound an explicit WaitForAttributeMatch or
WaitForStepCompletion, not assume acceptance means completion.

The actual Project-to-Setup call runs in Execute, with SYNC handler timeout30s,
external deadline20s and two minutes of retries. Exhaustion records
SETUP_FAILED without claiming the child is absent. Its late successful handoff
clears only that failure; deletion fences child admission before effects. See
[Project](../../internal/projects/README.md#dex-flow-changes).
The Session-to-workspace and workspace-to-provisioning calls likewise belong to durable
Execute: the Session's SendWorkspaceCommand Step (`dexflow.ProviderStepOptions()`, no
lock) submits its command, starting the Project's SessionWorkspaceFlow on the first one,
and a business rejection or retry exhaustion enters RecordWorkspaceFailure, which
records `WORKSPACE_START_FAILED` with its reason; the workspace Flow's ProvisionInstance
Step starts the instance's SandboxProvisionFlow, and a rejection or retry exhaustion
enters RecordProvisionFailure, which reports `WORKSPACE_START_FAILED` without claiming
the provisioning is absent. No cleanup start exists: the workspace Flow's reclamation
deletes the Pod, and the deleting Project's Session cleanup Execute calls
`CompleteProjectSession` and `CompleteSessionWorkspace` once each and releases every
resource by label.
Repository claims and Release/Deployment recovery remain durable business RPCs.

### The Project's Session start

A Project has exactly one Session, which ProjectFlow starts
([the current contract](agent-harness-design/session.md)).

- Identity. FlowID `session-<projectID>`, for example `session-a7k2`; the
  Project's `ChildFlows` key is the same FlowID.
- Trigger. `CommitProjectSetup` makes the Project READY and moves to the Step
  `StartProjectSession`, whose provider Execute, without a Project lock, calls
  `SessionLifecycle.StartForProject` and so `SessionAgentFlowClient.StartAccepted`
  with the default profile, the Project's AppManifest commit as HEAD and no
  message. A Project that is no longer READY, because its deletion began, starts
  none. The API has no Session start; Studio applies the model the person chose
  on the creation page with `UpdateSessionModel` when it differs from the
  Session's, then sends the creation intent as the first `SendMessage`.
- Start RequestID and options. `flowid.Fingerprint(struct{ProjectID string})`,
  `IDReuseDisallow`, `AlreadyStarted.IgnoreError: true`, ASYNC durability and no
  Flow timeout. A retried Execute, a replayed Step and a repeated commit, which
  finds the Project READY and starts nothing new, join the one execution; a start
  with another RequestID is `*dex.FlowAlreadyStartedError`.
- Recovery. Exhausted retries go to `RecordProjectSessionStartFailure`, which
  records `SESSION_START_FAILED` in the Project's SessionStartFailure, apart from
  the command failure in LastFailure, while the Project stays READY, unless the
  Session already registered itself. A start whose every response was lost still runs:
  the Session's first Step registers `session-<projectID>` in `ChildFlows`, which
  clears that failure. An operator restarts a Session that never started.
- First-Step admission. `InitializeSessionAgent` asks the Project to register the
  Session before any effect; a Project that froze admission for its deletion
  denies it, and the Session completes `admission_denied`. A deletion right after
  READY therefore ends without a Session, with `admission_denied`, or with a
  registered Session that the deletion completes.
- Completion. Only the Project's deletion completes the Session
  (`CompleteProjectSession`); there is no API close.

### Sandbox provisioning and Preview starts

The Session's SessionWorkspaceFlow owns each Sandbox instance
([the current contract](agent-harness-design/sandbox.md)), so neither the
provisioning nor the Preview reuses a FlowID or asks whether its request is still
wanted. All three are independent top-level Flows of their Session, not Project children.
SandboxFlow, whose FlowID was reused for the sequential instances of a Session, was
replaced by them before launch.

- Identity. `SessionWorkspaceFlow` is one execution per Project: FlowID
  `session-workspace-<projectID>`, for example `session-workspace-a7k2`, started by
  the Session's first command as the table above specifies.
  `SandboxProvisionFlow` is one execution per instance: FlowID
  `sandbox-provision-<projectID>-<workspaceRequestID>`, for example
  `sandbox-provision-a7k2-ws-41`. `PreviewFlow` is one execution per Project:
  FlowID `preview-<projectID>`, for example `preview-a7k2`.
- Trigger. The SessionWorkspaceFlow's ProvisionInstance Execute starts a provisioning for
  the Session's `provision` command through `SessionFlowService.StartProvisioning`,
  never the Session's start. The
  Session Service starts the Preview on the Project's first Preview command in the
  first-demand form, or its first automatic deployment, before it sends the command.
  A read starts neither.
- Business identity. The Session chooses the WorkspaceRequestID
  `"ws-" + <numeric suffix of the StepExecutionID>` of the RequestSessionWorkspace
  execution that opens the request, for example `ws-41` from
  `RequestSessionWorkspace-41`. It is persisted in the provisioning's `Request` and is
  carried and compared by the workspace commands, the lease, the outcome reports and the
  reclamation. RunID is
  never business identity.
- Start RequestID. `flowid.Fingerprint` of `{SessionFlowID, WorkspaceRequestID, retained
  volume reference, profile}` for a provisioning and of `{ProjectID}` for the Preview.
- Options. `IDReusePolicy: dex.IDReuseDisallow`, `AlreadyStarted.IgnoreError: true` and
  the initial Attributes of the Flow's start.

| Existing execution | Same RequestID | Different RequestID |
| --- | --- | --- |
| None | New execution | New execution |
| Running | Success; joins the running execution without replacing its input or Attributes | `*dex.FlowAlreadyStartedError`; a programming error here, because the FlowID names the request |
| Completed | Success; joins the completed execution and provisions nothing again | `*dex.FlowAlreadyStartedError` |

- No first-Step admission. A repeated start of a settled request joins its completed
  execution and causes no provider effect, so the provisioning does not ask whether its
  request is still wanted. The workspace Flow instead admits the provisioning's report
  (`ReportProvisionOutcome`) only while that instance is the one it provisions, and the
  Session admits the workspace Flow's outcome (`ReportWorkspaceOutcome`) only while it
  waits for exactly that request; a declined outcome is recorded, and the workspace
  Flow reclaims that instance.
- Acceptance is not readiness. Start returns only success or an error. The workspace
  Flow learns of the outcome from the provisioning's report, or reads it itself when its
  bounded wait (`provision.Budget` plus ten minutes) ends, and always reads it from the
  provisioning, which owns it; the Session's own bounded wait outlasts that.
- Completion and retention. A provisioning completes with `GracefulComplete(Receipt)`
  once it has reported its outcome or recorded an undelivered report; the workspace Flow
  copies what it keeps into its `Instance` Attribute, so nothing depends on the retained
  execution afterwards. The workspace Flow completes only when the deleting Project ends
  it (`CompleteSessionWorkspace`). The Preview completes only when the deleting Project ends it
  (`EndPreview`); `STOPPED` is a business state of its running execution.
- Compatibility. A pre-launch direct replacement: no SandboxFlow execution is read or
  migrated. Before the rollout every Project is deleted through the business API
  (rollout reconciliation),
  and the Session-keyed FlowIDs were replaced the same way
  (rollout reconciliation).

The workspace, provisioning and Preview Attribute, Channel, Stream, Step and RPC
inventory is specified in [the architecture](superverse-v2.md#sandbox-instance-flows), the
[Session workspace](../../internal/agents/sandbox/sessionworkspace/README.md), the
[provisioning](../../internal/agents/sandbox/provision/README.md) and the
[Preview](../../internal/agents/sandbox/preview/README.md) READMEs; this boundary
declares no AttributeMap, ChannelMap or Stream of its own.

Exact retries do not create another FlowID or intentionally another RunID.
New logical entities use new IDs; closed IDs cannot represent another operation.
The platform issue FlowID is the single exception: a report after its run closed is a
new run of the same FlowID, as stated above.
Previously accepted fingerprints are never recalculated or migrated implicitly.
PrepareRelease binds Start RequestID to the logical command plus the immutable
source fingerprint. A different command for the same commit yields typed
AlreadyStarted; the matching fingerprint lets it join the accepted source owner
or admit an explicit source-preparation retry without changing its identity.
Removed entities reject new commands. In particular, old CREATE fingerprints
including template default differ: their old IDs conflict with new identity
bytes. Normal cleanup and a new request is the supported transition; the
Session-keyed executions of earlier Projects end with their Projects.

### Subagent starts

A subagent belongs to its Session, which admits, records, stops and merges it, so
it is an independent top-level Flow and not a Project child
([SubagentFlow](superverse-v2.md#subagentflow)).

- Identity. One `SubagentFlow` execution per `start_agent` call: FlowID
  `subagent-<projectID>-<agentID>`, for example `subagent-a7k2-k3n8q5r1`, where
  the agent ID is
  `flowid.AgentTaskID(sessionFlowID, callID)`, so every retry of the call names the
  same subagent.
- Trigger. Only the Session's `start_agent` ExecuteTool, after RouteTool admitted the
  call and wrote its STARTING record and after the worktree was created, through
  `SessionFlowService.StartSubagent`. Neither an API call nor ProjectFlow starts one.
- Start RequestID and options. The fingerprint above, `IDReuseDisallow`,
  `AlreadyStarted.IgnoreError: true`, a nil input and the initial Attributes of the
  start (`Identity`, `Start`, `ProjectID`, `SubagentStatus`, `Workspace`); no Flow
  timeout, because the kind's duration is the subagent's own business timer.
- First-Step admission. A repeated start joins the one execution, but a start may be
  delayed past a Stop or the Project's deletion. The first
  Step therefore asks the Session (`AdmitSubagent`) before any effect, and the Session
  admits only the call its STARTING record names while it is ACTIVE and no Stop is
  pending; a denied subagent completes `DENIED` without an effect.
- Acceptance is not completion. Start returns only success or an error; the result of
  `start_agent` is the task ID and the snapshot commit, and the subagent's end
  arrives as a `task` notification. The Session's record and the subagent's
  `GetSubagentSnapshot` own its state.

### Platform issue reports

A platform failure of a Session is filed by the `PlatformIssueFlow` of its signature
([the platform issue reports](../../internal/agents/platformissue/README.md)), an
independent top-level Flow that belongs to no Project.

- Identity. One FlowID per signature and environment, `platform-issue-<env>-<sig16>`, for
  example `platform-issue-test-3f9a1c2b7d4e5f60`: the first 16 hex characters of the
  SHA-256 of the fault's owner, tool, code and error type, as its GitHub label
  `superverse-incident:<env>:<sig16>` carries them.
- Trigger. Only the Session's side branch `ReportPlatformIssue`, through
  `SessionFlowService.ReportPlatformIssue`. A read starts none.
- Start RequestID and options. `flowid.Fingerprint` of `{SessionFlowID, CallID,
  Signature}`, `IDReuseAllowIfNotRunning`, `AlreadyStarted.IgnoreError: true`, a nil input
  and the initial `Incident`.

| Existing run | Same RequestID | Different RequestID |
| --- | --- | --- |
| None | New run | New run |
| Running | Success; joins the running run without replacing its `Incident` | `*dex.FlowAlreadyStartedError`; the client answers it as joined |
| Closed | New run of the same FlowID | New run of the same FlowID |

- First-Step admission. The issue's owner is GitHub: `EnsurePlatformIssue` queries the
  label, open and closed issues, before it opens one, so a new run of a reused FlowID finds
  the issue an earlier run opened. An issue GitHub opened without the label is committed by
  the decision into `LabelPlatformIssue` before the label is attached, so a retried label call
  never opens another. GitHub's label listing is eventually consistent for a few
  seconds, so a run that starts within seconds after the run that opened the issue
  completed may still open a second one; this is accepted.
- Acceptance is not completion. The Session never waits for or reads the report; the run
  records its outcome and completes.

## Database Schema Changes

None. There is no start ledger, request lookup Flow, table or cache authority.

## UI/UX

Release candidate discovery returns commits only. The user chooses a commit,
never an infrastructure recipe: trusted server composition selects the build
recipe when the Release source is prepared, and each build revision retains its
accepted recipe across retry and recovery.

The Session, provisioning and Preview starts have no public acceptance DTO.
Studio reads the Project's Session by its ProjectID once the Project is READY,
repeating a not-found read for at most 30 seconds while ProjectFlow starts it;
the instance and Preview state reach Studio only through Get/Watch, which show
the instance identity but never a lease or a RunID.

## Tests

Registration replay and password conflict are real PostgreSQL tests of the
[Account service](../../internal/tenancy/accounts/README.md#tests). The Session's start identity,
a repeated setup commit, a joined and a conflicting start, and deletion before
and right after READY are Dex integration tests
(`TestProjectSessionIdentityDexIntegration`,
`TestProjectDeletionBeforeReadyDexIntegration`,
`TestProjectDeletionRightAfterReadyDexIntegration`), written and compiled, not
yet observed. The durable Project interrupted-client scenario passed with actual
Dex/GitHub/Kubernetes after its asynchronous config-update assertion was corrected;
the receipt
preserves the initial failure and both normal cleanups. It proves matching acceptance while that new delayed root advertises
an unavailable Worker, then explicitly restores the real Worker for provider work
and normal deletion. No existing execution or substitute dependency is involved. Full Local Native and final Local Kind still must validate current source,
Release recovery, Sandbox replay, provider uncertainty and deletion races. Static
checks are not evidence of those behaviors.

The provisioning and Preview starts require real Dex/Temporal and Kubernetes
scenarios against the pinned Server. Their Dex integration tests are written and
compiled (the [Session Service tests](../../internal/agents/README.md#tests)); none
is recorded as observed here:

- The Session's start creates no `sandbox-provision-<projectID>-<workspaceRequestID>`
  execution.
- A repeated start of the same complete request, while its provisioning runs or after it
  completed, joins that execution and keeps its accepted Attributes
  (`TestSandboxProvisionStartJoinsDexIntegration`).
- A Worker replaced while a provisioning polls leaves one execution that provisions the
  request (`TestSessionWorkspaceWorkerRestartDexIntegration`).
- A demand after a reclamation at the Sandbox deadline starts a new provisioning, with a
  new WorkspaceRequestID and FlowID, on the kept volume
  (`TestSandboxDeadlineReclaimDexIntegration`), and a late report of an earlier request
  does not change the Session.
- A read of a Project that never ran a Preview command reports absence and starts no
  execution (`TestMissingPreviewReadDexIntegration`); its first Preview command starts the
  one PreviewFlow execution of the Project.
- A retained read of a completed provisioning returns its outcome
  (`TestRetainedProvisionReadsDexIntegration`).
- A repeated start of a completed subagent joins its one execution, the subagent's
  FlowID and agent ID derive from the Project, the Session and the call, and the Session denies a
  late admission (`TestSessionMockSubagentInvestigateDexIntegration`); a subagent
  whose start a Project's deletion overtook is stopped or denied
  (`TestProjectDeletionStopsSubagentsDexIntegration`).
- A report of a platform failure from another call, while the run of its signature is
  open, joins that run, a retry of the first report joins it too, and the one run keeps the
  first report's incident (`TestPlatformIssueJoinDexIntegration`; against GitHub, the
  manual `TestPlatformIssueFlowGitHubIntegration` opens exactly one issue).

## Documentation

[Session](../../internal/agents/README.md),
[Project](../../internal/projects/README.md), [Sandbox](../../internal/agents/sandbox/README.md),
[Release](../../internal/delivery/releases/README.md) and [Deployment](../../internal/delivery/deployments/README.md)
retain the complete owning inventories. The Session's start is specified in
[the current contract](agent-harness-design/session.md), the provisioning and
Preview starts in [the current contract](agent-harness-design/sandbox.md), the
[provisioning](../../internal/agents/sandbox/provision/README.md) and
[Preview](../../internal/agents/sandbox/preview/README.md) READMEs, the subagent start in the
[subagent](../../internal/agents/session/subagent/README.md) README; the Session side of
the workspace commands are specified in the [Sandbox design](agent-harness-design/sandbox.md#gates-and-the-cold-path)
and their wiring in [composition](agent-harness-design/composition.md). Root rules require this start boundary in
future review. Historical source fingerprints and acceptance receipts are retained.
