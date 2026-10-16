# Business gRPC API

`api/superverse/v2/superverse.proto` is the only business protocol
(the current contract). It holds 11
services of one domain each, which `api.NewGRPCServer` registers on one private listener
of `superverse-server`. The Studio BFF is its only client.

- `make generate-proto` runs the pinned buf with the protoc plugins of the `go.mod` tool
  block into `gen/`, then rewrites the service table below from the same descriptors.
  `npm --prefix web/studio run generate:protobuf` regenerates the Studio types with the
  lockfile-pinned `proto-loader-gen-types`.
- `make check` runs `check-proto-format` (`buf format`), `check-proto` (Buf STANDARD lint
  without exceptions and `scripts/check-proto-services.py`) and `check-generated-proto`;
  `make check-generated-web` checks the Studio types. These are static checks, not
  integration evidence.
- Before launch, obsolete fields are removed without `reserved` numbers or names, and
  retained fields keep their wire numbers. Go and Studio regenerate after every edit and
  deploy together; no alias of an old name is kept.

`api.NewGRPCServer` assembles the listener. Its outermost unary and stream interceptors
recover a handler panic, log the panic value and stack, and return Internal with a fixed
message; no panic detail reaches the caller. A Watch's sender and source readers run on
their own goroutines, which the interceptors cannot reach. They recover in place: a panic
is logged the same way and ends that Watch with the same Internal status.

## Services

<!-- BEGIN GENERATED SERVICE TABLE (make generate-proto) -->

16 services, 102 RPCs. A field is a string unless a type is given. Every request
except RegisterAccount and Login also carries `CallerContext caller 100`, which the table omits.

| Service | RPCs | Reached through |
| --- | --- | --- |
| AccountService | 4 | `/api/auth` |
| TeamSpaceService | 6 | `/api/rpc/<Method>` |
| TeamSpaceMemberService | 4 | `/api/rpc/<Method>` |
| TeamSpaceConnectionService | 10 | `/api/rpc/<Method>` |
| ProjectService | 10 | `/api/rpc/<Method>` |
| ProjectMemberService | 4 | `/api/rpc/<Method>` |
| SessionService | 4 | `/api/rpc/<Method>` |
| RemoteAgentService | 4 | `/api/rpc/<Method>` |
| AgentService | 9 | `/api/rpc/<Method>` |
| PreviewService | 6 | `/api/rpc/<Method>` |
| ReleaseService | 10 | `/api/rpc/<Method>` |
| DeploymentService | 5 | `/api/rpc/<Method>` |
| FlowRunService | 8 | `/dex/...` |
| FlowDiagnosticsService | 9 | `/dex/...` |
| ConnectorService | 7 | `/dex/...` |
| ApplicationEnvironmentService | 2 | `/dex/...` |

**AccountService**

| RPC | Request | Response |
| --- | --- | --- |
| RegisterAccount | `request_id 1, email 2, password 3` | `AccountLogin login 1` |
| Login | `email 1, password 2` | `AccountLogin login 1` |
| Logout | `{}` | `{}` |
| GetCurrentAccount | `{}` | `AccountSnapshot account 1` |

**TeamSpaceService**

| RPC | Request | Response |
| --- | --- | --- |
| CreateTeamSpace | `request_id 1, name 2` | `team_space_id 1` |
| ListTeamSpaces | `int32 page_size 1, page_token 2, repeated TeamSpaceStatus statuses 3` | `repeated TeamSpaceSnapshot team_spaces 1, next_page_token 2` |
| GetTeamSpace | `team_space_id 1` | `TeamSpaceSnapshot team_space 1` |
| RenameTeamSpace | `team_space_id 1, uint64 expected_revision 2, name 3` | `TeamSpaceSnapshot team_space 1` |
| DeleteTeamSpace | `team_space_id 1, request_id 2` | `TeamSpaceSnapshot team_space 1` |
| GetTeamSpaceOverview | `team_space_id 1` | `TeamSpaceOverview overview 1` |

**TeamSpaceMemberService**

| RPC | Request | Response |
| --- | --- | --- |
| ListTeamSpaceMembers | `team_space_id 1` | `repeated TeamSpaceMemberSnapshot members 1` |
| AddTeamSpaceMember | `team_space_id 1, email 2, TeamSpaceRole role 3` | `repeated TeamSpaceMemberSnapshot members 1, uint64 revision 2, int32 regained_project_count 3` |
| SetTeamSpaceMemberRole | `team_space_id 1, account_id 2, TeamSpaceRole role 3` | `repeated TeamSpaceMemberSnapshot members 1, uint64 revision 2` |
| RemoveTeamSpaceMember | `team_space_id 1, account_id 2` | `repeated TeamSpaceMemberSnapshot members 1, uint64 revision 2` |

**TeamSpaceConnectionService**

| RPC | Request | Response |
| --- | --- | --- |
| ListConnectorCatalog | `{}` | `repeated ConnectorCatalogEntry connectors 1` |
| ListTeamSpaceConnections | `team_space_id 1, connector_id 2, int32 page_size 3, page_token 4, repeated TeamSpaceConnectionStatus statuses 5` | `repeated TeamSpaceConnectionSnapshot connections 1, next_page_token 2` |
| GetTeamSpaceConnection | `team_space_id 1, connection_id 2` | `TeamSpaceConnectionSnapshot connection 1, repeated stored_credential_fields 2` |
| CreateTeamSpaceConnection | `team_space_id 1, request_id 2, name 3, connector_id 4, module_path 5, version 6, auth_method_id 7, ConnectionClientSource client_source 8, settings_json 9, credentials_json 10, copy_from_connection_id 11` | `connection_id 1` |
| UpdateTeamSpaceConnection | `team_space_id 1, connection_id 2, uint64 expected_revision 3, settings_json 4, credentials_json 5, repeated keep_credential_fields 6, uint64 expected_credential_revision 7` | `TeamSpaceConnectionSnapshot connection 1` |
| DeleteTeamSpaceConnection | `team_space_id 1, connection_id 2, uint64 expected_revision 3` | `{}` |
| StartTeamSpaceConnectionOAuth | `team_space_id 1, connection_id 2, version 3, client_id 4, client_secret 5, credential_values_json 6, credential_secrets_json 7, NewTeamSpaceConnection new_connection 8` | `authorization_url 1, Timestamp expires_at 2` |
| CompleteTeamSpaceConnectionOAuth | `state 1, code 2, provider_error 3` | `TeamSpaceConnectionSnapshot connection 1` |
| StartTeamSpaceConnectionUiSession | `team_space_id 1, connection_id 2, connector_id 3, module_path 4, version 5` | `session_nonce 1, manifest_json 2, repeated platform_client_method_ids 3` |
| InvokeTeamSpaceConnectionCommand | `team_space_id 1, session_nonce 2, command_id 3, parameters_json 4` | `result_json 1` |

**ProjectService**

| RPC | Request | Response |
| --- | --- | --- |
| CreateProject | `request_id 2, name 3, ManagedRepositorySource managed_repository 6, team_space_id 8` | `project_id 1` |
| ListProjects | `int32 page_size 1, page_token 2, repeated ProjectStatus statuses 3, team_space_id 4` | `repeated ProjectSnapshot projects 1, next_page_token 2` |
| GetProject | `project_id 1` | `ProjectSnapshot project 1` |
| DeleteProject | `project_id 1, request_id 2, uint64 expected_revision 3` | `ProjectSnapshot project 1` |
| WatchProject | `project_id 1, resume_cursor 2` | stream `oneof event {ProjectSnapshot snapshot 1; ResourceProgress progress 2}, resume_cursor 3` |
| GetProjectConfiguration | `ProjectConfigurationTarget target 1` | `ProjectConfigurationSnapshot configuration 1` |
| ValidateProjectConfiguration | `ProjectConfigurationTarget target 1, request_id 2, uint64 expected_revision 3` | `ProjectConfigurationSnapshot configuration 1` |
| RemoveLive | `project_id 1, request_id 2, deployment_id 3, uint64 expected_live_revision 4` | `ProjectSnapshot project 1` |
| ListEnvironmentEvents | `project_id 1, ProjectScope scope 2, uint32 page_size 3, page_token 4` | `repeated EnvironmentEvent events 1, next_page_token 2` |
| ListRecentTriggerEvents | `project_id 1, ProjectScope scope 2` | `repeated RecentTriggerEvent events 1, Timestamp buffer_started_at 2, buffer_instance_id 3` |

**ProjectMemberService**

| RPC | Request | Response |
| --- | --- | --- |
| ListProjectMembers | `project_id 1` | `repeated ProjectMemberSnapshot members 1` |
| AddProjectMember | `project_id 1, account_id 2, ProjectRole role 3` | `repeated ProjectMemberSnapshot members 1, uint64 revision 2` |
| SetProjectMemberRole | `project_id 1, account_id 2, ProjectRole role 3` | `repeated ProjectMemberSnapshot members 1, uint64 revision 2` |
| RemoveProjectMember | `project_id 1, account_id 2` | `repeated ProjectMemberSnapshot members 1, uint64 revision 2` |

**SessionService**

| RPC | Request | Response |
| --- | --- | --- |
| GetSession | `project_id 1` | `SessionSnapshot session 1` |
| WatchSession | `project_id 1, resume_cursor 3` | stream `oneof event {SessionSnapshot snapshot 1; SessionProgress progress 2}, resume_cursor 3` |
| ListArchivedMessages | `project_id 1, int64 before_sequence 3, int32 page_size 4` | `AgentHistoryPage history 1` |
| RenewWorkspace | `project_id 1` | `bool renewed 1` |

**RemoteAgentService**

| RPC | Request | Response |
| --- | --- | --- |
| CreateRemoteAgentPairing | `project_id 1` | `connection_id 1, code 2, Timestamp expires_at 3, endpoint 4, uint64 basis_epoch 5, current_connection_id 6, Actor current_owner 7` |
| ListRemoteAgentConnections | `project_id 1` | `repeated RemoteAgentConnection connections 1, uint64 epoch 2, current_connection_id 3, Actor current_owner 4, endpoint 5` |
| RevokeRemoteAgentConnection | `project_id 1, connection_id 2` | `{}` |
| SetSessionExecutor | `project_id 1, uint64 basis_epoch 2` | `{}` |

**AgentService**

| RPC | Request | Response |
| --- | --- | --- |
| SendMessage | `project_id 1, message_id 3, text 4` | `{}` |
| AnswerQuestions | `project_id 1, AgentAnswerQuestionsInput input 3` | `{}` |
| UpdateSessionModel | `project_id 1, AgentModel model 3, optional uint64 expected_revision 4` | `{}` |
| DeleteQueuedMessage | `project_id 1, message_id 3` | `{}` |
| SteerQueuedMessage | `project_id 1, message_id 3` | `{}` |
| ResolveToolApproval | `project_id 1, call_id 3, optional bool is_approved 4` | `{}` |
| InterruptAgent | `project_id 1, int64 turn 3` | `{}` |
| ListMockScenarios | `{}` | `bool selectable 1, repeated AgentMockScenario scenarios 2` |
| GetFlowDesign | `project_id 1, AgentFlowDesignSlot slot 3, digest 4` | `AgentFlowDesign design 1` |

**PreviewService**

| RPC | Request | Response |
| --- | --- | --- |
| GetPreview | `project_id 1` | `PreviewSnapshot preview 1` |
| StartPreview | `project_id 1, request_id 3, uint64 expected_revision 4, uint64 generation 5, workspace_request_id 6, bool apply_configuration 7, uint64 configuration_revision 8` | `PreviewSnapshot preview 1` |
| StopPreview | `project_id 1, request_id 3, uint64 expected_revision 4, uint64 generation 5, workspace_request_id 6` | `PreviewSnapshot preview 1` |
| PreparePreviewSource | `project_id 1, request_id 3, uint64 expected_revision 4, uint64 generation 5, workspace_request_id 6` | `{}` |
| GetPreviewDefinitions | `project_id 1` | `PreviewSnapshot preview 1, repeated FlowDefinition flow_definitions 2` |
| UpdatePreviewAutoDeploy | `project_id 1, optional bool is_enabled 2` | `{}` |

**ReleaseService**

| RPC | Request | Response |
| --- | --- | --- |
| ListReleaseCandidates | `project_id 1` | `repeated ReleaseCandidate candidates 1` |
| PrepareRelease | `project_id 1, request_id 2, source_commit 3` | `release_id 1` |
| StartReleaseBuild | `project_id 1, request_id 2, release_id 3, build_revision_id 5, uint64 configuration_revision 6, uint64 expected_revision 7, bool auto_deploy 8, uint64 expected_live_revision 9, preview_skip_reason 10` | `build_revision_id 1` |
| RecoverReleaseBuild | `project_id 1, request_id 2, release_id 3, build_revision_id 4, uint64 expected_revision 5` | `{}` |
| ListReleases | `project_id 1, int32 page_size 2, page_token 3, repeated ReleaseStatus statuses 4` | `repeated ReleaseSnapshot releases 1, next_page_token 2` |
| DeriveLiveConfiguration | `project_id 1, release_id 2, uint64 expected_revision 4, uint64 expected_preview_configuration_revision 5` | `ProjectConfigurationSnapshot configuration 1` |
| GetRelease | `project_id 1, release_id 2` | `ReleaseSnapshot release 1, repeated FlowDefinition flow_definitions 2` |
| WatchRelease | `project_id 1, release_id 2, resume_cursor 3` | stream `oneof event {ReleaseSnapshot snapshot 1; ResourceProgress progress 2}, resume_cursor 3` |
| CancelReleaseBuild | `project_id 1, request_id 2, release_id 3, build_revision_id 4` | `{}` |
| GetReleaseBuildLog | `project_id 1, release_id 2, build_revision_id 3, cursor 4` | `repeated lines 1, next_cursor 2, bool complete 3, uint32 retry_after_ms 4` |

**DeploymentService**

| RPC | Request | Response |
| --- | --- | --- |
| StartDeployment | `project_id 1, deployment_id 2, request_id 3, release_id 4, uint64 configuration_revision 5, uint64 expected_live_revision 6, build_revision_id 7` | `deployment_id 1` |
| GetDeployment | `project_id 1, deployment_id 2` | `DeploymentSnapshot deployment 1` |
| ListDeployments | `project_id 1, int32 page_size 2, page_token 3, repeated DeploymentStatus statuses 4` | `repeated DeploymentSnapshot deployments 1, next_page_token 2` |
| WatchDeployment | `project_id 1, deployment_id 2, resume_cursor 3` | stream `oneof event {DeploymentSnapshot snapshot 1; ResourceProgress progress 2}, resume_cursor 3` |
| CancelDeployment | `project_id 1, deployment_id 2, request_id 3` | `{}` |

**FlowRunService**

| RPC | Request | Response |
| --- | --- | --- |
| GetFlowDefinitions | `FlowApiContext context 1` | `FlowApiDocument document 1` |
| GetFlowCatalog | `FlowApiContext context 1` | `FlowApiDocument document 1` |
| SearchFlows | `FlowApiContext context 1, flow_type 2, repeated FlowSearchFilter filters 3, int32 page_size 4, page_token 5, bool work_queue_only 6` | `FlowApiDocument document 1` |
| GetFlowDisplay | `FlowApiContext context 1, flow_type 2, flow_id 3` | `FlowApiDocument document 1` |
| UpdateFlowDisplayField | `FlowApiContext context 1, flow_type 2, flow_id 3, attribute_key 4, bytes value_json 5` | `FlowApiDocument document 1` |
| InvokeFlowAction | `FlowApiContext context 1, flow_type 2, flow_id 3, rpc_name 4, bytes input_json 5, bytes attribute_snapshot_json 6` | `FlowApiDocument document 1` |
| StartFlow | `FlowApiContext context 1, flow_type 2, flow_id 3, request_id 4, target_revision 5, bytes input_json 6` | `FlowApiDocument document 1` |
| RecoverFlowStart | `FlowApiContext context 1, flow_type 2, flow_id 3, request_id 4, target_revision 5, bytes input_json 6` | `FlowApiDocument document 1` |

**FlowDiagnosticsService**

