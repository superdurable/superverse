# API and Studio

## Business services

All of them live in `api/superverse/v2/superverse.proto`, hold at most ten RPCs each and
carry `CallerContext caller = 100` on every request.

| Service | RPCs |
|---|---|
| `TeamSpaceService` | CreateTeamSpace, ListTeamSpaces, GetTeamSpace, RenameTeamSpace, DeleteTeamSpace, GetTeamSpaceOverview |
| `TeamSpaceMemberService` | ListTeamSpaceMembers, AddTeamSpaceMember, SetTeamSpaceMemberRole, RemoveTeamSpaceMember |
| `ProjectMemberService` | ListProjectMembers, AddProjectMember, SetProjectMemberRole, RemoveProjectMember (ProjectService already holds eight RPCs) |
| `TeamSpaceConnectionService` | ListConnectorCatalog, ListTeamSpaceConnections, CreateTeamSpaceConnection (also the copy to a newer major version), GetTeamSpaceConnection, UpdateTeamSpaceConnection (settings and API keys), DeleteTeamSpaceConnection, StartTeamSpaceConnectionOAuth (also the creation of an OAuth connection), CompleteTeamSpaceConnectionOAuth, StartTeamSpaceConnectionUiSession, InvokeTeamSpaceConnectionCommand |

- New enums: `TeamSpaceRole` (ADMIN, MEMBER), `ProjectRole` (OWNER, BUILDER, OPERATOR),
  `TeamSpaceStatus`, `TeamSpaceConnectionStatus`, `CredentialStatus` and
  `ConnectionClientSource` (USER, PLATFORM); value names are the enum prefix plus the
  persisted value.
- Every revision fence, including the Live-pointer fences of RemoveLive and
  StartDeployment, refuses a stale write with the ErrorInfo reason `REVISION_CHANGED`, so
  Studio can tell it from a rule rejection, say the content changed and reread.
- `ProjectSnapshot`: `owner_account_id` becomes `creator_account_id`; new `team_space_id`
  and `caller_role`, which Studio uses only to choose what to show.
- `CreateProjectRequest` and `ListProjectsRequest` require `team_space_id`.
- `ListProjects` returns a reduced entry, without the repository and the manifest, for a
  TeamSpace member who holds no role in a Project; it is Studio's "not joined" row, and
  GetProject denies that member the same details.
