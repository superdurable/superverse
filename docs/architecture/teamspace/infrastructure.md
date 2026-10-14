# Infrastructure

## PostgreSQL

- PostgreSQL 17 on every lane, pinned by digest in `source-lock.json`:
  - Local Kind: a StatefulSet in the chart's `local-dependencies.yaml`, forwarded on
    28432 for the integration tests;
  - Local Native: a service in `infra-as-code/scripts/native/compose.yaml`, on 26432
    plus the instance's port offset;
  - AWS test: RDS from Terraform, in private subnets, reachable only from EKS,
    encrypted, with `manage_master_user_password` so no password enters Terraform
    state.
- Three roles with fixed names, which migrations grant to:
  - `superverse_owner`, which owns the schema and only migrations connect as;
  - `superverse_dex`: SELECT, INSERT and UPDATE on the synced tables, plus DELETE on
    their child tables;
  - `superverse_server`: read-only on the synced tables; full access to the accounts and
    connection tables.
- Each environment's private procedure creates the roles and their passwords; neither
  appears in migrations or Terraform state. The local lanes keep the passwords in the
  lane's private `postgres-credentials.json` and, on every start, create the missing
  roles and set every password through the container's socket as the superuser, so a
  regenerated file and a surviving data volume cannot drift apart. The integration tests
  read the same file.
- The platform Dex cannot start while its Attribute Store is unreachable, so PostgreSQL
  is a start-up dependency of the platform. AWS relies on RDS backups and restore; the
  local lanes restart the container.
- PostgreSQL latency also reaches the Flows. Dex runs each sync batch first as a local step
  of the Flow's own execution, and only after about 7 seconds falls back to a retry in the
  background (superdurable/dex). While the database is slow or a synced table is locked,
  the writes and RPCs of every Flow that syncs are held up to about 7 seconds, though no
  batch is lost.

## Migrations

