# Read model

Every read from outside a Flow goes to PostgreSQL. The Flows keep owning their facts;
the Dex Attribute Store writes those facts into tables keyed by business IDs, and the
API reads the tables with joins and group by. Reads inside a Flow (its Steps and RPCs)
read their own Attributes and are not affected.

## Why

- Listing a member's Projects, authorizing a request and finding a Live Runs target
  join facts of several owners. Dex search plus one strong read per item costs a Flow
  read per result and per request.
- Studio polls: Watches reread every 5 seconds, and every request authorizes.
- The cost is eventual consistency of reads and a database the platform Dex depends on;
  both are handled below and in [infrastructure.md](infrastructure.md).

## How the Flows reach the tables

- A Flow marks the Attributes and AttributeMaps it syncs with
  `dex.SyncToAttributeStore()`. Nothing else is shipped, so a Flow can sync a few small
  fields of a large state.
- Each Flow selects its store with `AttributeStoreNames` where it is started.
- The store's row key template splits the FlowID into primary key columns, and column
  names are the snake_case of Attribute names (XDEXSTORE). Business IDs come from the
  primary key and are not synced again.
- An AttributeMap syncs into a child table, one row per instance, whose primary key is
  the parent's key columns plus an instance key column (XDEXROWS).
- An upsert writes only the columns of its batch (`ON CONFLICT … DO UPDATE SET` lists
  exactly those), so two Flows can write different columns of one row.

| Flow | Table and row key | Synced Attributes | Maps → child tables (instance key column) |
|---|---|---|---|
| TeamSpaceFlow | `team_spaces`: `teamspace-{team_space_id}` | Name, TeamSpaceStatus, CreatorAccountID, Revision, LastFailure, CreatedAt, UpdatedAt | `Members` → `team_space_members` (`account_id`) |
| ProjectFlow | `projects`: `project-{project_id}` | TeamSpaceID, CreatorAccountID, Name, ProjectStatus, Revision, TemplateRevision, SetupFlowID, RepositoryBinding, AppManifest, ActiveDeployment, CurrentLive, ActiveCommand, LastFailure, SessionStartFailure, ProjectStorage, LiveExecution, CreatedAt, UpdatedAt | `Members` → `project_members` (`account_id`) |
| ProjectSetupFlow | also `projects`: `setup-{project_id}-{*}` | SetupStatus, SetupPhase, SetupSourceCommit, SetupFailure, SetupRevision | — |
| ProjectReleaseFlow | `releases`: `release-{project_id}-{release_id}` | ReleaseStatus, Revision, SourceCommit, BuildProfileVersion, PreparedSource, ActiveBuildRevisionID, ReadyBuildRevisionID, Failure, CreatedAt, UpdatedAt | `ReleaseBuildRevisions` → `release_build_revisions` (`build_revision_id`) |
| ProjectDeployFlow | `deployments`: `deploy-{project_id}-{deployment_id}` | ReleaseID, BuildRevisionID, DeploymentStatus, ConfigurationRevision, ProjectDeploymentLease, DeploymentURL, PinnedInputs, WorkloadGrant, ReadinessResult, DeploymentResources, ProviderOperationRefs, PhaseTimings, Failure, Revision, CreatedAt, UpdatedAt | — |

TeamSpace connections are not here: they are database-authoritative and need no sync
([connections.md](connections.md)).

### Setup in `projects`

- A Project has exactly one Setup: `SetupFlowID` is written when the Project is created
  and never changes, so no late write of an older Setup can overwrite a newer one.
- ProjectFlow and ProjectSetupFlow write different columns of the same row.
- Setup Attribute keys that collide with the Project's are named `SetupRevision`,
  `SetupSourceCommit` and `SetupFailure`; Setup's timestamps are not synced; the Setup ID
  comes from `setup_flow_id`.

### Rules for synced tables