- `TeamSpaceConnectionService` requests carry settings and credentials as `string` fields
  that hold JSON (`settings_json`, `credentials_json`, `credential_secrets_json`,
  `parameters_json` and the like), which the server type-checks when it parses them, because
  the Studio BFF accepts no Struct, map, bytes or Timestamp field in a request, and
  `scripts/check-proto-services.py` refuses such fields in the requests of browser services. Create takes
  `copy_from_connection_id` to copy a connection to a newer major version: it copies the
  settings, carries the `user` OAuth client, revalidates the settings and requires
  authorization ([connections.md](connections.md#version-lines-and-release-candidates)).
  `ListConnectorCatalog` returns the published catalog's released connectors and needs
  only an authenticated caller.
- `ConnectorService` loses SaveConnection, DeleteConnection, StartConnectorOAuth and
  CompleteConnectorOAuth and gains `SelectTeamSpaceConnection` (one connection name of
  one scope) and `ReleaseUnusedConnections`. The OAuth callback
  `/api/connector-oauth/callback` tells a TeamSpace authorization from its state.
  The [current configuration contract](../studio-setup/flow-changes.md) specifies that a Live selection lands on
  Live's next-publish layer (a target with `next_publish`), which no prepared version owns,
  and `follow_preview` removes it so that the name follows Preview's applied selection
  again; a connector tied to one address cannot follow (`CONFIGURATION_ITEM_CANNOT_FOLLOW`).
  The callback stays Studio's, not an environment's, so one OAuth connection serves Preview
  and Live of every Project in the TeamSpace ([resource API](../../api/resource-grpc.md#configuration)).
- Lists page by creation time descending plus ID (keyset), so pages are always full.
- There are 15 services. `scripts/check-proto-services.py`'s routes, the BFF's
  `BROWSER_SERVICES` and the mock's `SERVICES` follow. No field is named `permissions`.

All of these reads go to SQL ([read-model.md](read-model.md)); writes go to the owning
Flow, or to the database for accounts and connections.

## The credential read endpoint

`POST /internal/connection-credentials/read` serves applications, not Studio. It takes
a scope token, `sharedConnectionId` and an optional `rejectedRevision`, and returns the
credential without its host-kept fields (a refresh token, a client secret or a service
account key; [connections.md](connections.md#reading-credentials)) with its revision,
issue and expiry times, or `REAUTHORIZATION_REQUIRED` or `CONNECTION_NOT_SELECTED`. It
returns no settings: they are frozen in the configuration entries. It listens on its own port; [credential-read.md](../../api/credential-read.md)
is its contract and [connections.md](connections.md) has its behavior.

## The Slack event relay

Slack's deliveries to the platform's Slack application (`POST /api/slack/events`), the
applications' Slack trigger registration on the read endpoint's listener and the relayed
events the platform posts to applications serve Slack and applications, not Studio
([connections.md](connections.md#slack-event-relay)); [slack-relay.md](../../api/slack-relay.md)
is their contract. With the credential read endpoint they are the server's only APIs
outside the business protocol.

## Studio

Studio follows the [current frame](../studio-setup/studio.md) and uses server data. Setup, Preview apply and publishing in a Project are refined by the
[STUDIOSETUP design](../studio-setup/README.md), and Live's settings and publishing again by
[the current contract](../studio-setup/flow-changes.md). What has no data yet is absent, never drawn
disabled.

- **Sidebar**: the TeamSpace switcher (with "new TeamSpace") on top, then Jump to, New
  project and Home, then two sections with the same heading style: Projects, and Team
  (Connections, then People), which stays in view below the scrolling Projects. Knowledge
  joins Team with the knowledge area. Projects lists every Project of the
  TeamSpace with the caller's role word (build, operate or none). The platform admin
  sees every TeamSpace and can delete one, with a second confirmation.
- **Home** (the TeamSpace overview): Projects with status, Live presence and the
  caller's role, the member count and the connection count, all from SQL. No agent or
  Preview status and no "Needs you" yet.
- **People**: admins add registered accounts by email, set admin or member and remove
  members; Studio explains "at least one admin" and "the only owner of a Project". Adding
  someone who still holds roles in Projects of the TeamSpace shows "this person regains
  their roles in N Projects", counted by one SQL query over `project_members`.
- **Connections**: grouped by connector and major version. Create from a catalog version
  (`ListConnectorCatalog`) with settings, an API key or OAuth (the editor of  moves here); an OAuth
  connection exists only once its sign-in completes
  ([connections](connections.md#oauth-clients)); edit settings,
  reauthorize, copy to a new major version, delete when unused, and see which Projects
  use each connection.
  Each Project using a connection is marked P for Preview and L for current Live, from the
  usage rows; the current contract defers the prototype's dashed L for "Live from the next publish"
  ([connections](connections.md#lives-next-publish)). The page says that a new key or
  sign-in reaches every Project that uses the connection at its next read.
  Reauthorization required shows on the connection and in the Projects using it.
  Reauthorizing a connection that Live uses, or replacing its key, is refused unless the
  caller is a TeamSpace admin or a builder in every Project that uses it
  ([roles](teamspaces-and-roles.md#hierarchy-and-roles)); Studio says why.
  The server checks an OAuth authorization URL's scheme by profile before StartOAuth
  returns it (https only, except http in local profiles under
  `allowInsecureOAuthEndpoints`); the BFF checks only that it is an absolute http or https
  URL without userinfo or fragment, so the policy has one owner.
- **In a Project**:
  - Tools ▸ Permissions: only owners change members and roles.
  - Tools ▸ Connectors: each connection name shows the version the application
    declares (possibly a release candidate) and has a selector per Preview and Live that
    lists only connections of the same connector, major version and method. The selector
    offers "New connection…" for that version and, after a major upgrade, "Copy … to vN". Live
    selections change on the next version to publish, and a Preview picks up a new
    selection or changed settings at its next start; "Release unused connections" keeps
    only the usages of the current Live version and is refused while a deployment runs.
    Step settings and trigger bindings are configured in Setup.
    The [current configuration contract](../studio-setup/flow-changes.md) specifies that Setup on the Preview tab has a row per connection, with
    its triggers and Step settings under it, and two columns: Preview, what the Preview runs once
    applied, and Next publish, Live's next-publish layer. A Next publish dropdown opens on "Same
    as Preview"; every connection dropdown then lists the matching connections and "New
    connection…", which opens in the row with the TeamSpace Connections form (an OAuth
    connection exists once its sign-in completes). The
    Live tab shows the same controls under a banner and edits the same layer, so Live needs no
    prepared version. A connector tied to one address offers no "Same as Preview" and says why;
    a blue dot marks a setting that differs from current Live, which reaches Live only with a
    publish. Editing a connection from a row opens the TeamSpace connection's editor and says
    which sides of this Project use it; Setup's footer links to TeamSpace ▸ Connections.
  - Operators see Live only: no Preview switch, no chat and no "Ask to build" entry,
    neither a link nor an RPC.
  - The role comes from the server's `caller_role`; the surface registry keeps gating
    surfaces by it.
- Accessibility, keyboard use, cancellation and stale-response protection are kept. A
  fence conflict says the content changed and rereads.
- Directory limits: `components/` already has ten subdirectories, so
  `components/account` merges into `components/shell` to make room for
  `components/teamspace`; `mock/store` is full and splits out `mock/store/tenancy/`.
- The mock serves TeamSpaces, members, Project roles and connections, and its role
  lenses use the server's roles.
