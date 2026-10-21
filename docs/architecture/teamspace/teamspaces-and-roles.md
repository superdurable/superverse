# TeamSpaces and roles

A TeamSpace holds a team's Projects and people. People have one role in the TeamSpace
and, separately, one role in each Project they work on. This document is the contract
of `TeamSpaceFlow`, of the member changes to `ProjectFlow`, and of authorization.

## Hierarchy and roles

TeamSpace → Project → process (Flow type). Roles nest within a Project as the [Studio contract](../studio-setup/studio.md) defines them: each role holds
everything the roles below it hold, with no extra grants.

| Who | May |
|---|---|
| TeamSpace admin | Add and remove members and make them admin or member; manage shared connections; rename the TeamSpace; delete the TeamSpace's Projects. No other Project rights. |
| TeamSpace member | See every Project of the TeamSpace with their own role in it (or "not joined"); create Projects, becoming their owner; manage shared connections when they own or build at least one Project of the TeamSpace. A Project in which they hold no role appears as a reduced entry without its repository and manifest, and GetProject denies them those details. |
| Project owner | Everything in the Project, plus its members and roles, and deleting the Project. |
| Project builder | The agent, Flow, Interface, Preview, publishing and rollback, Runs, Inbox, Stop and Time travel, and the Project's configuration: Step settings, triggers, environment and connection selection. |
| Project operator | Live only: Runs, Inbox, starting runs by hand and handling any Inbox item; Flow and Interface read-only. No Preview, no Stop or Time travel. |
| Platform admin (configured platform administrator accounts (`accounts.platformAdminEmails`), ACTIVE) | Everything in every TeamSpace and Project, including creating Projects in a TeamSpace they are not a member of; the only role that deletes a TeamSpace. |

Rules:

- **Any ACTIVE account creates a TeamSpace** and becomes its admin.
- **Project roles need TeamSpace membership.** A Project member must be a member of
  the Project's TeamSpace, and authorization requires both, so removing someone from
  the TeamSpace ends their access to all its Projects at once, without touching any
  Project.
- **At least one admin and one owner.** A TeamSpace keeps at least one admin and a
  Project at least one owner; each owning Flow checks this atomically in the write
  RPC.
- **Removing a TeamSpace member who is the only owner of a Project** is rejected by
  the API, which asks for ownership to be transferred first.
- **Members and roles of a Project** are changed only by its owner (or the platform
  admin).