| RPC | Request | Response |
| --- | --- | --- |
| GetFlowSummary | `FlowApiContext context 1, flow_id 2, run_id 3` | `FlowApiDocument document 1` |
| GetFlowState | `FlowApiContext context 1, flow_id 2, run_id 3` | `FlowApiDocument document 1` |
| GetFlowHistory | `FlowApiContext context 1, flow_id 2, run_id 3, page_token 4, int32 page_size 5, int64 after_event_id 6` | `FlowApiDocument document 1` |
| WaitFlowHistory | `FlowApiContext context 1, flow_id 2, run_id 3, int64 after_event_id 6` | `FlowApiDocument document 1` |
| ReadFlowStream | `FlowApiContext context 1, flow_id 2, stream_name 3, resume_token 4, flow_type 5` | `FlowApiDocument document 1` |
| LoadFlowBlobs | `FlowApiContext context 1, flow_id 2, repeated FlowBlobReference values 3` | `FlowApiDocument document 1` |
| StopFlow | `FlowApiContext context 1, flow_id 2, run_id 3, int32 stop_type 4, reason 5` | `FlowApiDocument document 1` |
| DeleteFlowChannelMessage | `FlowApiContext context 1, flow_id 2, run_id 3, channel_name 4, message_id 5` | `FlowApiDocument document 1` |
| TimeTravelFlow | `FlowApiContext context 1, flow_id 2, run_id 3, int32 time_travel_type 4, reason 5, step_type 6, step_execution_id 7, history_event_time 8, int32 step_method 9` | `FlowApiDocument document 1` |

**ConnectorService**

| RPC | Request | Response |
| --- | --- | --- |
| ListConnections | `FlowApiContext context 1` | `FlowApiDocument document 1` |
| SelectTeamSpaceConnection | `FlowApiContext context 1, connector_id 2, connection_name 3, team_space_connection_id 4, bool follow_preview 5` | `FlowApiDocument document 1` |
| ReleaseUnusedConnections | `FlowApiContext context 1` | `FlowApiDocument document 1` |
| StartConnectorUiSession | `FlowApiContext context 1, connector_id 2, connection_name 3` | `FlowApiDocument document 1` |
| InvokeProviderCommand | `FlowApiContext context 1, session_nonce 2, command_id 3, map<string, string> parameters 4` | `FlowApiDocument document 1` |
| SaveTriggerBinding | `FlowApiContext context 1, connector_id 2, connection_name 3, trigger_name 4, binding_name 5, bytes configuration_json 6, bool follow_preview 7` | `FlowApiDocument document 1` |
| SaveOperationConfiguration | `FlowApiContext context 1, connector_id 2, connection_name 3, operation_id 4, flow_type 5, step_type 6, bytes configuration_json 7, bool follow_preview 8` | `FlowApiDocument document 1` |

**ApplicationEnvironmentService**

| RPC | Request | Response |
| --- | --- | --- |
| GetApplicationEnvironment | `FlowApiContext context 1` | `FlowApiDocument document 1` |
| SaveApplicationEnvironment | `FlowApiContext context 1, map<string, string> values 2, map<string, string> secrets 3, repeated remove 4, repeated follow_preview 5` | `FlowApiDocument document 1` |

<!-- END GENERATED SERVICE TABLE -->

## Message model

Buf lint or `scripts/check-proto-services.py` enforces each rule that can be checked
mechanically.

1. **M1. One request and response per RPC.** Each RPC has its own `<Rpc>Request` and
   `<Rpc>Response`, never shared and never `google.protobuf.Empty`. A response that returns
   a shared DTO wraps it in one field named after the resource: `project`, `session`,
   `preview`, `configuration`, `deployment`, `release`, `account`, `login`, `history` or
   `document`.
2. **M2. Identity.** IDs are flat string fields at the start of the request, in hierarchy
   order (`project_id`, then the child ID). There are no Ref or Mutation messages. The only
   shared target DTOs are `ProjectConfigurationTarget {project_id 1, ProjectScope scope 2,
   release_id 4}`, which `internal/dexai/projects/config` consumes, and `FlowApiContext`. A
   Project has one Session, so the Session, its Agent commands and its Preview are
   addressed by `project_id` alone.
3. **M3. Caller.** Every request except RegisterAccountRequest and LoginRequest has
   `CallerContext caller = 100` at the top level. Only the BFF sets it. No other field is
   numbered 100 or higher.