- **Only Dex writes them**; the database grants enforce it.
- **No foreign keys, no CHECK constraints, NOT NULL only on primary key columns.** A
  violated constraint fails the whole batch and blocks that Flow's sync queue until the
  retry budget drops the batch. The Flows guarantee values with typed constants, and
  tests compare the tables with the Flows.
- **Rows are never deleted by Dex**; a DELETED row stays and queries exclude it.
- **Column sets match the synced names exactly**, which a test checks, because Dex
  silently skips a value without a column or with the wrong type.

## Flows that are not projected

A Flow is projected when a list, join, group by or cross-entity read from outside needs
it. The rest stay on their Flows; these are their reads from outside a Flow:

| Flow or map | Reads from outside | Source and frequency | Why it stays |
|---|---|---|---|
| SessionAgentFlow | GetSession, WatchSession, ListArchivedMessages; admission before Agent commands and Preview RPCs; Runs Preview targets; Preview configuration targets | Browser. A Watch rereads the whole snapshot every 5 s per open builder page and reauthorizes on every progress message; Runs on Preview resolve their target per request | The conversation exists only in the Flow and must align with the Streams; command admission reads engine liveness; every read is one entity by FlowID |
| SessionWorkspaceFlow | Inside the Session reads above | Browser, same frequency | Its instance identity fences Preview writes; one entity per Project |
| PreviewFlow | Same, plus GetPreviewDefinitions, the configuration target and the read before Stop | Browser, same frequency; GetPreview has no Studio caller | Same as SessionWorkspaceFlow |
| ConnectorContributionFlow | One read per contribution in each Session snapshot | Browser, Watch frequency | Read by (Project, slug); the list comes from the Session |
| SubagentFlow | WatchSession relays running subagents' activity Streams | Browser, per Stream message | Streams are not Attributes; the subagent list comes from the Session |
| SandboxProvisionFlow, BackgroundCommandFlow, RepositoryBindingFlow, PlatformIssueFlow | None: only Flow Steps read them, `BackgroundCommandFlowClient` has only `Stop`, and `PlatformIssueFlowClient.Get` has no production caller | — | Nothing to serve; a repository claim must be atomic in its Flow |
| ProjectFlow `ChildFlows` | None: `ReadChild` is called only from Flow Steps and dependencies | — | Admission trusts only an exact read of the map; Release and Deployment lists come from their own tables |
| TeamSpaceFlow `Projects` | None: only the TeamSpace deletion reads it | — | `projects.team_space_id` already answers "which Projects", and a second copy would have two owners |

- **The trigger for projecting the Session** is the first cross-Project read of it,
  the current contract's "Needs you" list and dots. The Session would then sync a few small
  fields (status, workspace state, whether a question or approval is pending, update
  time), never the conversation; PreviewFlow would sync its status if shown.
- **Live Runs targets read SQL and Preview targets do not** for the same rule: the
  Project, Deployment and Release tables exist for lists and joins, while a Preview
  target is one entity by FlowID. Both read the Flow before a write.
- A Live Runs target is read from `deployments.pinned_inputs` and `workload_grant`. The
  read checks only what selects the target: `projects.current_live` names the deployment,
  the deployment is serving, and its release and build IDs agree. It does not compare the
  build's frozen configuration with the pinned inputs again: ProjectDeployFlow validated
  them when it pinned them, and an eventually consistent copy could not prove more.
- Until TEAMSPACE adds Project members, the creator is a Project's only owner, so
  Project authorization reads `projects.creator_account_id` as the owner.

## Reads that still go to a Flow

Only reads from outside a Flow are listed.