- **A re-added member regains their earlier Project roles.** Removing a TeamSpace member
  never touches a Project, so the Project rows still hold their roles, and authorization
  accepts them again as soon as the TeamSpace membership exists. There is no cross-Flow
  cleanup. Studio tells the admin how many Projects this affects when the person is added
  ([api-and-studio.md](api-and-studio.md#studio)).
- **Managing shared connections.** A TeamSpace admin, or the owner or builder of any
  Project of the TeamSpace, creates, edits and deletes connections and changes their
  settings. A credential change (reauthorizing, also with another account, or replacing an
  API key) to a connection that has a Live usage needs a TeamSpace admin, or builder rights
  (owner or builder) in every Project that has a Live usage, because it reaches every Live
  version at its next read ([when changes apply](connections.md#projects-use-connections)).
  One `NOT EXISTS` over the connection's usages joined to `project_members` decides it. A usage whose Project the
  read model does not hold yet counts as a Project the caller does not build; only DELETED
  Projects are excluded.
  Settings changes, and connections only Preview uses, need only a TeamSpace admin or the
  owner or builder of any Project.
- Participants and guests follow once the Inbox can assign items to people.

## Authorization

Login reads `login_sessions` joined with `accounts` (see [accounts.md](accounts.md)) and
yields `Caller{AccountID, CanonicalEmail, PlatformAdmin}`.

`ProjectFlowClient.Authorize(ctx, accountID, projectID, capability)` computes the
effective role with one query:

```sql
SELECT p.project_status, p.team_space_id, pm.role, tm.role AS team_space_role, a.platform_admin
FROM projects p
LEFT JOIN project_members pm ON pm.project_id = p.project_id AND pm.account_id = $2
LEFT JOIN team_space_members tm ON tm.team_space_id = p.team_space_id AND tm.account_id = $2
CROSS JOIN LATERAL (SELECT canonical_email = $3 AND status = 'ACTIVE' AS platform_admin
                    FROM accounts WHERE account_id = $2) a
WHERE p.project_id = $1
```

- A Project role counts only while the TeamSpace role exists; the platform admin counts
  as owner; a `DELETED` Project is forbidden to everyone.
- Capabilities are one closed set of typed constants, each declared with its least role
  and whether it is Live-only:

| Capability | Least role | Live only |
|---|---|---|
| ViewProject: reading the Project, including GetRelease (the Live definitions) and ListProjectMembers | operator | — |
| OperateLive: Flow Run reads, starting runs and Inbox actions, Flow diagnostics reads | operator | yes |
| Build: everything else of the Project, including Stop, Time travel and Preview | builder | — |
| ManageMembers | owner | — |
| DeleteProject | owner, or an admin of the Project's TeamSpace | — |

ListTeamSpaceMembers names no Project, so it needs TeamSpace membership, which every Project
role requires: operators read the TeamSpace's members too.

Checks happen in the API handlers, in the Flow API (`internal/dexai/studio/runs`, which also
applies "operator only on Live" by scope), in the configuration routes, in the Session
service, and where a Step re-checks its actor (the release phases and the publishing
service). All of them read SQL. Studio's `caller_role` only chooses what to show.

## TeamSpaceFlow

Package `internal/dexai/tenancy/teamspaces`, file `team_space_flow.go`.

### Identity and lifecycle

- **FlowID** `teamspace-<teamSpaceID>`, for example `teamspace-k3v9q2ma`.
  `flowid.TeamSpaceCreationID(accountID, requestID)` derives the eight-character ID in
  the domain `superverse-v2-teamspace-creation-v1`.
- **Start**: `TeamSpaceService.CreateTeamSpace`, with RequestID
  `Fingerprint{AccountID, RequestID, Name}`, `IDReuseDisallow` and `IgnoreError` on the
  same request, and the Attribute Store `team_spaces`. A replay of the request joins the
  execution; the same request ID with another name is `ALREADY_EXISTS`. The FlowID is
  never reused, so no admission Step is needed.
- **Response**: the ID only; Studio retries a NotFound briefly while the row appears.
- **Status**: READY → DELETING → DELETED; a TeamSpace starts READY. Business completion is
  DELETED: every Project and connection is deleted. The members stay with the closed Flow,
  and member listings exclude DELETED TeamSpaces. The Flow then completes gracefully and
  stays readable for its retention; the `team_spaces` row stays and queries exclude
  DELETED.
- **Relationships**: an independent top-level Flow. Projects register here; connections
  are found in the database by `team_space_id`. Deleting the TeamSpace deletes both
  through the `ProjectCleanup` and `ConnectionCleanup` ports, which the bootstrap
  injects. `ConnectionCleanup` is `DeleteTeamSpaceConnections(ctx, teamSpaceID) (done bool,
  err error)`, declared in `internal/dexai/tenancy/teamspaces` and
  `internal/dexai/tenancy/connections`; each call is bounded and idempotent, and is never
  refused because of usages.

### Attributes and AttributeMaps

| Name | Type | Index | Synced |
|---|---|---|---|
| StartIdentity | `dexflow.StartIdentity` | — | no |
| Name | `string`, 1–80 bytes | — | yes |
| TeamSpaceStatus | `string` (the `Status` constants) | — | yes |
| CreatorAccountID | `string`, a fact only | — | yes |
| Revision | `uint64`; every write locks it | — | yes |
| ActiveCommand, LastCommand | `*Command` | — | no |
| LastFailure | `*dexflow.Failure` | — | yes |
| CreatedAt, UpdatedAt | `time.Time` | — | yes |
| AttachmentStagingTotals | `StagingTotals{Count, Bytes}` of the staged attachments | — | no |

| Map | Instance key (example) | Value | Loading, locking, limits, cleanup |
|---|---|---|---|
| `Members` | AccountID (`Members/0f9c…`) | `{Role ADMIN\|MEMBER, AddedAt, AddedByAccountID}` | The creator, as ADMIN, is the initial instance, so sync writes their row. Member writes load it whole; `RegisterProject` loads only the caller's instance. Writes lock `Revision`, which guards "at least one admin". At most 500. Removed with the member; kept when the TeamSpace is deleted. Synced as the child table `team_space_members` (instance key column `account_id`). |
| `Projects` | ProjectID (`Projects/ab12`) | `{ProjectFlowID, RegisteredAt, RegisteredByAccountID}` | Exact loads; loaded whole only by the deletion. Unregistered when the Project is deleted. Not synced: `projects.team_space_id` already lists a TeamSpace's Projects. |
| `AttachmentStaging` | Attachment ID (`AttachmentStaging/a7f2c9xq4m1p`) | `StagedAttachment{FlowID, Bytes, UploaderAccountID, ReservedAt, HeldUntil}` | Exact loads, and the whole map only for `ListLapsedAttachmentStaging`. At most `attachments.teamSpaceStagedFiles` (200). Deleted at the attachment's release, or by the reclaim of a lapsed hold. Not synced ([attachments](../attachments/upload-and-preparation.md#teamspaceflow-changes)). |

No secrets: IDs, roles and names only.

### Channels and Streams

- `TeamSpaceCommands`, payload `Command{ID, Kind DELETE_TEAM_SPACE, ActorAccountID, Blocker}` (`Blocker` names the Project that keeps a deletion from completing).
  Produced by `RequestTeamSpaceDeletion` (platform admin only), accepted only while no
  command is active; the same ID returns the current state and another ID while one is
  active is `FAILED_PRECONDITION`. Consumed by `AwaitTeamSpaceCommands`. No expiry; no
  pending command at termination.
- ChannelMaps: none. Streams: none.

### Steps

| Step | Purpose | Options and routing |
|---|---|---|
| `AwaitTeamSpaceCommands` (start) | Waits for the deletion command. | → `DeleteTeamSpaceResources` |
| `DeleteTeamSpaceResources` | Asks every registered Project to delete itself through `ProjectCleanup` (idempotent; a DELETED Project counts as done), then `dexflow.PollUntil(ctx, 5s, 40s, check)` reads up to eight Project statuses per round. Once every Project is deleted (and with it its connection usages), each round calls `ConnectionCleanup.DeleteTeamSpaceConnections` (database rows and secret objects, idempotent) until it reports done, then deletes every version under `attachment-staging/<teamSpaceID>/` until clean. | `PollingStepOptions(6h)`, failure → `RecordTeamSpaceFailure`; no lock while polling |
| `CompleteTeamSpaceDeletion` | Locks `Revision`, sets DELETED and completes gracefully; the members stay. | Sync durability |
| `RecordTeamSpaceFailure` | Records the truncated `ctx.RecoveryError().Detail` and clears `ActiveCommand`, so deletion can be requested again. | → `AwaitTeamSpaceCommands` |

### RPCs

Writes use `dexflow.ShortRPC(dex.LockAttribute(Revision))`. The API passes the actor and
whether it is the platform admin; the Flow checks the actor's TeamSpace role in its own
`Members`.

| RPC | Kind | Checks | Effect |
|---|---|---|---|
| GetDexSummary, GetDexDisplay | read | — | Dex Web |
| AddTeamSpaceMember, SetTeamSpaceMemberRole, RemoveTeamSpaceMember | write | actor is admin or platform admin; READY; at least one admin remains; member limit | Returns the members |
| RenameTeamSpace | write | admin; expected revision | Renames |
| RequestTeamSpaceDeletion | write | platform admin only | Accepts the deletion command |
| RegisterProject | write | READY; the actor is a member or the platform admin | Adds to `Projects` |
| UnregisterProject | write | READY or DELETING; no actor check, because only ProjectFlow's `CompleteDeletion` calls it and the API does not expose it; an unknown instance succeeds | Removes from `Projects`, so a TeamSpace's own cascade can unregister its Projects |
| ReserveAttachmentStaging | write | A reserved ID succeeds in any status; otherwise READY, the actor is a member or the platform admin, within the staging files and bytes | Adds to `AttachmentStaging` and the totals; the Revision stays |
| ReleaseAttachmentStaging | write | Any status; an unknown instance succeeds | Removes from `AttachmentStaging` and lowers the totals |
| ListLapsedAttachmentStaging | read, the whole `AttachmentStaging` map | — | Lists at most ten attachment IDs whose `HeldUntil` passed, oldest first |

Idempotence: adding a member who already has that role, removing an absent member and
registering a registered Project all succeed without change. Deterministic rejections
return `dexflow.Rejection`; unexpected errors use `dex.ErrorWithStack`. The deletion
command ID is the only external-effect identity; member rows are written by sync.

### Compatibility and tests

A new Flow with no open executions. Integration tests (real Dex and PostgreSQL) cover
identity and replays, concurrent member changes that always keep an admin, the member
child table following the Flow, `RegisterProject` refusing non-members and a deleting
TeamSpace but accepting the platform admin, an uncertain registration ending in a DELETED
Project, reduced list entries for a member without a role, a re-added member regaining
their Project roles, exact loads, the deletion command race, the cascade through Projects
and connections, a Worker restart during deletion, and DELETED rows remaining.

## ProjectFlow changes

- **Identity**: the FlowID is unchanged. `CreateProject` requires `team_space_id`, which
  joins the start fingerprint. The Attribute Store is `projects`.
- **Attributes**: new `TeamSpaceID` (immutable). `CreatorAccountID` (SYNC renames it from
  `OwnerAccountID`, keeping its index) becomes a fact only and is no longer indexed, which
  frees keyword1.
- **AttributeMap `Members`**: instance key AccountID (`Members/0f9c…`), value
  `{Role OWNER|BUILDER|OPERATOR, AddedAt, AddedByAccountID}`. The creator starts as
  OWNER; at least one owner remains; at most 200 members; writes lock `Revision`. They
  stay when the Project is deleted, so its members can still read the final DELETED
  snapshot; queries exclude DELETED Projects. Synced as the child table `project_members`
  (instance key column `account_id`).
- **Steps**:
  - the new start Step `RegisterProjectInTeamSpace` calls `RegisterProject` with the
    creator and whether the creator is the platform admin. A rejection routes to
    `RecordTeamSpaceAdmissionRejection`, which sets DELETED with the reason and completes;
    nothing has been created yet. An uncertain outcome (the call failed after its retries)
    takes the Step's Execute failure route into the same Step, which sets DELETED with
    `LastFailure` `TEAM_SPACE_REGISTRATION_FAILED`: no resource exists yet, a registration
    that did succeed is harmless because `ProjectCleanup` treats DELETED as done, and the
    user creates the Project again with a new request. On success the existing
    `StartProjectSetup` follows;
  - deletion gains `ReleaseConnectionUsages`, which deletes the Project's connection
    usages and scope tokens through a port (idempotent, under the Step's retry policy);
  - `CompleteDeletion` calls `UnregisterProject` before setting DELETED.
- **RPCs**: new AddProjectMember, SetProjectMemberRole and RemoveProjectMember; the
  actor must be an owner or the platform admin.
- **Unchanged**: Channels, Streams, `ChildFlows` and `StartProjectSession`.
- **Compatibility**: the start Step and Attribute keys change; existing executions are
  reset explicitly (pre-launch).

Registration goes through `TeamSpaceFlow` rather than SQL because a Project created a
moment ago may not be synced yet, and a deletion must not miss it. It is the same
pattern as `ChildFlows` for a Project's Sessions, Releases and Deployments.

## Members in SQL

Both `Members` maps reach SQL as child-table rows (see [read-model.md](read-model.md)):
adding, changing or removing a member writes only the map instance, and Dex upserts or
deletes the row. Writes do not wait for sync: the RPC returns the new member list and
Studio shows it directly.

A removed member keeps access for the sync delay, normally seconds. Only a PostgreSQL
outage longer than the 168-hour retry budget drops the deleting batch; recovering from
such an outage includes checking the member tables against the Flows by hand
([infrastructure.md](infrastructure.md)).