4. **M4. Idempotency and fences.** `request_id` appears on every deduplicated start or
   command. The fences are `uint64 expected_revision` (the addressed resource's revision),
   `uint64 generation` with `workspace_request_id` (the Sandbox instance) and
   `uint64 expected_live_revision` (the Project's Live pointer revision). The page sends
   what it observed; the BFF never fills in a fence or an ID. Every request ID requires 1
   to 128 bytes: the API checks it right after authentication and rejects any other value
   with `INVALID_ARGUMENT` ("request ID must contain 1 to 128 bytes") before a Flow client
   sees the request, including a RemoveLive on a Project without Live.
5. **M5. Integer types follow the Go domain type.** uint64 holds revisions, generations
   and fences; int64 holds Agent sequences, plan revisions, task indices, `repository_id`
   and `after_event_id`; int32 holds the values Go holds as int32: page sizes, exit codes,
   `AgentEvent.attempt` and the Dex numeric pass-through values (`stop_type`,
   `time_travel_type`, `step_method`). uint32 is only `FlowApiDocument.http_status`.
6. **M6. Timestamps** use `google.protobuf.Timestamp`, with one exception: the Agent DTO
   date strings keep RFC 3339 (the Agent conversation contract). A request of a
   `/api/rpc` service never carries one (see M14).
7. **M7. Enums.** Closed sets are top-level enums; the Agent DTOs keep theirs nested. The
   value name is the UPPER_SNAKE enum-name prefix plus the persisted domain value,
   upper-cased, and zero is `<PREFIX>_UNSPECIFIED`, meaning absent. Dex `keyword3` search
   values are unchanged. Open sets stay strings: `Failure.code` and `.phase`, every
   progress `phase`, `PhaseTiming.phase`, `SetupSnapshot.phase`, model IDs and
   `reasoning_effort`.
8. **M8. Lists.** Requests carry `page_size` (0 means 50, at most 100), `page_token` and
   `repeated <Status> statuses`; a zero or undeclared status is InvalidArgument. Responses
   carry the plural items and `next_page_token`. ListReleaseCandidates is not paged,
   and ListArchivedMessages pages
   by a sequence anchor (`before_sequence`, with `page_size` from 10 to 200 in steps of 10
   and 10 by default). ListProjects requires `team_space_id` and lists, newest first, the
   Projects of that TeamSpace that are not DELETED from the read model's `projects` table,
   for one of its members or the platform admin; anyone else gets PermissionDenied. Each
   entry carries the caller's effective `caller_role`. An entry in which the caller has no
   role is reduced to `project_id`, `name`, `status`, the setup `status`, `live` and
   `team_space_id`.
9. **M9. Watch.** The request is the IDs, `resume_cursor` and the caller. The response is
   `Watch<R>Response {oneof event {<R>Snapshot snapshot 1; <P> progress 2}; resume_cursor
   3}`, and the first frame is always a snapshot. `ResourceProgress {phase 1; uint64
   revision 2; Timestamp created_at 3}` serves Project, Release and Deployment; it carries
   the Step name and the event time, never the free-text detail of the waiting Step.
   `SessionProgress` is `{oneof content {AgentEvent activity 3; reasoning_summary 7;
   assistant_text 8}; Timestamp created_at 5; source 6; task_id 9}`.
10. **M10. Acceptance responses.** A start returns the resource-specific ID of the resource
    it started, from validated input, without a snapshot read: `project_id`,
    `release_id`, `deployment_id` or `build_revision_id`. ProjectFlow starts the Project's
    Session; no RPC does. The Dex StartFlow behind a start,
    including a matching-request replay, causes no immediate snapshot read and promises
    neither readiness nor current state; Get and snapshot-first Watch keep their full
    authority and error contracts. PrepareRelease binds its request ID into the Dex start
    identity, and typed AlreadyStarted is its only Release snapshot and recovery branch.
    Deployment keeps its reads for unresolved Project-owned effects and typed conflict
    recovery. Studio selects and navigates by the returned IDs, without inserting acceptance
    into resource state or showing a synthetic initial status, revision or time. A command
    on a resource the caller fully named returns `{}`, or the updated snapshot where the
    page renders it immediately: DeleteProject, RemoveLive, StartPreview, StopPreview and
    ValidateProjectConfiguration. RenewWorkspace answers whether it renewed. Responses never echo the
    caller's other inputs.
11. **M11. Field numbers.** Fields that survive keep their numbers. Removed fields are
    deleted, with no `reserved` before launch. A new field takes a number above the highest
    number its message used before, below 100. Fields are declared in ascending number
    order; a oneof block sorts by its lowest member.
12. **M12. Persisted payloads are frozen.** `AppManifestSnapshot`, `ObjectReference`,
    `ConnectorRequirement` and `ConnectorTriggerBinding` are the persisted Attribute and
    Step values of ProjectFlow, ProjectSetupFlow, ProjectReleaseFlow and PreviewFlow, which
    the Dex SDK encodes with encoding/json keyed by the protoc-gen-go field names. Their
    message names, field names, numbers, types and labels never change;
    `check-proto-services.py` pins them. Editing them is a persisted-key change that needs
    local execution reconciliation, even before launch.
13. **M13. Naming.** Message, enum, service and RPC names are PascalCase with acronyms
    written as words (`FlowApi`, `ConnectorUi`); `OAuth` is the one exception.
14. **M14. Browser requests.** A request of a service the browser reaches through
    `/api/rpc` carries JSON as `string *_json` fields (`settings_json`,
    `credentials_json`, `credential_secrets_json`, `parameters_json` and the like), which
    the server parses and type-checks, and reaches no `bytes`, `map`, `Struct`, `Value`,
    `ListValue` or `Timestamp`, at any depth; `check-proto-services.py` refuses them.
    Responses may carry them. The Flow API services keep their `bytes` and `map` fields.

## Enums

Every value comes from a write in the owning code; the Go names are the domain constants.

| Enum | Values after the prefix | Owner | Used by |
| --- | --- | --- | --- |
| ProjectStatus | CREATING, READY, SETUP_FAILED, DELETING, DELETED | ProjectStatus Attribute (`projects.Status*`) | ProjectSnapshot.status, ListProjects filter |
| SetupStatus | PENDING, RUNNING, COMPLETE, SETUP_FAILED, CANCELLED, CLEANING | SetupStatus Attribute (`setup.Status*`); the API also reports PENDING before the Setup Flow exists | SetupSnapshot.status |
| SessionStatus | ACTIVE, ARCHIVED, FAILED | `agentloop.SessionStatus*` (ACTIVE, ARCHIVED); the API derives FAILED from a stopped execution, which is never indexed | SessionSnapshot.status |
| WorkspaceStatus | INITIALIZING, PROVISIONING, READY, CLOSING, CLOSED, FAILED | `agentloop.InstanceState.WorkspaceStatus()`: an instance `provisioning`, `ready`, `reclaiming`, `reclaimed` or `failed` is PROVISIONING, READY, CLOSING, CLOSED or FAILED; INITIALIZING is no longer produced | SessionWorkspaceSnapshot.status |
| WorkspaceState | ABSENT, CLEAN, DIRTY, UNKNOWN | `sandbox.Workspace*` | SessionWorkspaceSnapshot.workspace_state |
| PreviewStatus | NOT_STARTED, STARTING, READY, STOPPING, STOPPED | `preview.Preview*`; an empty value is UNSPECIFIED | PreviewSnapshot.status |
| PreviewSourceStatus | NOT_PREPARED, PREPARING, READY, FAILED | `sandbox.PreviewSource*` | PreparedPreviewSourceSnapshot.status |
| PreviewDeploymentStatus | RUNNING, READY, INCOMPATIBLE, FAILED, SKIPPED, CANCELLED | `preview.Deployment*`; UNSPECIFIED while only a queued request exists | PreviewDeploymentSnapshot.status |
| ConfigurationRequirementGap.Kind (`KIND_`) | CONNECTION_REQUIRED, AUTHORIZATION_REQUIRED, CONNECTION_INCOMPLETE, TRIGGER_BINDING_REQUIRED, TRIGGER_BINDING_INVALID, OPERATION_CONFIGURATION_REQUIRED, ENVIRONMENT_REQUIRED | `appmanifest.Gap*`, mapped by `appmanifest.RequirementGapSnapshots` beside each kind | ProjectConfigurationSnapshot.requirement_gaps, PreviewSnapshot.configuration_gaps |
| ProjectScope | PREVIEW, LIVE | `infra.Scope.Kind`, mapped only by `infra.ScopeOf` | ProjectConfigurationTarget, ProjectConfigurationSnapshot, FlowApiTarget |
| ConfigurationStatus | DRAFT, NEEDS_REVIEW, READY | `internal/dexai/projects/config` | ProjectConfigurationSnapshot.status |
| ReleaseStatus | PENDING, DRAFT, BUILDING, READY, FAILED, RECOVERING, REMOVING, REMOVED, CANCELLED (a build revision a person cancelled) | `releases.Status`, one set for a Release, its prepared source and each build revision | ReleaseSnapshot, PreparedReleaseSourceSnapshot, ReleaseBuildRevisionSnapshot, ListReleases filter |
| FailureRemedy | OPEN_SETUP, APPLY_AGAIN, ASK_AGENT, REAUTHORIZE, FREE_WORKSPACE, PUBLISH_AGAIN, CONTACT_OPERATOR, CANCEL_DEPLOYMENT (the deployment that `RELEASE_BUILD_DEPLOYING` names in its ErrorInfo metadata `deploymentId`) | `dexflow.FailureRemedy`, declared with each failure code and ErrorInfo reason; UNSPECIFIED on a failure recorded before it | Failure.remedy, AgentNotification.remedy, ErrorInfo metadata `remedy` |
| FailureOwner | APPLICATION, CONFIGURATION, PLATFORM | `conversation.DeliveryOwner`, recorded beside a Preview, Release build or source failure, derived from a Deployment's phase; UNSPECIFIED elsewhere | Failure.owner |
| AutoDeployStatus | PENDING, ACCEPTED, REJECTED, UNKNOWN | `releases` automatic deployment outcomes; UNSPECIFIED when `auto_deploy` is false | ReleaseBuildRevisionSnapshot.auto_deploy_status |
| DeploymentStatus | PENDING, DEPLOYING, READY, FAILED, RECOVERING, CANCELLING, REMOVED, RETIRING, RETIRED, CANCELLED | `deployments.Status` | DeploymentSnapshot.status, ListDeployments filter |
| AgentMessage.Role (`ROLE_`) | SYSTEM, USER, ASSISTANT, TOOL, NOTIFICATION | conversation | AgentMessage.role |
| AgentNotification.Kind (`KIND_`) | TASK, DELIVERY, CONNECTOR | conversation | AgentNotification.kind |
| AgentNotification.Source (`SOURCE_`) | SUBAGENT, COMMAND, PREVIEW, RELEASE, DEPLOYMENT, CONNECTOR | conversation | AgentNotification.source |
| AgentNotification.TaskStatus (`TASK_STATUS_`) | EXITED, LOST; COMPLETED, FAILED, STOPPED, TIMED_OUT | agenttasks (a background command's end; a subagent's end) | AgentNotification.task_status |
| AgentNotification.Outcome (`OUTCOME_`) | READY, FAILURE, NOTICE | conversation (`conversation.Delivery*`); UNSPECIFIED except for a delivery | AgentNotification.outcome |
| AgentNotification.Owner (`OWNER_`) | APPLICATION, CONFIGURATION, PLATFORM | conversation (`conversation.DeliveryOwner*`), which the Preview's deciding Step or the Release or Deployment that records the failure sets; UNSPECIFIED except for a delivery failure or notice | AgentNotification.owner |
| AgentSubagent.Kind (`KIND_`) | INVESTIGATE, CHANGE, CONNECTOR_SELECT, CONNECTOR_DEVELOP | agenttasks | AgentSubagent.kind |
| AgentNotification.ConnectorEvent (`CONNECTOR_EVENT_`) | PULL_REQUEST_FAILED, RELEASE_CANDIDATE_PUBLISHED, RELEASE_CANDIDATE_FAILED; UNSPECIFIED on every other notification | `conversation.ConnectorEvent` | AgentNotification.connector_event |
| ConnectorContribution.Kind (`KIND_`) | NEW, OPERATION, FIX | `conversation.ContributionKind` | ConnectorContribution.kind |
| ConnectorContribution.Status (`STATUS_`) | PULL_REQUEST_PENDING, UNPUBLISHED, PULL_REQUEST_FAILED, DRAFT, CLOSED | `contribution.Status` | ConnectorContribution.status |
| ConnectorContribution.Location (`LOCATION_`) | UPSTREAM, LOCAL_GIT_HOST, SANDBOX | `connectorlibrary.Location` | ConnectorContribution.location |
| ConnectorContribution.ReleaseCandidateStatus (`RELEASE_CANDIDATE_STATUS_`) | PUBLISHED, FAILED; UNSPECIFIED before the first candidate | `contribution.ReleaseCandidateStatus` | ConnectorContribution.release_candidate_status |
| AgentSubagent.Status (`STATUS_`) | STARTING, RUNNING, COMPLETED, FAILED, STOPPED, TIMED_OUT, MERGED, DISCARDED | agenttasks | AgentSubagent.status |
| AgentBackgroundCommand.Status (`STATUS_`) | RUNNING, EXITED, STOPPED, LOST | agenttasks | AgentBackgroundCommand.status |
| AgentBackgroundCommand.EndReason (`END_REASON_`) | EXIT, LIFETIME_LIMIT, OUTPUT_LIMIT, STOPPED, LOST; UNSPECIFIED while it runs | agenttasks | AgentBackgroundCommand.end_reason |
| AgentToolExecutionMetrics.Outcome (`OUTCOME_`) | SUCCEEDED, KNOWN_FAILURE, UNKNOWN | conversation | AgentToolExecutionMetrics.outcome |
| AgentDescription.Status (`STATUS_`) | INITIALIZING, WAITING_FOR_MESSAGE, COMPACTING_CONTEXT, CALLING_MODEL, ROUTING_TOOL, WAITING_FOR_TOOL_APPROVAL, EXECUTING_TOOL, WAITING_FOR_TIMER | agentloop | AgentDescription.status |
| AgentPendingUserInput.Kind (`KIND_`) | QUESTION, REQUIREMENTS | agentloop; an empty domain value (a request_user_input batch) is QUESTION | AgentPendingUserInput.kind |
| AgentRequirementConnection.Status (`STATUS_`) | EXISTING, NEW | agentloop | AgentRequirementConnection.status |
| AgentTodoItem.Status (`STATUS_`) | PENDING, IN_PROGRESS, COMPLETED | conversation | AgentTodoItem.status |
| AgentEvent.Kind (`KIND_`) | INPUT_CONSUMED, USER_INPUT_ANSWERED, USER_INPUT_CANCELLED, SNAPSHOT_REQUIRED, STEERING_APPLIED, COMPACTION_FAILED, COMPACTED, MODEL_STARTED, MODEL_FAILED, MODEL_COMPLETED, MODEL_TOOL_CALL, USER_INPUT_REQUESTED, TOOL_APPROVAL_REQUESTED, TOOL_APPROVAL_RESOLVED, TIMER_STARTED, TIMER_RESOLVED, TOOL_PROGRESS, TOOL_FAILED, TOOL_COMPLETED, MODEL_FALLBACK, AGENT_INTERRUPTED | conversation | AgentEvent.kind |

- A value name may repeat its prefix, as `SETUP_STATUS_SETUP_FAILED` does, because the
  domain value is persisted.
- There is no AccountStatus, ConfigurationValidationStatus, ReleaseSourceStatus or
  BuildRevisionStatus enum: an Account is always ACTIVE to the API, a validation receipt
  only exists for a success, and the Release statuses form one set.
- Go maps in one place, `internal/dexai/api/boundary`. `boundary.Enum[E](domain)` looks up
  `<UPPER_SNAKE(enum name)>_<upper(domain)>` through the enum descriptor; `""` maps to the
  zero value, and any other unknown value is a programming error that panics and returns
  Internal for that RPC. `boundary.domainValue` turns a request enum back into its domain
  value; a zero or undeclared number is InvalidArgument. List handlers pass the domain
  values to the Dex search, and the Agent Resolution and Action are lower-cased.
  `internal/dexai/projects/config` and `internal/dexai/studio/runs` cannot import `internal/dexai/api`, so
  they use the generated constants directly and map scope through
  `infra.ScopeOf(pb.ProjectScope)`; anything other than PREVIEW or LIVE yields an invalid
  Scope that `Validate` rejects.
- Studio pages use the generated full names, for example `PROJECT_STATUS_READY`, typed
  against the generated enum, and render text through `enumText(value, prefix)`, so the
  visible text does not change. The transport-free Agent UI keeps its lowercase literals:
  `lib/session/agent-view.ts` maps the prefixed Agent enums to them and maps Resolution and
  Action back before a command is sent.
- An unknown persisted value fails every RPC that reads it, including a whole ListProjects
  page. A rollout therefore first takes a bounded read-only inventory of the distinct
  `keyword3` values of every Flow type; a value outside this table blocks it.

## Transports

The browser never calls gRPC. It reaches AccountService only through `/api/auth`, the four
Flow API services only through `/dex/...` and the other six services only through
`/api/rpc/<Method>`.

The business server logs each failed unary or streaming RPC with its method, gRPC code,
bounded status message, and the request's caller account and Project ID when present.
Client cancellation is omitted. Configuration host responses with HTTP 4xx or 5xx inside
successful gRPC documents also log their HTTP status, coded error and bounded message.
Studio logs failed upstream gRPC calls with method and code. These logs never include
request or response bodies, caller tokens, or gRPC metadata.

Responses carry the generated `<Rpc>Response__Output` JSON of `@grpc/proto-loader` with
`keepCase: false, longs: String, enums: String, defaults: true, oneofs: true`: lowerCamel
field names, 64-bit integers as decimal strings, enum value names, scalar defaults and empty
lists filled in, an unset message field as `null`, a Timestamp as `{seconds, nanos}`, and a
oneof named by its own key (`event: "snapshot"`,
`content: "assistantText"`) with only the set member present. A proto3 `optional` field is
absent when unset, and when set its synthetic `_<field>` key names it. The BFF does not translate statuses, invent IDs, profiles or
installation IDs, embed one resource in another or join results into a second protocol.

### `/api/rpc/<Method>`

`app/api/rpc/[method]/route.ts` handles GET and POST, chooses the backend and calls
`handleRpcRequest(request, method, backend)` in `lib/server/rpc/rpc-handler.ts`.

1. **Exact routing.** The method segment must match `^[A-Z][A-Za-z]{2,63}$` and be a key of
   a `Map` built at load from the loaded package definition; anything else is 404. Dispatch
   uses that entry's path and codecs and never looks a URL value up as a property of a
   client, so lowerCamel aliases and client methods are unreachable.
2. **Allowlist.** Only `BROWSER_SERVICES` (ProjectService, SessionService, AgentService,
   PreviewService, ReleaseService, DeploymentService, TeamSpaceService,
   TeamSpaceMemberService, ProjectMemberService and TeamSpaceConnectionService) are
   routable. A new service is unreachable until it joins the list.
   StartTeamSpaceConnectionOAuth and CompleteTeamSpaceConnectionOAuth are reachable only
   through `/api/connector-oauth/start` and its callback (see [Flow API](#flow-api)).
3. **Load-time assertions.** If one fails, the BFF refuses to serve and returns 503: method
   names are unique across all 15 services; every browser request has `CallerContext caller
   = 100`; every field of every browser request, recursively, is a string, bool, 32- or
   64-bit integer, enum, message, repeated field or oneof member, with no bytes, map,
   Struct or Timestamp. RegisterAccount and Login are an explicit constant that only
   `/api/auth` calls; any other method called without a caller returns 401.
4. **Order of checks.** The verb comes first: a unary RPC accepts only POST and a Watch only
   GET, and anything else returns 405 with an `allow` header. A POST then passes
   `assertSameOrigin` (403). The caller cookie is checked (401) before the body is read.
   Decoding follows (400), then dispatch.
5. **Decoding** (`lib/server/rpc/request-decoder.ts`, walking the request descriptor at any
   depth). The body is a JSON object of at most 1 MiB keyed by the lowerCamel JSON names.
   The decoder rejects unknown keys, a body `caller`, `null`, and more than one member of a
   oneof (the oneof's own key is accepted only when it names the member present); a string
   that is not a JSON string and a bool that is not a JSON boolean; an int32 or uint32 that
   is not an in-range integer; a uint64 that is neither `/^(0|[1-9]\d*)$/` up to 2^64-1 nor
   a safe-integer number; an int64 that is neither the same digits with an optional `-`
   within range nor a safe integer; an enum that is neither a declared value name nor a
   declared number, or that is explicitly its zero value; and a repeated field that is not
   an array. Watch GET query parameters map to the request's top-level string and 64-bit
   fields only; unknown or repeated parameters and `caller` are rejected, and
   `Last-Event-ID` overrides `resumeCursor`.
6. **Injection.** The BFF sets only the top-level `caller` from the signed cookie. It adds
   no request ID, message ID, revision, fence or page-size default; Go applies defaults and
   owns business validation, including incomplete Preview observations, zero or undeclared
   status filters and a missing CreateProject source.
7. **SSE relay** (`lib/server/event-stream.ts`, the single owner of the SSE stream). The
   first frame must be a snapshot. Each frame is written as `id: <cursor>` (without CR or
   LF), `event: snapshot` or `event: progress`, then `data:` with the
   `Watch<R>Response` JSON verbatim. The relay counts bytes against a 256 KiB high-water
   mark per HTTP connection: it pauses the gRPC readable when that queue fills and resumes
   when the HTTP consumer pulls; the queue can exceed the mark by one encoded frame, and
   each gRPC message keeps the 8 MiB receive limit. No frame is discarded to signal
   success. Successful upstream completion closes and drains the queue; an upstream error,
   a request abort, an invalid first frame or an encoding failure errors the queue at once
   and discards unread frames, and cancellation reaches the upstream at most once. A failed
   gRPC status reaches the relay through the stream's error event, so it is not delayed
   behind frames that were received but not yet read. While the
   stream is open the relay writes the comment `: keepalive` every 20 seconds, so an idle
   Watch outlives the ALB's 60-second idle timeout, and every exit path stops that timer.
   The relay sends `cache-control: no-store, no-transform`.
8. **Errors.** `errorResponse` maps gRPC codes to HTTP statuses, with UNIMPLEMENTED as 503;
   401 and 5xx messages stay masked and no provider error, arbitrary detail or raw cause is
   forwarded. A `google.rpc.ErrorInfo` detail crosses to the browser, as HTTP `errorInfo`
   beside the sanitized `error` string, only in these cases:

   | Method | ErrorInfo that crosses |
   | --- | --- |
   | Every AgentService method | `AGENT_COMMAND_NOT_DISPATCHED` when the BFF never dispatched the call; otherwise `AGENT_COMMAND_NOT_ADMITTED` or a stopped-execution reason |
   | Every other dispatched method | `REVISION_CHANGED`, or `SESSION_EXECUTION_FAILED`, `SESSION_EXECUTION_CANCELED` or `SESSION_EXECUTION_TERMINATED` |
   | TeamSpaceConnectionService | a connection refusal (`connections.RefusalReason`) or `DEPLOYMENT_IN_PROGRESS` |
   | ReleaseService | `PREVIEW_VALIDATION_REQUIRED`, `RELEASE_CONFIGURATION_STALE`, `RELEASE_BUILD_NOT_CANCELLABLE` or `RELEASE_BUILD_DEPLOYING` |
   | DeploymentService | `DEPLOYMENT_NOT_CANCELLABLE` |

   The metadata that crosses is `remedy`: the `FailureRemedy` value name the server
   declared with the reason (`internal/dexai/api/boundary/error_reasons.go`, the connection
   refusals and the stopped Session, which declare it with their own sets), and, for
   `RELEASE_BUILD_DEPLOYING` only, `deploymentId`, the deployment its remedy
   `CANCEL_DEPLOYMENT` cancels, when it is a deployment ID. Studio reads the fix from them and
   never chooses a remedy from a reason.

The backend is `RpcBackend {unary(method, input, caller, signal): Promise<object>;
watch(method, input, caller, signal, upstreamFailed): AsyncIterable<object>}`, where
`upstreamFailed` reports an upstream failure while frames still wait. `grpcBackend` lives in
`lib/server/grpc-client.ts`, which loads `api/superverse/v2/superverse.proto` (env
`SUPERVERSE_PROTO_PATH` in the image) with the `google.rpc` protos once, builds the method
map over all 15 services and opens one grpc-js client; `callRpc` uses
`makeUnaryRequest` and `watchRpc` `makeServerStreamRequest`, and the request payload always
sets the caller at the top level. The Studio mock implements the same seam in `mock/`, and
only the route file chooses between them, so the allowlist, decoder, caller check,
ErrorInfo table and SSE relay run in both modes.

The browser client mirrors the seam. `lib/api/transport/unary.ts` posts
`unary(method, input, {signal})` to `/api/rpc/${method}` and throws `ApiError {status,
message, errorInfo}` on a non-2xx response; `lib/api/transport/watch.ts` opens an
EventSource and requires a snapshot first, and reconnecting with the last cursor stays with
its caller. `lib/shared/rpc-methods.ts` types both from the generated definitions:
`RpcInput<M>` is the generated request without `caller`, the synthetic `_field` keys, null
and Long, exactly what the decoder accepts, and `RpcOutput<M>` is the generated
`<Rpc>Response__Output`. One module per browser service names its functions after the RPCs
and keeps that service's rules, and the page sends the values the BFF used to default: a
new `crypto.randomUUID()` SendMessage `message_id` for every send (the creation intent sends
its stored key), RemoveLive's `live.deploymentId` and `live.revision`, and the CreateProject
source.

### `/api/auth`

`/api/auth` is the only caller of AccountService and the only writer of the HttpOnly
`sv2_session` cookie. Registration maps its Idempotency-Key to `request_id` and, like
login, turns the returned `AccountLogin` into the cookie envelope. Logout and
GetCurrentAccount carry the caller from that cookie. The Workspace server layout reads
GetCurrentAccount once per document (see [Workspace account display](#workspace-account-display)).

### `/dex/...`

The four Flow API services keep their fixed `/dex/...` routes with CSRF and exact target
binding; see [Flow API](#flow-api). The `/dex` session-source route also calls
GetPreviewDefinitions and reads `flowDefinitions`.

## Callers and authentication

Every authenticated request carries an explicit typed `CallerContext` (AccountID,
canonical email and signed session token). The BFF takes these fields only from the
HttpOnly cookie envelope issued from the Go login response; the decoder rejects a
browser-supplied `caller`. Go verifies the token's HMAC and expiry and that it names
`account-<canonicalEmail>` for this email, then reads the exact login row joined with its
account: unexpired, the same token hash and authentication epoch, an ACTIVE account with
the caller's opaque AccountID. Session expiry stays fixed at seven days, and Logout
deletes only the exact login. Accounts and logins are rows of the PostgreSQL `accounts`
and `login_sessions` tables ([accounts](../architecture/teamspace/accounts.md)); the
Argon2id algorithm, version, parameters, salt and hash are the account's
`password_record`, and plaintext and hash are never in a public DTO.

The configured ACTIVE, authenticated platform administrator account (`accounts.platformAdminEmail`) may view and manage every Project
through these same resource APIs. ListProjects lists any TeamSpace; all child resources
and mutations still validate the owning Project and scope. The server checks the
administrator's opaque AccountID against its account row. Browser
`isAdmin` values do not grant access, and admin actions do not change Project ownership.
Project Watch shares this access check, including its final DELETED snapshot; ordinary
reads and child operations reject DELETED. An unavailable authority read stays an error
instead of being disguised as an ordinary permission rejection.

Business gRPC has no BFF service token or service-authentication interceptor.
Authenticated calls carry only the typed caller; user identity is never inferred from
metadata. Registration and login are the public account entry points on the private
business listener. Registration is idempotent in the database: replaying the same request
with the same password logs in, and any other registration of the email, or a replay with
another password, is `ALREADY_EXISTS`. Concurrent registrations of one email meet a unique
index and one wins. Accounts of the earlier AccountFlow release are not migrated; users
register again (runbook).

The request path is browser `sv2_session` cookie, then the Studio BFF, then business gRPC.
The Go server independently validates the signed session HMAC, fixed expiry, canonical
email, opaque AccountID and the non-revoked login row with its token hash, then authorizes
the requested resource. Platform network policy permits business gRPC port 8080 only from
Studio BFF pods. The cookie is HttpOnly, SameSite=Lax and Secure when hosted.

Authentication has no cache: every request makes the one query above, so a Logout or an
epoch change takes effect on the next request of every instance, and the process-local
caller cache and its `SUPERVERSE_AUTH_CACHE_TTL_SECONDS` setting no longer exist. There is
no BFF scope cache either. Every Flow API operation authenticates the same way before
reading its resource. See the [Account service](../../internal/dexai/tenancy/accounts/README.md).

Registration and login return the account summary plus the new login, never the password
record. `AccountLogin {account, session_token, expires_at}` answers both RegisterAccount
and Login, and `AccountSnapshot` has no status because the API only serves ACTIVE
accounts. There is no password length policy, including empty passwords; Argon2id
record validation and the total HTTP and gRPC message limits stay enforced.

Studio account authentication is separate from any future application end-user
authentication. Preview and Live application workloads never receive or reuse Studio
sessions or browser signing keys; connector access uses their own scope-, generation- and
configuration-bound workload grants.

### Workspace account display

The Workspace server layout calls GetCurrentAccount once when entering a new Workspace
document and passes the generated safe `account` through one client context. AppShell,
nested ProjectShell, AppHeader and AccountMenu share it. AccountMenu mount and toggle and
ordinary Link navigation issue no account read or `/api/auth/session` fetch; a reload or an
explicit `router.refresh` may perform a new layout read. The context holds no bearer or
password and never authorizes business requests; later APIs and Watches keep Go
authentication. `/api/auth/session` remains for explicit external or login recovery, and
sensitive OAuth preflights stay separate from ordinary Workspace display. The real
invocation-count scenario counts `/superverse.v2.AccountService/GetCurrentAccount` in a
complete private grpc-js channel trace and needs an existing READY Project and Session; its
static receipt
does not claim the unexecuted browser test passed.

## Credential read endpoint

`POST /internal/connection-credentials/read` serves applications, not Studio, and is outside
the business protocol: it is not in `api/superverse/v2/superverse.proto`, it bypasses the
Studio BFF, it carries no `CallerContext` and it authenticates a per-scope token whose hash
the platform stores. [The credential read endpoint](credential-read.md) owns its request,
response, status codes, limits and transport.

## Projects

Project and Setup IDs and the template revision are server-owned. CreateProject
requires the request ID, name and `ManagedRepositorySource managed_repository = 6`.
Setup creates a new private managed repository from the embedded template. A missing
managed_repository is InvalidArgument before any Flow start or provider effect. There
is no repository source choice or repository import API. The CREATE request fingerprint
keeps its canonical encoding independently of the durable binding shape; it excludes
provider results, credentials and the server-selected template revision.
CreateProject returns only project_id. GetProject and WatchProject return the current
state, repository and accepted template revision. There is no GetApplicationInfo or
browser configuration-discovery round trip.

Project names are immutable. RenameProject, RetryProjectSetup and CancelProjectSetup are
absent. Setup uses bounded automatic Step retries; failure is SETUP_FAILED, with cleanup
references retained until DeleteProject. There is no manual setup recovery Channel. Only
the owning deleting Project may invoke the internal RequestProjectSetupCleanup RPC. The
AppManifest is returned inside ProjectSnapshot or GetProjectConfiguration; there is no
GetAppManifest RPC or app-manifest route.

### Durable creation

CreateProject starts only ProjectFlow. Its durable StartProjectSetup Step independently
starts the fixed accepted Setup, so closing the initial API request does not drop that
responsibility. The response does not read the Project or Setup after a successful start.
GetProject and WatchProject later read the canonical CREATING or READY state and Setup
view. In that read path, the pinned SDK maps an absent read RPC to FlowNotActive; only that
typed error triggers a bounded two-second absence probe of the existing target, and only
typed FlowNotFound proves PENDING. Other failures stay errors. PENDING contributes no
nonexistent Setup Stream source. Each fresh snapshot rechecks Setup, adding its source and
realigning readers once it exists. No initial CREATING state is returned as a fabricated
current snapshot.

### Initial template Design source

`SetupSnapshot.source_commit` returns the immutable SourceCommit already owned by
ProjectSetupFlow, including retained completed Setup reads. Project reads and Watches stay
authorized by the owning resource service. A READY Project whose old Setup has expired
omits this optional view; it does not become PENDING. Studio may use the pinned template
FDG for a read-only initial Design only for a Project on that exact template
revision whose Session HEAD matches this commit. Prepared source, Preview and Runs keep
their authorization, instance and definition checks.

### Live pointer and RemoveLive

`ProjectSnapshot.live` is the Project's committed serving pointer, `ProjectLive
{deployment_id, release_id, build_revision_id, revision, url, is_removing}`, absent without
Live. It names the serving deployment, not a copy of its state; callers that need the full
operation read GetDeployment. `live.revision` is the fence that StartDeployment,
StartReleaseBuild with `auto_deploy` and RemoveLive send as `expected_live_revision`.
ProjectSnapshot carries no configuration state; that is read per prepared Release commit
through GetProjectConfiguration.

`ProjectSnapshot.failure` is the failure of the Project's last command (`LastFailure`), and
`session_start_failure` is `SESSION_START_FAILED` while the READY Project has no Session
(`SessionStartFailure`). They are separate so that neither hides the other: a command's
failure, retry or success never clears the start failure, which only the Session's
admission clears, and an operator restarts the Session as the
rollout reconciliation
runbook describes. Studio shows it on the Project page and in Build.

The optional `active_deployment` is a safe reference containing the deployment ID and lease
generation from Project's ActiveDeployment Attribute. It is not a Deployment snapshot or
status mirror. GetProject and every Project Watch snapshot expose the same reference so
returning pages can attach GetDeployment and WatchDeployment to the exact active resource.
`live` stays the last committed serving pointer and may name another deployment while its
replacement is in progress. An absent active lease does not prove an operation succeeded;
the Deployment snapshot owns its result.

RemoveLive is a ProjectFlow command, so `ProjectFlowClient` serves it. It takes the request
ID with the `deployment_id` and `expected_live_revision` the page observed. After
authorization, a Project without Live is returned unchanged; a deployment ID or Live
revision that differs from the current pointer returns ABORTED ("Live deployment changed");
otherwise the client calls `ProjectFlow.RequestLiveRemoval` at the Project revision it read.
This prevents a stale page from removing a replacement. RemoveLive joins or resumes its
existing REMOVE_LIVE command. While that command runs or after it failed, `live.is_removing`
stays true and Live never returns to a plain serving state to enable a retry; Studio shows
`project.failure` and offers Retry Live cleanup for a retained failure, with the
prerequisite confirmation when `is_recoverable` is false. Removal retains native Project
configuration access, configuration, credentials, Releases, history and data.

### Deletion and cleanup

DeleteProject requires the current Project revision. Project deletion freezes child
admission, removes serving, revokes its grant, clears Live, then cleans children and
finally durable infrastructure and data. Cleanup polling stays in bounded durable Project
Steps; the injected Go effects run no background orchestration loops.

DeleteProject also recovers failed cleanup after navigation or reload. While ProjectStatus
is DELETING, the server resolves a valid new request ID to the already admitted deletion
command. Running cleanup joins without another queue publication; failed cleanup requires
the current Project revision and keeps the original command identity for external-effect
reconciliation. Studio shows Retry cleanup for a retained failure, sends DeleteProject
again and keeps other mutations disabled. `Failure.phase` and `.message` describe the safe
reason. When `is_recoverable` is false, the confirmation explicitly requires addressing the
failure before retrying; it does not imply automatic recovery. No extra status or retry API
exists.

RemoveLiveServing and RevokeDeploymentGrant treat the typed ErrCleanupPending provider
result as an unfinished observation. Each phase is one long-running Execute that polls its
effect every two seconds, without Project locks, until it converges or 30 minutes after the
phase's first attempt. Ordinary pending observations consume no provider-error attempts.
A call that goes unanswered while its round goes on, cut off by its own bounded wait,
canceled or answered `Unavailable` by the Dex Server, is pending too, because every cleanup
effect queries its exact identities before it mutates; it is never OUTCOME_UNKNOWN.
Deadline exhaustion records CLEANUP_TIMEOUT with the last pending reason, without marking it
automatically recoverable. Typed PERMISSION_DENIED, OWNERSHIP_CONFLICT,
CLEANUP_OWNER_MISSING, REPOSITORY_HOST_MISMATCH (the repository belongs to another
repository host than the server's, which the message names) and OUTCOME_UNKNOWN failures
keep the original operation;
OUTCOME_UNKNOWN is only for an outcome that cannot be confirmed. Known
terminal causes route directly to the failure Step; unclassified errors keep the bounded
Execute retry policy and the PROVIDER_FAILURE fallback. Every `Failure` carries the
`remedy` its code declares (`dexflow.FailureRemedy`, persisted with the record; the Preview, the
Release and the Deployment declare theirs in their code tables) and the `owner` who must act on
it (`conversation.DeliveryOwner`, the one owner set, recorded beside the failure by the Step that
decides it; UNSPECIFIED where no owner is recorded). `boundary.OwnedFailureSnapshot` maps both with
`boundary.Enum`. A public Failure carries an authored
message followed by the error text, truncated to 2 KiB, which provider clients build
without credentials; it never carries an engine RunID. Current Live is cleared only after
serving removal and grant revocation complete.

Child executions register their exact cleanup membership from the first durable Step,
before external effects. The Session, `session-<projectID>`, is the Project's only
Session-scoped child: its workspace Flow, its provisionings, its Preview and its subagents
are not Project children and hold no `ChildFlows` membership. The Session's
`SessionWorkspaceFlow`, `session-workspace-<projectID>`, owns its instances
([the current contract](../architecture/agent-harness-design/sandbox.md)).
Cleanup never treats a registered but missing execution as proof that no resources
exist. Deleting-Project cleanup completes the Session once (`CompleteProjectSession`),
ends its workspace Flow (`CompleteSessionWorkspace`), waits for a provisioning that may
still run, stops the subagents that have not ended, ends
the Project's Preview and releases every resource of the Session, the Preview's route
objects included, by label and owner, and only then acknowledges the Session membership.
Project cleanup rotates bounded eight-child rounds within its polling Step, so pending
cleanup does not starve independent Session shutdown. Release cleanup waits for Deployment
cleanup. Within child reconciliation, a typed SDK `RPCLockConflictError` is pending cleanup,
not a completed child or a provider failure, and so is a child or membership call that,
while the round goes on, its own bounded wait cut off, that was canceled or that found the
Dex Server unavailable: every such call is idempotent and confirmed by the child's cleanup
receipt or the membership, so it is never OUTCOME_UNKNOWN. Project keeps the membership and
polls again within its 30-minute cleanup budget. Other RPC, authorization, business-revision
and provider errors stay errors; generic gRPC `ABORTED` and error text do not select this
behavior. After resource absence, Failed (including a server-side timeout), Canceled and
Terminated Session engines may discharge exact membership only under current Project
DELETING admission; running archived Sessions still require graceful completion, and
Completed Sessions require their matching receipt. Missing executions and wrong receipts
stay errors. OAuth objects keep their SDK storage protocol; the owning Project's scope
cleanup removes them without a separate platform OAuth Flow.

Project RegisterChildFlow and AcknowledgeChildCleanup return ChildAdmissionResult, not a
full Project snapshot. Exact map instance load and lock and the shared admission
coordination locks are unchanged; acknowledgement never enumerates children.
`SessionAgentFlow` is the only Session-scoped child type that RegisterChildFlow admits;
neither a provisioning nor a Preview is registered. GetChildFlow returns an empty reference, not an error, for
a child that is not registered.

## Sessions

A Project has exactly one Session, `session-<projectID>`, which ProjectFlow starts when the
Project becomes READY ([the current contract](../architecture/agent-harness-design/session.md));
SessionService, AgentService and PreviewService address it by `project_id`. No RPC starts,
lists or stops a Session: GetSession of a Project that is not READY yet, or whose Session is
still starting, answers NOT_FOUND, and Studio repeats that read for at most 30 seconds once
the Project is READY. The Session starts with the default profile on the Project's AppManifest
commit and without a message. The person chooses the model and the first message on the
creation page, which preselects Sonnet, or the mock model for the platform administrator
where `ListMockScenarios` offers it; once the Session starts, Studio applies the chosen model with
UpdateSessionModel (at the snapshot's `model_revision`, free before the first message) when
it differs from the Session's, then sends the stored creation intent as the first
SendMessage, with the intent's key as the message ID. A new SessionSnapshot has no
`workspace`. The Session requests its workspace on first demand: its first model turn
(repository context for the prompt), a tool dispatch that needs the workspace, an accepted
StartPreview or PreparePreviewSource. A Preview configuration read reads stored state alone
and never requests one. Only the Project's deletion closes the Session.

RenewWorkspace is Studio's "Keep running for 7 days": it moves the Sandbox deadline of the
Session's READY instance to at least seven days from now and answers `renewed`, false while
the Session is not ACTIVE or its workspace not READY, which leaves the deadline unchanged.
It authorizes the Session like an Agent command, so a stopped engine execution answers
FAILED_PRECONDITION with its `SESSION_EXECUTION_*` reason.

### Stable Session reads

Agent source identity is FlowType, FlowID and Stream name; signed cursors contain no RunID.
Approval and recovery identity, message sequence and archive anchors are stable for the
Session's lifetime. GetAgentSnapshot, WaitForWaitingInputRound, ListRecentEvents and the
internal GetCurrentRun are removed; no replacement engine-summary polling is added.
GetSession reads lifecycle plus chat in one internal GetSnapshot invocation. Workspace and
Preview keep their independent Sandbox authority and revisions.

ListArchivedMessages pages the archived application history back from `before_sequence`,
which must identify a ten-message archive boundary, with `page_size` from 10 to 200 in
steps of 10 and 10 by default. It returns `AgentHistoryPage history` with the messages and
the optional `next_before_sequence` of the next older page.

`SessionSnapshot.workspace` is the Session's own view of its current Sandbox instance,
read from the Session's `Workspace` Attribute. It is absent until the Session first
requested an instance. `SessionWorkspaceSnapshot` has exactly these fields:

| Field | Meaning |
| --- | --- |
| `status = 1` | WorkspaceStatus `PROVISIONING`, `READY`, `CLOSING` (being reclaimed), `CLOSED` (reclaimed: the Pod is gone, the volume kept) or `FAILED`. |
| `generation = 2` | Constant `1`; nothing increments it. |
| `revision = 3` | The Session's revision. |
| `workspace_state = 4` | Separate safety classification, WorkspaceState `ABSENT`, `CLEAN`, `DIRTY` or `UNKNOWN`, as the checkout or the last reclamation found the volume. |
| `updated_at = 5` | When the instance state last changed. |
| `failure = 8` | The failure of the current request or of the last reclamation; absent otherwise. |
| `expires_at = 9` | The Session's Sandbox deadline of its READY request: when the idle instance is reclaimed. It only moves later; absent unless the request is READY. |
| `workspace_request_id = 10` | The instance identity. |
| `phase_timings = 11` | The `PhaseTiming`s of the provisioning that ended with this instance, in order: `provision`, then `checkout` (at most two). Empty while the instance provisions; the SessionWorkspaceFlow copies them from the provisioning's outcome when it records it, and a new instance replaces them. |

The Session has its SessionWorkspaceFlow reclaim an instance at its Sandbox deadline, one hour after the main agent
or a Preview command last used it by default (`sandbox.hold`), or seven days after a
Preview became `READY` on it (`sandbox.previewHold`); a running tool or background command
postpones it. The reclamation stops a Preview that serves on the
instance first, stops the Preview engine, records the volume state and deletes the Pod,
keeping the workspace volume (with the Preview engine's state), the namespace and the
Project's Preview configuration; the Preview's route goes with the stop. A reclamation
that cannot finish
records `FAILED` and keeps every resource for the next request or the Project's deletion. The
Session stays `ACTIVE`; its next workspace demand provisions a new instance on the kept
volume. There is no dormant state and no Pod or volume idle deadline.

The Session does not close when its instance is reclaimed. Studio presents an absent
workspace as not requested, `PROVISIONING` as preparing and a `CLOSING` or `CLOSED`
instance as reclaimed; with a usable lease, tools run immediately, and the next
workspace demand after reclamation provisions a new instance. A READY instance shows its
deadline with "Keep running for 7 days" (RenewWorkspace); the volume stays until the
Project is deleted. The Session's accepted lease, internal endpoints and credentials never
reach the browser.

### Stopped Session execution presentation

GetSession and WatchSession keep typed application history after an engine stop. While the
durable SessionStatus is ACTIVE, the Flow client verifies current engine
liveness by stable FlowID. A terminal engine is returned through the existing
SessionSnapshot fields as status `SESSION_STATUS_FAILED` and a fixed
`SESSION_EXECUTION_CANCELED`, `_TERMINATED`, `_FAILED` or `_CLOSED` failure code. This is
an interaction observation, not an ARCHIVED lifecycle transition or cleanup receipt.
Revision and workspace observations keep their meanings; RunID and raw engine errors are
not returned. Every other operation that needs the Session running (Agent commands, the
archive read, RenewWorkspace and Preview operations) fails with FAILED_PRECONDITION and the
ErrorInfo reason `SESSION_EXECUTION_FAILED`, `_CANCELED` or `_TERMINATED` with empty
metadata, also when it raced the end of the execution; Studio then presents the Session as
FAILED with interaction disabled. The Preview configuration read is no such operation: it
reads stored state and answers from it. A Session has no Flow timeout and never expires; it
closes only through its Project's deletion. A Sandbox instance whose engine execution stopped reads as
`WORKSPACE_STATUS_FAILED` with the failure code `WORKSPACE_EXECUTION_STOPPED`.

## Agent commands

AgentService holds the seven commands to the Session Agent, `ListMockScenarios` and
`GetFlowDesign`. `GetFlowDesign` reads the body of the Flow design of the waiting card (slot
`PENDING`) or of the confirmed requirements (slot `CONFIRMED`) at the digest of the reference
that `AgentRequirements.design` carries; a slot without a design is NOT_FOUND and another digest
is ABORTED with `REVISION_CHANGED` ([flow design](../architecture/design-first/flow-design.md#api)).
Every command request starts with `project_id`, and every command response is empty:
success confirms actual command admission, with no post-admission GetSession round trip, and the Session Watch owns the
state that follows. Studio keeps its Watch during a command and corrects accepted work
through canonical snapshots. An uncertain transport response stays an explicit command
warning across reads, with no automatic retry of ordinary user messages. A deliberate retry
is a new send with a new `message_id`: the Agent does not deduplicate SendMessage by
`message_id`, so retrying a send that was in fact admitted queues a second message.

Command rejection has two allowlisted ErrorInfo reasons in the `superverse.v2` domain, whose
only metadata is the declared `remedy`. `AGENT_COMMAND_NOT_DISPATCHED` is emitted by the BFF only before a matched
command has been dispatched, for example when decoding rejects it.
`AGENT_COMMAND_NOT_ADMITTED` comes from Go's pre-command authorization and input checks or
an authoritative typed false command receipt, which every command to an ARCHIVED Session,
one whose Project is being deleted, receives (FAILED_PRECONDITION). A command to a Session whose engine execution
stopped carries `SESSION_EXECUTION_FAILED`, `_CANCELED` or `_TERMINATED` instead (see
[stopped Session execution presentation](#stopped-session-execution-presentation)). Studio
displays all of them as rejection, not uncertain execution. Other errors stay uncertain; HTTP status classes, exception text and later
snapshots do not prove command admission.

AnswerQuestions answers a question batch with one answer per question, or a requirements
card with an `AgentRequirementsDecision` and no answers. ResolveToolApproval approves or denies the pending call with an explicit
`is_approved`. A failed tool call never waits for a user decision: its result returns to
the model.

InterruptAgent is the Stop button. Its `turn` is `AgentDescription.turn` of the Snapshot the
user saw: it stops that running turn, every running background command and every subagent
that may still run, and a Stop of another turn, or while neither a turn nor a task runs, is
not admitted (`AGENT_COMMAND_NOT_ADMITTED`). The turn ends at once when its work in flight can be
cancelled (a model call, a tool other than `commit_and_push`, a wave, a timer or an approval),
otherwise at its next boundary. A Stop repeated while the first ends the turn is admitted and
joins it. The history then shows one `Stopped by user` marker and the
`agent_interrupted` activity tells Studio to drop text the cancelled model call still streams.

### Notifications and background commands

A Session notification is an automated report of something that happened outside the
Agent's turn, such as a background command that ended or a failed Preview or Live
deployment. The history projects it as `ROLE_NOTIFICATION` whose `content` is its summary
and whose `notification` carries its ID, kind, source, time and, for a task, the task ID and
status; the report the model reads is not projected. It is never a user message. A
`delivery` notification also carries its `outcome` (`failure` when the application owns
the failure, otherwise `notice`), the `owner` who must act (application, configuration or
platform), the failure's `code` and `phase`, the `commit` it concerns, the `resource_id`
(the Preview's Sandbox instance, the Release build revision, empty for the Release source,
or the Deployment), at most the first eight configuration `gaps` (names only) of
`gap_count`, and the bounded, redacted `message`. The Preview reports its commands and
automatic deployments; the Live release reports a failed source, build or automatic
deployment and the Live deployment its failure, except while the Project is being deleted.
Only an application-owned failure of the Session's HEAD opens an automatic turn, within
`agent.platformTurns`; a configuration or platform notice only notifies. `AgentDescription.background_commands` lists the
Session's background commands in start order (the running ones and the latest ended ones)
with their description, status, end reason, exit code, output size and times, never their
command text or output. `AgentDescription.subagents` lists the Session's subagents in start
order (the ones that may still run and the latest 50 ended) as `AgentSubagent`: the task ID
(`agent-<agent ID>`), kind, description, status, start and end times, the summary of its end
and, once it ended with saved changes, the files they touch with their added and deleted
lines; never its prompt or report. `lost` marks a subagent that has not reported within an
hour after its kind's time limit: the Session no longer counts it as running, so it holds no
running place and no Stop applies to it, Studio shows it among the ended ones, and a late
report still ends it. A subagent's end arrives as a `task` notification from
`SOURCE_SUBAGENT` with its status. While a subagent runs, the Session Watch relays its
best-effort activity as `SessionProgress.activity` with its `task_id`, the subagent's live
status line; Studio shows only its newest line, and the snapshot's status stays
authoritative. When automatic continuation pauses, after a Stop or at its bound,
the history shows the marker `Automatic continuation paused; reply to continue`.

`SessionSnapshot.connector_contributions` lists the connectors the Session's `connector_develop`
subagents contributed to the public connectors library, in the order the Session named them, each
read from its own `ConnectorContributionFlow` (`connector-contribution-<projectID>-<slug>`), never
from a copy in the Session: the slug, module, kind, status, branch and head commit, where the
branch was pushed, the draft pull request's number and link once it opened, why it could not be
opened, and the release candidate of the latest head: its version, such as `v0.1.0-rc.2`, whether
it was published, and why it failed. All of it is public. A server without an upstream library
(`github.connectors`) records `STATUS_UNPUBLISHED`. A completed develop subagent's `task`
notification carries its contribution for the model, with the candidate its finish published on the
local Git host. A `connector` notification, which opens a bounded automatic turn like a task
notification, reports a pull request that could not be opened (`CONNECTOR_EVENT_PULL_REQUEST_FAILED`,
ID `connector/<slug>/pull_request_failed/<12-hex head>`) and a candidate the upstream library
published (`CONNECTOR_EVENT_RELEASE_CANDIDATE_PUBLISHED`,
`connector/<slug>/release_candidate_published/<version>`) or did not
(`CONNECTOR_EVENT_RELEASE_CANDIDATE_FAILED`, `connector/<slug>/release_candidate_failed/<12-hex head>`)
([the current contract](../architecture/agent-harness-design/connectors.md)).

### Session model update

UpdateSessionModel selects the model the Session's next turn starts on, at any time: an
`AgentModel` and the observed `AgentDescription.model_revision` as `expected_revision`, which
is optional on the wire only to detect an omitted value, and omission is rejected. The
snapshot is the source of the committed model and its new revision; a stale revision is a
typed command rejection, and retrying the same accepted model at the exact next revision
succeeds without another increment. The model change rolls the context over at the next turn
start, with a summary that the model the segment ran on writes; a Session that has no context
segment yet, before its first message, opens its first segment on the selected model. The
change does not answer a pending question or start an Agent turn.

After the Session is authorized, the server admits the model for this environment and caller,
or refuses with `AGENT_COMMAND_NOT_ADMITTED`:

| Model | Admitted when | Otherwise |
| --- | --- | --- |
| `AGENT_MODEL_OPUS_5_5`, `_SONNET_5_5`, `_HAIKU_4_5` | the server has a Claude key | `FAILED_PRECONDITION` "this environment has no Claude key" |
| `AGENT_MODEL_MOCK` | the server offers the mock model (`agent.mockModel`, never in prod), and the environment is Local Kind or Native or the caller is the ACTIVE administrator | `FAILED_PRECONDITION` where the mock model is not offered, `PERMISSION_DENIED` for any other caller outside Local |

The administrator check reads the administrator account itself; a browser role grants nothing.

### Mock model scenarios

`AGENT_MODEL_MOCK` runs the Session on the mock model, which serves scripted scenarios in the
server process for testing without model cost; Dex, the Sandbox, GitHub, Preview and Releases
stay real. `ListMockScenarios` takes only the caller and returns `selectable`, whether this
caller may select the mock model under the rules above, and, only when it may, the catalog
compiled into the server in name order: each `AgentMockScenario` has its `name`, `title`,
`description`, its `category` (`CATEGORY_PRODUCT`, `CATEGORY_AGENT_FEATURE`,
`CATEGORY_PROJECT_FEATURE`, `CATEGORY_PLATFORM_ERROR`, `CATEGORY_USER_ERROR` or
`CATEGORY_AGENT_ERROR`, which Studio groups the scenarios by), `needs_new_project` (the scenario
edits, merges a subagent's changes, pushes or confirms requirements, so Studio starts it only in a
new Project), `test_action` (what only its Dex test does between its inputs; Studio does not
offer such a scenario), the `arguments` a person may set with their `default_value`, and its `inputs` in
order. An `AgentMockScenarioInput` has its `number` from 1, a `label` and a `kind`:
`KIND_MESSAGE` (SendMessage) and `KIND_REQUIREMENTS_CHANGE` (AnswerQuestions with the requested
changes) send exactly their `content`, which carries the `[[scenario:<name>#<number>]]` marker,
followed by one `[[scenario-arg:<name>=<value>]]` per argument; `KIND_REQUIREMENTS_CONFIRM`
confirms the pending card; `KIND_ANSWERS` answers the pending questions with `answers` in
question order. Each mock reply reports where its scenario stands, which the Snapshot carries as
`AgentDescription.mock_progress` (the scenario, the number of the input the newest mock reply
answers and the number of the next input, 0 after the last); the mock composer offers exactly
that next input. Studio shows the mock model only when `selectable`, and the mock model itself
answers an input that is not the next one with a text that names the expected input. The
catalog holds no secret.

### Agent call measurements

`AgentMessage.model_call` and `tool_execution` are optional generated messages. They expose
the observed model and effort, request and first-event timing, token counters, and the
authoritative tool outcome, timing, result bytes, omission count and command exit code
where available. An absent counter is not zero. These fields are kept with application
messages and mapped explicitly to Studio; Dex engine history stays diagnostic only. No
provider replay material is exposed.

### Actor attribution

Session attribution uses response-only `Actor{account_id,email}` fields, frozen from the
authenticated caller after authorized command admission. `AgentMessage.actor` and
`AgentUserMessage.actor` identify the original author; `AgentMessage.steered_by` and
`AgentPendingUserMessage.steered_by` identify the collaborator who steered that message,
without replacing its author. Retained and archived history keep those separate identities.

`AgentDescription.model_changed_by` and `preview_auto_deploy_changed_by` record the accepted
model change and effective auto-deploy toggle; `AgentConfirmedRequirements.confirmed_by`
records the person who confirmed the requirements. Legacy entries may lack attribution,
and automatic entries carry no invented human actor. No request carries an Actor; neither
the browser nor another client supplies attribution or authority.

## Preview

Build owns the Project's Preview preparation, configuration and deployment; Publishing owns
Live only. PreviewService addresses the Project's one Preview by `project_id`. Every
command also carries the instance the page observed, `generation` and
`workspace_request_id`, and the Preview's `expected_revision`; StartPreview, StopPreview and
PreparePreviewSource add a `request_id`, which identifies the command across retries. Go
checks Project access and the ACTIVE Session, and rejects an incomplete observation with
InvalidArgument; the Preview compares the observation it admits.

PreparePreviewSource returns acceptance only. The Session snapshot and Watch carry
`preparedSource` status, safe failure, source commit, immutable FDG reference and digest,
and DefinitionRevision. No Release is created. The preparation registers the source with
the configuration host and then stores the commit's definition bundle with its AppManifest;
the Preview configuration of the Session's HEAD reads that bundle from storage, never
starts the application and never wakes a Sandbox, and an unprepared HEAD answers
FAILED_PRECONDITION "Prepare the current source". A Project without a Preview reports
NOT_PREPARED. GetPreviewDefinitions is read-only and returns the Preview with the prepared
`flow_definitions`.

PreviewSnapshot keeps `revision` as the Preview's command fence and carries
`workspace_request_id` (the Session's instance the Preview last acquired), the current
`prepared_source`, and the running `source_commit`,
`definition_revision`, `serving_revision` and `phase_timings 17`. The serving revision changes only at exposure
or stop, so a preparation cannot silently replace a running catalog.

The Preview status comes from the Project's PreviewFlow as `NOT_STARTED`, `STARTING`,
`READY`, `STOPPING` or `STOPPED`. `SessionSnapshot.preview` is always present: a Project
that never ran a Preview command reports `NOT_STARTED`. `READY` enables the probed application
link and the stop control; a new start requires an explicit `NOT_STARTED` or `STOPPED` snapshot.
Missing or unknown state does not authorize a start.

StartPreview and PreparePreviewSource send the observed `workspace_request_id`, generation
and expected revision. `0/0/empty` is the first-demand form, valid only while the Project
has no Preview: it starts the Project's PreviewFlow, and a retry with the same request ID
joins it. Any other command names the Preview the caller observed, which the Preview
compares under `CommandLock`; stale or partial observations are rejected. A Preview that
never acquired an instance, such as one whose first command was refused, is observed with
its revision and generation and an empty `workspace_request_id`, which Go accepts and the
Preview compares like any other observation.

StartPreview with `apply_configuration` applies the Preview's configuration: it names
`configuration_revision`, the head revision the page showed after its autosave settled, and
the start freezes exactly that revision; a head past it refuses the start
`PREVIEW_CONFIGURATION_CHANGED`. `configuration_revision` is required exactly when
`apply_configuration` is set (`INVALID_ARGUMENT` otherwise). Every other start, the menu Start
and the automatic deployment of a push, runs the version the last Apply froze, revalidated
against its commit; a version that lacks what the commit declares refuses it
`PREVIEW_CONFIGURATION_INCOMPATIBLE` with that version's gaps. Before the first Apply any start
applies the head. A retried request ID with another flag or revision is `COMMAND_CONFLICT`.
The commands record the caller as their actor. `PreviewSnapshot.applied_configuration` names
the version the last Apply froze, which a stop keeps; `recent_starts` holds the newest 50
starts and automatic deployments, each with its actor, commit and version, revision, whether it
applied, its outcome and its phases; `serving_version` and `prepared_source.version` are the
versions of the served and prepared commits. A Preview prepares only commits that `main`
reaches: another is `PREVIEW_HEAD_NOT_READABLE`.

A command that needs the workspace acquires the Session's READY instance. When the
Session has none, the command is still accepted: the Session requests an instance, the
Preview waits until the Session announces the settlement, and a failed request ends the
command with `PREVIEW_WORKSPACE_FAILED`. StopPreview addresses the Preview directly.
Before the SessionWorkspaceFlow reclaims the instance a READY Preview serves on, it asks the
Preview to release it: the Preview stops, and the Session stays `ACTIVE`; a later StartPreview
acquires the next instance. A command that arrives while the Session replaces the instance
the Preview holds releases it first, then waits for the next one: a start stays
`STARTING`, and a source preparation leaves a Preview that served on the released
instance `STOPPED`.

A failed command keeps its resource references and cleanup responsibility and supplies
`failure` separately; it does not synthesize a Preview `FAILED` state. A start, sent by a
person (`start-preview`) or the automatic deployment after a push (`deploy-preview`), that
ends before READY after it pinned its configuration is followed by the Preview's own
`release`, the stop chain, so the Preview reads `STOPPED` with the start's `failure` (phase
`start-preview` or `deploy-preview`), such as `PREVIEW_FAILED` with the application's
redacted log tail when it exited at startup, and Studio shows that failure and offers
StartPreview again. A `start-preview` that ends before it pinned its configuration, such as
one refused `PREVIEW_SOURCE_NOT_PREPARED` or `PREVIEW_CONFIGURATION_INCOMPATIBLE`, started
nothing and admits no `release`: a Preview it found `READY` keeps serving with the same URL
and pinned configuration and returns to `READY` with the start's `failure`, one it found
`STOPPING` returns to `STOPPING`, and any other reads `STOPPED` with its failure at once. The next admitted command clears the failure. Until
a deployment starts the Preview it leaves the Preview's status as it was, so a READY
Preview keeps serving through a deployment refused before. Studio displays the failure and
permits an explicit retry of a retained `stop-preview` or `release` cleanup failure
through StopPreview. `is_recoverable` is advisory; when it
is false, the displayed prerequisite must be addressed before retrying. `STOPPED` means
the application stopped, its grant was revoked and its route was removed, so nothing is
exposed on the Project's Preview host until the next start reconciles the route again
under the same URL. The Project's one Preview takes no lease: a start without a READY preparation of this instance
records `PREVIEW_SOURCE_NOT_PREPARED` (`VerifyPreviewSource`), which leaves the instance
READY.

A start deploys the prepared commit on the Preview engine inside the Session's Sandbox
([the current contract](../architecture/agent-harness-design/sandbox.md)), even
when the workspace has moved on since the preparation. An application that fails to build,
exits or misses its health probe ends the start with `PREVIEW_FAILED`, which names the
application as the owner; the engine's own refusals are `PREVIEW_STATE_FULL`,
`PREVIEW_STATE_INCONSISTENT` and `PREVIEW_ENVIRONMENT_FAILED`, and a replaced Sandbox,
Service, grant or route object is `PREVIEW_OWNERSHIP_CONFLICT`. An explicit StopPreview
keeps the engine running; a reclamation stops it before the Session deletes the Pod. A
configuration that lacks what the prepared commit requires ends the start with
`PREVIEW_CONFIGURATION_INCOMPATIBLE`, whose message names every missing connection,
authorization, trigger binding and environment value, and leaves a running Preview
untouched.

Each successful, pushed and unblocked `commit_and_push` result schedules its pinned Preview
process build immediately beside the Agent continuation. The Agent does not wait; Stop does not
cancel the pushed commit's accepted build. One command runs and the newest queued push wins:
intermediate commits already passed the pre-push source gate (Go compilation, FDG rendering,
and application tests), and superseding a queued Preview build never skips those checks.
A reclamation race resumes the Session workspace for building; a closed Session or deleted
Project is never resurrected. Direct shell/external pushes are not Session push events.

`prepared_source` records PREPARING for the admitted commit; `ReadPreviewSourceVersion`
commits its Git-owned version before `PreparePreviewSource` compiles/renders, then the source
becomes READY or FAILED. Missing Preview configuration is checked
after the READY build, so process readiness is independent of Preview restart. A failed new
build does not change the version an existing Preview serves. An immutable READY process
bundle stays readable across Sandbox replacement; a manual start rebinds that existing
preparation to the acquired instance while runtime instance/UID fences remain unchanged. Studio calls this step **Build
processes**, shows versions as **VN · subject**, and offers manual building only when automatic
refresh is off or the current build failed. Failure retries use the Session's current pushed HEAD.

`PreviewSnapshot.automatic_deployment` is the newest deployment of a pushed commit the
Preview handled, `PreviewDeploymentSnapshot{commit, status, failure, updated_at,
queued_commit}` with the commit of the newest request that waits while another Preview
command runs, and is absent before the first request. `configuration_gaps` names what the
configuration lacks for the commit that the last refused start or deployment
(`PREVIEW_CONFIGURATION_INCOMPATIBLE`) prepared, at most 32, names only; an admitted start
or deployment clears them.

UpdatePreviewAutoDeploy owns the single automatic process refresh preference for the Project's
Session, labeled **Build each push automatically**; it controls building and the following
Preview deployment attempt, with no independent refresh setting.
`is_enabled` is optional on the wire only so that an omission is rejected with
InvalidArgument. It is a command of the Session's Agent, so the handler reaches the Session
through the Session Service: a Session that is not ACTIVE answers FAILED_PRECONDITION, a
stopped engine execution its `SESSION_EXECUTION_*` reason, and a repeat changes nothing.
`AgentDescription.is_preview_auto_deploy_enabled` reads it, and a new Session starts with
it on. A commit handed over while it is off is never built later automatically; the next push
builds again once it is on. The command and DTO names retain their existing transport identity.

The Session Watch carries Preview state; there is no Preview Watch and no progress Stream.
`PreviewSnapshot.phase_timings` holds the phases of the current or last start in order:
`configuration`, `environment`, `grant`, `application`, `route` (at most five). An admitted
start replaces them, a zero `duration_ms` marks the phase in progress, and a failed start keeps
the phases it reached. The engine status is Flow state that `PreviewSnapshot` does not map yet.

## Releases

A Release is one main commit of a Project, and its ID is the full lowercase commit SHA.
ListReleaseCandidates returns the latest 100 main commits, without an author filter, each with
its `version` (the commit's number on main and its subject, `repohost.DescribeCommit`); the
server keeps the versions it described in process, at most 1024 by commit SHA, since a
commit's version never changes. Trusted
server composition selects the immutable build recipe when the Release source is prepared,
and each build revision keeps its accepted recipe across retry and recovery; browsers never
choose one, and there is no application-info API. See
[StartFlow admission](../architecture/start-flow-admission.md).

PrepareRelease admits source preparation for one main commit and returns only
`release_id`. StartReleaseBuild names the Release by `release_id` and adds the logical
`build_revision_id` (`build-<UUIDv4>`), the `configuration_revision` of the version's frozen
copy, the Release's `expected_revision`, optional `auto_deploy` and `expected_live_revision`; it
returns the
accepted `build_revision_id` without a post-admission snapshot read, and a missing or
invalid `build_revision_id` is INVALID_ARGUMENT. RecoverReleaseBuild targets the exact
build at its own expected revision and returns `{}`.

StartReleaseBuild also carries `preview_skip_reason`, trimmed, at most 500 code points
(INVALID_ARGUMENT otherwise), and empty when none is given. A request whose
`build_revision_id` was already admitted replays its original acceptance before any other
check. A new build is admitted only when the version met the publishing rule, which its
record keeps as `ReleaseBuildRevisionSnapshot.preview_validation`: `KIND_LIVE_CODE` for the
Release current Live serves, `KIND_VALIDATED` with the READY Preview start
(`start_command_id`, `started_at`) of the commit, or `KIND_SKIPPED` with the reason. Without
one it is FAILED_PRECONDITION `PREVIEW_VALIDATION_REQUIRED`. A frozen copy that is absent or was
built before current Live, Preview's applied version or Live's next-publish layer changed is
FAILED_PRECONDITION `RELEASE_CONFIGURATION_STALE` (DeriveLiveConfiguration first), and a copy
revision other than `configuration_revision` `REVISION_CHANGED`. Gaps are not an
admission refusal: the build's freeze fails with them itemized. A build created before the
validation was recorded has no `preview_validation`.

`PreparedReleaseSourceSnapshot.version`, `ProjectLive.version` and `DeploymentSnapshot.version`
are the Release's version, computed once when its source was prepared; they are absent for a
Release prepared before versions were recorded, and a Project write's response leaves
`ProjectLive.version` unset until the next read or Watch snapshot.

ReleaseSnapshot exposes the prepared source, the active and ready build IDs and bounded
exact reads of those two build records. Each record distinguishes the draft revision from
the frozen runtime revision and exposes `provider_settled` for new-build admission after a
confirmed failure. GetRelease returns the Release with `flow_definitions`, the complete
immutable Flow Definitions of its prepared source, which stay empty until
`release.prepared_source.status` is READY. A FAILED Release returns its snapshot and
failure, with empty definitions even if its source was prepared: failure diagnostics do
not depend on an object-store download. Removed or cleaning resources also return metadata
without reading deleted artifacts, and a READY Release with missing or invalid artifacts
fails explicitly. No read waits for a build milestone. A timed-out GetRelease read is
UNAVAILABLE, not a manufactured FAILED build; Studio returns HTTP 503 with `Retry-After: 1`,
including when its own 35-second upstream deadline expires before the server answers.
Caller cancellation stays cancelled, authorization and absence keep their ordinary
statuses, and command deadline semantics do not change. Server logs identify the slow or
failed read phase (authentication, Project authorization, Release snapshot, each exact
build revision or prepared definitions) and its duration without payloads or credentials.
ListReleases and WatchRelease keep the lightweight ReleaseSnapshot, so
they never download the definition bundle, and there are no separate definition-read RPCs.
Prepared-source views use GetRelease; serving Runs read exact Deployment or Preview
definitions through the Flow API. Live Runs and Inbox do not preflight the current
Release's prepared definitions: a newer FAILED build does not invalidate the READY
build pinned into the existing Deployment. Their client scope remounts when the
observed Deployment ID changes, including another deployment of the same Release.

AppManifestSnapshot is the one validated exact-commit snapshot of `dex-app.yaml`. Project
owns the current reference; an immutable Release pins its accepted copy. Connector and
environment sub-digests stay admission inputs for configuration and deployment. No
parallel manifest state or old-name compatibility API is kept.

StartReleaseBuild and StartDeployment also admit explicit recovery of their existing
failed resource. Every accepted request ID is kept in a bounded receipt slice inside that
Flow's existing provider record. A new request may resume only its exact failure revision;
original and older request replays neither restart work nor reset budgets. READY resources
never restart. Deployment recovery keeps its pinned inputs and Project-issued generation; a
different desired target cannot replace unreconciled effects. No RetryRelease,
CancelRelease or RetryDeployment public API remains, and internal deleting-Project cleanup
stays. A person cancels a build or a deployment with CancelReleaseBuild and CancelDeployment
([the current contract](../architecture/studio-setup/flow-changes.md)).

### Build cancellation and the build log

CancelReleaseBuild cancels the Release's active build revision until its artifacts commit, for
the build capability on the READY Project; `request_id` is required, and any request for a build
already cancelled or cancelling is accepted unchanged, keeping the first request and actor. A
build whose automatic deployment may be admitted (READY with `auto_deploy`, or deploying) is
FAILED_PRECONDITION `RELEASE_BUILD_DEPLOYING`, whose ErrorInfo metadata `deploymentId` names the
deployment to cancel instead (remedy `CANCEL_DEPLOYMENT`); a build that is not the active one or
not in progress is FAILED_PRECONDITION `RELEASE_BUILD_NOT_CANCELLABLE`; a missing build is
NOT_FOUND. ProjectReleaseFlow stops the provider build and ends the revision `CANCELLED` with a
`Cancellation{actor_account_id, requested_at}` as `ReleaseBuildRevisionSnapshot.cancellation`,
and the Release's status mirrors it, as for a failure; the Release stays open, and a new build is
admitted afterwards as after a failure. A cancellation the provider cannot confirm still ends
`CANCELLED`, without `provider_settled` and with the failure `BUILD_CANCEL_UNCONFIRMED`
([releases](../../internal/dexai/delivery/releases/README.md#build-cancellation)).

GetReleaseBuildLog reads one page of a build revision's provider log from `cursor` (empty from the
start, at most 1024 bytes, InvalidArgument for a cursor no page returned), for the build
capability: `lines`, at most 64 KiB of whole redacted lines, `next_cursor`, `complete` once the
build ended and nothing follows, and `retry_after_ms`, the earliest useful next read. Each page is
redacted on the server with the build's configured values and the Project's infrastructure names,
and nothing is stored. Per replica the viewers of one page share one read for 2 seconds, and a
token bucket of `builds.logReadsPerSecond` (default 2, burst 4) bounds the reads it starts; a read
beyond it, or one the provider throttled, answers an empty page at the same cursor with a longer
`retry_after_ms` instead of failing
([deployments](../../internal/dexai/delivery/deployments/README.md#build-logs)). The hosted builder reads
CloudWatch's `nextForwardToken`, the local one a byte offset
([build port](../../internal/dexai/delivery/releases/build/README.md)).

## Deployments

A Deployment serves one READY Release build with its frozen configuration as the Project's
Live application. `StartDeployment.deployment_id` is a caller-owned lowercase DNS label of
1 to 36 characters: letters, digits and interior hyphens, with an alphanumeric first and
last character, except `preview`, the workload ID of the Project's Preview grants
(`flowid.PreviewWorkloadID`), whose `app-preview-<generation>` names share the Project's
storage with the Live grants. UUID request identities and readable IDs such as
`deploy-checkout-1` are used unchanged. Invalid IDs return INVALID_ARGUMENT before a Flow starts; the same
constraint guards new internal starts. Existing Flow lookup and cleanup keep the original
persisted ID. Studio reuses its request UUID as the deployment ID, and retries keep both
identities. Resource names are `app-<deploymentID>-<generation>`; a uint64 generation keeps
that name within the Kubernetes Service limit without hashing or truncating the ID.
StartDeployment requires the exact `build_revision_id`, and DeploymentSnapshot and the Live
pointer carry it.

A `PhaseTiming {phase 1; Timestamp started_at 2; int64 duration_ms 3}` is one phase the
owning Flow measured durably. `phase` is an open set of code constants, never provider text,
and a zero `duration_ms` marks the phase in progress. `DeploymentSnapshot.phase_timings 12`
holds the deployment's phases, one per phase and per Live preparation branch, the latest
run of each. `ReleaseBuildRevisionSnapshot.phase_timings 15` holds the build's phases:
CONFIGURATION_FREEZE, the builder's QUEUE, HOST_PREPARATION, DEPENDENCY_PREPARATION,
ANALYSIS, COMPILATION and IMAGE_PUBLICATION, then ARTIFACT_PUBLICATION.
`PreparedReleaseSourceSnapshot.phase_timings 6` holds the builder's phases of the source
preparation. `PreviewSnapshot.phase_timings 17` and `SessionWorkspaceSnapshot.phase_timings 11`
hold the Preview start's and the workspace start's phases. A timing is a measurement, never a deadline.

GetRelease and GetDeployment read the owning typed RPC after Project authorization.
Listings use the FlowType plus the indexed ProjectID, bounded candidates, current typed
reads, FlowID deduplication, and principal- and filter-bound signed cursors. Removed
deployments stay historical records until Project deletion. GetDeployment and
WatchDeployment serve historical and active attempts; there is no GetLiveDeployment.

CancelDeployment cancels a deployment, for the build capability on the READY Project, before it
commits the new Live (PENDING, DEPLOYING or RECOVERING, before `CommitCurrentLive`), or a FAILED
one that never went live; `request_id` is required, and any request for a deployment already
cancelled or cancelling is accepted unchanged, while a new one admits a failed cancellation's
cleanup again. A deployment that is live, chose to commit the new Live, is retiring or has ended is
FAILED_PRECONDITION `DEPLOYMENT_NOT_CANCELLABLE`. The deployment is `CANCELLING` while its cleanup
revokes the generation's grant, removes its generation and releases its Project lease, then ends
`CANCELLED` with its `Cancellation` as `DeploymentSnapshot.cancellation`. The running Live, its
generation, route and Worker endpoint, its connection usages and its Slack trigger registrations
are untouched. A deployment of a Project being deleted is still `CANCELLING` and ends `REMOVED`
([deployments](../../internal/dexai/delivery/deployments/README.md#cancellation-by-a-person)).
`ListEnvironmentEvents` lists a cancelled build or deployment of Live as `KIND_PUBLISH_CANCELLED`.
Migration `0009_publish_cancellation.sql` adds the nullable `cancellation` columns of
`release_build_revisions` and `deployments`, which only the Dex Attribute Store sync writes.

## Configuration

GetProjectConfiguration and ValidateProjectConfiguration take a
`ProjectConfigurationTarget`: the Project, the PREVIEW or LIVE scope, and the Session or
the prepared Release. They expose safe prepared source and revision metadata, immutable
pins and the request-specific validation receipt. The Flow API owns the native connection,
environment, provider-command and OAuth operations. Studio owns all form UI; Go hosts the
source-adapted handlers and uses the released standard Connector SDK storage package. No
management reverse proxy or standalone Dex Web is required, and Dex Server does not
understand Git main, Release, build or deployment business concepts.

Prepared Preview and Live validation completes synchronously. Live validation requires the
prepared Release commit, except on a target with `next_publish`, whose empty `release_id` names
the source the Preview prepared; another Live target without one is InvalidArgument. Validating
a next-publish target checks the next publish as it resolves at the layer revision it names and
writes nothing.
`expected_revision` is the CAS precondition on the current native configuration document,
with zero denoting an absent document. Initial successful validation creates and freezes
revision one; validating an existing nonzero revision keeps that revision. The response's
`configuration.revision` and `configuration.validation.configuration_revision` both
identify the accepted revision. `ProjectConfigurationValidation {request_id,
configuration_revision}` only exists for a success, so the record itself is the receipt and
a failed validation is an RPC error. Clients match the submitted request ID, the selected
Project, scope, Session or Release and the accepted native revision before accepting the
receipt. Validation never changes the running Live deployment's exact configuration. No
generic Operation resource is added.

Prepared Preview reads and Live reads, which resolve the next publish for the target's source,
derive READY from a read-only validation of the exact current draft, manifest and credential
state. A configuration that is not READY names every requirement of its source that it does not
meet in `requirement_gaps`, a `ConfigurationRequirementGap{kind, connection_name, connector_id,
trigger_name, binding_name, environment_name, unit_label, flow_name, step_name, operation_id,
follow_refusal}` each, names only and never a value (`unit_label` names the required
Connector UI unit a saved trigger binding, or the Step operation use that `flow_name`, `step_name` and `operation_id` name,
lacks); it names
none when the report itself cannot be made, such as for a connector release that cannot be
resolved. The internal check
returns before initialization or freezing and neither starts an application nor stores a
second validation lifecycle. Start and Build revalidate and accept exact immutable inputs.

A Preview read (`scope` PREVIEW) lists the `items` of the configuration head against the version
the Preview last applied, by the same `ConfigurationItemKey`: `UNCHANGED`, `CHANGED`, `ADDED`
or `REMOVED`, with a connection also compared by its TeamSpace connection's current settings,
which an Apply freezes, and a secret by its reference. A connection whose selected TeamSpace
connection was deleted is `CHANGED` with the warning `CONNECTION_DELETED`. `unapplied_change_count`
counts the items that are not `UNCHANGED`; before the first Apply every item is `ADDED`.
`applied_configuration.configuration_revision` is the frozen document's revision: an Apply that
copies changed TeamSpace settings writes them as one more head revision, so it can be the applied
revision plus one. Compare versions through `unapplied_change_count`, not by revision.

A Live read (`scope` LIVE) returns Live's next publish
([the current contract](../architecture/studio-setup/flow-changes.md)) as it
resolves for the target's source. Live's settings for the next publish are one sparse
next-publish layer per Project that holds only Live's own items; an item it does not hold
follows Preview's applied value, or current Live's when Preview's applied version lacks it
([configuration host](../../internal/dexai/studio/confighost/README.md#live-next-publish-layer)). The
read lists the `items` by `ConfigurationItemKey{kind, connector_id, connection_name, trigger_name,
binding_name, operation_id, flow_type, step_type, environment_name}`: `FOLLOWS_PREVIEW` (the layer
does not hold it), `OWN` (the layer holds Live's own value) and `NO_LONGER_USED` (current Live has
it, the source does not declare it). `differs_from_live` marks an item whose resolved value differs
from what current Live runs, so it waits for a publish, and `differs_from_preview` one whose value
differs from Preview's applied value; both compare a connection by the TeamSpace connection it
selects and a secret by its reference. `live_value_json` and `preview_value_json` hold current
Live's and Preview's applied non-secret values, empty for a secret. `follow_refusal` (`SECRET` or
`ENDPOINT_BOUND`) marks an item that can never follow: a secret environment value, or a connection
of a connector whose connections are tied to one address; without a value of Live's own it is a
requirement gap carrying the same `follow_refusal`. A Slack trigger binding whose channel another
Project's Live of the same TeamSpace connection listens to carries `CHANNEL_SHARED`, with that
Project's name only for a member of it. A connection item whose selected TeamSpace connection is
missing or no longer ACTIVE carries `CONNECTION_DELETED`. `next_publish_revision` is the layer's
revision (zero while it is absent) and `preview_configuration_revision` the configuration revision
of the version the Preview applied (zero before the first Apply). For a next-publish target
`revision` is the layer's; for a Release target it is the revision of that version's frozen copy,
and `is_stale` reports that the copy is absent or was built before current Live, Preview's applied
version or the layer changed. A Project without a layer reads one computed from what it runs:
current Live's items that differ from Preview's applied ones (or that Preview did not apply), every
secret and every connection tied to one address are Live's own, the others follow. The first write
or publish stores it, so no running Live changes.
DeriveLiveConfiguration builds the Release version's frozen copy, the only configuration a
publish of it freezes, from the layer: the items the version declares, resolved, with each
connection selected again for the Live scope and Live's own credential broker. `expected_revision`
is the layer revision the caller showed (zero while it is absent) and
`expected_preview_configuration_revision` Preview's applied configuration revision; either moved
is `ABORTED` with `REVISION_CHANGED`. It answers the Live read of the Release. A secret is never
copied across scopes or shown. Nothing waits for a confirmation and nothing conflicts: the layer
holds one value per item.
A refreshed page must not discard a successful current configuration by always reporting
NEEDS_REVIEW, nor assume that a historical receipt validates new edits.

Setup reconciles the Project's configuration storage and registers its exact AppManifest before
READY; the first Live deployment prepares the Live Temporal and Dex.
Preview has separate Session storage and engine resources. Preparation registers an
immutable source and FDG without starting the application or replacing the serving
catalog. The manifest pins the official Connector module and release, operations and
trigger bindings. Configuration reads never extract source or refresh tokens.

New Live Release preparation registers the commit's source without seeding a configuration;
DeriveLiveConfiguration builds the version's frozen copy from Live's next-publish layer when it is
published. Save and Build validates the copy revision and pins an immutable version of the
canonical Live configuration key.
Deployment selects that exact successful build, independently of a newer draft. Preview
edits its Session canonical head; a running Preview keeps its accepted exact object version
and changes only on explicit restart. Shared Live credentials take effect on the next
actual connection use across deployed builds; application SDK refresh stays actual-use
only under its scoped storage grant. No secret is compiled into a Release image or exposed
in a Flow reference.

### Single-method Connector identity

Project AppManifest v1 authorizes exactly one authentication method for each named
connection, and SelectTeamSpaceConnection admits only a TeamSpace connection of that
method; an undeclared method is rejected with `CONNECTOR_AUTH_METHOD_INVALID`. There is no
multiple-method selection. The request names
the Connector `module_path` but no release, and a module path other than the declared one
is rejected with `CONNECTOR_MODULE_CONFLICT`. The AppManifest declaration at the request's
AppManifest revision selects the release that validates the fields, and stored
configuration and credentials record no release. Catalog reads, validation and the frozen
copy's selection of each connection compare the stored module path and method with the
declaration; a different Connector release never conflicts.

An AppManifest connector declaration requires an explicit string `authMethodId`. The
canonical empty string selects a released manifest's legacy single method; it is not
replaced with the method's auth type (`oauth2`, `apiKey` and so on). Missing or null fields
are rejected during source admission. Named methods keep their exact release IDs.
Configuration validation checks the selected method against the checksum-verified Connector
release before any credentials are accepted. Single-method Connector setup consumes the
released manifest unchanged: the canonical empty method ID stays empty in the public API,
the ordinary connection configuration and the provider credential payload. The Connector
SDK requires a nonempty private credential-envelope marker, so Superverse uses `default`
only at that storage boundary and compares credential metadata through the same mapping.
Named methods keep their exact ID. Existing drafts written with the old synthetic public
`default` ID stay Conflict until explicitly saved with CAS; there is no automatic
credential rewrite or provider exchange.

A scope's configuration holds no credential: each connection name selects a TeamSpace
connection ([TeamSpace connections](#teamspace-connections)).
`SelectTeamSpaceConnection` names one connection name of one scope and the
`team_space_connection_id`; a Live selection lands on Live's next-publish layer. The selection
admits only an ACTIVE connection of the Project's TeamSpace with the declared connector,
major version and method; a declared release whose OAuth scopes differ from those the
connection was authorized for is refused with the ErrorInfo `CONNECTION_SCOPES_DIFFER` and
changes nothing, and a release that does not list its host-kept credentials with
`CONNECTOR_VERSION_UNSUPPORTED`. The entry carries the connection's settings as the
declared release reads them and the document's credential broker names the platform's
credential read endpoint with the scope's token. A Preview selection records the scope's
usage at once; a Live deployment records the usages of the version it serves before it
switches traffic. `ReleaseUnusedConnections` (Live only, for a builder of the Project, and
refused with `DEPLOYMENT_IN_PROGRESS` while a deployment runs) releases the Live usages
that the current Live version's frozen configuration does not name, so the connections can
be deleted.

The configuration writes (SelectTeamSpaceConnection, SaveTriggerBinding,
SaveOperationConfiguration and SaveApplicationEnvironment) on a LIVE target with `next_publish`
edit Live's next-publish layer, checked against the declarations of the source the target names;
a Release target's frozen copy is read only (InvalidArgument). `follow_preview` (on
SaveApplicationEnvironment, the variable names) removes Live's own value, so the item follows
Preview again. Following is refused with HTTP 409 and the code `CONFIGURATION_ITEM_CANNOT_FOLLOW`
in the `FlowApiDocument` on the Preview scope, for a secret variable and for a connection of a
connector tied to one address. `ConnectorCatalogEntry.endpoint_bound` marks such a connector, whose
provider signs inbound webhooks for one endpoint, and `endpoint_bound_reason` says why: Stripe,
Typeform, Help Scout and Calendly, a platform list until the connectors library's manifest declares
it.

## TeamSpace connections

`TeamSpaceConnectionService` serves a TeamSpace's named connector configurations and
credentials, one connector major version each. The database owns them
([connections](../../internal/dexai/tenancy/connections/README.md)); no Flow does, and every
request is one strongly consistent query or transaction. The read model decides who may do
what ([`internal/dexai/api/tenancy`](../../internal/dexai/api/tenancy/README.md)):

- A member of the TeamSpace, or the platform admin, lists and reads its connections, with
  the Projects and scopes that use each one. Any authenticated caller lists the connector
  catalog: the published catalog's released connectors, not paged.
- A TeamSpace admin, or the owner or builder of any of its Projects, creates, edits and
  deletes connections and opens the editor's forms, whose provider commands use the
  connection's credential, while the TeamSpace is READY.
- A credential change (replacing an entered credential, editing the setting the OAuth
  scopes come from, or StartTeamSpaceConnectionOAuth) also needs an admin or a builder of
  every Project whose Live version uses the connection; DELETED Projects do not count.
  Otherwise it is PermissionDenied and changes nothing.

Creation is idempotent: the connection ID derives from the caller and `request_id`, a replay
returns the same ID, and the same request ID with other content is AlreadyExists without
an ErrorInfo. A name
another ACTIVE connection holds is AlreadyExists with the ErrorInfo
`CONNECTION_NAME_TAKEN`. `copy_from_connection_id` copies a connection to a newer major
version: the settings and the `user` OAuth client, revalidated, and a new authorization.
CreateTeamSpaceConnection refuses any other connection of an OAuth method with
InvalidArgument: StartTeamSpaceConnectionOAuth with `new_connection` (instead of
`connection_id`) validates the same creation and requires its name to be free
(`CONNECTION_NAME_TAKEN`), and the completion creates the connection READY with its first
credential. A provider's refusal or a refused code creates nothing.
Update and Delete fence `expected_revision`, and a replaced credential
`expected_credential_revision`; a stale fence is ABORTED with `REVISION_CHANGED`. Deletion
of a connection a scope still uses is FailedPrecondition with `CONNECTION_IN_USE`; usages
of DELETED Projects are removed with the connection. Reads never return a credential value:
GetTeamSpaceConnection names the stored credential fields, which an edit may keep with
`keep_credential_fields`. Lists page by creation time, newest first, with ID as the
tiebreaker (M8); statuses default to ACTIVE.

StartTeamSpaceConnectionUiSession returns a release's Studio manifest and the methods for
which the platform offers its own OAuth application (`platform_client_method_ids`: the
authorization fills every required credential field and the application's scopes cover the
method's). InvokeTeamSpaceConnectionCommand runs one provider command of that form, which
its nonce binds to the caller and the connection, for ten minutes. The OAuth flow is under
[Flow API](#flow-api); its redirect URI is the Studio origin plus
`/api/connector-oauth/callback`. Refusals Studio branches on carry their ErrorInfo reason:
`CONNECTOR_VERSION_UNSUPPORTED`, `CONNECTION_SCOPES_DIFFER`, `CONNECTION_DELETED`,
`CONNECTION_IN_USE`, `CONNECTION_NAME_TAKEN`, `OAUTH_DENIED`, `DEPLOYMENT_IN_PROGRESS` and
`REVISION_CHANGED`.

## Resource Watches

WatchProject, WatchSession, WatchRelease and WatchDeployment each begin with their typed
snapshot, including when a signed resume cursor is supplied. Readers attach from observed
tails before the snapshot is read. Bounded buffering and five-second authoritative
reconciliation correct lost or pre-commit progress; progress never certifies business
completion. Reader cancellation follows BFF disconnection and repeated authorization
checks. A Session read reuses its freshly scope-validated lifecycle snapshot only within
that request. Each reconciliation read returns the authoritative snapshot and the sources
derived from that same read. Before publication, those identities are compared with the
attached readers, including when the snapshot is unchanged; a snapshot equal (as a
protobuf message) to the last one sent is not sent again. The initial pre-subscription
source read and every progress-event source check stay independent and freshly authorized.
The Session Watch reads its first sources from the snapshot, its three constant Agent event
Streams and the activity Streams of the running subagents it lists, so a reconnect while a
subagent runs resumes every cursor it carries instead of realigning on fresh tails. It then
confirms them by authorizing the Session instead of rereading it, and its five-second reread
notices a changed set of running subagents; the other Watches derive their sources from the
snapshot read. A subagent's Stream that cannot be
read stays silent instead of ending the Watch. A Dex Stream source is identified as `<FlowID>/<Stream name>`, and signed cursors are
keyed by that identity. There is no cross-cycle authorization cache. This is a
point-in-time read, not an atomic authorization and Stream transaction or a promise that a
revocation occurring after a read is visible before the next check. A progress source
repeats its Dex long poll when the Server's long-poll cap or a per-poll deadline expires
without a message, so an idle Watch stays open until the client leaves. There is no
Operation resource, generic GetOperation or WatchOperation API.

A WatchSession progress frame carries one Agent Stream event as `SessionProgress.content`:
`activity` for the activity Stream, `reasoning_summary` for the reasoning Stream and
`assistant_text` for the assistant Stream, with its `created_at` and `source`; an `activity`
with a `task_id` is the live status line of that running subagent. Studio
reconnects every Watch explicitly with its last cursor.

Only fixed sanitized errors cross this boundary. Each RPC maps its errors once, where it
returns them: authentication failures, including refresh failures in every Watch, become
Unauthenticated or PermissionDenied with fixed messages, an invalid resume cursor becomes
InvalidArgument, and a status a dependency returned never passes through, so provider and
SDK error details stay hidden. Already transmitted bytes cannot be retracted.

A single sender owns each Go Watch's gRPC writes, with at most one outstanding frame and a
ten-second delivery deadline. While a send is blocked, five-second authoritative reads
continue to check authorization and source identity, bounded by the same deadline. A source
change during delivery ends that connection with Unavailable ("Watch sources changed;
reconnect for a fresh snapshot"); reconnect authorizes the new source and begins with a
snapshot. Idle source changes still realign in place. Every authoritative read
re-authenticates the caller first and, together with that check, is bounded by 30 seconds,
as is the tail read of each source; a read past that bound ends the Watch with
DeadlineExceeded. On failure the Watch cancels and joins its Dex readers and returns from
the RPC handler, which cancels the gRPC transport and releases any blocked sender; it must
not join that sender before handler return. These connection-local limits add no Flow
timers or persisted state. Saturated HTTP/2 transport and exact reader-exit runtime
verification stay separate from the real BFF unread-queue regression and static
compilation.

## Flow API

The Flow API is the API of the embedded Dex Flow UI: Runs, diagnostics, connectors and the
application environment of one served application. It is the same term everywhere:
`FlowApiTarget`, `FlowApiContext` and `FlowApiDocument` in the proto, `flow_api.go` in
`internal/dexai/api` over the domain packages `internal/dexai/studio/{runs,confighost,flowapi}`, and
`lib/server/flow/` (`handleFlowApiRequest`, `dispatchFlowApiRequest`, `flowApiTarget`,
`FlowApiPurpose = 'runs' | 'configuration'`) and `lib/api/flow/` in Studio. Studio reaches
it only through its fixed `/dex/...` routes, which keep their URLs; browser fields cannot
choose an upstream, HTTP path or method, Worker endpoint, bypass flag or permissions. No
standalone Dex Web, management iframe, shared Dex UI package, BFF scope authorization cache
or HTTP reverse proxy remains. The user application's isolated Preview iframe is
unaffected. Internal adapted HTTP handlers are invoked in process, without a second
listener or bootstrap.

The four Flow API services and their requests are in [Services](#services).

Each request is `FlowApiContext context = 1`, the operation's fields and the caller at
field 100; each response is exactly `FlowApiDocument document = 1`. `FlowApiContext` holds
the typed `FlowApiTarget` with `definition_revision`, `app_manifest_revision`,
`configuration_revision` and `credential_revision`. `FlowApiTarget` identifies the Project,
the ProjectScope and, for Live, the Release and, for Runs, the Deployment, and its
`next_publish` addresses Live's next-publish layer as `ProjectConfigurationTarget`'s does;
Preview Runs require the observed `workspace_request_id` and `generation`, and a configuration target
names neither a deployment nor a Sandbox instance (InvalidArgument otherwise). Go re-authenticates each
request and resolves resource ownership independently. Safe body limits and typed revision
checks apply before the selected adapter runs. `FlowApiDocument {bytes json; uint32
http_status}` returns the exact JSON bytes, which keep FDG and renderer number tokens, and
the HTTP status the Flow UI expects; it never exposes provider credentials or an arbitrary
transport operation. Run IDs are diagnostic selectors only and never select a business
resource or start target. WaitFlowHistory is a unary long poll after `after_event_id`, and
TimeTravelFlow is the Dex ResetFlow call.

Idle diagnostic reads have a 30-second server window. WaitFlowHistory bounds its
Dex history wait with a cancellation-owned context; ReadFlowStream sends Dex's
`ReadStreamRequest.wait_time_seconds = 30` and preserves the supplied resume token.
Either wait expiring maps Dex DeadlineExceeded to a `FlowApiDocument` with HTTP
status 408. The BFF relays that document while its unary call is still within its
35-second deadline; the browser's 35-second bound remains a backstop. Diagnostics
continues from the same history event or Stream resume token after an idle timeout,
without an error banner. These windows bound observation, not Flow execution or
Stream retention. Streams remain best effort. Chromium may retry a 408 on a reused
HTTP/1.1 connection; the browser bound still prevents indefinite waiting there.

Live Runs requires Project READY and the exact current READY Deployment. Its Release, build
revision, image, immutable definitions, frozen configuration and unrevoked workload grant
must match the authoritative serving pointer, never the current draft. Preview Runs
requires the ACTIVE Session's accepted READY workspace and a READY Preview. Its workspace
identity, generation and resource UIDs must match Sandbox. Definitions come from the
running Preview's accepted object, not a newer prepared source. The instance and revision
selectors of Preview Runs are required and checked even on reads. Prepared Build definitions stay read-only through
GetPreviewDefinitions and GetRelease, and Runs and Inbox select explicit Preview or Live
tabs.

The Go Runs service reads and verifies exact FDG bytes and digest before exposing a
catalog. Query, Display, field update, action, StartFlow and RecoverFlowStart require the
exact DefinitionRevision; diagnostic reads may omit it, and supplied revisions still match.
`internal/dexai/studio/runs` derives the fixed Worker and target revision and calls a typed
operation of the imported Flow adapter in process, which calls the pinned public Dex SDK
over authenticated, resource-selected private gRPC; no trusted browser header or network
Dex Web endpoint supplies these values. Infrastructure verifies the accepted
namespace and Dex Service UID and owned readiness, then dials fixed engine port 8801: for
Live the scope's Dex Service, for Preview the Sandbox's own Service, whose Sandbox, instance
label and READY engine must match the Session's accepted workspace; in Local Native the
recorded network, container and its published port take their place. Each request closes its
gRPC connection. Worker health independently verifies the
accepted controller and Service, UID, generation and fixed Live 8811 endpoint; a Preview's
Worker listens on 127.0.0.1:18811 inside the Pod, so the Preview supervisor, asked through
sandboxd, must report the served commit, configuration digest and grant generation with
that Worker listening. No fallback selects another target or an older Release. Engine and
API calls are cancellation-owned with a 55-second outer bound; StartFlow's owned Worker
check has a 15-second bound, including a two-second probe. Strict FDG
revision, the business Start schema and owned Worker readiness are checked before
admission.

New Start RequestIDs use `sv2-start-v1.<definition SHA-256>.<logical SHA-256>` in the
engine request memo. The fingerprint binds the actor, exact target, definition, FlowType,
FlowID, operation UUID and canonical input, with explicit Disallow reuse and same-request
IgnoreError. Success is acceptance-only and returns the FlowID without a snapshot. A lost
response queries the original FlowID through RecoverFlowStart and validates the same full
identity; an explicit rerun uses a fresh FlowID and operation UUID. Search and diagnostic
summaries expose `definitionBinding` as CURRENT, DIFFERENT or UNAVAILABLE. The current FDG
may render business Display and actions only for an exact binding; legacy, unbound or
different runs stay inspectable through diagnostics, without applying the current graph.
RunID is allowed only in explicitly diagnostic or engine-recovery selectors, never in Start
identity, serving readiness or public business resource references. See
[the current contract](../architecture/superverse-v2.md).

Configuration reads and mutations authorize an exact prepared Live source or, for Preview,
the source the Preview prepared for the Session's HEAD, which `ReadPreviewTarget` reads
from stored state without a workspace; a next-publish target authorizes the Release it names
or, without one, that Preview source. The source commit and the form's revisions fence
it. Configuration requires scope and namespace ownership and versioned
storage, not an application Worker or Dex engine. The configuration RPCs map to fixed Go
handlers and enforce AppManifest, configuration and credential revisions. Native provider
sessions (StartConnectorUiSession, InvokeProviderCommand) bind the actor, source, manifest
and definition revisions and module identity. The session nonce is a signed opaque
token, accepted as one bounded URL segment; connector and configuration resource
identities retain their separate path validation. Browser-provided trusted
headers are rejected; Go passes the authorized target to the configuration host as a typed
in-process admission, never as headers or a token. The legacy Kubernetes Dex Web admin
token is not read. Configuration JSON uses
`/dex/projects/:projectId/configuration/live/releases/:commit` (a version's frozen copy, read
only), `/dex/projects/:projectId/configuration/live/next` (Live's next-publish layer checked
against the Preview's source), `/dex/projects/:projectId/configuration/live/next/releases/:commit`
(the layer checked against that Release's) or
`/dex/projects/:projectId/configuration/preview`, which expose only native
configuration APIs, never Connector HTML or an arbitrary provider target, and Preview
publishing links redirect to Build Configuration. See
[Project configuration](../../internal/dexai/projects/config/README.md) for canonical keys and
private host lifetime, and the [configuration host](../../internal/dexai/studio/confighost/README.md)
for shared credential semantics. Credential values are never included in mapping errors
or acceptance receipts.

The BFF keeps same-origin mutation checks, bounded input, strict parsing and CSRF tokens
labeled `superverse-flow-api-csrf-v1` and bound to the actor, login, purpose and exact
selected resource. Studio's private `BROWSER_SIGNING_KEY`, an independent 32-byte key
encoded as canonical unpadded base64url, signs only these CSRF tokens and the connector
OAuth route cookies; it is neither sent in gRPC metadata nor exposed to browsers. The Go
login and cursor key stays `SUPERVERSE_COOKIE_SIGNING_KEY` and is not supplied to Studio.
Replacing the browser key invalidates existing CSRF tokens and OAuth route cookies; refresh
the configuration page or restart OAuth authorization. Request cancellation propagates
through gRPC to handlers and provider calls. Switching environment or Deployment cancels
old requests and closes the Start form. Rejected or stale targets require an
explicit reload; they never retarget silently.

OAuth authorizes TeamSpace connections only. `POST /api/connector-oauth/start` takes the
StartTeamSpaceConnectionOAuth request plus `returnPath`, a path of Studio's own origin, and
the stable `GET /api/connector-oauth/callback` completes it; neither RPC is reachable through
`/api/rpc`. Go and Studio use the same operator-configured origin (`studio.publicOrigin` of
the server configuration, `SUPERVERSE_STUDIO_PUBLIC_ORIGIN` for Studio); Native and Kind use
`http://localhost:23000`, and the redirect URI is that origin plus the callback path.
Browser Origin or forwarded headers cannot choose the provider callback. Go checks the
authorization URL's scheme by profile; the BFF checks only that it is an absolute http or
https URL without userinfo or fragment that carries one state. A version-7 signed,
host-only, HttpOnly, SameSite=Lax route cookie binds the actor and login hash, state digest,
origin, the TeamSpace connection, the return path and an expiry of at most ten minutes. It
is Secure on HTTPS and restricted to the callback path; an older cookie requires restarting
authorization. CompleteTeamSpaceConnectionOAuth freshly authenticates and completes only
the caller's own attempt, which its state names and which binds the redirect URI. Success
and every definitive failure clear the cookie and return to the page with
`oauth=success&connection=<id>`, or `oauth=failed` and the refusal's reason; a transient
failure keeps it, so a reload retries. The callback is classified as a mutation despite its
browser GET. Stored token answers reconcile response loss and fence credential
publication under the connection's lease ([connections](../../internal/dexai/tenancy/connections/README.md)).
No platform OAuth Flow, mirrored state or new credential store is added.

Actual-dependency acceptance must cover the real Studio, BFF and gRPC path, real scoped
storage, Dex and Worker, OAuth and provider boundaries and all three full Kind cases. Tests
for the removed scope cache are historical and do not validate this boundary. Tracing and
screenshots must not record key entry or OAuth credential inputs. Static compilation and
ready Pods are not acceptance.

## Dex Flow Changes

All eight independent Flow definitions share one registry, Worker, Client and BlobCache.
Project owns child cleanup admission, the repository binding reference, scoped resource and
configuration references and the current Live and Preview pointers. Release and Deployment
own their operation results. The Session owns its conversation, tool scheduling, confirmed
coding HEAD, workspace requests and accepted workspace lease; its SessionWorkspaceFlow owns
its instances and the responsibility for every Sandbox resource they leave, and one
provisioning execution provisions exactly one workspace instance. See the full
per-Flow primitive inventory in `docs/architecture/superverse-v2.md` and the generated FDG
2.0 in `docs/flows/`.

## Database Schema Changes

The `accounts` and `login_sessions` tables hold the accounts ([accounts](../architecture/teamspace/accounts.md)).
PostgreSQL also holds the synced read-model tables, the TeamSpace and membership tables and
the connection tables ([read model](../architecture/teamspace/read-model.md),
[TeamSpaces and roles](../architecture/teamspace/teamspaces-and-roles.md),
[connections](../architecture/teamspace/connections.md)). There is no Redis or outbox.
Versioned object storage holds immutable artifacts and configurations and encrypted
credential material; Flow references stay authoritative.

## UI/UX

Studio uses the resource Get and Watch APIs for progress and corrects temporary deltas from
snapshots. Preview and Live keep separate configuration and credential scopes.
Configuration requests belong to the exact Project, scope, Preview Session or Live Release
(including an empty pre-Release ID). Loading or selecting source candidates does not
remount that same target or cancel its accepted validation action. Changing the actual
target cancels the old request; snapshots from another target are not shown while the new
read is pending. Stopping Live preserves Project data; complete Project deletion removes
it. Stop Session and Stop Live fence on what the page observed; see [Sessions](#sessions)
and [Live pointer and RemoveLive](#live-pointer-and-removelive).

## Tests

Retained tests call actual dependencies. The Flow API requires real authentication, exact
serving and configuration targets, stale definition and instance rejection, provider
response recovery and logout through the merged Go server. The removed BFF scope-cache
scenario is historical, not coverage of this boundary. Session scenarios exercise snapshot
and reconnect behavior and real paginated lists. The Local Kind create-project case also
covers the `/api/rpc` negative cases (an unknown field, a body caller, `Login`,
`GetFlowCatalog` and a lower-case method returning 404, a wrong verb returning 405, a
malformed uint64 and an undeclared or zero status returning 400), and template approval
reads Diagnostics for the approved run. Their prerequisites and executed scope are recorded
in implementation receipts; unexecuted scenarios and compilation do not establish runtime
coverage.

The Session workspace contract requires real-dependency coverage through the merged BFF
and gRPC path: a new Session reports no `workspace` and a `NOT_STARTED` Preview until its
first demand; StartPreview on a Session without a running instance is accepted and reads
report `STARTING` with a constant generation, a non-decreasing revision and no failure
until `READY`; a Preview-scope configuration read without a `READY` instance returns
`FAILED_PRECONDITION` and succeeds once the instance is `READY`; a reclamation at the
Sandbox deadline yields a `CLOSED` workspace while the Session stays `ACTIVE`, and the next
demand provisions a new instance on the same volume; Project deletion completes with a
never-requested, provisioning, `READY`, reclaimed or failed instance. The Dex integration
scenarios of the [Session Service](../../internal/dexai/agents/README.md#tests) cover the Flow
side; the BFF scenarios are required, not yet claimed.

Mock and unit tests are not acceptance. The Go services enforce the fresh resource, UID,
revision and scope rules described above; BFF checks alone grant no authority. All three
full Local Kind journeys remain required before test publication, followed by complete
hosted AWS acceptance.

## Documentation

the current contract records the
service split, message model, enum rule, Flow API term and transports. The
[TeamSpace design](../architecture/teamspace/README.md) owns the design behind the TeamSpace,
membership and connection services, and [the credential read endpoint](credential-read.md)
owns that endpoint's contract. Template `.github/`
automation is intentionally excluded. Agent runtime behavior is kept through the unified
package and Studio, while obsolete standalone entry points, repository metadata, unused MCP
and duplicate UI shells are removed. Implementation receipts record identities, commands,
results and gaps.