| Read | Source and frequency | Why not SQL |
|---|---|---|
| The Session conversation: GetSession, WatchSession snapshots, ListArchivedMessages | Browser: every 5 s per open builder page; on open, focus and return; while paging back | A Watch attaches every Stream reader at its tail before it reads the snapshot, so later progress cannot be missed. A lagging snapshot would miss changes committed between the row and the tail until the next reread. The conversation also exists only in the Flow. |
| Admission before a write: the seven Agent commands, Preview start, stop and source preparation, RenewWorkspace, configuration saves (a read-only configuration validation authorizes in SQL), and Runs writes on Preview or Live (start, actions, Stop, Time travel) | Browser, per user action | An engine-stopped Session writes no Attribute, so only the engine read shows it. Preview writes must hit the instance the Session holds; Live writes must hit the current Live deployment, which SQL shows a few seconds late after a redeploy or removal. |
| Runs Preview target resolution for reads: which Sandbox to reach and whether it is ready, from the Session, workspace and Preview Flows | Browser: at least 3 requests per 5 s for an open running run; one per Flow type per 60 s for waiting runs | One entity by FlowID, no list or join; it reads only those three summaries, never the conversation |
| The Preview configuration target (`ReadPreviewTarget`) and GetPreviewDefinitions | Browser: opening or saving configuration; a new Preview version | Rare, one entity by FlowID |
| A failed build's log tail (`ReadBuildSnapshot`) | Browser, when a failed build's log is opened | A diagnostic read of one entity by FlowID; it needs the build's repository, fingerprint, operation attempt and build profile digest, which are not synced |
| Watch progress: the Dex Streams of Sessions, subagents, Setups, Releases and Deployments | Browser, per Stream message | Streams are not state; a reconnect recovers from the snapshot |
| The remote agent dialog's current executor: `ReadExecutor` behind `CreateRemoteAgentPairing` (its basis epoch), `RevokeRemoteAgentConnection` (refusing the current executor) and `ListRemoteAgentConnections` (marking the current one) | Studio, once per dialog open or action; no polling | Who the executor is and its epoch exist only in the Session, which is their sole authority; credentials and authentication stay in SQL, and no executor copy is kept in a table ([remote agent connection](../remote-agent/connection.md#activation-and-takeover)) |
| A connected remote agent's `sync`: the Session's entries after its cursor and its Stop state (`ReadRemote`), woken by the `AgentActivity` Stream | The Bridge: one cycle per change or per 30 s, at most one connection per Project | The entries and the Stop state exist only in the Session, ordered by commit and fenced by the executor epoch ([remote agent connection](../remote-agent/connection.md#flow-reads-from-outside-the-flow)) |

A Watch reauthorizes on every progress message; its Project part reads SQL, and the
Session liveness part reads the Flow. The credential read endpoint reads the database
and object storage, never a Flow.

## Consistency

- Watches reread every 5 seconds.
- A write returns the owning Flow's state, and Studio shows it, so the writer never sees
  its own write as stale.
- A write rejected for a stale fence is followed by a reread.
- A resource created a moment ago: its Watch waits up to 10 seconds for the row, and
  Studio retries a NotFound briefly after its own create.
- Responses carry `revision`.

## When sync goes wrong

There is no automatic or periodic repair: no reconciliation loop, resync command or
refresh RPC. Sync goes wrong in two ways only:

- PostgreSQL cannot be written for longer than the 168-hour retry budget and a batch is
  dropped. That is an incident.
- A column name or type does not match its Attribute and the value is skipped. That is a
  bug, which the column-set test catches.

A slow or locked database does not lose writes, but it slows the Flows: each sync batch
first runs within the Flow's execution and falls back to the background only after about 7
seconds, so every syncing Flow's RPCs wait that long while the database stalls
([infrastructure](infrastructure.md#postgresql), superdurable/dex).

Either is fixed by hand ([runbook](../../runbooks/read-model-sync-incidents.md)):
`dexcli flow state` shows the Flow's real values, the row is corrected, and the bug is
fixed. Before service resumes after a long PostgreSQL outage,
the member tables are checked, because a dropped batch may have held a member's
removal. A later migration that adds a synced column backfills old rows itself if they
need a value; before launch every environment is reset instead.

## SQL access

SQL lives only in DAO packages: queries, row structs and transactions, no business
rules. Methods are named for their use, such as `AuthorizeProject`,
`ListProjectsForMember` or `ListReleases`. A DAO returns a JSON column as raw JSON, and
the domain package that owns the value's type decodes it, so no second definition can
fall behind the Flow's type. SQL that reads inside a JSON column, such as the deployment
ID in `current_live`, names the owner type's Go fields (PascalCase, since those types
have no JSON tags); such paths stay few, and each has a DAO integration test whose data
is a value the Flow writes. Not-found is a sentinel and other errors wrap the
operation. The pool and
transaction helpers and the migrations live in `internal/platform/postgres`; DAOs import
only it and `internal/comm`.

| Package | Tables and queries |
|---|---|
| `internal/tenancy/accountdao` | `accounts`, `login_sessions` |
| `internal/tenancy/memberdao` | `team_space_members`, `project_members` (read-only): member lists, "the only owner of a Project", "an owner or builder of any Project" |
| `internal/tenancy/teamspacedao` | `team_spaces`: a member's TeamSpaces, the overview |
| `internal/tenancy/connectiondao` | The three connection tables ([connections.md](connections.md)) |
| `internal/projects/projectdao` | `projects` with its Setup columns: a member's Projects, details, the authorization join |
| `internal/delivery/deliverydao` | `releases`, `release_build_revisions`, `deployments`: lists, details, the Live Runs target join |

## Synced tables

```sql
-- Dex-written: primary keys from row key templates (plus the instance key for child
-- tables); no foreign keys, no CHECK constraints, NOT NULL only on primary key columns.
CREATE TABLE projects (project_id TEXT PRIMARY KEY,
  team_space_id TEXT, creator_account_id TEXT, name TEXT, project_status TEXT, revision BIGINT,
  template_revision TEXT, setup_flow_id TEXT, repository_binding JSONB,
  app_manifest JSONB, active_deployment JSONB, current_live JSONB, active_command JSONB,
  last_failure JSONB, session_start_failure JSONB, project_storage JSONB, live_execution JSONB,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ,
  -- written by ProjectSetupFlow (row key setup-{project_id}-{*})
  setup_status TEXT, setup_phase TEXT, setup_source_commit TEXT, setup_failure JSONB, setup_revision BIGINT);
CREATE INDEX projects_team_space ON projects (team_space_id, project_status, created_at DESC);
CREATE TABLE releases (project_id TEXT, release_id TEXT,
  release_status TEXT, revision BIGINT, source_commit TEXT, build_profile_version TEXT,
  prepared_source JSONB, active_build_revision_id TEXT, ready_build_revision_id TEXT,
  failure JSONB, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, release_id));
CREATE INDEX releases_recent ON releases (project_id, created_at DESC);
CREATE TABLE release_build_revisions (project_id TEXT, release_id TEXT, build_revision_id TEXT,
  actor_account_id TEXT, configuration_revision BIGINT, build_profile_version TEXT, status TEXT,
  revision BIGINT, image_reference TEXT, image_digest TEXT, provenance JSONB, artifacts JSONB,
  failure JSONB, auto_deploy BOOLEAN, auto_deployment_id TEXT, auto_deploy_status TEXT,
  auto_deploy_failure JSONB, timings JSONB, provider_settled BOOLEAN, configuration JSONB,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, release_id, build_revision_id));
CREATE TABLE deployments (project_id TEXT, deployment_id TEXT,
  release_id TEXT, build_revision_id TEXT, deployment_status TEXT, configuration_revision BIGINT,
  project_deployment_lease JSONB, deployment_url TEXT, pinned_inputs JSONB, workload_grant JSONB,
  readiness_result JSONB, deployment_resources JSONB, provider_operation_refs JSONB, phase_timings JSONB,
  failure JSONB, revision BIGINT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, deployment_id));
CREATE INDEX deployments_recent ON deployments (project_id, created_at DESC);
CREATE TABLE team_spaces (team_space_id TEXT PRIMARY KEY,
  name TEXT, team_space_status TEXT, creator_account_id TEXT, revision BIGINT,
  last_failure JSONB, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ);
CREATE TABLE team_space_members (team_space_id TEXT, account_id TEXT,
  role TEXT, added_at TIMESTAMPTZ, added_by_account_id TEXT,
  PRIMARY KEY (team_space_id, account_id));
CREATE INDEX team_space_members_account ON team_space_members (account_id);
CREATE TABLE project_members (project_id TEXT, account_id TEXT,
  role TEXT, added_at TIMESTAMPTZ, added_by_account_id TEXT,
  PRIMARY KEY (project_id, account_id));
CREATE INDEX project_members_account ON project_members (account_id);
```

## Dex Attribute Store changes

Both are server changes in superdurable/dex, optional per store, shipped in one server
release; without the new keys the store behaves as today. They support PostgreSQL and
MySQL, and a warehouse or MongoDB store configured with them fails at startup. The
Attribute Store documentation and the skills' `data-handling.md` describe them.

**Row keys and column naming (XDEXSTORE)**

- `rowKey` splits the FlowID into primary key columns, for example
  `release-{project_id}-{release_id}`. Each placeholder matches up to the next literal
  and the last takes the rest; `{*}` matches without a column, so
  `setup-{project_id}-{*}` writes a Setup into its Project's row.
- Startup checks that the table's primary key is exactly those columns, all strings.
  Several stores may target one table when their templates yield the same key columns;
  each writes only its batch's columns.
- A FlowID that does not match its template skips the batch with an error log; an
  Attribute that maps to a key column is skipped, as today.
- `columnNaming: snake_case` maps `DeploymentURL` to `deployment_url`; two Attributes
  that map to one column fail startup.

**AttributeMaps as child tables (XDEXROWS)**

- A store entry declares `attributeMaps: {Members: {tableName: team_space_members,
  instanceKeyColumn: account_id}}`.
- The child table's primary key is the parent's key columns plus the instance key
  column, all strings; the instance key column holds the unescaped key.
- A JSON-object value is flattened: each top-level field goes to the column of the same
  name under the store's naming, fields without a column are skipped and logged, a field
  that maps to a key column is skipped, and nested values go to json/jsonb columns. A
  value that is not an object goes to a column named `value`.
- An instance write upserts its row with only that value's columns; an instance delete
  deletes its row. One batch applies the parent upsert and all child upserts and deletes
  in one transaction, keeping the per-Flow batch order and the latest value per key.
- Dex never deletes rows when a Flow completes. There is no foreign key to the parent
  and no resync API; a Flow that selects several stores writes each map only to the
  stores that declare it.

Superverse configures every store with `rowKey` and `columnNaming: snake_case`, and
`attributeMaps` where a Flow syncs a map; `projects` has two stores, ProjectFlow's and
ProjectSetupFlow's. The sync retry budget is `syncRetryPolicy.totalDuration: 168h`, at
the cost of a closing Flow waiting for its queue to drain. The DSN uses the
`superverse_dex` role.

## Tests

Real PostgreSQL and Dex integration tests check that the rows and child rows of every
synced Flow match its state (read with Dex's state API); that key columns match the
FlowID; that ProjectFlow and ProjectSetupFlow writing one row never overwrite each
other; that `release_build_revisions` follows builds; that column sets equal the synced
names; that sync catches up after PostgreSQL returns; that Dex fails to start when a
store is unreachable; and every DAO query. At the gRPC boundary, lists, details and
Watches match the Flows, pagination is stable, a new resource's Watch waits for its row,
a stale fence recovers, and a Live target is refused after a removal or redeploy. The
Dex repository tests the store changes against real PostgreSQL and MySQL.