- Files `internal/platform/postgres/migrations/NNNN_*.sql`, embedded in the binary:
  `0001` accounts and login sessions, `0002` the synced Project, Release and Deployment
  tables, `0003` TeamSpaces and members, `0004` connections, `0005` the Slack event relay's
  provider account columns and trigger registrations
  ([connections.md](connections.md#database-schema-changes)).
- `superverse-server migrate` takes an advisory lock, records each file in
  `schema_migrations` and refuses a file whose checksum changed.
- It runs as the server Pod's `migrate` init container on Kind and EKS
  (`infra-as-code/kubernetes/charts/superverse-platform/templates/server.yaml`), the only
  container that receives the owner's password, and in the Native launcher before it
  starts the server. The server never migrates at start-up. There is no Helm hook: a hook
  Job would run before the chart's own Kind PostgreSQL exists on a first install.
- Migrations only add. Dex reads a table's columns at startup and on its refresh interval,
  so a migration that adds a synced column must reach Dex before the new server writes the
  matching Attribute, or Dex skips those first values. Until Dex refreshes a table's columns on its own when a batch names an unknown column, the Kind deploy and the Native launcher restart the platform
  Dex after a migration that changes synced tables and before they start the new server. A
  first install needs nothing extra: Dex cannot start while its tables are missing.
- The Dex `attributeStore` configuration is written only after the migration succeeds:
  by `deploy` on Kind and by the start-up script on Native.

## Configuration

- The non-secret parts of the DSN are keys of `cmd/superverse-server/config` with
  per-lane profiles (`postgres.host`, `port`, `database`, `sslMode`, `maxConnections`;
  `sslMode: disable` only with `localKind`). The passwords are the secrets
  `SUPERVERSE_POSTGRES_PASSWORD` (server) and `SUPERVERSE_POSTGRES_OWNER_PASSWORD`
  (migrations only).
- The credential read endpoint: its listen address and the fixed address advertised to
  applications are configuration keys. `connections.allowInsecureOAuthEndpoints` allows
  `http` OAuth endpoints; only the local profiles set it and `Validate` rejects it
  elsewhere.
- Platform OAuth applications ([connections.md](connections.md#oauth-clients)): the key
  `connections.platformClients` maps `<connector>/<method>` to `{clientId, scopes}`, and
  the secret `SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS`, read in
  `internal/bootstrap/secrets.go`, is a JSON map from the same keys to the client
  secrets. The server refuses to start when a configured client has no secret. The local
  profiles configure none; an environment adds entries as PLATAPP registers applications.
- Kind and EKS gain a NetworkPolicy that lets Project namespaces and Sandboxes reach the
  read endpoint's port and nothing else of the server, and a Live application's egress
  gains a rule to that port. Kind's kindnetd enforces NetworkPolicy, but the platform
  policies are skipped there, so Kind also gets a companion policy that keeps the server's
  other ports open. Native has no NetworkPolicy: the endpoint listens on the loopback and
  containers reach it as `host.docker.internal`.
- The Slack event relay's keys and secret are [below](#slack-event-relay).
- The local OAuth server for the end-to-end case (Keycloak, with a test realm, client and
  user) runs in the Kind chart's `local-dependencies.yaml` and the Native
  `compose.yaml`; AWS does not need it. It needs roughly 0.5 to 1 GB of memory per lane.

## Slack event relay

The relay itself is in [connections.md](connections.md#slack-event-relay); this section
holds its configuration and network paths.

### Configuration

- The secret `SUPERVERSE_SLACK_SIGNING_SECRET`, read in `internal/bootstrap/secrets.go`,
  is the platform Slack application's Signing Secret. Never a configuration key. The
  public events listener serves only when it is set, and the server refuses to start when
  `connections.platformClients` has a `slack/...` entry without it. Hosted environments add
  `SUPERVERSE_SLACK_SIGNING_SECRET` to the pre-provisioned `<product>-server` Secret before
  Slack's Request URL is set; without it the server does not open its events listener, and
  the ALB rule routes `/api/slack/events` to a port nothing serves.
- Keys under `connections.slackRelay`, with their defaults, ranges and reference rows in
  `cmd/superverse-server/config`:

  | Key | Default | Meaning |
  | --- | --- | --- |
  | `listenAddress` | per lane profile | The public events listener, which serves only `POST /api/slack/events` |
  | `queue.implementation` | `memory` | The queue implementation; `memory` is the only accepted value in Beta |
  | `queue.maxEvents` | 10000 | Queued items in all scopes of one replica |
  | `queue.maxBytes` | 64 MiB | Queued bytes in all scopes of one replica |
  | `queue.maxEventBytes` | 1 MiB | One event; also the endpoint's body limit |
  | `queue.maxScopeEvents` | 100 | Queued items of one Project scope |
  | `delivery.timeout` | 10 s | One POST to an application |
  | `delivery.concurrency` | 16 | Deliveries in flight across scopes; one per scope |

  The defaults are initial values, to be tuned from measurements in the test environment.

- The local lanes generate a random test Signing Secret into their private credentials
  file, so the relay tests can sign events; they configure no Slack platform client.

### Network paths

A Project's application port is public on both scopes today, and the relay delivers through
those routes; it adds no port to the application or the Sandbox:

- Live: an HTTPRoute on the project Gateway to the serving generation's Service, which follows
  each redeployment (`internal/delivery/deployments/kube/live_serving_route.go`,
  `live_serving.go:52-55`); on Local Native, the lane gateway's route to the Live container
  (`internal/delivery/deployments/docker/route.go`).
- Preview: an HTTPRoute on the project Gateway to the Sandbox's port 3000
  (`internal/agents/sandbox/kube/instance_provisioner_preview.go`); on Local Native,
  the lane gateway's route to the coding container's published port 3000.

The server reaches these routes exactly as it already probes them when it activates a Live
generation or exposes a Preview:

- AWS: the public URL itself (`internal/projects/infra/kube/hosted/backend.go`), through
  the platform load balancer's wildcard rule and the Envoy proxies of the project Gateway. The server's platform NetworkPolicy already allows
  egress to public addresses on 443 (`network-policy.yaml:30-34`), so no rule is added.
- Local Kind: the Envoy proxies of the project Gateway, which route the Live and Preview hosts
  by Host header to those Services (`internal/projects/infra/kube/local/backend.go`,
  `project-gateway.yaml`); the chart's server policies are not rendered on Kind.
- Local Native: the lane's gateway, with the route's Host header
  (`internal/projects/infra/docker/routes.go`), on the host loopback.

The public events listener:

- The platform's internet-facing load balancer, whose Ingress (`ingress.yaml`) sends `/` to
  Studio, gains a second Ingress in the same group with an `Exact` rule for
  `/api/slack/events` to the server Service's new `slack-events` port, so Slack's raw request
  reaches the server, not Studio. Its health check names the server's health port and
  `/readyz`, because the events listener serves only the POST. The ALB's health checks come
  from its own subnets, so the chart admits port 8082 to the server Pods from
  `ingress.targetCIDRs`, the platform ALB's public subnet CIDRs that Terraform supplies, in a
  NetworkPolicy of its own; the slack-events Ingress carries `group.order` 10 and Studio's
  20, so the exact path is evaluated before Studio's `/` prefix.
- The server's platform NetworkPolicy admits that port from anywhere, as it admits Studio's
  3000.
- Local Kind and Native have no public Ingress, so locally only the tests reach the
  listener; the AWS test environment is where Slack reaches it.

### Code layout

- `internal/tenancy/slackrelay/`: the events endpoint, routing, the registration handler,
  the uninstall handling and the delivery consumer. It reaches the scopes' public routes
  through a port it declares, which `internal/bootstrap` implements with the public routes
  and probe base the deployments and the Preview already use, so `tenancy` imports no
  `projects`, `agents` or `delivery` package.
- `internal/platform/eventqueue/` with the interface, the item type and the typed errors,
  and `internal/platform/eventqueue/memory/`, the only implementation, which only
  `internal/bootstrap` imports.

## Dex

- The platform Dex pin moves to the server release with the Attribute Store changes
  (`source-lock.json`, the chart and Terraform). Projects' Live Dex is unaffected.
- Every store sets `rowKey` and `columnNaming: snake_case`, and `attributeMaps` where a
  Flow syncs a map; `syncRetryPolicy.totalDuration` is `168h`; the DSN uses
  `superverse_dex`.

## Code layout

`internal/` keeps its ten entries:

- `internal/tenancy/accounts/` and `internal/tenancy/accountdao/` (both exist), beside
  `internal/tenancy/teamspaces/` (TeamSpaceFlow), `internal/tenancy/connections/`
  (connections and the read endpoint) and the DAO packages `memberdao`, `teamspacedao`
  and `connectiondao`;
- `internal/projects/projectdao` and `internal/delivery/deliverydao`;
- `internal/platform/postgres/` for the pool, transactions and migrations (exists);
- `internal/api/tenancy/` for the account, TeamSpace, member and connection services.

Project members stay in `internal/projects` (`project_flow_members.go`); the Project's
connection selection stays in the configuration host. A child imports its parent
(`projects` imports `tenancy/*`); TeamSpaceFlow reaches Projects and connections through
the `ProjectCleanup` and `ConnectionCleanup` ports the bootstrap injects (`teamspaces`
declares them; `projects` and `connections` implement them). The DAO
packages sit at level 1 and their users move up accordingly; `api/tenancy` is level 10,
`api` 11 and `bootstrap` 12.

## Moving an environment

Existing data is reset, not migrated (pre-launch). On AWS this joins the next wave of
the agent plan, which already deletes every old Project through the business API, and
adds: ending the AccountFlow executions with dexcli, creating RDS and the database roles,
updating the private secrets, and having users register again
(runbook). Deployment
and cleanup records stay private.

## Sync incidents

When PostgreSQL could not be written for longer than the 168-hour retry budget, batches
may have been dropped. Before service resumes, compare the member tables, and any other
table in doubt, with `dexcli flow state` of the owning Flows, and correct the rows by
hand. The [sync incident runbook](../../runbooks/read-model-sync-incidents.md) carries the
commands.
