# Superverse V2 architecture

This document defines the current platform architecture and its durable owners.

## Summary

Build V2 independently in `superversev2`, with one Go business server and
Dex-owned application state. Preserve V1 while V2 is built and verified. Do not
migrate V1 data, modify V1 deployments, or take ownership of V1 infrastructure.

Current architecture:

- Keep the open-source Dex Server, CLI and Go SDK and the Connector SDK/library.
  The application template and the Agent's Dex skills are Superverse-owned source
  in `app-template/` and `dex-skills/`; applications are not pinned to Dex
  versions.
  Import audited management UI/API source into Studio/Go; no standalone Dex Web
  runtime or shared UI package is required.
- Copy the complete SuperAgent repository source into this repository. Remove
  the remote Go module and UI release-tarball dependencies.
- Put reusable infrastructure implementation under `infra-as-code/`.
- Merge the Java Control Plane and Go Agent Server into one
  `superverse-server` Go binary. Expose one business gRPC server with the
  eleven domain services of one `api/superverse/v2/superverse.proto`
  ([business API](../api/resource-grpc.md)).
- Preserve Studio, the BFF, strict FDG 2.0 validation, server-derived authorization,
  Connector setup, OAuth authorization and refresh, coding tools, prompts and rules.
- Use one platform Temporal namespace per environment: `superverse-test`,
  `superverse-staging`, or `superverse-prod`.
- Platform environments remain test/staging/prod. Project deployment targets
  are Preview in a Sandbox and Live only. There is no project staging or
  promotion phase.
- Support source configuration and OAuth before Release. Build Release and
  Deploy Live remain separate project-level actions.
- Implement immediate email/password registration and login without OIDC,
  email verification, invitations or onboarding hooks. Accounts and login
  sessions are PostgreSQL rows ([accounts](teamspace/accounts.md)).
- Users access only their own projects. The configured platform administrator account (`accounts.platformAdminEmail`) registers through
  the same flow and may view and operate all projects. Do not add a separate
  administrator bootstrap procedure.
- The active administrator lists every non-DELETED Project and can use the same
  configuration, Session/Agent, Preview, Release, Deployment and Project deletion
  actions as each owner. Server-authenticated account identity grants access;
  the browser's `isAdmin` field is presentation only. The original owner remains
  unchanged. Terminal Project Watch snapshots remain available to owner/admin
  for cleanup verification; ordinary resource reads reject deleted Projects.
- A Project belongs to a TeamSpace, and people reach it by TeamSpace membership and a
  Project role (owner, builder or operator); the platform administrator manages every
  TeamSpace and Project through the same server-side checks ([TeamSpace](teamspace/README.md), [read model](teamspace/read-model.md), [roles](teamspace/teamspaces-and-roles.md) and [connections](teamspace/connections.md)).
- Retain Project, Release and Deployment records, and the Project's one coding
  Session, until the Project is explicitly deleted. The Session never expires.
  Retain the existing approximately 1,000-message Agent window, not unlimited
  conversation history. Do not introduce archive-segment Flows.
- The coding Agent is specified in [the Agent harness design](agent-harness-design/README.md):
  Claude (Opus 5.5 by default, Sonnet 5.5 or Haiku 4.5 selectable, and the mock
  model for tests where the server offers it), an append-only model context, one
  Session per Project, a Stop button that stops everything, subagents, and
  failures that go to the model rather than to a manual recovery.
- PostgreSQL holds the accounts and login sessions, the read model, which only the
  Dex Attribute Store writes, and the TeamSpace connections
  ([read model](teamspace/read-model.md), [connections](teamspace/connections.md)). The database
  is never a second owner of a Flow's facts. Do not introduce Redis, an ORM, an
  outbox or a repair loop.
- Every major change must pass its three E2E cases and its Dex integration tests
  on the local lane `make required-lanes` names, with the mock model, and the
  Local Kind acceptance on `local-kind-acceptance.yaml` precedes a deployment to
  AWS ([Tests](#tests)). Unit tests, mock UI, and ready Pods are not that
  acceptance gate.


## Repository and service architecture

### Source import

V2 retains the SuperAgent Agent Flow, models, generic ToolRegistry, coding
tools, shared UI and regression tests. Standalone repository metadata and caches
are excluded.

Remove unused external MCP transport, configuration, capability fields, Portal
controls and dependencies. Tool availability is governed by the enabled-tool
selection, independent of the removed MCP enable switch or synthetic server.
Keep approvals, parallel execution, uncertain-effect reconciliation, Git and
Sandbox behavior.

After verifying V2 gRPC/BFF parity, remove old cross-repository export facades,
standalone HTTP/OpenAPI entrypoints and `cmd/superagent`. Place the Session Agent
in `internal/agents/session` and its sub-packages (see
the current contract); Sandbox
transport and Git live under `internal/agents/sandbox/workspace`. The
root proto owns business transport. Studio consumes its local transport-free
Agent components and generated proto types.
Do not retain a second server or manual duplicate browser DTO contract.

There is no per-Session Agent configuration Attribute. The model is the Session's
`SessionModel`, and the tools and system prompt are frozen with each context
segment ([model and context](agent-harness-design/context-management.md)); the
imported MCP configuration, enabled-tool selection and `AgentConfig` do not
exist. Do not migrate standalone external-MCP executions or reinterpret an
unknown external effect as success. The accepted StartIdentity remains
authoritative; an incompatible start fingerprint conflicts rather than being
silently rewritten. Before launch every Agent change is a direct replacement:
the local lanes and AWS delete their Projects instead of migrating open executions
(rollout reconciliation).

Also import V1 coding tools, profiles and prompts, Sandbox/Git providers, and
the required project resource adapters. Remove the credential broker. Do not reduce Agent behavior merely
to simplify deployment topology.

Use constructor-injected typed interfaces for former Agent-to-Control-Plane
callbacks. Do not replace those callbacks with
self-gRPC calls. The BFF calls the business gRPC API using generated clients.

The Dex SDK owns an internal Worker listener, which remains necessary. It does not create another business-service deployment. Health and
protocol-specific ingress do not reintroduce a second project/session API.

Every Flow stores the Worker target `superverse-server-worker.<platform namespace>.svc.cluster.local:50051`
with headless routing (`dex.workerHeadless`): platform Dex resolves the headless Service to the server Pods and
routes a Flow to the Worker that last served it, whose BlobCache already holds its payloads. Platform Dex
re-resolves the Service every two seconds (`worker.headlessAddressRefreshInterval: 2s`). A hosted server rolls out
with a rolling update, which starts the replacement beside the old Pod; the old Pod keeps serving for 15 seconds (a
`preStop` sleep) while Kubernetes removes it from the Service, so Dex keeps invoking it until it routes to the
replacement. Local Kind's server rolls out with the Recreate strategy, which starts the replacement only after the
old Pod exited, and has no `preStop` sleep, which would only delay the replacement: Dex cannot invoke a Worker from
the old Pod's exit until shortly after its replacement is ready, about six to eight seconds per rollout or Pod
deletion (up to about three and a half of them after the replacement is ready) and ten to twelve for two rollouts in a
row.
A Flow keeps the target it started with:
Flows started without headless routing keep resolving through gRPC, which re-resolves a name at most every 30
seconds and can keep a replaced Pod's address that long
(measurements).

The server shares one Flow registry and process-local BlobCache between its
Dex Client and Worker. Cache contents are disposable, not durable authority.

### Business package ownership

`internal/` is organized by bounded context: `agents` (the Session Service, the
Session Agent and Sandbox instances), `projects`, `delivery` (Releases and
Deployments) and `studio`, with `tenancy` (the accounts today; TeamSpaces and
connections in the [TeamSpace design](teamspace/infrastructure.md#code-layout)),
the business gRPC boundary `api`, the composition root `bootstrap` and the
shared roots `app`, `comm` and `platform`. The
package tree and limits decision
records the tree, the import levels and the package rules that `make check`
enforces; each package README carries its Purpose, its callers and its
dependencies, generated from the imports. A capability with several
implementations keeps its interface in the parent package and each
implementation in a sub-package of its own (`kube`, `docker`, `local`, `hosted`,
`github`, `localgit`): only `internal/bootstrap` imports an implementation, only
the `kube` packages, `platform/kubeapi` and `bootstrap/kubernetes.go` import the
Kubernetes API client, and only the `docker` packages and `platform/dockerapi`
import the Docker Engine API client. Every
package owns the following;
`make generate-package-readmes` renders this table from the README Purposes:

<!-- BEGIN GENERATED PACKAGE TABLE (scripts/check-package-readmes.py; edit the README Purpose) -->

| Package | Purpose |
| --- | --- |
| [`agents`](../../internal/agents/README.md) | Coordinates each Project's one Session across its Project, Session Agent, provisioning and Preview Flows for the API and those Flows: start, reads, workspace cold path, Preview commands, cleanup. |
| [`agents/diagnostics`](../../internal/agents/diagnostics/README.md) | Runs the Session Agent's read-only platform tools on the server for the Session's own Project, redacted and bounded, without the workspace. |
| [`agents/platformissue`](../../internal/agents/platformissue/README.md) | Files the platform faults of one signature as one GitHub issue: PlatformIssueFlow finds or opens it by the signature's label, records the outcome and completes. |
| [`agents/remoteagent`](../../internal/agents/remoteagent/README.md) | Owns remote agent pairing, credential authorization and the independent Bridge HTTP endpoint while Session owns executor authority. |
| [`agents/remoteagent/bridgeapi`](../../internal/agents/remoteagent/bridgeapi/README.md) | Declares the remote agent endpoint's paths, body limits, JSON requests, responses, reports and error codes, shared by the endpoint and the Bridge. |
| [`agents/remoteagentdao`](../../internal/agents/remoteagentdao/README.md) | Owns PostgreSQL remote connection credentials, bounded pairing admission, revocation and liveness without owning the Session executor. |
| [`agents/remoteexec`](../../internal/agents/remoteexec/README.md) | Defines Session executor admission, report fences, bounded inbox items, stable remote calls, public delivery and platform handoff metadata without transport or credentials. |
| [`agents/sandbox`](../../internal/agents/sandbox/README.md) | Defines a Session's Sandbox instance, its resources and the InstanceProvider runtime that creates, exposes and removes them; the sub-packages provision instances and serve Previews. |
| [`agents/sandbox/docker`](../../internal/agents/sandbox/docker/README.md) | Runs a Session's Sandbox instances as hardened Docker containers of the coding image on its own network and workspace volumes, and exposes and reclaims them by exact identity. |
| [`agents/sandbox/kube`](../../internal/agents/sandbox/kube/README.md) | Applies the Kubernetes resources of Sandbox instances from the coding profile, checks and exposes their Preview, and deletes them by UID, behind `sandbox.InstanceProvider`. |
| [`agents/sandbox/preview`](../../internal/agents/sandbox/preview/README.md) | Serves a Project's Preview: one PreviewFlow execution per Project runs its Preview commands on the current Sandbox instance of the Project's Session, which it acquires from the Session. |
| [`agents/sandbox/provision`](../../internal/agents/sandbox/provision/README.md) | Provisions one Sandbox instance of a Session: one SandboxProvisionFlow execution creates its Pod on the Session's volume, checks the repository out, verifies it and reports the outcome. |
| [`agents/sandbox/sessionworkspace`](../../internal/agents/sandbox/sessionworkspace/README.md) | Owns a Project Session's Sandbox instances in one SessionWorkspaceFlow: runs the Session's provision and reclaim commands one at a time and reports each outcome. |
| [`agents/sandbox/workspace`](../../internal/agents/sandbox/workspace/README.md) | Gives Session tools and the Sandbox provider one admitted Sandbox's workspace: the pooled sandboxd transport resolved from an accepted lease and the Preview supervisor. |
| [`agents/sandbox/workspace/git`](../../internal/agents/sandbox/workspace/git/README.md) | Runs the platform's own Git commands in a Session's Sandbox for checkout, Agent commits and subagent worktrees, with a trusted environment and per-operation repository credentials. |
| [`agents/sandbox/workspace/sandboxd`](../../internal/agents/sandbox/workspace/sandboxd/README.md) | Speaks the sandboxd file and process protocols of exactly one Sandbox, and runs the coding image's JSON helpers, for the workspace, Git and Agent tool packages. |
| [`agents/sandbox/workspace/sandboxd/sandboxdtest`](../../internal/agents/sandbox/workspace/sandboxd/sandboxdtest/README.md) | Connects integration tests of the Sandbox workspace packages to an owned, disposable real sandboxd: one the environment names, or a coding image container the test runs. |
| [`agents/session`](../../internal/agents/session/README.md) | Owns SessionAgentFlow, the durable coding Agent of a Project's one Session, for the Session Service and API: its Flow, client, bootstrap and every helper that touches its primitives. |
| [`agents/session/agentloop`](../../internal/agents/session/agentloop/README.md) | Computes the Session Agent's pure loop decisions for SessionAgentFlow: state machine, turns, questions, context segments, tool catalog, batches and results, without touching Flow primitives. |
| [`agents/session/agenttasks`](../../internal/agents/session/agenttasks/README.md) | Holds the task records of the Session Agent's background commands and subagents, their kinds, bounds and admission, the subagent start and launcher port, and the task notifications they send. |
| [`agents/session/backgroundcommand`](../../internal/agents/session/backgroundcommand/README.md) | Follows one background command of a Session in the Sandbox's command ledger, as a Dex SubFlow of the Session, until the command ends. |
| [`agents/session/contribution`](../../internal/agents/session/contribution/README.md) | Owns ConnectorContributionFlow, one connector a Project's Session contributes to the connectors library: its record, its draft pull request and its client and bootstrap. |
| [`agents/session/conversation`](../../internal/agents/session/conversation/README.md) | Defines the values SessionAgentFlow exchanges with its model: the append-only entry log and its public view, request assembly, requests and replies, tool calls, activity events and prompts. |
| [`agents/session/flowdesign`](../../internal/agents/session/flowdesign/README.md) | Owns the Flow design a requirements card carries: its validation, canonical JSON, digest and outline, and the main story it shares with the Flow canvas. |
| [`agents/session/modelprovider`](../../internal/agents/session/modelprovider/README.md) | Implements the Session Agent's model client over the Claude Messages API: model profiles, byte-exact replay of stored blocks, streamed summaries and tool activity, fallback cleanup and error classification. |
| [`agents/session/modelprovider/mockmodel`](../../internal/agents/session/modelprovider/mockmodel/README.md) | Serves the mock model: a loopback Claude Messages API whose embedded scenarios answer ordered user inputs, so Sessions run real tools without model cost. |
| [`agents/session/sandboxtools`](../../internal/agents/session/sandboxtools/README.md) | Runs the Sandbox tools of the Session Agent and its subagents over an accepted workspace lease, without any Flow access: files, commands, Git, subagent worktrees and the Connector catalog. |
| [`agents/session/subagent`](../../internal/agents/session/subagent/README.md) | Runs a Session's subagents: each SubagentFlow execution works on one bounded task in its own git worktree, with its own conversation and tools, and reports its end. |
| [`agents/session/toolinput`](../../internal/agents/session/toolinput/README.md) | Owns the JSON Schemas the model sees for each Session Agent tool and the strict decoding of tool arguments against them. |
| [`agents/toolfailure`](../../internal/agents/toolfailure/README.md) | Declares, once each, the tool failure codes the Session Agent's model reads, with their owner, hint and source-gate flag, and the owners and effects of a failed call. |
| [`api`](../../internal/api/README.md) | Serves the business gRPC services that the Studio BFF calls, one file per service domain, mapping each RPC onto its owning Flow client or Service. |
| [`api/boundary`](../../internal/api/boundary/README.md) | Holds the trust boundary every business gRPC handler shares: caller authentication, panic recovery, request input checks, list paging and the mapping of errors to public statuses. |
| [`api/remoteagent`](../../internal/api/remoteagent/README.md) | Authorizes Builder pairing, connection reads, removal and platform executor selection through the generated resource contract. |
| [`api/tenancy`](../../internal/api/tenancy/README.md) | Serves AccountService, TeamSpaceService and TeamSpaceMemberService: accounts, TeamSpaces and their members, written through TeamSpaceFlow and read from the read model. |
| [`api/watch`](../../internal/api/watch/README.md) | Serves a resource to API Watch RPCs as authoritative snapshots followed by best-effort Dex Stream progress, with signed resume cursors and realignment on loss. |
| [`app/appmanifest`](../../internal/app/appmanifest/README.md) | Owns the application contracts a project's source declares, for Release, Preview and Studio: the dex-app.yaml manifest, its connector and environment requirements and strict Flow Definition Graphs. |
| [`app/apptemplate`](../../internal/app/apptemplate/README.md) | Embeds the go-react-v1 application template that Project setup pushes into new managed repositories, with its revision and locally computed Git object IDs. |
| [`bootstrap`](../../internal/bootstrap/README.md) | Assembles and runs the single Superverse server process for `cmd/superverse-server`: configuration, providers, Flows, Flow clients, Services, the Dex Worker, the business gRPC server and health endpoints. |
| [`comm`](../../internal/comm/README.md) | Holds the domain-free helpers every package shares: `Must`/`MustDo` for bug-only errors, UTF-8-safe text cuts, strict JSON decoding and long poll windows on the local clock. |
| [`comm/dexflow`](../../internal/comm/dexflow/README.md) | Holds the Dex plumbing Flow packages share: persisted failure, progress and start identity shapes, failure causes, initial Attributes, option presets, polling, long-call heartbeats, lock-retrying RPCs and retained reads. |
| [`comm/dexflow/dexflowtest`](../../internal/comm/dexflow/dexflowtest/README.md) | Connects integration tests to an actual Dex Server and checks engine state, never substituting a Flow, Worker or dependency. |
| [`comm/flowid`](../../internal/comm/flowid/README.md) | Derives the stable FlowIDs, server-assigned business IDs and Start RequestID fingerprints of the platform Flows, and checks the formats of identifiers that callers supply. |
| [`comm/signedtoken`](../../internal/comm/signedtoken/README.md) | Seals short-lived claims into HMAC-SHA256 tokens with a key derived per purpose by HKDF, so any server replica opens what another sealed without shared state. |
| [`delivery/deliverydao`](../../internal/delivery/deliverydao/README.md) | Reads the releases, release_build_revisions and deployments tables that the platform Dex Attribute Store writes, and the Live Runs target join. |
| [`delivery/deployments`](../../internal/delivery/deployments/README.md) | Owns Live deployments for the API and Releases: ProjectDeployFlow deploys one exact Release build, Serving serves its generations and PublishingFlowService links Releases to configuration and automatic deployment. |
| [`delivery/deployments/docker`](../../internal/delivery/deployments/docker/README.md) | Serves each Live deployment generation as a Docker container of its Release image on its own network, moves the project's Live route to it and reads its log for diagnostics. |
| [`delivery/deployments/generationapp`](../../internal/delivery/deployments/generationapp/README.md) | A Live application that the deployment integration tests build in several versions to observe which generation's Worker runs each Step of an open Flow. |
| [`delivery/deployments/kube`](../../internal/delivery/deployments/kube/README.md) | Serves each Live deployment generation as an application Deployment and Service in the project namespace, activates its route and reads its environment and log, for `deployments.Serving`. |
| [`delivery/releases`](../../internal/delivery/releases/README.md) | Owns Releases for the API and Live deployment: ProjectReleaseFlow prepares one main-branch commit's source, builds its immutable build revisions through the build port and cleans them up. |
| [`delivery/releases/build`](../../internal/delivery/releases/build/README.md) | Declares the `Builder` port that builds Release images for ProjectReleaseFlow, its wire types and the embedded build profiles, which the `local` and `hosted` packages implement. |
| [`delivery/releases/build/hosted`](../../internal/delivery/releases/build/hosted/README.md) | Builds Releases on CodeBuild, pushes the images to ECR and keeps its admission receipts in S3 outside Local Kind, behind the `build.Builder` port. |
| [`delivery/releases/build/local`](../../internal/delivery/releases/build/local/README.md) | Builds Release images through the authenticated Local Kind build fixture on the host, behind the `build.Builder` port. |
| [`platform`](../../internal/platform/README.md) | Classifies the outcomes of Kubernetes, AWS, S3, GitHub and gRPC provider calls for every adapter and business package, without reading error text. |
| [`platform/connectorlibrary`](../../internal/platform/connectorlibrary/README.md) | Reaches the connectors library that connector_develop subagents contribute to: fetch and push remotes, the committing bot identity and the upstream library's draft pull requests. |
| [`platform/deploymentnames`](../../internal/platform/deploymentnames/README.md) | Declares the deployment environments and owns their static, operator-selected names (prefixes, clusters, registries, IAM paths and domains) that Go, Terraform, Helm and the infra scripts share. |
| [`platform/dockerapi`](../../internal/platform/dockerapi/README.md) | Gives the Docker runtime packages one raw REST client of the Docker Engine API on its unix socket, with the container, network, volume and image calls they build on. |
| [`platform/eventqueue`](../../internal/platform/eventqueue/README.md) | Declares the bounded, best-effort keyed queue that the Slack event relay offers events to and consumes them from, with its limits and typed refusals. |
| [`platform/eventqueue/memory`](../../internal/platform/eventqueue/memory/README.md) | Implements the event queue in one process's memory: per-key FIFO sub-queues, total item and byte limits, and bounded concurrent consumption. |
| [`platform/githubapp`](../../internal/platform/githubapp/README.md) | Authenticates as one installation of the GitHub App by signing its JWT and minting installation and repository tokens, and calls the GitHub REST API with them. |
| [`platform/githubissues`](../../internal/platform/githubissues/README.md) | Files each platform incident once as a GitHub issue, found or opened by the label of its signature, as one installation of the GitHub App. |
| [`platform/kubeapi`](../../internal/platform/kubeapi/README.md) | Gives every Kubernetes provider one HTTP client of the Kubernetes API and the unstructured-object toolkit it builds and reads resources with. |
| [`platform/kubeapi/kubeapitest`](../../internal/platform/kubeapi/kubeapitest/README.md) | Connects integration tests to the real Kubernetes API of the isolated Local Kind cluster as the platform server's ServiceAccount. |
| [`platform/objectstore`](../../internal/platform/objectstore/README.md) | Stores the immutable, versioned payloads that owning Flows reference by ObjectRef, content-addressed by SHA-256 and verified on every read. |
| [`platform/postgres`](../../internal/platform/postgres/README.md) | Opens the server's PostgreSQL pool, runs transactions, applies the embedded schema migrations under an advisory lock and shares the read model's paging and row decoding. |
| [`platform/postgres/postgrestest`](../../internal/platform/postgres/postgrestest/README.md) | Connects integration tests to the PostgreSQL of the local lane that SUPERVERSE_TEST_LANE names, never substituting the database. |
| [`platform/repohost`](../../internal/platform/repohost/README.md) | Defines the Git host interface, its repository, commit and credential values and its typed errors, so every caller works with GitHub and the local Git host alike. |
| [`platform/repohost/github`](../../internal/platform/repohost/github/README.md) | Implements the repository host on GitHub for Project setup, Releases and Sandbox Git: repository reads, scoped credentials, managed repositories and template initialization. |
| [`platform/repohost/localgit`](../../internal/platform/repohost/localgit/README.md) | Hosts bare Git repositories inside the server for the local lanes, served over smart HTTP with repository-scoped credentials and a read-only JSON interface for the end-to-end tests. |
| [`platform/repohost/localgit/releases`](../../internal/platform/repohost/localgit/releases/README.md) | Keeps the release assets of the local Git host's repositories as immutable files and serves them read-only at GitHub's download path. |
| [`projects`](../../internal/projects/README.md) | Owns Projects for the API, Sessions, Releases and Deployments: ProjectFlow keeps each Project's identity, repository, Live lease, commands and child membership, and starts its one Session. |
| [`projects/config`](../../internal/projects/config/README.md) | Authorizes and admits the configuration of a project's scopes for the platform's Flows and Studio, serving each scope through a pooled in-process configuration host. |
| [`projects/infra`](../../internal/projects/infra/README.md) | Declares the roles through which the platform consumes a project's storage, Temporal, Dex and workload grants, with the references, targets and errors they share. |
| [`projects/infra/docker`](../../internal/projects/infra/docker/README.md) | Provisions, reaches and removes each project's Live network, MinIO, Temporal and Dex containers in the local Docker Engine, grants workloads that storage and serves the gateway its routes. |
| [`projects/infra/docker/dockertest`](../../internal/projects/infra/docker/dockertest/README.md) | Gives integration tests the Docker runtime of the local engine with the pinned images and a gateway of their own, and checks that a deleted project leaves nothing. |
| [`projects/infra/docker/dockertest/configprobe`](../../internal/projects/infra/docker/dockertest/configprobe/README.md) | Loads a workload's project configuration through the Connector SDK from its grant environment, as an application starts, for the Docker integration tests. |
| [`projects/infra/kube`](../../internal/projects/infra/kube/README.md) | Provisions, reaches and removes each project's Live namespace, Dex, storage and Temporal in the Kubernetes cluster, and grants Live and Preview workloads that storage. |
| [`projects/infra/kube/connect`](../../internal/projects/infra/kube/connect/README.md) | Dials a project's Live Dex or a Sandbox's Preview Dex and checks the Live application Worker, after verifying the accepted namespace, controller and Service identities. |
| [`projects/infra/kube/hosted`](../../internal/projects/infra/kube/hosted/README.md) | Implements the project cloud account of hosted environments for project infrastructure: S3 buckets, IAM roles with EKS Pod Identity and Temporal Cloud namespaces and keys. |
| [`projects/infra/kube/hosted/identity`](../../internal/projects/infra/kube/hosted/identity/README.md) | Owns hosted IAM roles, Pod Identity associations, policy documents, grant lifecycle, and cloud resource ownership tags. |
| [`projects/infra/kube/hosted/storage`](../../internal/projects/infra/kube/hosted/storage/README.md) | Creates, verifies, and deletes hosted project buckets and exposes their object clients. |
| [`projects/infra/kube/kubetest`](../../internal/projects/infra/kube/kubetest/README.md) | Gives integration tests the Kubernetes runtime of the isolated Local Kind cluster as the platform server, so tests outside the Kubernetes packages need not import the Kubernetes API client. |
| [`projects/infra/kube/local`](../../internal/projects/infra/kube/local/README.md) | Runs a Local Kind project's own MinIO and Temporal start-dev in its namespace and grants workloads the storage with MinIO service accounts, for the Kubernetes runtime. |
| [`projects/projectdao`](../../internal/projects/projectdao/README.md) | Reads the projects table, which the platform Dex Attribute Store writes from ProjectFlow and ProjectSetupFlow: one Project, its authorization join and the keyset-paged list. |
| [`projects/projecttest`](../../internal/projects/projecttest/README.md) | Gives integration tests throwaway READY Projects of their own, created and deleted through ProjectFlow as the business API does, never substituting a Flow or dependency. |
| [`projects/repobinding`](../../internal/projects/repobinding/README.md) | Owns the exclusive binding of each managed repository to one Project for Project setup and deletion, through RepositoryBindingFlow claims and releases. |
| [`projects/setup`](../../internal/projects/setup/README.md) | Owns Project setup attempts for ProjectFlow: ProjectSetupFlow prepares the repository, template, app manifest and configuration storage, then hands them to the Project. |
| [`studio/confighost`](../../internal/studio/confighost/README.md) | Hosts one project scope's configuration, its selections of TeamSpace connections and their provider UI sessions in process for the project configuration boundary, in the scope's versioned storage. |
| [`studio/confighost/configitems`](../../internal/studio/confighost/configitems/README.md) | Models configuration items and resolves them: the Preview head against its applied version, and Live's next publish from its layer, Preview's applied version and current Live. |
| [`studio/confighost/connectormanifest`](../../internal/studio/confighost/connectormanifest/README.md) | Resolves and verifies official Connector releases, validates values against their manifests, resolves their value sources and grants, and sends their OAuth token and identity requests. |
| [`studio/flowapi`](../../internal/studio/flowapi/README.md) | Serves the Dex Flow UI document operations in process for Studio Runs: catalog, search, Display, edits, Actions, Start and RunID-bearing engine diagnostics. |
| [`studio/flowapi/fdg`](../../internal/studio/flowapi/fdg/README.md) | Defines and validates the Flow Definition Graph 2.0 contract for the Flow API, configuration host and Studio Runs: bundles, typed values, Start input and visibility queries. |
| [`studio/runs`](../../internal/studio/runs/README.md) | Authorizes Studio Runs operations against an exact serving Live deployment or Session Preview for the API and serves them through the Flow API in process. |
| [`tenancy/accountdao`](../../internal/tenancy/accountdao/README.md) | Reads and writes the accounts and login_sessions tables: registration, login admission, token authentication, revocation and the platform admin check. |
| [`tenancy/accounts`](../../internal/tenancy/accounts/README.md) | Registers, logs in, logs out and authenticates accounts, which the database owns, with Argon2id password records and HMAC-signed login tokens. |
| [`tenancy/connectiondao`](../../internal/tenancy/connectiondao/README.md) | Reads and writes the TeamSpace connection, usage and scope token tables: creation, settings, refresh leases, credential commits, usages, deletion and read authorization. |
| [`tenancy/connections`](../../internal/tenancy/connections/README.md) | Reads TeamSpace connection credentials for applications and the platform, refreshing and minting them as manifests declare, and records which Project scopes use which connection. |
| [`tenancy/memberdao`](../../internal/tenancy/memberdao/README.md) | Reads the team_space_members and project_members child tables that the platform Dex Attribute Store writes from the Members maps of TeamSpaceFlow and ProjectFlow. |
| [`tenancy/slackrelay`](../../internal/tenancy/slackrelay/README.md) | Receives the platform Slack application's signed events, routes them to registered scopes, queues and delivers them signed to applications' public trigger routes. |
| [`tenancy/teamspacedao`](../../internal/tenancy/teamspacedao/README.md) | Reads the team_spaces table, which the platform Dex Attribute Store writes from TeamSpaceFlow: a member's TeamSpaces, keyset-paged, and one TeamSpace with its Home counts. |
| [`tenancy/teamspaces`](../../internal/tenancy/teamspaces/README.md) | TeamSpaceFlow owns one TeamSpace: its name, its members and their admin or member role, the registry of its Projects, and its deletion. |

<!-- END GENERATED PACKAGE TABLE -->

Each Flow source file is named for its Flow type, and every Flow has its own
client that fronts exactly that Flow, for example `session_agent_flow.go` with
`SessionAgentFlowClient`, `sessionworkspace/session_workspace_flow.go` with
`SessionWorkspaceFlowClient`, `provision/sandbox_provision_flow.go` with
`SandboxProvisionFlowClient`, `preview/preview_flow.go` with `PreviewFlowClient`,
`project_flow.go` with `ProjectFlowClient`, `setup/project_setup_flow.go` with
`ProjectSetupFlowClient` and `repobinding/repository_binding_flow.go` with
`RepositoryBindingFlowClient`. `SessionAgentFlowBootstrap`
(`session_agent_flow_bootstrap.go`) assembles the Session Agent Flow with its
process-local resources. Preview uses its actual Session scope and never
fabricates a Deployment snapshot or Deployment ID.

`agents` owns no Flow: its `SessionFlowService` (`session_flow_service.go`)
is the Service above the Flows that
joins Project, Session, workspace, provisioning and Preview authority. The
Flows consume it through `sessionagent.SessionAgentFlowDependency`,
`sessionworkspace.SessionWorkspaceFlowDependency`,
`provision.SandboxProvisionFlowDependency`, `preview.PreviewFlowDependency` and
`projects.SessionLifecycle`, and ProjectReleaseFlow and ProjectDeployFlow
report their failures to it through `releases.DeliveryFeedback`;
`delivery/releases` consumes the deployments `PublishingFlowService` through
`releases.ProjectReleaseFlowDependency`, and ProjectFlow starts and cleans up
its Setup through `projects.SetupLifecycle`.
The `api` handlers are the single trust boundary for user input.

Shared packages import no business package, and Attribute, Channel and Stream
definitions stay in each Flow file. Go package paths are not persisted, so the
package layout does not change any Flow, Step or primitive identity.
Hand-written Go files have at most 1000 lines; a Flow DSL file (`*_flow.go`) may
have up to 1600 because the pinned `dexcli` needs every Step, RPC and primitive
of the Flow in that one file.

The business package ownership decision
records the original per-Flow review; the
bounded-context layout decision
records the cycle ports and every Flow change of the later cleanup, and the
package tree and limits decision
records that the later package moves changed no Flow.

### Source authority

| Source | Authority |
| --- | --- |
| Dex skills | `dex-skills/`, Superverse-owned source (the current contract) |
| Platform | Go SDK `v1.5.0`, Server `v1.6.1`, CLI `v1.6.4`; kept at the newest stable release |
| New-project template | `app-template/`, served as `app-template@<tree ID>` (the current contract) |

Do not upgrade generated application dependencies merely because the
platform uses a newer Dex Server. Preserve their declared template versions.

### Infrastructure isolation and domains

Use independent V2 VPC/EKS, IAM roles, KMS keys, buckets, Terraform state,
workspaces, images, and release automation. Resource names/tags follow the authoritative
deployment catalog: test/Native/Kind retain `superverse-v2`
and `sv2`; new staging uses `superverse` and `sv`. IAM permissions remain exact
account/environment/path/tag scoped; no historical V1 resources are adopted.

| Environment | Platform | Project Preview | Project Live |
| --- | --- | --- | --- |
| test | `test.dexai.dev` | `preview-<projectID>.test.dexai.dev` | `live-<projectID>.test.dexai.dev` |
| staging | `staging.dexai.dev` | `preview-<projectID>.staging.dexai.dev` | `live-<projectID>.staging.dexai.dev` |
| prod | `dexai.dev` | `preview-<projectID>.prod.dexai.dev` | `live-<projectID>.prod.dexai.dev` |

Manage only V2-owned DNS records. Do not transfer V1 zone/state ownership.
There is one Preview route per project: the Project's one Preview creates it at
its first start and keeps it until the Project is deleted
([the current contract](agent-harness-design/session.md)).

Use platform namespaces `superverse-test`, `superverse-staging`, and
`superverse-prod`. V1's control-plane/coding-agent namespaces remain untouched.
Each hosted user Live App still has separate Dex, Temporal namespace, bucket,
and workload permissions. User application code cannot access platform state.

Reuse foundation, artifacts, Temporal, platform charts, policy tests, and
operation-journal logic from IaC. Remove RDS without disabling the S3, IAM,
Secrets, or build resources currently grouped under persistence flags. Do not
activate copied organization/runner roots that manage V1 or shared resources.
Read shared account-level GitHub OIDC issuer identity without importing its
ownership; create a V2-specific trust role for the verified V2 repository.

Local Kind uses cluster `superverse-v2`, its own kubeconfig/state root, and
separate ports. Suggested fixed host ports are Studio 23000, business gRPC
28080, and Temporal UI 28233. Record any dynamic ports in V2 state. Use
`docker build` and `kind load docker-image`, with no local registry.

Local Native is the second local lane and uses no Kind cluster. The server,
Studio and the Release builder run on the host; platform Dex, Temporal and MinIO,
the gateway and a local registry run as one Docker Compose project with Kind's
pinned images and configuration; and the server's `docker` runtime
(`runtime: docker`) runs every Sandbox, Preview and Live workload as containers of
the local Docker Engine. It serves fixed loopback ports of its own, apart from
Kind's, and keeps its own locks, Dex data, Projects and repositories, so the two
lanes run at the same time. The
[Local Native decision](../runbooks/local-native.md) defines the
identity mapping, the routes, the generated credentials and the accepted
deviations from Kind. `make required-lanes` names the lanes a change needs; Native
success does not satisfy the final Local Kind gate before an AWS wave or hosted
AWS acceptance.

## Dex Flow Changes

### Identity and Run semantics

Flow/Step engine types derive directly from precise Go types using SDK defaults,
without `GetFlowType`/`GetStepType` overrides. They are the Go type names without
the package, as shown in the inventory below. The [pre-launch identity decision](../flows/README.md)
records the exact Step-name changes and local history reconciliation boundary.
Business FlowIDs are unchanged; the
bounded-context layout decision
records the later pre-launch Step, RPC, primitive and payload changes, which
open executions created before them do not support.

Business IDs identify resources. A FlowID identifies one logical lifecycle.
Dex assigns a RunID to each execution; application code does not generate
RunIDs or use them as resource primary keys.

- A new logical lifecycle creates a FlowID and its initial RunID.
- Retrying the same start reuses FlowID and Start RequestID; it must not
  intentionally create another execution.
- Step retries, ordinary RPCs, Channel messages and credential refresh do not
  create another Flow.
- Normal APIs address the current execution by FlowID. Business RPCs, DTOs,
Stream cursors, UI readiness and effect identity never depend on RunID;
messages, approvals and sequences are durable Flow state. Exact RunID addressing
  is reserved for diagnostics or an explicit history contract.
- Root starts default to disallowing reuse of closed FlowIDs
  (`dex.IDReuseDisallow`). A new operation
  uses a new business request ID. Existing entity Flows accept new commands by
  RPC instead of being restarted.
- Accounts have no Flow, so no account start exists ([Accounts](#accounts)).
  `TeamSpaceFlow` starts with `dex.IDReuseDisallow` and its FlowID is
  never reused ([contract](teamspace/teamspaces-and-roles.md#identity-and-lifecycle)).
  The Project's one `session-workspace-<projectID>` starts with
  `dex.IDReuseDisallow` on the Session's first command, and every command
  confirms the start through its RPC, which an ended execution refuses.
  Every Sandbox instance has its own provisioning FlowID,
  `sandbox-provision-<projectID>-<workspaceRequestID>`, started
  with `dex.IDReuseDisallow`, so a retry of the same request joins its
  execution, also a completed one, and no instance is provisioned twice
  ([the current contract](agent-harness-design/sandbox.md)).
- `PlatformIssueFlow` starts with `dex.IDReuseAllowIfNotRunning` (plus
  same-request IgnoreError) under the FlowID `platform-issue-<env>-<sig16>` of a
  fault's signature: a report while the run is open joins it, and a report
  after it closed starts a new run, whose label query finds the issue the
  earlier run opened ([PlatformIssueFlow](#platformissueflow)).
- Stable FlowID plus different logical start input is a conflict, not a
  successful retry. Query uncertain provider outcomes before repeating work.
- Every Flow type below but the BackgroundCommandFlow SubFlow is an
  independently started top-level Flow, not a parent-child SubFlow. Project `ChildFlows` is cleanup membership, not a
  SubFlow relation. The Session is the Project's one Session, which ProjectFlow
  starts when the Project becomes READY, and its only Session-scoped member; its
  SessionWorkspaceFlow owns its Sandbox instances, and the Session owns its
  subagents, which are no Project children either: one `SubagentFlow` per `start_agent` call, started with
  `dex.IDReuseDisallow` and admitted by its Session in its first Step.

Business IDs are readable contracts:

- The canonical email is the trimmed, lowercase-ASCII email, without
  provider-specific dot/plus rewriting; a login token signs the name
  `account-<canonicalEmail>`. Reject Dex-reserved `/`, `$`, `:`.
- AccountID is a separate opaque identifier with at least 128 random bits, the
  primary key of the `accounts` table; Flows hold it only as a non-PII actor or
  creator reference.
- TeamSpaceID is eight characters derived from AccountID and the logical
  request ID, as ProjectID is, in its own domain
  ([contract](teamspace/teamspaces-and-roles.md#identity-and-lifecycle)).
- ProjectID is server-owned lowercase alphanumeric, configurable length, default
  four; SetupID has eight characters. A domain-separated SHA-256 byte stream over
  AccountID and the logical request ID is rejection-sampled into lowercase base36.
  Retrying the same request derives the same IDs without an allocator, lookup Flow,
  mapping table or collision registry. The complete safe immutable input fingerprint,
  including the full request ID, rejects a short-ID collision instead of reusing a
  different project. Configured ID lengths remain stable for request replays.
- Browser CreateProject requests cannot set project/setup IDs or template revision.
  CREATE uses the application template embedded in the server build, revision
  `app-template@<Git tree ID>` (the current contract).
  A Setup whose accepted revision the running server does not embed fails
  explicitly instead of initializing different content. Accepted
  ProjectSnapshot returns the actual IDs and template revision. This deterministic
  business-ID derivation does not replace readable FlowIDs with opaque hashes.
- A Project has exactly one Session, which its ProjectID addresses; there is no
  Session ID ([the current contract](agent-harness-design/session.md)).
  Deployment IDs are readable alphanumeric values allocated once.
  Scoped SDK storage owns connection and OAuth identities; neither is a platform Flow.
- Release type identity is the full main-branch commit SHA.
  Do not add an unnecessary FlowID hash.
- Start RequestID uses a stable fingerprint of complete canonical non-secret
  logical start input. Exclude timestamps and credentials. Conflicting immutable
  inputs cannot masquerade as an accepted retry. Start first with explicit reuse policy
  and same-request IgnoreError, without a pre-read solely for deduplication or
  snapshot read or fresh-default comparison after acceptance-only success; see [StartFlow admission](start-flow-admission.md).
  The server-selected CREATE template and the Session's server-selected profile
  and model are not logical input; the Session's Start RequestID fingerprints its
  ProjectID alone.
  Release builds use a server-selected immutable profile that each build revision retains.

| FlowType | Exact FlowID; example | Creation and function | Business/engine end |
| --- | --- | --- | --- |
| TeamSpaceFlow | `teamspace-<teamSpaceID>`; `teamspace-k3v9q2ma` | Create by any ACTIVE account; owns the TeamSpace, its members (admin or member) and its Project registry ([contract](teamspace/teamspaces-and-roles.md#teamspaceflow)). | READY; DELETING while the platform administrator's deletion removes its Projects and connections; DELETED, then engine completion. The `team_spaces` row stays. |
| RepositoryBindingFlow | `repository-<repositoryID>`; `repository-123456789` | Managed repository claim; serializes its exclusive ownership and binding generation. | The release after the repository's archive sets RELEASED and completes the engine execution, which stays readable for the engine's closed-execution retention; never claimed again. |
| ProjectFlow | `project-<projectID>`; `project-a7k2` | Create; owns project, app manifests, scoped resources, exact configuration pins, the current Live pointer, the start of the Project's Session when it becomes READY, and cleanup admission. | DELETING during cleanup, DELETED after all owned cleanup and handoffs, then engine completion. |
| ProjectSetupFlow | `setup-<projectID>-<setupID>`; `setup-a7k2-k9m4p7w2` | Accepted create; creates a private managed repository, claims its exclusive binding, initializes the template and extracts manifests. | Completes after permanent results reach Project. Bounded Step retries end in business-terminal SETUP_FAILED with no active Step. The retained execution keeps cleanup references until DeleteProject; cleanup then completes the engine execution. |
| SessionAgentFlow | `session-<projectID>`; `session-a7k2` | ProjectFlow's StartProjectSession once the Project is READY: the Project's one Session, which creates no Sandbox; owns conversation, models, tools and their serial/parallel scheduling, approvals, recovery, Git, the confirmed coding HEAD, the workspace requests, the accepted lease, the Sandbox deadline and the decision that the instance is idle. | Only the Project's deletion archives it (CompleteProjectSession) and completes it; a start whose Project admission is denied completes at once. |
| SessionWorkspaceFlow | `session-workspace-<projectID>`; `session-workspace-a7k2` | The Session's first workspace command; owns the Session's instances: runs its `provision` and `reclaim` commands one at a time, starts each instance's provisioning, reclaims Pods keeping the volume and reports each outcome to the Session. | Only the Project's deletion ends it (CompleteSessionWorkspace); never reused. |
| SandboxProvisionFlow | `sandbox-provision-<projectID>-<workspaceRequestID>`; `sandbox-provision-a7k2-ws-41` | The SessionWorkspaceFlow's provisioning of a new instance; provisions its Pod on the Session's volume, checks the repository out, verifies the instance and reports the outcome to the workspace Flow. | Completes once the outcome is reported; never reused. |
| PreviewFlow | `preview-<projectID>`; `preview-a7k2` | The Project's first Preview command or automatic deployment; serves the Project's one Preview on the Session's current instance, which it acquires from the Session. | A stopped Preview stays a running execution; the Project's deletion ends it. |
| SubagentFlow | `subagent-<projectID>-<agentID>`; `subagent-a7k2-k3n8q5r1` | The Session's `start_agent` call; one background agent that works on a bounded task in its own git worktree and reports to the Session. | COMPLETED, FAILED, STOPPED, TIMED_OUT or DENIED ends it; completes once its report was delivered or its wait passed; never reused. |
| BackgroundCommandFlow | `SubFlow:<sessionFlowID>-<stepExecutionID>-0`, generated by Dex; `SubFlow:session-a7k2-AwaitBackgroundCommand-3-0` | A SubFlow of the Session's `AwaitBackgroundCommand` Step for one background command; polls the Sandbox's command ledger with the accepted lease from its input. | Completes with the command's end (lost after its watch limit or retries); never reused, and never stopped with its parent Step. |
| ConnectorContributionFlow | `connector-contribution-<projectID>-<slug>`; `connector-contribution-a7k2-newsapi` | The Session Service, once the Session admitted the report of a completed `connector_develop` subagent; owns the contribution record and opens its draft pull request in the connectors library. | Only the Project's deletion closes it; a closed contribution is final for that connector in that Project. |
| PlatformIssueFlow | `platform-issue-<env>-<sig16>`; `platform-issue-test-3f9a1c2b7d4e5f60` | A Session's report of a platform fault, beside the turn; files one GitHub issue per signature, or only logs it. Belongs to no Project. | Completes once the outcome is recorded; a later report of the signature is a new run. |
| ProjectReleaseFlow | `release-<projectID>-<commitSHA>`; `release-a7k2-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa` | Prepare source and admit immutable build revisions for one commit draft. | READY builds remain available while later builds run; Project deletion closes the owner. |
| ProjectDeployFlow | `deploy-<projectID>-<deploymentID>`; `deploy-a7k2-d6m4p7w2` | Deploy Live; applies exact Release/config, provisions/reuses resources, grants access, verifies and commits Live; retires its generation after a newer one replaced it. | READY/FAILED/REMOVED ends that business operation; RETIRED completes the execution; receipt retained until project deletion. |

All project subordinate FlowIDs use `<type>-<projectID>-<typeID>`, excluding
repository/project. Accounts have no Flow ([Accounts](#accounts)). Refreshing
credentials does not create a new Flow.
Explicit new deployments do. A new Sandbox instance after a reclamation or a
lost transport has a new `WorkspaceRequestID` and its own provisioning.

### Indexed Attributes and listing

There is no `Deleted` Attribute and no `bool1`/`IndexBool` application index.
Business status is the single authority for deletion and resource visibility.

| Physical slot | SDK type | Logical Attribute | Required query |
| --- | --- | --- | --- |
| keyword2 | IndexKeyword | ProjectID on Project and project subordinate Flows | Project-scoped releases and deployments, and a deleting Project's subagents. |
| keyword3 | IndexKeyword | The Flow's Binding/Project/Setup/Session/Sandbox/Subagent/Release/DeploymentStatus | FlowType-qualified business-state filtering, such as the subagents that have not ended. |

No other Attributes are indexed; keyword1 is unused, because ListProjects reads the
read model's `projects` table. CreatedAt/UpdatedAt remain display timestamps.
Do not index email, name, secrets, configuration, message content, provider
identity or AttributeMaps. Dex's system indexes keep their reserved names.

```sql
-- A deleting Project's subagents that have not ended
FlowType = 'SubagentFlow'
AND keyword2 = 'a7k2'
AND keyword3 IN ('INITIALIZING', 'RUNNING', 'FINISHING')
```

DELETING projects remain visible for cleanup progress. REMOVED deployments
remain inspectable; REVOKED connections can be reauthorized.
Authorize the parent Project before subordinate search. Search discovers candidate
executions, then typed current reads verify scope/state and deduplicate FlowID.
Do not exclude failed current executions by searching only Running.

Use one tested query-literal encoder, never raw interpolation. Default page size
50, maximum 100 candidates; opaque cursors bind principal/filter/page size.
Filtering stale rows may yield short/empty pages with a next token. Creation
responses supply immediate UI state without waiting for index propagation.
Use engine-supported default ordering; do not promise global UpdatedAt/CreatedAt
sorting, offset/page numbers, or snapshot-consistent pagination.

### Primitive conventions

- **A** means a singleton typed Attribute, with no instance key.
- **AM** means AttributeMap, with the exact instance key specified below.
- **C** means a singleton Channel, with no instance key.
- **CM** means ChannelMap, with the exact instance key specified below.
- **S** means Stream, addressed by FlowID and Stream name, not an instance key.

A struct containing a dictionary is still one A; it is not automatically an
AM. Use AM only for independently loaded, locked, or deleted records. Register
every primitive in PersistenceSchema. Dynamic read-modify-write must select
the same exact instance for loading and locking.

Use only the three confirmed Keyword slots above for owner/project/status lists. Never use search as a uniqueness constraint, an authorization grant, or
a replacement for canonical entity reads. One fact has one authoritative owner.

ProgressEvent contains the source FlowID, phase, related revision, time and
detail. The phase is the Step name only; a waiting Step's provider state or
pending reason goes into the detail, which can name internal resources and is
never mapped to the API. Progress Streams are best effort; use a 10 MiB declaration per Flow type/Stream
name, not a promised 10 MiB per individual Flow. Resume tokens are opaque;
after loss/trimming, reload Attributes through the read RPC.

Execute owns external effects; WaitFor declares durable conditions. Default
short handlers to ASYNC, and explicitly configure SYNC for model calls and
known long operations. Before implementing each Step, declare its timeout,
heartbeat, retry budget, exact loads/locks, and recovery route. Preserve the
verified V1 policies where the operation is inherited. RPCs remain short and
schedule durable provider work rather than executing orchestration inline.

### Exact value types and execution policies

Each Flow additionally registers `StartIdentity`, `Revision: uint64`,
`CreatedAt: time.Time`, and `UpdatedAt: time.Time`. StartIdentity is the shared
`dexflow.StartIdentity` (accepted request identity, non-secret fingerprint and
actor), or the fingerprint string in SessionAgentFlow. SandboxProvisionFlow keeps
only its immutable `Request`, and PreviewFlow registers Revision, CreatedAt and
UpdatedAt without a StartIdentity: neither FlowID is ever reused, so a different
start of it fails instead of joining. There is no common Deleted.

Shared types: ObjectRef contains exact bucket/key/version/digest/media type;
SecretRef is opaque and internal; `dexflow.Failure` is a sanitized domain failure;
ProjectCommand contains command ID, kind, expected revision and phase.
ProgressEvent includes
source Flow, related revision, phase, time and the unmapped diagnostic detail.
Its revision is correlation, not proof of commit.

The following is the complete singleton type inventory beyond the common fields;
Map/Channel/Stream declarations and exact keys remain in each Flow section.

| Flow | Singleton Attribute types |
| --- | --- |
| TeamSpace | `Name/TeamSpaceStatus/CreatorAccountID: string`; `ActiveCommand/LastCommand: optional<Command>`; `LastFailure: optional<dexflow.Failure>`; the `Members` and `Projects` AttributeMaps and the `TeamSpaceCommands` Channel are in the [contract](teamspace/teamspaces-and-roles.md#attributes-and-attributemaps) |
| RepositoryBinding | `RepositoryID: int64`; `BoundProjectID/BindingStatus/AcceptedClaimID: string` (BoundProjectID is empty while unbound and keeps the releasing Project once released); `BindingGeneration: uint64` |
| Project | `ProjectID/CreatorAccountID/Name/TemplateRevision/SetupFlowID/TeamSpaceID/ProjectStatus: string`; `RepositoryBinding: RepositoryBindingRef`; `AppManifest: optional<AppManifestSnapshot>`; `ActiveDeployment: optional<DeploymentLease>`; `CurrentLive: optional<LivePointer>`; `ProjectStorage: infra.StorageRefs`; `LiveExecution: infra.ExecutionRefs`; `ActiveCommand/LastCommand: optional<ProjectCommand>`; `LastFailure/SessionStartFailure: optional<dexflow.Failure>`; the `Members` AttributeMap ([contract](teamspace/teamspaces-and-roles.md#projectflow-changes)) |
| ProjectSetup | `ProjectID/SetupID/SourceCommit/TemplateRevision/SetupStatus: string`; `SourceRepository: RepositoryBindingRef`; `SetupPhase: string`; `AppManifestResult: optional<AppManifestSnapshot>`; `ProjectDeletionRequested: bool`; `Failure: optional<dexflow.Failure>` |
| SessionAgent | `ProjectID/StartIdentity/HeadCommit/CloseReason/SessionStatus/Profile: string`; `SessionModel: SessionModel`; `AgentState: AgentState`; `WaitingInputRound: WaitingInputRound`; `ContextSegment: ContextSegment`; `TodoList: TodoList`; `ConfirmedRequirements: ConfirmedRequirements`; `PendingApproval: PendingApproval`; `PendingTimer: PendingTimer`; `PendingUserInput: PendingUserInput`; `AgentInterrupt: AgentInterrupt`; `NotificationReceipts: []string`; `Workspace: SessionWorkspace`; `WorkspaceUse: WorkspaceUse`; `PreviewHandoff: PreviewHandoff`; `PreviewAutoDeploy: bool`; `MessageWindowLock: bool` |
| SandboxProvision | `Request: Request`; `Resources: sandbox.Resources`; `Checkout: Checkout`; `WorkspaceTimings: []dexflow.PhaseTiming`; `Outcome: optional<Outcome>` |
| Preview | `ProjectID/WorkspaceRequestID/WorkspaceState/HeadCommit: string`; `Identity: sandbox.Identity`; `Generation: uint64`; `Resources: sandbox.Resources`; `AppManifest: optional<AppManifestSnapshot>`; `PreparedPreviewSource: PreparedPreviewSource`; `PreviewState: PreviewState`; `PreviewEnvironment: PreviewEnvironmentState`; `PreviewConfigurationRef: PreviewConfigurationRef`; `ActiveCommand/LastCommand: Command`; `Failure: optional<dexflow.Failure>`; `PreviewDeployment: PreviewDeployment`; `OutcomeReport: optional<SessionNotification>`; `CommandLock: bool` |
| ProjectRelease | `ProjectID/ReleaseID/SourceCommit/BuildProfileVersion/BuildProfileDigest/ActiveBuildRevisionID/ReadyBuildRevisionID/ReleaseStatus: string`; `RepositoryBinding: projects.RepositoryBindingRef`; `PreparedSource: PreparedReleaseSource`; exact-entry `ReleaseBuildRevisions` AttributeMap of `ReleaseBuildRevision`; `CancellationRequested: bool`; `Failure: optional<dexflow.Failure>` |
| ProjectDeploy | `ProjectID/DeploymentID/ReleaseID/BuildRevisionID/DeploymentStatus/DeploymentURL: string`; `ConfigurationRevision/ExpectedLiveRevision: uint64`; `PinnedInputs: Inputs`; `ProjectDeploymentLease: projects.DeploymentLease`; `DeploymentResources: DeploymentResources`; `WorkloadGrant: WorkloadGrant`; `ProviderOperationRefs: ProviderRefs`; `PhaseTimings: []dexflow.PhaseTiming`; `ReadinessResult: Readiness`; `CleanupRequested: bool`; `Failure: optional<dexflow.Failure>` |

Single-fact ownership follows the Flow table. Every public subordinate operation
authorizes through Project; retained child fields are not another owner authority.
Map-wide invariants use the corresponding registered non-indexed Boolean Attribute
as a coordination lock, not a hand-rolled Boolean flag. All Step/RPC/timeout writers
participate. Exact instance mutations exact-load and lock the same key. Revision
CAS fences concurrent resources/leases/commands. The Go configuration host and
standard SDK own scoped mutable configuration revisions. Entity growth lasts until explicit
removal, bounded message retention, expired-login pruning, or child handoff;
no elapsed-age cleanup can remove still-referenced entity/blob state.

| Work | Mode, timeout and recovery |
| --- | --- |
| New state-only Steps | ASYNC; 30-second method timeout; `dexflow.StepRetry`: retries one second apart at first, doubling up to 30 seconds, for two minutes without an attempt cap, so a Worker restart or Pod replacement, whose refused attempts count, never exhausts them; then explicit recoverable failure. A Step whose attempts are expensive, such as a model call, keeps an attempt cap through `dexflow.CappedStepRetry`, whose retries start 30 seconds apart. |
| WaitFor | ASYNC bounded handler returning durable Conditions for Flow business signals (Channels, RPCs) and business timers (TTL, inactivity) only, never for an external system; timeout does not bound the durable wait. |
| New short RPC | Ten-second deadline, no long provider work inline; only identity-safe/reconciled retries. |
| Provider calls | SYNC when normally over five seconds; 30-second request deadline; classify transient/idempotent/reconciled retries. |
| Build/provision/readiness | Derive the external operation identity deterministically, then wait in one lock-free, long-running Execute: `dexflow.PollUntil` runs a round within its own timeout (10 seconds for one call, up to 40 seconds for several), heartbeats and sleeps a few seconds (10 for a build), under `dexflow.PollingStepOptions(maxWait)`: SYNC, ExecuteMethodTimeout and ExecuteRetry.TotalDuration (measured from the first attempt, across every attempt) both maxWait, a one-minute heartbeat timeout and five attempts with backoff. The engine enforces these wait limits; code never computes or stores a deadline. A passed limit fails the Step into its Execute failure route, whose failure-record Step chooses the business failure code from the failed Step and `dexflow.RecoveryTimedOut`. "Not ready" is never an error, and a short locked Step commits the result. A business condition that is not a fixed duration, such as a lease the Flow does not own, is an explicit check in the loop. No WaitFor timer re-enters a Step and no retry policy or `RetryAfter` serves as polling. Wait limits: release build 60 minutes; source preparation 5.5 minutes (4 for the provider and 45 seconds before and after); Live infrastructure 15 minutes; readiness 10 minutes; Sandbox provider waits, each cold-path Step of a Session workspace request, Session workspace release and cleanup 30 minutes; project configuration storage 10 minutes; the Sandbox checkout 15 minutes (5 for sandboxd reachability and 10 for Git). |
| Cleanup | Query-before-delete by identity/generation; NotFound on delete is success. Convergence polls like the row above until its wait limit, then a retained failure and explicit retry. |
| Model/tools | Preserve imported policies. Model SYNC, ten-minute attempt/30-minute total budget; the call heartbeats every 20 seconds while it runs, so silence before the first event is valid. Tools retain declared deadlines/progress/uncertainty recovery and heartbeat every 20 seconds while they run. |
| OAuth exchange/refresh | No blind repeat of uncertain non-idempotent effects; reconcile secret versions or require reauthorization. |

External operation identity derives from FlowID, Step purpose and generation.
Uncertain results query the provider/domain authority before another mutation.
Internal GetDexSummary/GetDexDisplay return safe state; manual actions have
state conditions and permissions. Login metadata/secrets and underlying maps
are never exposed through generic summaries.

<a id="accountflow"></a>
### Accounts

Accounts and login sessions are rows of the PostgreSQL `accounts` and
`login_sessions` tables, written only through `internal/tenancy/accountdao`; no
AccountFlow exists ([accounts](teamspace/accounts.md),
[the current contract](teamspace/read-model.md) and the
[Account service](../../internal/tenancy/accounts/README.md)). The canonical email,
unique per account, trims surrounding whitespace and lowercases ASCII, without
provider-specific dot or plus-tag rewriting. An account holds a random AccountID,
the Argon2id `password_record`, a credential revision and an authentication epoch;
a login holds only the SHA-256 of its token, its issue and expiry times and the
epoch.

Registration hashes the password first and inserts the account in one statement.
Replaying the same request with the same password logs in; any other registration
of the email, or a replay with another password, is rejected as already existing,
and knowing the request ID alone never authenticates. Concurrent registrations of
one email meet the unique email index, and one wins. Login verifies the record and
admits a login row only for an ACTIVE account still at the verified credential
revision.

Password hashing and verification stay inside the trusted Go credential code. The
record (Argon2id algorithm, version, parameters, random salt and hash) is a JSONB
column; plaintext never enters Flow payloads, Attributes, Channels, Streams, logs
or browser responses, and the hash is omitted from public DTOs. There is no
password length policy; empty and arbitrarily long passwords within the transport
message limit are accepted, and the record structure and parameters are still
validated.

Login issues a random bearer secret in a signed HttpOnly, SameSite=Lax cookie;
hosted cookies are Secure. The lifetime is a fixed seven days, without per-request
sliding writes. The cookie carries account and login identities plus the secret;
the database stores only the secret's hash and the expiry. Administrator
authorization is derived server-side from the configured platform administrator account (`accounts.platformAdminEmail`)
account identity, not from browser-provided roles.

Accounts that existed as AccountFlow executions are not migrated: the executions
are ended and users register again
(runbook).

### Caller authentication

Every authenticated request is checked against the database; there is no cache and
no cache setting. The server verifies the token's HMAC, expiry and account
binding, then one query joins `login_sessions` with `accounts` and must find an
unexpired login with the same token hash and authentication epoch, for an ACTIVE
account whose AccountID is the caller's. Logout deletes the exact login row, so it
takes effect on the next request of every server instance, and a login whose epoch
differs from the account's is rejected. Flow API operations authenticate the same
way before fresh resource authorization, and no BFF scope cache exists. A Watch
authenticates and authorizes when it starts and every 5 seconds on its own
ticker, which ends it on failure, rather than on every streamed event
([Watch authorization](runs-target-cache-and-watch-authorization.md#watch-authorize-per-connection-on-its-own-ticker)). The Account service owns
the real PostgreSQL tests of registration, login, logout and rejection.

### RepositoryBindingFlow

FlowID: `repository-<repositoryID>`, using the repository host's immutable numeric
ID: GitHub's, or the monotonic, never reused ID of the
[local Git host](../../internal/platform/repohost/localgit/README.md) in the local lanes.
Lifetime: UNBOUND from its start (generation 0) until the Setup's claim, BOUND
to one Project's generation, then RELEASED, which completes the execution.
Every release follows the archive of the repository (Project deletion and
Setup cleanup both archive first), so a completed binding means an archived
repository. The closed execution answers GetRepositoryBinding until the engine's
closed-execution retention passes; `RepositoryBindingFlowClient.Find` then
returns nil. That absence is safe only because a repository ID is never reused
and the Setup refuses an archived repository before it claims one, so the
FlowID is never claimed, and so never started, again.

**A:** RepositoryID, BoundProjectID, BindingGeneration, BindingStatus
(`UNBOUND`, `BOUND`, `RELEASED`; indexed keyword3), AcceptedClaimID, Revision.

**AM:** None. **C / CM:** None. **S:** None.

**Steps:** CompleteRepositoryBinding, execute-only and reached only from
ReleaseRepository: it returns graceful completion with the binding
(`dexflow.StateStepOptions()`, no lock, no external effect).

**RPCs:** ClaimRepository, GetRepositoryBinding, ReleaseRepository.

Claim/release lock the binding state. Claim of a RELEASED binding is
FAILED_PRECONDITION; a Claim that finds the execution closed (its StartFlow joins
the ended execution under the same RequestID, so no Run starts) is classified
the same by the client. Release must match the generation and, unless the
binding is UNBOUND (generation 0 for a claim whose start committed and whose RPC
never did, or above 0 for a binding the earlier release cleared), the ProjectID;
any other release is ABORTED. Release
records RELEASED with the releasing ProjectID and moves into
CompleteRepositoryBinding. A repeated matching release answers from the open
RELEASED state; one that finds the execution closed is confirmed by the client's
retained read of RELEASED with the same ProjectID and generation, never by the
inactive error alone. Managed repositories remain exclusive without a SQL
uniqueness table or search-before-create race.

Open executions from before the terminal release: a BOUND binding completes at
its Project's deletion; an UNBOUND binding released by the earlier code (generation
above 0) completes when the retried deletion of its still DELETING Project releases
it, and otherwise is stopped once by the operator, following the
rollout reconciliation.

### ProjectFlow

Every Project owns a private managed repository and its exclusive binding
generation. The repository's DefaultBranch is the sole source branch. The
binding is an ordinary Attribute value, not an AttributeMap: RepositoryID and
InstallationID:int64; FullName, URL and DefaultBranch:string; Generation:uint64.
Project and Setup admission carry no repository seed; their repository Attributes
start empty and only Setup records the managed provider identity.
There is no alternate working branch, pinned import base or repository ownership
selector. See [the owning inventory](../../internal/projects/README.md).

FlowID: `project-<projectID>`. Lifetime: authoritative project entity through
explicit deletion and resource cleanup.

**A:** ProjectID, CreatorAccountID, TeamSpaceID, Name, ProjectStatus, RepositoryBinding,
TemplateRevision, SetupFlowID, AppManifest, ActiveDeployment, CurrentLive,
ProjectStorage, LiveExecution,
ActiveCommand, LastCommand, LastFailure, SessionStartFailure, CreatedAt,
UpdatedAt, Revision. CreatorAccountID is the creating account, a fact only;
TeamSpaceID is the Project's TeamSpace. All of them but ProjectID and
LastCommand sync to the read model's `projects` row through the Attribute Store
`projects` (row key `project-{project_id}`), which ProjectFlow selects where it
starts.

**AM:** `Members`, keyed by AccountID, holds each member's role (OWNER, BUILDER
or OPERATOR), when and by whom it was added; it syncs as the child table
`project_members` and stays after deletion. The creator starts as its one owner.

The start Step `RegisterProjectInTeamSpace` registers the Project with its
TeamSpace, deletion releases the Project's connection usages and scope tokens
(`ReleaseConnectionUsages`), and AddProjectMember, SetProjectMemberRole and
RemoveProjectMember change `Members`
([ProjectFlow changes](teamspace/teamspaces-and-roles.md#projectflow-changes)).

CurrentLive is a struct of deployment/release/configuration identities and
generation. ProjectStorage (`infra.StorageRefs`, snapshot field
`Storage`) holds the project's configuration storage: the namespace and its
UID, the bucket, its key prefix and, in Local Kind, the MinIO Service.
LiveExecution (`infra.ExecutionRefs`) holds the Live execution environment:
the Temporal namespace, the credential that reaches it (Local Kind: the
Temporal Service; hosted: the `temporal-credential[-<generation>]` Secret)
and the Live Dex Service. Both are ordinary structs without secrets or index,
loaded with the snapshot, of constant size and retained for the Flow's
lifetime. Each is extend-only: a recorded UID or name is added, never
replaced. The one exception is a rotated hosted API key: a credential whose
`Generation` is the next one in the same boundary replaces the recorded one. Setup is the
only writer of ProjectStorage, and the deployment that holds the active lease
the only writer of LiveExecution, so Setup prepares only storage and the first
Live deployment prepares the execution environment that later deployments
reuse (the current contract).
A deployment's generation Service belongs to that deployment, not to Project.
The Go configuration host owns scoped mutable configuration; each Release
build owns its exact frozen configuration reference. Project holds current pointers rather than an
unbounded list of all releases or deployments.

Project Get/Watch snapshots expose only the ID and generation of the existing
ActiveDeployment lease. A returning Studio page uses that reference to observe
the owning Deployment directly; it must not infer an in-progress replacement
from CurrentLive or a visibility listing. Deployment status remains exclusively
owned by ProjectDeployFlow. The additive response field changes no persisted
primitive, index, Step, FlowID or RunID.

**AM:** ChildFlows<ChildFlowReference>, keyed by full subordinate FlowID, e.g.
`session-a7k2`. Value: child FlowID and FlowType only.
This is authoritative cleanup membership, not copied child business state, a
listing projection, or start deduplication. Exact load for registration/removal;
deletion may enumerate. Temporary children remove entries after acknowledged
handoff; retained entities stay until project cleanup.

Members are Setup, Session, Release and Deployment executions. The Session,
`session-<projectID>`, is the only Session-scoped member: its admission calls
`ProjectFlowClient.RegisterSessionChild(projectID)`, which registers
`SessionAgentFlow` under that key only and clears a recorded
SessionStartFailure; its workspace Flow, its provisionings, its Preview and
its subagents are never members. Its SessionWorkspaceFlow owns its instances,
and the deletion ends that Flow and releases every resource they leave behind
by label through the Session's cleanup pass.

Eventually consistent search cannot prove complete cleanup membership. Every
child registers before effects; registration and project deletion share the
Revision lock. Deletion freezes admission. Rejected children acquire
no external resources. A Sandbox instance is fenced through its owning Session
instead: its first Step asks the Session for admission before any provider
effect, and a Session that left ACTIVE, or whose current request is another
one, denies it.

**C:** ProjectCommands<ProjectCommand>: command ID, kind, phase and expected
revision. Kinds: Live removal (`REMOVE_LIVE`) and project deletion (`DELETE_PROJECT`). Resource RPCs produce, AwaitProjectCommands consumes.
Current/last command identity fences duplicate admission; stale revisions fail.
Deletion blocks new work and converges pending commands into cleanup.

**CM:** None.

**S:** ProjectProgress<ProgressEvent>, produced by the command phase runner during
Live removal and deletion; consumed by the resource-specific WatchProject API. Project Attributes are canonical.

**Steps:**

```text
StartProjectSetup -> AwaitProjectCommands
  REMOVE_LIVE: RemoveLiveServing -> RevokeDeploymentGrant -> CommitLiveAbsent
  DELETE_PROJECT: RemoveLiveServing -> RevokeDeploymentGrant -> CommitLiveAbsent
    -> StopAndCleanChildren -> DestroyLiveDataAndInfrastructure
    -> ReleaseConnectionUsages -> ArchiveManagedRepository -> ReleaseRepositoryBinding
    -> CompleteDeletion
RPC CommitProjectSetup, the Project READY -> StartProjectSession
  Execute retries exhausted -> RecordProjectSessionStartFailure
```

StartProjectSetup independently starts the fixed accepted SetupFlowID from Project
Attributes, through `SetupLifecycle.StartForProject` (`setup.ProjectSetupFlowClient`), in a SYNC Execute (30-second method deadline, 20-second call deadline,
two minutes of retries), without holding a Project lock. Exact Start RequestID
and IgnoreError make uncertain responses retryable. RecordProjectSetupStartFailure
uses the existing Revision lock to retain SETUP_FAILED and a safe LastFailure;
DeleteProject remains available. A late exact Setup handoff sets READY and clears
only that start failure. No new primitive/channel/stream/index is introduced.
Existing waiting orphan Projects are not automatically migrated. The full changed
Step contract and real interrupted-client source are in
[the owning inventory](../../internal/projects/README.md#dex-flow-changes).

StartProjectSession starts the READY Project's one Session,
`session-<projectID>`, through `SessionLifecycle.StartForProject`
(`SessionFlowService`) and `SessionAgentFlowClient.StartAccepted`, in a
provider Execute without a Project lock. The Start RequestID is
`flowid.Fingerprint(struct{ProjectID string})`, with `dex.IDReuseDisallow` and
same-request IgnoreError, so a retried or replayed Step and a repeated
CommitProjectSetup join the one execution; a Project that is no longer READY,
because its deletion began, starts none. Exhausted retries go to
RecordProjectSessionStartFailure, which records `SESSION_START_FAILED` under
the Revision lock while the Project stays READY, unless the Session already
registered itself. It is the Project's own SessionStartFailure Attribute, not
LastFailure: a command's failure, its retry and its success own LastFailure and
leave the start failure, which only the Session's admission clears, so a start
whose every response was lost recovers by itself
([the current contract](agent-harness-design/session.md)).

CompleteDeletion alone loads the complete ChildFlows Map under the Revision
lock, proves it empty, writes ProjectStatus=DELETED and closes the
engine execution. Ordinary CommitProjectPhase writes do not load the Map.
DELETING remains observable while cleanup is incomplete.

ArchiveManagedRepository reconciles the repository host's archive effects in one SYNC polling
Execute with the same 30-minute cleanup budget, 40-second rounds and one-minute
heartbeat timeout as other converging cleanup phases. Each call reads the exact
owned repository before archiving it; an archived or absent repository is done. The host
reports an archive whose effect is unknown as `repohost.ErrOutcomeUnknown` (GitHub: HTTP 5xx,
uncertain transport/response outcomes, HTTP 409 and an archive request answered 404). Such an
outcome stays pending, with disposable two/four/eight-second capped backoff within subsequent
rounds, and re-reads before another mutation. Other refusals record a deterministic failure
(`ErrPermissionDenied`: PERMISSION_DENIED; `ErrRejected` and `ErrNotFound`: PROVIDER_FAILURE),
retaining the host's cause. A binding recorded under another repository host than the server's
is refused before any archive call (`ErrOtherHost`: REPOSITORY_HOST_MISMATCH, naming the host
to run the server with), since this server's host would read the repository as absent and
archival would succeed without archiving it. Budget expiry records
CLEANUP_TIMEOUT. No primitive, RPC, Flow/Step identity or persistence shape
changes; the [owning Step contract](../../internal/projects/README.md#dex-flow-changes)
describes cancellation, retry and compatibility. The repository host owns repository archival
state; ProjectFlow alone owns deletion admission, phase and completion.

Repeated DeleteProject requests while DELETING resolve to the existing admitted
deletion command ID, even after the browser loses its original request ID. The
API still requires a nonempty request ID of at most 128 bytes, and each command
RPC sets its own kind.
Running or queued cleanup joins without publishing another command. A failed
cleanup can resume only with the current Project revision under the existing
Revision lock; it requeues that same command and preserves all
external cleanup identities. A concurrent recovery joins after the first queues
it, and stale revisions cannot resume a later failure. This adds no FlowID,
RunID, primitive, status, timer or public recovery API. Project retains failure
and cleanup responsibility until reconciliation succeeds.

During `StopAndCleanChildren`, a call that reads, cleans up or acknowledges a
child and ends before the round learns its outcome leaves that child pending:
the pinned SDK's typed `RPCLockConflictError` and, while the round goes on, a
call cut off by its own bounded wait (such as a Session's stop or completion
call past the two-second Project cleanup lock wait), canceled or answered
`Unavailable` by the Dex Server. Every such call is idempotent and confirmed by
the child's state, its cleanup receipt or the membership, which the next round
reads before it calls again, so it is never recorded as `OUTCOME_UNKNOWN`. The
Step's two-second polling rounds, rotating eight-child batches and the engine's
30-minute wait limit own the wait; a pending child neither acknowledges
membership nor consumes the provider-failure retry budget, and one still
pending at the limit ends the command as `CLEANUP_TIMEOUT`. Generic `ABORTED`,
domain conflicts and other provider errors retain their existing error paths.

A Session child is cleaned by one reconciliation pass of
`SessionFlowService.CleanupProjectSession` per `StopAndCleanChildren` pass. It
requires the Project to be DELETING and proceeds in this order:

1. A running Session is completed once: `CompleteProjectSession` cancels
   every branch, a workspace command and its wait included, archives it and
   completes it. A Session whose engine already ended abnormally is not
   asked, and one archived for the cleanup is not asked again.
2. The Session's SessionWorkspaceFlow is ended (`CompleteSessionWorkspace`),
   which refuses later commands and cancels a provisioning or reclamation,
   and a provisioning of the instance it recorded that may still run is
   waited for, so that nothing is created after the release. The workspace
   Flow is read again once the Session completed, and the pass answers true
   only once it has ended too.
3. Every subagent of the Session that has not ended is stopped and waited for:
   subagents are not Project children, so they are found by FlowType,
   `ProjectID` and status.
4. The Project's PreviewFlow is ended (`EndPreview`); its resources are released
   only once it no longer runs.
5. Every resource of the Session is released by label and owner through the
   instance provider: the namespace, volume and Sandbox found by their
   deterministic names, the Pod, the Preview's route objects that no stop
   removed, the Project's Preview data in the Live storage, the volume and
   the namespace. Neither a finished Session nor a
   missing provisioning alone proves that no resource remains.
6. A registered Session child without an execution is an error, never an
   acknowledgement. Failed (including a server-side timeout), Canceled and
   Terminated Session executions may be acknowledged only after the release,
   the exact membership and the current Project DELETING admission are
   rechecked. Every other Session must return the matching Project/Session
   cleanup receipt of `CompleteProjectSession`; missing engines and wrong
   receipts remain failures. No terminal state alone proves resource cleanup.

The pass addresses the provisioning and the Preview by their deterministic
FlowIDs and the resources by label, so Project deletion does not depend on the
Session's Steps being runnable. A typed refusal keeps its classification: a
provider's access denial, an owner or UID conflict, a missing owner or a
receipt that does not match. A Project that still holds a
`session-<projectID>-<sessionID>` or `sandbox-<projectID>-<sessionID>`
membership entry from an earlier definition is not migrated or aliased; the
rollout deletes such Projects before the deployment
(rollout reconciliation).

The same rule applies to the existing REMOVE_LIVE command while Project is READY.
RemoveLive first fences the Live pointer the caller observed, its deployment ID
and revision; its Project RPC joins or recovers the original removal command
under the current Project revision. The Project's `live.is_removing` stays true
while cleanup is failed, so proxy access stays blocked; LastFailure permits an
explicit retry and does not imply that serving or grant revocation succeeded.

**Cleanup failure and provider reconciliation:** every command phase takes the
internal `projectPhaseInput{Command, Phase, Failure}`; `Failure`
(`*dexflow.Failure`) carries a classified failure to RecordProjectFailure. The
waiting phases (RemoveLiveServing, RevokeDeploymentGrant, StopAndCleanChildren,
DestroyLiveDataAndInfrastructure) are each one long-running Execute without
Project locks that polls its effect through `dexflow.PollUntil` every two
seconds under `dexflow.PollingStepOptions(30m)`. A round makes several cloud,
Kubernetes and child Flow calls, so its timeout is 40 seconds. The engine owns
each phase's 30-minute wait limit across every attempt; a phase still pending
at the limit fails into RecordProjectFailure, which records `CLEANUP_TIMEOUT`
when `dexflow.RecoveryTimedOut` reports the limit and `PROVIDER_FAILURE` for
exhausted retries. Every effect of these phases queries its exact identities
before it mutates, so a call that goes unanswered while its round goes on, cut
off by its own bounded wait (a Kubernetes call after 30 seconds, a hosted cloud
call after 20, a deployment snapshot read after 15), canceled or answered
`Unavailable` by the Dex Server, leaves the phase pending for the next round,
as a round cut off by its own timeout does. `OUTCOME_UNKNOWN` remains for an
outcome that cannot be confirmed, such as a Sandbox cleanup that recorded one.
ReleaseConnectionUsages does not poll; its unanswered, idempotent release goes
to the Step's retry policy. ReleaseRepositoryBinding runs after the archive and
does not poll; it ends the binding, which completes the RepositoryBindingFlow,
and its unanswered, idempotent release goes to the Step's retry policy. A
progress entry is
written when a phase starts. Project exclusively owns LastFailure, SessionStartFailure,
ActiveCommand and ChildFlows. No other durable owner is introduced.

`ProjectDeployFlowClient` consumes the injected `Serving` capability directly to remove Live serving.
Its empty Flow reference identifies SDK methods and never supplies providers.
Bootstrap validates the infrastructure settings where it reads them (catalog
environment, Local Kind only in test, the hosted provider of the same
environment and digest-pinned project images) and reaches the Kubernetes API
where its settings are read, before any listener admits work. Namespace cleanup validates canonical name, owner/scope
labels and retained UID; a same-UID deletionTimestamp yields pending before any
namespaced reads. A typed namespaced 403 rechecks this exact namespace: absent
permits namespaced cleanup to finish, terminating remains pending, and Active,
wrong-scope/owner or replacement UID remains failure. Hosted cleanup continues
with the retained UID to verify/delete external identities, Temporal and buckets
when the namespace is absent. Normal configuration/grant admission reads retain
their stricter active-namespace semantics. No RBAC expansion is introduced.

At Execute boundaries, known typed permission/ownership/missing-owner failures,
local missing-provider defects, interrupted/uncertain effects and an expired
cleanup wait limit route to RecordProjectFailure with an authored safe message and code.
These cases require reconciliation or correction before an explicit retry, and
are not marked recoverable automatically. The message ends with the error
text, truncated to 2 KiB. Unknown errors end the attempt with a stack and keep
the phase's retry policy; retry exhaustion writes PROVIDER_FAILURE with the final
error detail from `RecoveryError()` and retains the operation. Pending results
poll again within the thirty-minute wait limit, and child cleanup rotates through
eight-child rounds. Streams remain best-effort with the existing 10 MiB budget shared by
FlowType/Stream name; snapshots remain authoritative. Current-revision explicit
recovery resumes the original admitted command, regardless of the advisory
Recoverable flag, after the caller addresses the displayed prerequisite.

The authorized 2026-10-03 hosted history purge was an environment-clearing
exception after external reconciliation, not the normal cleanup implementation.
Normal Project completion still requires all cleanup and empty membership;
normal Session archival remains distinct from engine completion.

**RPCs:** GetProject, CommitProjectSetup, RecordProjectStorage,
RegisterChildFlow, GetChildFlow,
AcknowledgeChildCleanup,
AcquireDeployment, RecordLiveExecution, CommitLiveDeployment, ReleaseDeployment,
RequestLiveRemoval, RequestProjectDeletion.

The resource RPCs hold the Revision lock and hand over external identities in
every polling round, so a failed or interrupted owner leaves nothing without a
Project owner:

- RecordProjectStorage(`ProjectStorageUpdate{SetupFlowID, Expected, Storage}`)
  is admitted for the Project's own Setup while the Project is not DELETED, so
  an in-flight Setup keeps handing off during deletion.
- RecordLiveExecution(`LiveExecutionUpdate{Lease, Expected, Execution}`) is
  admitted while the Project is READY or DELETING, its ProjectStorage is
  complete and the ActiveDeployment is exactly `Lease`. It cannot race Live
  removal: RemoveLive is ABORTED while a lease exists, AcquireDeployment is
  ABORTED while a removal runs, and a lease taken between RemoveLive's read
  and its invoke changes the revision RemoveLive admits against.
- Both answer an equal value as success, an `Expected` that is not the current
  value as ABORTED and a replacement of a recorded identity as
  FAILED_PRECONDITION.
- CommitProjectSetup requires complete ProjectStorage before READY: the
  Project is READY when its code and configuration storage are usable.
  CommitLiveDeployment takes `{Lease, Pointer}` and requires complete
  LiveExecution.

Live removal keeps LiveExecution and the running Live Dex, and RemoveLive's
Live-pointer fence never reads them; the next deployment reuses them. A failed
first deployment leaves LiveExecution, and possibly a started Live Dex,
without CurrentLive: RemoveLive returns that Project unchanged, and only
DeleteProject destroys them. DestroyLiveDataAndInfrastructure destroys
`ScopeRefs{ProjectStorage, LiveExecution}`, also discovering by deterministic
name and tags what a lost response never recorded.

The Project's one Preview takes no lease: its route keeps the Project's host
and exists while the Preview serves; a stop and a reclamation remove it, and
the deletion releases one that no stop removed.
Cleanup keeps unknown external references until reconciliation; terminal Dex
status alone does not prove provider resources are gone.

Project creation extracts the AppManifest within Setup. Release and Preview
preparation each extract their exact commit's manifest inside their own durable
preparation Steps; Project's AppManifest is not refreshed afterwards. The public
RefreshAppManifest API/request and BFF route are removed.

RegisterChildFlow and AcknowledgeChildCleanup return a small ChildAdmissionResult
with an optional typed rejection. Register reads only ProjectID/status plus the
exact child instance; acknowledgement reads only that instance. Both retain the
shared Revision lock and exact Map lock. They do not read or return the
full Project snapshot. New RPC objects decode old extra Project fields harmlessly;
old clients checked only rejection and did not consume that unrelated snapshot.
GetChildFlow is an unlocked read that exact-loads one instance. For a FlowID
that is not registered it returns the empty `ChildFlowReference`, not an error;
callers test `FlowID == ""`.

### ProjectSetupFlow

FlowID: `setup-<projectID>-<setupID>`, for example `setup-a7k2-k9m4p7w2`.
ProjectFlow StartProjectSetup starts this independent execution with accepted server-derived project/setup
IDs and the complete safe StartIdentity fingerprint. The existing identity
contract applies to duplicate starts, collisions and retries.
Setup creates the private managed repository, claims its exclusive binding,
initializes the accepted template, extracts contracts and transfers permanent results to Project. Successful
handoff removes cleanup membership and completes the execution. Ordinary Step
retries handle transient provider failures. Exhaustion records SETUP_FAILED and
returns DeadEnd: business-terminal and read-only except Project deletion, with no
active Step, timer, recovery queue or provider work. Keep that execution active so
cleanup references cannot expire with closed-history retention. A new attempt
requires deleting the failed project, then a new CreateProject request identity.

**A:** Common StartIdentity, SetupRevision (the Revision lock), CreatedAt and
UpdatedAt; string ProjectID, SetupID, SetupSourceCommit and TemplateRevision;
SourceRepository:RepositoryBindingRef (the managed identity and exclusive claim
generation described above); SetupPhase:string;
AppManifestResult:optional<AppManifestSnapshot>;
SetupStatus:string (PENDING/RUNNING/SETUP_FAILED/CLEANING/COMPLETE/CANCELLED);
ProjectDeletionRequested:bool; SetupFailure:optional<Failure>. SetupStatus,
SetupPhase, SetupSourceCommit, SetupFailure and SetupRevision sync to the read
model's `projects` row of the Project through the Attribute Store
`project_setups` (row key `setup-{project_id}-{*}`); their keys differ from the
Project's, so the two Flows write different columns of one row. ProjectID uses
keyword2/IndexKeyword and SetupStatus uses keyword3/IndexKeyword as defined in the
common index inventory; all other setup fields are unindexed. Collections inside
structs remain ordinary Attributes. SetupPhase is the phase in progress, which a
failure records; ReconcileProjectStorage is the phase of the storage wait. The
configuration storage wait limit is ReconcileProjectStorage's StepOptions,
which the engine enforces; no deadline is stored. Every Attribute is retained
for the Flow's lifetime; no retry, recovery or restart resets one.
ReconcileProjectStorage hands partial storage references to Project in every
round through RecordProjectStorage; Setup creates no Temporal namespace and no
project Dex. RegisterAppManifest admits the exact source contract through the in-process
configuration host to scoped versioned storage. No raw credentials are persisted. The state
is bounded by one repository, binding, phase and source snapshot.

**AM:** None. **C:** None. **CM:** None.

**S:** SetupProgress<ProgressEvent>, produced by setup/cleanup Steps and consumed
by WatchProject alongside authoritative GetProjectSetup. A frame is written when
a Step starts, and ReconcileProjectStorage writes one whenever its pending
reason changes, in Detail, which the API never maps. The declared
10 MiB buffer is shared by Flow type and stream name. The common best-effort,
truncation, resume and snapshot-first Watch rules apply; progress is not a commit.

**Steps:**

```text
AdmitSetup -> CreateManagedRepository -> ClaimRepositoryBinding
  -> InitializeTemplate -> ExtractAppManifest
  -> ReconcileProjectStorage -> CommitProjectStorage -> RegisterAppManifest -> ConfigureRepository
  -> CommitProjectSetup -> CompleteProjectSetup
bounded retry exhaustion or storage wait limit -> RecordSetupFailure -> DeadEnd (SETUP_FAILED)
DeleteProject -> RequestProjectSetupCleanup -> CleanupSetup -> End (CANCELLED)
CleanupSetup of a repository of another repository host -> RecordSetupFailure -> DeadEnd (SETUP_FAILED)
```

**RPCs:** GetProjectSetup reads the authoritative safe state; GetDexSummary and
GetDexDisplay return safe identity/source fields. RequestProjectSetupCleanup is
internal to DeleteProject: it verifies the current owning Project is DELETING,
locks Revision, sets ProjectDeletionRequested and resumes CleanupSetup from
SETUP_FAILED. Repeated calls while CLEANING do not schedule duplicate Steps.
Running Steps observe the deletion flag at their next boundary; the same Revision
lock prevents an in-flight provider result racing the flag. There is no public
retry/cancel API and no RequestSetupRetry/RequestSetupCancellation or recovery
Channel. Project names are immutable; RenameProject is absent at every boundary.

State Steps use ASYNC with 30-second method timeout and two minutes of
retries. Provider Steps use SYNC under the same Revision lock; template
initialization allows five minutes per attempt and fifteen minutes of retries, and heartbeats every 20 seconds through `dexflow.KeepAlive` while its GitHub
requests (each bounded to 30 seconds) run, under the default one-minute heartbeat
timeout. ReconcileProjectStorage is one
long-running Execute without the Revision lock: it calls `EnsureStorage` (the
namespace, the platform server's RoleBinding, `default-deny` and the storage
backend) through `dexflow.PollUntil` every two seconds (30-second round
timeout) under `dexflow.PollingStepOptions(10m)` and writes no Setup Attribute
until the wait ends. Readiness moves to the short locked CommitProjectStorage,
which records the next phase; the engine's 10-minute wait limit fails the Step
into RecordSetupFailure. RecordSetupFailure keeps a Failure a Step decided, or
writes SETUP_FAILED with the final error from `RecoveryError()` (truncated to
2 KiB, internal resource names replaced by `Infrastructure.Redact`) after
exhausted retries or a passed wait limit, saying for ReconcileProjectStorage at
its limit that the project configuration storage did not become ready within
10 minutes; the last pending reason is not stored. A restart or lost Worker
resumes with a new attempt that queries before it creates. Each round rereads the
owning Project through its client; typed child-admission closure ends the poll
when deletion precedes the cleanup RPC. The accepted deletion flag at an attempt's
start or admission closure during a round moves directly to CleanupSetup without
recording SETUP_FAILED. Other provider errors
retain the failure route. CleanupSetup releases only the repository: the storage belongs to
Project, whose deletion destroys it. A repository the Setup recorded under another repository
host than the server's (`repohost.ErrOtherHost`) is not archived through this server's host,
which would read it as absent: CleanupSetup decides SETUP_FAILED with a message that names the
host to run the server with, and the Setup client refuses to request cleanup of such a Setup, so
the Project's deletion records REPOSITORY_HOST_MISMATCH. RPCs have ten-second deadlines.
External identity is the deterministic project/setup repository name
(`<prefix>-<name slug>-<projectID>`) plus exact binding claim and generation, the
project namespace, its RoleBinding and the Local Kind MinIO objects with the
`application` bucket or the hosted bucket. Only Setup derives the repository name:
CreateManagedRepository reads the Project's immutable name from the Project, because
Setup keeps no name Attribute, and CleanupSetup looks a repository up by that name only
when Setup recorded none. Once Setup has recorded the repository, its recorded full
name is the identity of every later read, credential, release, archive and cleanup.
Unknown creation/claim results are queried before
cleanup. Cleanup retries exhaustion keeps SETUP_FAILED and responsibility with
the deleting Project, whose bounded deletion path can request cleanup again.
The owned managed repository is archived.
Denied first admission completes without creating provider effects.

Tests use real Dex to exhaust three provider attempts, preserve the failure and
cleanup references, reject cleanup before Project deletion, and delete during an
in-flight provider call. They require typed cleanup completion and no remaining
owned effects. The storage scenarios run on real Dex, GitHub and Local Kind: a
READY Project has complete ProjectStorage, zero LiveExecution and no Dex or
Temporal Deployment; a deletion during ReconcileProjectStorage cancels the
Setup and deletes the Project; and a Worker replaced during that wait resumes
it in a new attempt that converges on the same namespace and storage. These
unshipped V2 definition changes use fresh test executions;
no V1 histories or production executions are migrated. No retained Channel/Map
state requires migration; future open executions follow the common compatibility
contract. ReconcileProjectDex and CommitProjectDex are removed, so an open Setup
of the earlier graph cannot continue: Local Kind and hosted test clear the exact
frozen Project execution inventory before that rollout, following the
rollout reconciliation.
The Kind cluster, accounts, shared infrastructure and unbound repository owners remain.
The storage-poll cancellation correction changes no persisted identity, primitive,
RPC, timeout or retention rule and requires no compatibility adapter.

### SessionAgentFlow

The Project's one long-lived coding conversation and Agent, as an independent top-level Flow and
the Project's only Session-scoped cleanup child
([the current contract](agent-harness-design/session.md)). FlowID `session-<projectID>`, for
example `session-a7k2`, started by ProjectFlow's StartProjectSession when the Project becomes READY,
with `dex.IDReuseDisallow`, the Start RequestID `flowid.Fingerprint(struct{ProjectID string})` and
same-request IgnoreError; it has no Flow timeout and creates no Sandbox. It owns the conversation
entries, turns, questions and the requirements card, the Stop request, the notification receipts and
automatic-turn counters, the confirmed coding HEAD, the workspace requests with the accepted lease and
the Sandbox deadline, the decision that the instance is idle, the Preview handoff of a pushed commit,
and its subagent and background-command records. Its SessionWorkspaceFlow owns the Sandbox instances
([the current contract](agent-harness-design/sandbox.md)); the Preview, subagents,
connector contributions and platform issue reports are owned by their own Flows. Business completion
is ARCHIVED, which only the Project's deletion makes (`CompleteProjectSession`), and the engine
completes with the cleanup receipt right after; a start whose Project admission is denied archives
with `admission_denied` and completes. Streams are best effort, the Snapshot is canonical, and no
database is introduced. On the normal path its Sandbox tools make no Flow RPC, StartFlow or Flow
read. The complete contract (every Attribute, AttributeMap, Channel, ChannelMap, Stream, Step, RPC
and recovery rule) is in [the Session design](agent-harness-design/session.md#sessionagentflow-contract);
the surrounding topics are listed in the [harness README](agent-harness-design/README.md#the-pieces),
and the package file map in the [Session README](../../internal/agents/session/README.md).

### SubagentFlow

One background agent of a Session's main agent per `start_agent` call: FlowID
`subagent-<projectID>-<agentID>`, for example `subagent-a7k2-k3n8q5r1`, started by the Session's
`ExecuteTool` through the Session Service with `dex.IDReuseDisallow` and a RequestID fingerprinting
the call and its snapshot commit, admitted by its Session in its first Step, with its own
conversation, tools, git worktree and limits; its terminal `Outcome` (COMPLETED, FAILED, STOPPED,
TIMED_OUT or DENIED) is reported to the Session as a `task` notification, and its changes reach the
workspace only through `merge_agent_changes`. It is not a Project child; a deleting Project finds it
by FlowType and `ProjectID`. The design is in [subagents](agent-harness-design/subagents.md), the
per-Flow contract in the [subagent README](../../internal/agents/session/subagent/README.md), and the
records, kinds and bounds in [agenttasks](../../internal/agents/session/agenttasks/README.md).

### PlatformIssueFlow

A platform failure of a Session is reported, beside the turn, to the `PlatformIssueFlow` of its
signature, `platform-issue-<env>-<sig16>`, for example `platform-issue-test-3f9a1c2b7d4e5f60`, started
with `dex.IDReuseAllowIfNotRunning` and same-request IgnoreError; a report while the run is open joins
it, and its label query before the write is the admission check of a run that reuses the FlowID. It
belongs to no Project and files one GitHub issue per signature, or only logs the incident where the
server names no issue repository. The design is in
[tool orchestration](agent-harness-design/tool-orchestration.md#platform-issue-reporting) and the
per-Flow contract in the [platform issue README](../../internal/agents/platformissue/README.md).

### BackgroundCommandFlow

A background command's watch, one long polling Step, runs as a Dex SubFlow of the Session's
`AwaitBackgroundCommand` Step, so the Session holds only a wait while the command runs. Dex
generates its FlowID `SubFlow:<Session FlowID>-<Step execution ID>-0` and starts it once for the Step
execution; it completes with the command's end, and the Session records the end and publishes the
`task` notification. Dex does not stop a SubFlow with its parent, so the Session's close stops each
running watch through its ChannelMap `BackgroundCommandStops`. The design is in
[tool orchestration](agent-harness-design/tool-orchestration.md#background-commands) and the per-Flow
contract in the [backgroundcommand README](../../internal/agents/session/backgroundcommand/README.md).

### ConnectorContributionFlow

One connector a Project's Session contributed to the connectors library through a completed
`connector_develop` subagent: FlowID `connector-contribution-<projectID>-<slug>`, for example
`connector-contribution-a7k2-newsapi`, started by the Session Service once the Session admitted the
subagent's report, with `dex.IDReuseDisallow` and a RequestID fingerprinting the Session, the slug, the
branch and the head commit; it owns the contribution record and opens the draft pull request, and the
deleting Project closes it. The design is in [connectors](agent-harness-design/connectors.md#contributions)
and the per-Flow contract in the [contribution README](../../internal/agents/session/contribution/README.md).

### Sandbox instance Flows

The Session requests each Sandbox instance, holds its lease and its Sandbox deadline, lends it to its
Preview and decides when it is idle; its SessionWorkspaceFlow owns the instances, provisions them and
reclaims their Pods, keeping the volume until the Project is deleted
([the current contract](agent-harness-design/sandbox.md)). Three Flows serve the Session,
each an independent top-level execution and none a Project child; the deleting Project ends the
workspace Flow and the Preview and releases every resource by label ([ProjectFlow](#projectflow)).

#### SessionWorkspaceFlow

`session-workspace-<projectID>`, one execution per Project, started by the
Session's first workspace command (`dex.IDReuseDisallow`); runs the Session's `provision` and
`reclaim` commands one at a time, so one volume carries one Pod, and reports each outcome to the
Session. Contract: [the Session workspace README](../../internal/agents/sandbox/sessionworkspace/README.md).

#### SandboxProvisionFlow

`sandbox-provision-<projectID>-<workspaceRequestID>`, one execution per
instance (`dex.IDReuseDisallow`, so a retry joins); creates the Pod on the Project's volume, checks
the repository out, verifies the instance and reports the outcome to the workspace Flow. Contract:
[the provisioning README](../../internal/agents/sandbox/provision/README.md).

#### PreviewFlow

`preview-<projectID>`, the Project's one Preview, started by its first Preview
command or automatic deployment; serves the Preview on the Sandbox-local engine
([the current contract](agent-harness-design/sandbox.md)) on the instance the Session lends
it, deploys each pushed commit on request and reports its outcomes to the Session
([the current contract](agent-harness-design/sandbox.md)). Build owns Preview
and Publishing owns Live only (the current contract).
Contract: [the Preview README](../../internal/agents/sandbox/preview/README.md).

The design (the lease and its instance identity, the cold path, the Sandbox deadline and the
reclamation, the Preview engine, source preparation and start chain, grants, Runs, the route and
auto-deploy) is in [the Sandbox design](agent-harness-design/sandbox.md). Every Project-keyed Preview
name comes from one helper, `infra.PreviewKeysOf`; the Preview's configuration lives in the Project's
Live storage below the Project's Preview prefix, and reading it never needs a workspace
([configuration without a workspace](agent-harness-design/sandbox.md#configuration-without-a-workspace)).
No externally used Flow identities are migrated: SandboxProvisionFlow and PreviewFlow replaced
SandboxFlow before launch, and SessionWorkspaceFlow took the instance lifecycle over from
SessionAgentFlow the same way; before the rollout every Project is deleted through the business API
(rollout reconciliation).

### ProjectReleaseFlow

New FlowID: `release-<projectID>-<40-character-main-commit>`, for example
`release-a7k2-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`. One logical Release owns
source preparation and multiple immutable build revisions for one editable
commit draft. Source preparation reaches DRAFT; successful builds reach READY
without ending the engine execution. Failed or pending builds preserve the last
READY build. Only Project cleanup completes the Flow.

The complete per-Flow review is
[the Release contract](../../internal/delivery/releases/README.md). Ordinary
singleton Attributes hold the identity, recipe, PreparedSource,
ActiveBuildRevisionID and ReadyBuildRevisionID, and
`ReleaseBuildRevisions<ReleaseBuildRevision>` uses the exact key `build-<UUIDv4>`.
The Release's status, revision, source, recipe version, prepared source, build
pointers, failure and timestamps sync to its `releases` row through the
Attribute Store `releases` (row key `release-{project_id}-{release_id}`), and each
build entry to its `release_build_revisions` row, with its frozen Configuration
(references, revisions and digests only) and ProviderSettled but not its
Fingerprint, BuildProfileDigest, Operation or ExpectedLiveRevision.
The entry owns immutable configuration/source/image references, accepted logical
input, provider recovery and optional automatic deployment intent. Exact map
entry loads/locks accompany the Release Revision lock. Build count grows until
Project deletion; each entry's receipt budget is bounded independently.

Channel: `ReleaseBuildCommands<ReleaseBuildCommand>`. ChannelMaps: None. BuildProgress retains its
shared 10 MiB best-effort budget; Snapshot and exact map reads recover loss.
PrepareRelease starts once with IDReuseDisallow and complete RequestID. Save & Build
admission and recovery retain their build UUID; a new explicit build gets a new
UUID without a new FlowID.

Preparation checks main membership and renders strict FDG/manifest without an
image. Build freezes validated configuration before inspect/start/await, artifact
publication, readiness and optional deployment. Each build revision records its
phase timings (`[]dexflow.PhaseTiming`, at most 16): CONFIGURATION_FREEZE and
ARTIFACT_PUBLICATION measured by the Flow and, between them, the phases the
build provider measured (QUEUE, HOST_PREPARATION, DEPENDENCY_PREPARATION,
ANALYSIS, COMPILATION, IMAGE_PUBLICATION), replaced by every committed
observation; PreparedSource keeps the provider's phases of source preparation.
Hosted builds take QUEUE and CodeBuild's share of HOST_PREPARATION from CodeBuild
and the rest from the runner's v2 result. The linked contract declares every
Step, RPC, deadline, heartbeat, failure route and cleanup owner. Public operations
are PrepareRelease, StartReleaseBuild and RecoverReleaseBuild; cancellation is internal
to Project deletion. No credentials enter Flow state or build artifacts.

**Live failure feedback.** The Release tells the Project's Session when it
fails, through the port `releases.DeliveryFeedback`, which the Session Service
implements. The Step that commits a failure decides its owner from the typed
cause and, outside cancellation, moves to NotifyReleaseFailure, which makes
one call of the port without a lock and returns to AwaitReleaseBuildCommands;
its failure route AbandonReleaseFailureNotification drops a notification that
exhausted its retries. The hooks are CommitReleasePublishingSource (H1: the
build provider's rejected analysis or an invalid dex-app.yaml is the
application's, every other refusal, exhausted retry and missing build service
the platform's, recorded as `PreparedSource.FailureOwner`),
CommitReleaseBuildRevision (H2: `BUILD_FAILED` is the application's,
`BUILD_UNKNOWN` and a rejected automatic deployment the platform's) and
RecordReleaseBuildFailure (H3: a draft that FreezeReleaseBuildConfiguration
refuses with the configuration host's gaps is the configuration's, anything
else the platform's). The notify Step's input is the failure as the commit
recorded it, whose ID names that commit's Revision:
`release/<releaseID>/source/r<revision>` or
`release/<releaseID>/<buildRevisionID>/r<revision>`, so a retry or a recovery
admitted meanwhile repeats it and the Session's receipts admit it once. The
Session records an application failure of its HEAD as a failure that may open
an automatic turn and every other as a notice.

### ProjectDeployFlow

FlowID: `deploy-<projectID>-<deploymentID>`. One explicit Live deployment. New deployment
requests can target the same Release; retries remain on the original identity.
Retain the resulting operation record until explicit project cleanup.

New DeploymentIDs are lowercase DNS labels of 1–36 characters with alphanumeric
ends; interior hyphens are allowed. Studio's request UUID is preserved as the
DeploymentID. API and new-Flow admission enforce this bound before start, while
retained Flow lookup remains compatible. Live providers, scoped grants, Native
forwarding, and Kubernetes admission use the same bound without widening
Project or Session identity rules. `app-<deploymentID>-<uint64 generation>` is
at most 61 characters; labels retain the exact ID. The required BuildRevisionID
selects the exact Release build; PinnedInputs and CurrentLive carry the same
identity. No Flow/Step
identity, Map, Channel or Stream changes accompany it.

Project owns the storage and Live execution references; the deployment keeps
its `DeploymentResources` copy, which VerifyReleaseAndConfiguration pins with
the Project's storage (and its execution environment once complete) and which
also holds the generation's Service. PrepareLiveExecution prepares the Live
execution environment on those references in two parallel branches: the first
deployment creates the Temporal namespace and its credential, whose data plane
must accept the credential, and the access of Dex and the applications, and
later deployments reuse them through queries. StartLiveDex then starts the
Live Dex. Every round hands each new execution identity to Project through
RecordLiveExecution under the deployment's lease before it reports pending or
an error. Cleanup revokes generation-bound grants before deleting the exact
owned generation resources, on the pinned storage. An absent grant receipt does
not prove that issuance had no effect.

**A:** DeploymentID, ProjectID, ReleaseID, BuildRevisionID,
ConfigurationRevision, ExpectedLiveRevision, DeploymentStatus, DeploymentURL,
PinnedInputs, ProjectDeploymentLease, DeploymentResources, WorkloadGrant,
ProviderOperationRefs, PhaseTimings, ReadinessResult, CleanupRequested,
Failure, StartIdentity, CreatedAt, UpdatedAt, Revision. PinnedInputs holds the
exact build, image, configuration, definition and manifest references;
DeploymentStatus is PENDING, DEPLOYING, READY, FAILED, RECOVERING, CANCELLING,
REMOVED, RETIRING or RETIRED. ReplacedLive names the Live generation the
deployment replaces and Retirement the request of the generation that replaced
it ([the current contract](studio-setup/flow-changes.md)); neither
syncs. ProviderOperationRefs holds the phase, the application identity,
the request receipts and PreparationRound, the latest preparation round.
PhaseTimings (`[]dexflow.PhaseTiming`) measures each phase and each
preparation branch, its latest run only. All but DeploymentID, ProjectID,
ExpectedLiveRevision, CleanupRequested and StartIdentity sync to
the read model's `deployments` row through the Attribute Store `deployments`
(row key `deploy-{project_id}-{deployment_id}`).

WorkloadGrant contains project/deployment identity, grant generation, accepted
configuration reference/revision and revocation state. It contains no bearer token
or storage credential. Provider-owned generation Secrets retain revocation fences;
a hosted revoked generation's IAM role stays a deny fence for 12 hours, longer
than any credential Pod Identity issued for it, and the project's next Live
issuance deletes it.

**AM:** None.

**C:** DeploymentRecoveryDecisions<string>, a wakeup carrying the ID of the
request that admitted a retry of the current failure or Project cleanup. The Project cleanup fence is
the CleanupRequested Attribute, checked at safe boundaries.

**CM:** LivePreparationResults<LivePreparationResult>, key
`prepare-<generation>-<round>` from the lease generation and PreparationRound,
for example `prepare-7-2`. Each preparation branch publishes exactly one
result: its Execute decision or its failure route RecordLivePreparationFailure,
never both. AwaitLiveExecutionPreparation consumes both; a new round starts
only after that, so no stale result exists.

**S:** DeploymentProgress<ProgressEvent>, a frame at each Step start and per
new pending reason, the reason in the unmapped detail, to WatchDeployment.

**Steps:**

```text
AdmitDeployment -> AcquireProjectDeployment -> VerifyReleaseAndConfiguration
  -> PublishDefinitions -> PrepareLiveExecution
     -> GoToMany(PrepareLiveTemporal, PrepareLiveAccess) -> AwaitLiveExecutionPreparation
  -> StartLiveDex -> ApplyApplication -> VerifyReadiness -> CommitCurrentLive
  -> RequestReplacedRetirement -> ReleaseProjectDeployment -> AwaitDeploymentRecovery
replaced READY generation -> RETIRING -> DrainReplacedGeneration (live.retirementDrain)
  -> RetireReplacedGeneration -> CommitDeploymentPhase (RETIRED, completes)
CommitDeploymentPhase commits each phase result under the Revision lock
branch failure or wait limit -> RecordLivePreparationFailure (publishes the branch's result)
provider failure -> RecordDeploymentFailure -> CommitDeploymentPhase -> NotifyDeploymentFailure
  -> AwaitDeploymentRecovery (cleanup: CommitDeploymentPhase -> AwaitDeploymentRecovery)
Project cleanup -> CleanupCancelledDeployment -> ReleaseProjectDeployment (REMOVED)
```

PrepareLiveAccess first records the Live usages of the TeamSpace connections
the frozen configuration selects, idempotently, so the version's applications
may read their credentials before traffic switches; a selected connection that
was deleted fails the round. The wait limits are 20 minutes for
PrepareLiveTemporal, 15 for PrepareLiveAccess and 10 for StartLiveDex. A
timed-out Temporal branch is classified by one bounded read-only data-plane
check: an API key the data plane did not accept, an unreachable data plane, or
a namespace that became ready afterwards. Every FAILED commit passes through
CommitDeploymentPhase's failure branch, which outside Project cleanup tells the Project's Session through
NotifyDeploymentFailure (the Release's port `releases.DeliveryFeedback`, one
call without a lock; its failure route AbandonDeploymentFailureNotification
drops a notification that exhausted its retries) and then moves to
AwaitDeploymentRecovery, and during cleanup moves there directly. The failed
phase decides the owner: ApplyApplication and VerifyReadiness are the
application's, VerifyReleaseAndConfiguration the configuration's and every
other phase the platform's. The notification's ID
`deployment/<deploymentID>/g<generation>/r<revision>` names the lease
generation and the Revision of the FAILED commit. The
[Live deployment contract](../../internal/delivery/deployments/README.md)
declares every Step deadline, retry policy and recovery route.

**RPCs:** GetProjectDeployment, RequestLiveDeployment, RequestProjectCleanup,
RevokeDeploymentGrant.
Public StartDeployment admits start/join/recovery; project deletion retains internal cleanup.

Serialize deployments through Project's active-deployment ownership. Pin image
digest, Release, build revision, configuration revision, object version, and digest. Preserve
last-known-ready pointers on failure; a failed deployment keeps its lease and
partial resource identities for its StartDeployment recovery or its deleting Project's
cleanup. Do not create EnvironmentProvision, StagingDeployment or
ProductionPromotion Flows.

### Project configuration ownership

ConnectorConnectionFlow and OAuthAuthorizationFlow remain absent from the platform
registry. No replacement Flow, Attribute, Map, Channel, Stream, Step or index is
introduced. `internal/projects/config` coordinates owner-authorized source and scope;
`internal/studio/confighost` runs the audited configuration/OAuth handlers in the
existing Go process. The released `sdkgo/projectconfig` v0.26.0 standard storage
package defines configuration, credential, OAuth and exchange records shared with
application SDKs; the platform writes `v1alpha2` connection records. A scope's
configuration document is `v1alpha2` unless it selects a TeamSpace connection or
carries a credential broker, which makes it `v1alpha3`, so applications built with
an earlier SDK still read the documents of scopes without shared connections. Records of the earlier `v1alpha1` schemas are not migrated, and
pre-launch projects that hold them are discarded. Application Connector operation
implementations remain in the application. Management UI source belongs to Studio,
including native Connector forms; there is no shared UI package, management iframe,
Dex Web listener or management reverse proxy.

Project Dex uses the pinned engine image with `start --services api,interpreter`.
Its only service port is 8801. The Preview engine is the Dex Server that the
coding image's `dexcli dev` embeds, inside the Session's Sandbox
([the current contract](agent-harness-design/sandbox.md)); its Dex Web
stays on the Pod's loopback. The Go host resolves verified published Connector
descriptors and supported provider protocols with a private disposable artifact
cache. It does not require Connector artifacts inside the Dex image. Unsupported
protocols and unpublished authorities are explicit failures. This boundary needs
no new Dex release and does not upgrade application template/SDK pins.

The storage root isolates `projects/<projectID>/live` from
`projects/<projectID>/preview` (`projectconfig.Scope{ProjectID, Kind}`), both in
the Project's Live bucket; a Preview has no storage of its own. Existing private objects, versions and
namespace UIDs are retained. Configuration access verifies scope and accepted
namespace UID rather than Dex engine readiness. Conditional ETag admission protects
mutable heads; accepted configurations pin exact immutable versions. Live credentials
remain shared across commits and deployed builds, with actual-use rotation/refresh.
Preview credentials belong to the Project's Preview scope. Native host caches are disposable,
bounded and hold no authoritative business or OAuth state.

Provider exchange/refresh requires durable admission before dispatch. Observers
join the admitted attempt and never redispatch because a lock expired. Immutable
private results recover an ambiguous response; absent evidence requires recovery
or reauthorization. Credential replacement, revocation and late results compare
the accepted fence. OAuth state, PKCE and callback material remain encrypted in
existing scoped storage, bound to actor, source and expiry. OAuth state and
native command nonces bind the source commit, so a changed source (for Preview
the Session's HEAD) invalidates old callbacks and commands; no Sandbox instance
takes part.

A connection selects exactly one authorization method: the method ID that the
AppManifest declares, or the empty ID for a Connector without `auth.methods`.
There is no multiple-method selection. The stored configuration entry names the
Connector module and method but no release, and published credential material
records only the method, so credentials are not bound to a Connector version:
upgrading the declared release keeps an authorized connection `Ready`. The release
that the AppManifest declares, pinned by the request's AppManifest revision,
validates fields during setup and release validation.

A token response must grant every requested scope it declares, except
`offline_access`: it requests refresh material and grants no resource permission,
and GitHub honors it without listing it. A connection reports `Conflict` only when
its stored configuration names another module path or authorization method than
the AppManifest declares, or credential material it published names another
authorization method; a different Connector release never conflicts. Studio keeps
the setup form open for a `Conflict`, and saving it with the declared module and
method replaces the connection under CAS. A failed or abandoned first exchange has
published no material; the record is `Reauthorization required` (or `Missing`
while unfinished) and authorizes again against its current revision.

The `llm` Connector declares one `apiKey` method with the `api_key` credential.
Its required `provider` configuration field (`openai`, `anthropic`, `gemini`,
`qwen`, `deepseek`, `meta`, `mistral`, `kimi` or `xai`) and optional `region`
(`global` by default, `us`, `eu`, `china` or `hong-kong`) select the provider API,
and `model` holds a model ID exactly as the provider names it, never
provider-prefixed. Its Studio model picker lists only the saved provider's models
in the saved region and runs no provider command while no provider is saved.

AppManifest optionally declares `application.environment` fields with name,
required, secret, minLength and enum. Canonical fields and enum values are sorted;
empty declarations preserve existing contract digests. These declarations extend
the existing AppManifest struct without a new Flow, primitive, index or lifecycle.
Named Go Studio environment operations own editing and validation. Reads never
return secret values or their object references.

Ordinary application values and secret references are fixed to the accepted
configuration version. Application secrets use immutable
`projects/<projectID>/<scope>/app-secrets/<NAME>/<objectID>` objects with exact
version and digest. Provider admission reads safe configuration references and
grants exact-key secret reads without loading secret content. Application startup
checks scope/version/digest before applying values; edits require a new Preview
startup or Live deployment. Connector credentials instead resolve the current
version at actual use and may rotate without restarting the application.

Preview and Live share one App/SDK loader. Superverse passes project/scope,
configuration key/version/digest, storage identity and service addresses. In main,
the application loads and applies configuration before business clients, Workers,
HTTP servers or goroutines. Invalid or missing accepted configuration prevents
serving; package variables and init functions must not read business values. The
basic template uses the same loader even without connectors. No Live configuration
reader init container or Preview configuration file is used. Preview's private
storage credential file remains; admission and grant-scope validation are retained.

Full source/storage and host lifetime contracts are in
[Project configuration](../../internal/projects/config/README.md), and the
application environment, credential and OAuth contracts in the
[configuration host](../../internal/studio/confighost/README.md).



### Cross-Flow consistency and retention

- Cross-Flow handoffs use typed RPCs, stable business identities, revisions or
  generations, and explicit acknowledgements. Do not add a generic dispatcher.
- Project stores current pointers; Session, Release and Deployment own their records. Scoped versioned storage owns
  configuration and credential records through the Go/Connector SDK protocol. Short operations publish required durable outcomes before
  completing.
- Channels carry pending intent, not permanent event logs. Define rejected,
  cancelled, expired and leftover-message handling at each terminal boundary.
- Every business record has canonical read RPCs. Streams never prove that a
  mutation committed, cleanup succeeded, or a Flow is active.
- Project, archived Session, Release, Deployment and TeamSpace records remain
  readable entity Flows. A RepositoryBinding is permanent only while its
  repository is in use: its release after the archive completes it, and its
  absence after closed-history retention is safe because the repository ID never
  returns. Disable unnecessary timers when a
  record becomes read-only; do not use closed-history retention as permanence.
- Setup Flows may complete after handoff. Every Sandbox execution completes
  after `expire` or `shutdown`; the resources that outlive it are referenced by
  the owning Session's `Workspace.Retention`, never only by closed Sandbox
  history.
- Do not set blob age expiration that deletes data referenced by retained
  entities. Explicit deletion blocks access and cleans resources, then closes
  relevant Flows; physical history/blob/backup removal follows retention/GC.
- All observable Flows expose GetDexSummary/GetDexDisplay and appropriate
  indexed fields. Action RPCs specify input, state condition, and permission.
- Preserve open V2 execution identities once launched. Do not import V1 history
  compatibility shims without an actual migration requirement.

## Public API

Every authenticated gRPC request carries explicit `CallerContext` fields:
AccountID, canonical email and signed session token, injected by the trusted BFF
from the HttpOnly server-issued login envelope. Go validates the signed token and
its exact `login_sessions` row, then checks opaque AccountID equality
([Caller authentication](#caller-authentication)). User identity is never inferred
from metadata. There is no service token, service-auth metadata or BFF-auth
interceptor. Registration and Login use their account boundaries; authenticated
requests use the typed caller contract.

Browser HttpOnly `sv2_session` -> Studio BFF -> private business gRPC is the request
path. Go independently verifies token HMAC/expiry, exact account identity and
the non-revoked login row's token hash before resource ownership. Business port 8080
allows only Studio BFF pods under platform network policy. The Account/Watch
signing key remains server-only. Studio uses an independent canonical 32-byte
base64url `BROWSER_SIGNING_KEY` for Studio CSRF and OAuth route-cookie signatures;
it is not a service credential or the Account master key. Studio auth
is separate from future Preview/Live application end-user authentication. App
workloads must not reuse Studio sessions and use scoped workload grants instead.

ProjectID, SetupID and the basic-template version are selected by the server.
CreateProject requests provide request identity and user-facing project inputs;
the accepted ProjectSnapshot returns actual IDs and template revision. There is
no GetApplicationInfo RPC or configuration-discovery endpoint.

One `api/superverse/v2/superverse.proto` holds eleven business services of at
most ten RPCs each ([the current contract](../api/resource-grpc.md));
the [resource API contract](../api/resource-grpc.md) carries the generated
service table, the message rules and the enum inventory. Studio's BFF calls
private gRPC through generated definitions: the browser reaches AccountService
only through `/api/auth`, the four Flow API services only through `/dex/...` and
the other six through `/api/rpc/<Method>`, and never reaches private server or
Dex ports. No OperationService, generic operation DTO/ID, GetOperation,
WatchOperation, or arbitrary-Flow operation endpoint exists. A start returns
only the ID of the resource it started; long mutations are observed through
that resource's Get/Watch API.

| Service | RPCs |
| --- | --- |
| AccountService | RegisterAccount, Login, Logout, GetCurrentAccount |
| ProjectService | CreateProject, ListProjects, GetProject, DeleteProject, WatchProject, GetProjectConfiguration, ValidateProjectConfiguration, RemoveLive |
| SessionService | GetSession, WatchSession, ListArchivedMessages, RenewWorkspace |
| AgentService | SendMessage, AnswerQuestions, UpdateSessionModel, DeleteQueuedMessage, SteerQueuedMessage, ResolveToolApproval, InterruptAgent, ListMockScenarios |
| PreviewService | GetPreview, StartPreview, StopPreview, PreparePreviewSource, GetPreviewDefinitions |
| ReleaseService | ListReleaseCandidates, PrepareRelease, StartReleaseBuild, RecoverReleaseBuild, ListReleases, GetRelease, WatchRelease |
| DeploymentService | StartDeployment, GetDeployment, ListDeployments, WatchDeployment |
| Flow API: FlowRunService, FlowDiagnosticsService, ConnectorService, ApplicationEnvironmentService | Named Runs/Inbox/Diagnostics, connection, provider, binding, environment and OAuth operations of one served application |

Each mutation has stable request identity and revision where needed. All lists,
reads, mutations, watches, Preview/artifacts/OAuth and proxy paths authenticate
the account/session and authorize through Project. Browser owner/role headers
are not authority. Admin acts retain the true project owner and actor.

The TeamSpace services, `TeamSpaceService`, `TeamSpaceMemberService`,
`ProjectMemberService` and `TeamSpaceConnectionService`, bring the proto to 15
services, all reached through `/api/rpc/<Method>`
([API and Studio](teamspace/api-and-studio.md#business-services)). Authorization
joins the TeamSpace and Project memberships in one SQL query
([roles](teamspace/teamspaces-and-roles.md#authorization)). Applications read their
shared connection credentials through `POST /internal/connection-credentials/read`
on a listener of its own, which is not part of the business protocol, bypasses the
Studio BFF and authenticates a per-scope token instead of a caller
([connections](teamspace/connections.md#the-read-endpoint-and-scope-tokens),
[credential read endpoint](../api/credential-read.md)).

### Release reads and AppManifest

GetRelease returns a GetReleaseResponse with `ReleaseSnapshot release = 1` and
`repeated FlowDefinition flow_definitions = 2`, which stays empty until the
prepared source is READY. It reads the complete immutable bundle only for the
singular resource. ListReleases and WatchRelease continue to use ReleaseSnapshot
without definitions. Pending and failed builds remain readable without a
published bundle; missing/corrupt artifacts for READY fail explicitly. The
separate definition list/detail business APIs and BFF routes are removed. Studio
source views use GetRelease; serving Runs resolve their exact Deployment or
Preview definitions through the Flow API.

AppManifestSnapshot is the single validated, canonically represented immutable
snapshot of `dex-app.yaml` at an exact source commit. Project owns its current
reference, while a Release pins the accepted immutable snapshot. Connector and
environment sub-digests remain inside AppManifest for configuration/deployment
admission. This pre-publication rename does not add parallel state or aliases.
No V1 execution is migrated. Historical test receipts keep their observed
execution identities; new V2 executions use the renamed primitive/Step contract.

### Release and Deployment recovery admission

PrepareRelease/StartReleaseBuild implement the per-commit contract in
[Release](../../internal/delivery/releases/README.md).
Each build has its own exact map entry and bounded recovery receipts; there is no
128-build lifetime cap. The following recovery rules apply to Deployment, which
retains at most 128 accepted request receipts in ProviderOperationRefs.Requests. Each is
`{RequestID: string, FailedRevision: uint64}`; zero denotes start/join. A fresh
request may recover only the current FAILED revision, preserving the FlowID,
immutable inputs, external identities and resource generation. Replaying the
original or any older accepted request returns the snapshot without resetting
budgets or publishing another wakeup. READY never restarts. Receipts are not
pruned, and capacity exhaustion leaves cleanup available.
Executions lacking admission history fail closed for new recovery. The recovery
Channel remains a wakeup; the locked receipt/status/revision authorizes work.
No new Attribute, Map, index, table or generic operation resource is introduced.

Project.ActiveDeployment discovers an unresolved same-desired deployment without
Search. If lease release succeeded but its response was lost, the exact failed
CurrentLive deployment is also read and reconciled; READY still allows explicit
new deployment. StartDeployment joins or resumes that exact Flow for the same
Release/config, even if the request proposed a new DeploymentID. A changed
desired target is rejected until existing effects are reconciled. Recovery
preserves the original ExpectedLiveRevision and recognizes its own committed
CurrentLive after response loss. Old lease cleanup cannot release a newer lease.
Public RetryRelease, CancelRelease, RetryDeployment, CancelDeployment and
GetLiveDeployment are absent. The Project's `live` pointer (ProjectLive) selects
serving; GetDeployment/List/Watch retain history. RemoveLive fences on that
pointer's deployment ID and revision, which the page already shows, so neither
the BFF nor Go reads the deployment before removal.

### Snapshot-first resource Watches

GetProjectConfiguration exposes safe source/configuration metadata, exact references
and a resource-specific validation receipt. ValidateProjectConfiguration admits
Release-bound validation; Preview startup performs the same native validation.
Studio owns native forms and Go owns connection, schema, provider and OAuth
operations through named gRPC methods. PrepareRelease makes source available
before configuration; StartReleaseBuild freezes a validated draft. Provider
authorization opens its scoped OAuth page with callback identity preserved.

| Watch | Resource selector | First frame | Progress |
| --- | --- | --- | --- |
| WatchProject | ProjectID | ProjectSnapshot including current setup's safe view | ResourceProgress from ProjectProgress/current SetupProgress |
| WatchSession | ProjectID | SessionSnapshot with current messages, state, pending inputs, workspace and Preview | SessionProgress: AgentActivity/AssistantText/ReasoningSummary |
| WatchRelease | ProjectID, ReleaseID | ReleaseSnapshot | ResourceProgress from BuildProgress |
| WatchDeployment | ProjectID, DeploymentID | DeploymentSnapshot | ResourceProgress from DeploymentProgress |

Every successful connection, including resume, starts with the resource snapshot.
Each Watch has its own typed `Watch<R>Response {oneof event {snapshot; progress};
resume_cursor}`, not a universal Operation envelope. Snapshot uses the resource
DTO from Get; Release uses the nested `GetReleaseResponse.release`, without its
definition bundle. The Session Watch carries the Preview in its snapshot; there
is no separate Preview Watch.
Project's current setup is read from SetupFlow without persisted mirroring;
carry source revisions and do not claim cross-Flow atomicity.

Authorize and identify source/generation, acquire tail cursor and begin bounded
buffering, then read/send snapshot first, then release buffered progress. Do not
read snapshot first and then begin at a later tail. Rebind after source/generation
changes; stale setup progress cannot affect the new resource. The Preview has no
progress source: the PreviewSnapshot inside each Session snapshot is its only
authority.

Streams can silently trim old cursors and emit before commit. Every active watch
rechecks authoritative snapshot every five seconds and sends replacements only
when changed; reconnect, loss/buffer overflow and source changes also reconcile.
This is connection-local read-only work, not a Flow timer or projection. Each
reconciliation reads the authenticated snapshot then fences its source before
publication, without an extra preliminary source read. Session Get reuses only
its own fresh scope-validated lifecycle snapshot; no authority is cached across
requests or Watch refreshes.
Agent text remains temporary rendering, corrected by message snapshot on resume.
Do not claim exactly-once events, full logs, or an atomic snapshot/Stream boundary.
Disconnect, cancellation or authorization loss releases readers/buffers.
Each Go Watch permits one outstanding gRPC send with a ten-second deadline;
five-second authoritative checks continue while it is pending. A pending-send
source change ends the connection so a new connection can authorize and snapshot
the new source. The handler cancels/joins Dex readers and returns without joining
a transport-blocked sender; gRPC handler return cancels that transport. The BFF
closes/drains only successful upstream completion. Failure or request abort errors
its queue and discards unread frames, with once-only upstream cancellation and
fixed sanitized errors. Already transmitted bytes cannot be retracted. These are
connection lifetimes, not additional Flow timers, attributes or durable records.
See the [resource API contract](../api/resource-grpc.md) for queue budgets.

A not-started Preview returns a not-started snapshot without creating a Sandbox.
StartPreview on a Session without a running READY Sandbox instance is accepted
without waiting for the workspace: the Session records the pending Preview
request, starts the workspace on demand and forwards the start to the READY
instance. The acceptance returns Preview `STARTING` with generation 1 and
revision 1, and every later Session or Preview read reports `STARTING`, never an
absent or `NOT_STARTED` Preview, with the same generation and a revision that
does not fall below the accepted one, until the instance reports its own Preview
state. The caller's generation and expected revision are ignored in this branch
because no instance exists to conflict with.
StopPreview addresses the running instance directly.
Missing/unauthorized resources fail before a fabricated first frame. Completion
comes from snapshot, never a progress event. Project deletion may emit its final
DELETED snapshot before closing that watch; other access is then denied.

### Flow API

The Flow API is the API of the embedded Dex Flow UI. Studio's `/dex/...` routes
map fixed operations to the named RPCs of FlowRunService, FlowDiagnosticsService,
ConnectorService and ApplicationEnvironmentService; each request carries a
`FlowApiContext` (the `FlowApiTarget` and the revisions the page observed) and
the caller, and each response is a `FlowApiDocument`. Browser input supplies
typed resource selectors and declared business fields, never an upstream, Worker
address, permission list or arbitrary HTTP path. The Go API freshly
authenticates each Flow API request, then the owning service authorizes
Project owner/ACTIVE administrator access (Project roles with the TeamSpace work)
and reads the exact resource. The old BFF Dex scope cache and management reverse
proxy are removed; authentication has no cache, so these management checks and
the ordinary ones read the same database state.

Live Runs requires the exact current READY Deployment and its accepted Release,
build, image, grant, configuration and immutable FDG. Preview Runs requires an
ACTIVE Session, an accepted READY workspace and a READY Preview. Writes bind
the observed WorkspaceRequestID and Generation, not Generation alone. The running
Preview definitions come from its accepted reference, never the next prepared
source. Changed selection cancels old UI requests and closes its Start form.
`internal/studio/runs` invokes the imported Flow API adapter in process and the
public pinned Dex gRPC client. Infrastructure verifies Namespace/Service ownership
and UID before dialing fixed port 8801: a Live scope's Dex Service, or for Preview
the Sandbox's own Service (`connect.DialSandboxDex`), which also verifies the
Sandbox and its instance label, or in Local Native the instance's container and
the port it publishes; each request closes its connection. Preview Runs also requires the engine READY behind the same
Sandbox and Service. Start additionally checks the owned Worker: the fixed Live
8811 endpoint, or for Preview `superverse-previewctl status` over sandboxd, which
must report the served commit, configuration digest and grant generation with
127.0.0.1:18811 accepting inside the Pod. There is no fallback to an older
target.

Configuration selects an exact prepared Live source, or for Preview the source
the Preview prepared for the Session's HEAD, which it reads from stored state
without a workspace; its target names no deployment and no Sandbox instance. It
uses namespace-UID-checked scoped ObjectStore access and does not need a running
application or Dex engine. Native command sessions bind actor, source and
manifest/definition/module revisions. Go passes the authorized
target to the configuration host as a typed in-process admission, never as headers
or a token. Storage keys and provider credentials never cross the browser boundary.

The BFF retains same-origin mutation, bounded input, CSRF and trusted-header
rejection. Its version-7 signed OAuth route cookie binds actor/login, state digest,
the TeamSpace connection, the return path, fixed origin and expiry. Go completes
only the caller's own authorization attempt, which binds the redirect URI, and
publishes the credential under the connection's lease. Both processes use the same
operator-configured `SUPERVERSE_STUDIO_PUBLIC_ORIGIN`. No browser-derived origin
or network admin token is trusted. See the [resource API contract](../api/resource-grpc.md#flow-api)
for named operations and exact recovery rules. No new Flow, database, projection
or public listener is introduced.

### Configuration before building a Release

PrepareRelease owns exact main source extraction and strict FDG generation before
configuration. The Preview's source preparation owns its current clean, pushed Sandbox
source. Both register immutable manifest/FDG references with the Go configuration
service without starting an application or replacing its serving catalog. Studio
renders audited native controls; Go owns settings, credentials, OAuth, validation
and provider commands through the standard Connector SDK storage package.
Unsupported protocols or unpublished authorities fail explicitly.

Each Live commit has one ordinary configuration draft. The first preparation may
copy compatible settings from CurrentLive's exact frozen snapshot once. Existing
drafts are never overwritten by preparation. Credentials remain shared within the
Live environment; Preview uses its independent Session scope. Save & Build freezes
a validated draft to an exact immutable version of the canonical runtime key.
ReleaseBuildRevisions owns that reference and build recipe, and Deployment pins
that exact READY build. Subsequent drafts/builds do not change an existing Live.

GetProjectConfiguration authorizes the exact prepared Release or current Sandbox
source and reads safe metadata without extracting source or refreshing credentials.
A Preview read may request missing workspace initialization and report not-ready,
as documented in the current contract. Authenticated configuration context binds source and
resource identity; provider command sessions and OAuth callbacks retain their
original actor/source/revisions and Preview instance. Browser headers cannot select trusted scope or
Worker targets. New Live validation checks the draft synchronously; durable build
freeze owns its immutable acceptance receipt and uncertain-response recovery.

StopPreview and a reclamation retain Session configuration; the Project's
deletion removes the Session's Preview data. Project cleanup settles active build jobs, removes
Release source/build artifacts and finally destroys private configuration, seed,
freeze and credential objects with the Project-owned storage scope. No new database exists.

### Remove Live versus delete project

RemoveLive removes serving Deployments/Services/routes and active
workload grants, clears current Live and marks the deployment REMOVED. Retain
Project/binding/sessions/Releases/artifacts/config/credentials, application
Temporal execution/history/blob data and the Live execution environment
(LiveExecution and the running Live Dex), which the next deployment reuses. UI says "Live not deployed; data retained."
Redeployment grants new access and restores serving.

DeleteProject freezes admission and fully cleans owned resources/data/secrets;
managed repositories follow archive policy.
ProjectStatus transitions DELETING to DELETED; no Deleted flag or Boolean index.

## Database Schema Changes

PostgreSQL holds `accounts` and `login_sessions` (migration `0001`), written by the
server only through `internal/tenancy/accountdao`; the synced tables of the read model
(`0002`) and of the TeamSpaces and memberships (`0003`); the TeamSpace connection
tables (`0004`); and the `schema_migrations` ledger that `internal/platform/postgres`
keeps ([read model](teamspace/read-model.md), [connections](teamspace/connections.md),
[infrastructure](teamspace/infrastructure.md)). A synced
table is a projection written only by the Dex Attribute Store, never a second
owner of a Flow's facts; project a Flow only for a demonstrated list, join or
cross-entity read.

| Data | Authoritative store |
| --- | --- |
| Email, account state, login-session metadata | PostgreSQL `accounts` and `login_sessions` |
| Projects, bindings, sessions, releases, deployments | Their owning Flows |
| Agent message window and context summary | SessionAgentFlow Attributes/AttributeMaps |
| Mutable configuration, connections, OAuth and credential revisions | Scoped versioned objects managed by Go through the standard Connector SDK protocol; TeamSpace connections replace the per-Project connections ([the current contract](teamspace/connections.md)) |
| TeamSpaces, their members and Project registry; Project members and roles | TeamSpaceFlow and ProjectFlow Attributes and AttributeMaps, synced to PostgreSQL for reads ([roles](teamspace/teamspaces-and-roles.md)) |
| TeamSpace connections, usages and scope tokens | PostgreSQL tables; the credentials themselves are KMS-encrypted objects ([connections](teamspace/connections.md)) |
| Accepted configuration and scoped resource references | ProjectFlow (Live scope resources) / ProjectReleaseFlow / ProjectDeployFlow / PreviewFlow |
| Workspace request, accepted lease and Sandbox deadline | SessionAgentFlow `Workspace` Attribute |
| The current instance and the resources retained between instances | SessionWorkspaceFlow `Instance` Attribute |
| One provisioning and its outcome | SandboxProvisionFlow |
| Credential metadata, OAuth attempts and references | Existing Project/Session-scoped versioned encrypted storage shared with application SDK |
| Large Attribute payloads | Dex Blob Store |
| Password hashes, salts and Argon2id parameters | PostgreSQL `accounts.password_record` |
| OAuth/provider secrets | KMS-encrypted secret objects |
| Immutable Release artifacts and configuration snapshots | Versioned object storage |
| Source and commits | Git |

The database is not the only persistence: Temporal, blobs, Git, artifacts, and
secrets remain necessary. Migrations only add. Do not bring over destructive
Flyway schema-reset procedures or touch V1 databases. Cleanup cannot claim immediate
physical erasure of Temporal history or backups.

## UI/UX

Preserve Studio conversation, keyboard behavior, focus, cancellation and explicit
mock development. A Project page is the Builder ([the current contract](studio-setup/studio.md),
visual spec [prototype.html](studio-setup/studio.md)): the sidebar, one 48px bar
lined up with the columns, the Project's chat column on every surface, and one of the surfaces
Flow, Runs, Inbox, Interface and Tools (Publishing, Connectors, Permissions, Configuration). One
Preview | Live switch in the bar sets the version every surface shows (`?v=live`) and never changes
by itself. Publish in the bar is a popover over the Release APIs: it prepares the selected version
when needed, requires its Live configuration, and builds it with an automatic deployment fenced on
the Live revision shown.
All Dex management UI source is local to Studio, with no shared Dex component
package or management iframe; the user application Preview iframe remains isolated.

- Add immediate email/password registration and login. Ordinary users see
  personal projects; the administrator can access all projects. With the TeamSpace
  work, Home lists the selected TeamSpace's Projects with the caller's role
  ([Studio](teamspace/api-and-studio.md#studio)).
- Tools, Publishing has Config and Deploy tabs. Config uses a searchable list of the latest
  100 main commits, native forms and Save & Build; Deploy selects a Release
  and its last successful exact build.
  Preserve project-level publishing independent of coding sessions.
- A Project page owns one Project Watch shared by its shell, publishing and
  configuration surfaces and, once the Project is READY, its one Session's
  complete snapshot/Watch.
  UI/BFF resource responses use generated protobuf contracts directly; no V1
  project, release or environment DTO is synthesized. Configuration state comes
  from an exact GetProjectConfiguration read, not from the Project snapshot.
- Release discovery loads bounded pages with explicit Load more and retains
  loaded identities. Accepted Build, current Live and route-selected Release IDs
  use exact Get/Watch independently of eventual listing visibility. An incomplete
  listing cannot establish that no Release exists.
- The Project has one Preview with one URL. Its credentials/configuration are
  scope-isolated from Live and are not silently copied from Live.
- The Builder's chat opens the Project's one Session directly: "Preparing project..." while
  Setup runs, "Starting the agent..." until the Session answers, then the
  stored creation intent becomes its first turn: Build applies the model the
  person chose on the creation page when it differs from the Session's, then
  sends the first message once. The creation page shows the Session's model
  picker and composer, preselected on Sonnet, or for the platform administrator
  on the mock model where the server offers it, and the mock composer replaces
  the text box when the mock model is chosen. There is no Session list, start
  action, `?session=` or Session Stop.
- A new Session shows no workspace. A Session does not close when its
  instance is reclaimed at the Sandbox deadline: with a usable lease
  tools run immediately, and otherwise the next workspace demand provisions a
  new instance on the kept volume while the conversation shows workspace
  preparation. Workspace statuses are PROVISIONING, READY, CLOSING, FAILED and
  CLOSED; CLOSED is presented as reclaimed.
  `SessionWorkspaceSnapshot.expires_at` is the Sandbox deadline of a READY
  instance, shown with "Keep running for 7 days"; the volume stays until the
  Project is deleted.
- Start Preview stays available on an ACTIVE Session without a running READY
  workspace; the Preview waits until the Session's instance is READY. The
  Preview configuration surface reads the source prepared for the Session's
  HEAD and never wakes a Sandbox: an unprepared HEAD reads "Prepare the
  current source", and only that explicit preparation or a Preview start
  reaches a workspace.
- The conversation shows a Session notification as a notice, never a user
  bubble. A delivery notice is the platform variant: its title by source and
  outcome, the failure code, the short commit, who must act, a collapsible
  message and, for configuration gaps, the gap names, with links to the
  Preview and its configuration, the Release and its configuration, or the
  Deployment's Release. The Preview panel holds the "Configure Preview" entry,
  the switch "Update the Preview after every push" (on for a new Session,
  `UpdatePreviewAutoDeploy`), "Preview is at X, HEAD is Y", the newest
  automatic deployment with the commit that waits, and the gaps of a refused
  start or deployment; the configuration surface highlights the
  `requirement_gaps` of a configuration that is not READY.
- Preserve manifest-provided provider URLs, field guidance, defaults, alternative
  auth methods (a connection selects one), read-only pickers, and verified derived
  fields.
- Non-secret configuration changes create a revision and need redeployment;
  credential rotation takes effect on the next connector call.
- Missing Release/FDG/backend data shows unavailable or blocked state, never
  fixture fallback or another project's definitions.
- Use strict FDG 2.0 and server-derived permissions. Development fixtures do not
  provide production authentication or authorization.
- The BFF maps fixed native management operations to generated gRPC. Go freshly
  authenticates management requests, reads owning resources and selects exact
  configuration storage or application Dex gRPC. No BFF scope cache or management
  HTTP reverse proxy supplies authority.
- Keep tokens, secret storage paths, KMS details, credential values, the
  workspace lease and internal Sandbox endpoints out of
  browser responses, iframes, chat and progress feeds.

## Infrastructure as Code

The repository includes:

```text
infra-as-code/
  README.md, Makefile, sv2, source-lock.json, build-inputs.json
  environments/
    local-kind/values.yaml
    {test,staging,prod}/{cloud,kubernetes}.tfvars  # loaded by each HCP run from its own commit
    terraform-inputs.json  # owner of every variable an environment file does not set
  terraform/{cloud,kubernetes}/
  kubernetes/charts/superverse-platform/
    templates/  # platform workloads, immutable profiles and fixed admission policies
    charts/controller/  # pinned upstream agent-sandbox source
  scripts/
    kind/, native/, acceptance/, release/, hcp/  # one package per lane; native/compose.yaml is the Native platform
    commands.py, paths.py, pins.py, private_files.py, operator_errors.py, credential_http.py
    build_inputs.py, image_identity.py, published_dependencies.py, chart_validation.py
    git_host.py, config_files.py, coding_image.py, release_builder.py, local_platform.py  # shared by both lanes
  dependencies/published/  # reviewed published dependency identities, never sample digests
  releases/test/  # sanitized, reviewed acceptance and exact HCP plan identities
  tests/e2e/{local-kind,hosted-test}/README.md
```

One `sv2` entry selects local/terraform/hcp actions. Local and AWS deploy the same
Helm source; there is no parallel Kustomize or raw manifest copy. The Terraform
cloud layer owns ECR/artifacts, VPC/EKS, storage/KMS/IAM, platform Temporal and
cloud prerequisites. Platform consumes only its matching V2 cloud state and owns
the shared chart plus pinned upstream controllers. HCP bootstrap creates exactly
two V2 workspaces; it is not a third infrastructure layer. The `kubernetes`
directory and CLI layer select the exact catalog-named Platform workspace with
the neutral `superverse` and `platform` tags. Existing test workspace/state
identities remain; new staging uses independent `superverse-staging-platform`.

Hosted inputs are typed immutable image references, existing certificate and
Secret names, the actual Temporal endpoint, explicit dependency CIDRs and coding
workspace size. No local `values_file`, arbitrary Helm override map or secret
payload is passed through Terraform. Security policies are fixed with the parent
chart. Immutable profile hashes and retained reader grants survive template
relocation; the server receives the exact generated profile name.

Cloud component files preserve existing Terraform addresses, expressions and
resource names. The credentials-named KMS key remains the active blob encryption
key. Existing credentials/configuration buckets and IAM are retained until their
exact V2 remote state and data can be audited; an unused application reference
alone does not establish that deleting storage is safe.

Test/staging ordinary platform capacity is fixed On-Demand. Production keeps a
controller baseline and adds ordinary Spot capacity. Coding Sandboxes use a
separate tainted gVisor pool. Local Kind explicitly uses a development profile;
it does not prove hosted gVisor or IAM enforcement.

Server, Studio and coding images have independent input fingerprints. The server
input set follows Linux Go dependencies/embeds; Studio includes its local
`web/studio/components/agent` sources within the single application;
coding includes only its source/profile and verified immutable external contexts.
Business acceptance separately binds all runtime/IaC/E2E inputs, deployed resource
and secret identities, and exact images. Pure documentation does not rebuild images.

Kind has one dependency lane. `sv2 kind deploy` requires a committed approval
from `infra-as-code/dependencies/published/`, which names the published `bp1`
BuildProfile, project Dex engine image and official Connector artifact authority.
The local SDK lane (the `go-react-local-v1` profile, the coding image `local-sdk`
target and its `go.work` override) is retired by
the current contract. The Go configuration host uses
published Connector authorities and project-scoped storage only; it has no local
setup mode. Dex runs only the engine and consumes no
Connector catalog. Flow identities, primitives and persistent ownership remain
unchanged.

Root workflows provide static infrastructure checks and a manual test-release
dispatch. Complete real Local Kind acceptance on reviewed published dependencies
is required before publication; Native acceptance cannot authorize AWS. Exact accepted images are promoted without rebuilding. The release wrapper
inspects and approves only an existing VCS-backed HCP test run whose source,
images, input state and full plan digest match the reviewed record. It never
uploads a replacement configuration or invents another state owner. These paths
have not yet completed a real HCP/ECR/AWS run. See the
release runbook. Provider lockfiles remain
recorded.

The [project infrastructure roles](../../internal/projects/infra/README.md), which
the [Kubernetes runtime](../../internal/projects/infra/kube/README.md) implements
with a Local Kind backend and a [hosted backend](../../internal/projects/infra/kube/hosted/README.md),
own concrete isolated S3/KMS, workload IAM/Pod Identity and Temporal Cloud
effects behind the existing Project, Deployment and Sandbox Steps. Accepted
Flow references retain business authority; provider generations and UID checks
fence cleanup. Local Kind admission preserves the four Project/component owner
labels and the presence/value of the optional Session label on resource UPDATE;
Live deployment ID and generation labels may move with the existing Project
HTTPRoute. Namespace identity remains immutable. The hosted activation policy has
no whole-label-map equality constraint. A hosted grant or revocation that is still pending keeps its
lock-free polling Step waiting (`dexflow.PollUntil`), without adding another
Flow or mirrored lifecycle. The platform server checks the data plane of each
project's Temporal namespace with the project's own credential before a project
Dex uses it, so the hosted platform network policy admits the server, like
platform Dex, to public addresses on 7233. Its source and
chart checks do not establish actual AWS propagation, revocation or cleanup.

The initial complete cloud plan can create repositories with hosted builds
disabled. A later complete plan enables the builder with actual immutable
`build-host` and `build-tools` images; no targeted apply or placeholder digest is
used. Platform activation requires the enabled output. Published dependency
changes remain subject to the separately approved publication boundary.

Reuse the artifact version/digest and credential/config-reader boundaries.
Remove RDS and split old persistence flags so storage/IAM/build remain enabled.
Remove broker resources, Java/separate Agent services and speculative KMS ARN
fallback. The Go configuration host has scoped storage/provider capabilities for
explicit authenticated setup and OAuth; application identities use narrowly
scoped workload grants and perform expired credential refresh at actual use.

Use V2/environment names and tags, with account/region identity for buckets.
Do not adopt V1 state/resources. Read existing DNS/OIDC identities without taking
ownership; V2 TXT owner and name-scoped permissions prohibit overwriting V1.
Do not activate copied org/repository/runner roots. Actual implementation gaps
and cloud inputs are tracked in infra-as-code/README.md and receipts; this
inventory alone does not assert that hosted provisioning is complete.


### Platform Dex operator ingress

Hosted test and staging expose `dex-server.<environment>.dexai.dev` (TLS/gRPC to
platform Dex port 8801) and `dex-web.<environment>.dexai.dev` (HTTPS to platform
Dex Web port 8802). This is the explicitly authorized public operator boundary:
there is no login or operator IP allowlist. Product management remains in Studio/Go;
project Live Dex and the Sandbox's Preview Dex remain API-only and production
has no debug surface.
Flow/Step identities, primitives and persisted payloads do not change.

Local Native and Kind also enable the platform Dex Web listener. Kind's ClusterIP
Service retains gRPC 8801 and adds direct Web 8802, and Native's Dex container
publishes both; Kind serves loopback gRPC at 28811 and Web at 28812 and Native at
26811 and 26812, and the Kind forward supervisor and the Native launcher own the
cancellation and cleanup of their own. There is no local ALB, Web proxy,
forwarded-embedding trust or public listener. The lanes run at the same time;
each runs one owner of its own (Kind's action lock, Native's owner lock). Changes to an
active Native run require its owned stop and a new start; the lane's data,
credentials and Temporal/S3 facts survive. This
operator UI introduces no new Flow, Step, primitive, business lifecycle, database
schema or migration. Debug reads do not establish business E2E acceptance.

The debug surfaces share the environment's one platform ALB with Studio (IngressGroup
`<product>-platform`) and pin it to Cloud-owned public subnet IDs. Project traffic
reaches that ALB through one wildcard rule, last in the group, to the in-cluster Envoy
Gateway (the shared project Gateway); no per-Project object joins the group, so
no Project's route can block it.
NetworkPolicy admits those subnet CIDRs to gRPC 8801 and a stateless same-Pod Web
proxy on 8083; Web Service 8802 targets that proxy. The actual Dex Web 8802 listener
has no external admission. The digest-pinned official NGINX unprivileged proxy
replaces browser-provided forwarded/embedding metadata with fixed root, HTTPS and
environment-host values, sets `embedded=false`, and strips embedded CSRF/OAuth
metadata.
ALB's partial `X-Forwarded-Proto` is never trusted directly. No Flow, schema,
business image or Project listener changes. Bounded proxy resources and temporary
storage own no durable facts. NetworkPolicy independently preserves
private-client gRPC access to 8801. Cloud owns exact DNS write permissions; External
DNS owns the matching A/AAAA/CNAME and TXT records without taking V1 ownership.
The pinned CLI has no TLS dial option, so `cmd/dex-debug-tunnel` provides a
localhost-only verified TLS/H2 proxy with the fixed upstream authority. Existing
certificate, storage, Secret and Temporal identities remain unchanged. The formal
Kind release gate is unchanged; AWS-first operational recovery authorization and
observed results belong in the AWS receipt. See the linked IaC debug runbook.

Project Dex and application workloads keep their scoped storage identities. The
Go configuration host uses the existing Project storage scope and no network
admin token; unused admin-token fields and existing private credential objects
stay until the owning scope's final cleanup. Narrow compute reconciliation preserves namespace/Service UIDs,
volumes, Temporal namespaces and objects; Flow-owned reconciliation and cleanup
remain responsible for partial or uncertain provider effects. Provider TLS
verification remains mandatory.

## Tests

Every change executes the same three real journeys on the lanes
`make required-lanes` names: Local Native always, and Local Kind as well when it
touches a Kind-required path. Keep their runtime identities and receipts
separate. Never run both platform Workers concurrently or treat a Native receipt
as a Kind receipt.

Every major change must pass all three complete Local Kind scenarios on
`local-kind-acceptance.yaml` before an AWS wave, with the mock model or a real Claude
model as the platform developer decides before the run; the receipt records the answer. Real Studio/BFF/gRPC, Dex/Temporal, Agent, Git push, build, immutable
configuration, Go/SDK scoped storage and business application behavior are required.
Scripted fixtures are not business acceptance. Local API coverage does not replace
hosted provider acceptance.

1. **Registration:** register, log in, verify identity, log out, reject old session.
   Also wrong password, duplicate registration, expiry/revocation and two users.
2. **Create:** basic template project, whose creation page takes the model and
   the first message, its one Session opened by the Builder's chat, which applies the
   model and sends the message, "Keep running for 7 days", real Agent UI edit, checks,
   commit/push and remote SHA, Preview acceptance, Release artifact/FDG identity,
   Live business acceptance, deployment deletion/old-grant denial, project cleanup.
3. **Template approval:** basic template project whose one fixed prompt makes the
   real Agent add a human approval Step (`AwaitSummaryApproval`, Channel
   `SummaryApprovals`) and Action (`ApproveSummary`) to `TextSummaryFlow`, checks,
   commit/push and remote SHA; in Preview and in Live (Release build, configuration,
   deployment) start the Flow in Studio, see it wait at `awaiting-approval`, approve
   it through the Studio Action and require a completed real summary; deployment
   deletion and project cleanup.

Snapshot-first checks cover all five Watches including resume, subscription/read
races, pre-commit Stream rollback, silent trim/overflow, source/generation change,
Agent text recovery, cancellation and authorization loss. Validate that no
Deleted/bool1 or generic Operation API is generated. Named Studio APIs reject
cross-project access, stale serving definitions/instances/Deployments, arbitrary
Worker targets and readiness/UID mismatch.

Required scenarios:

- Agent behavior: backend, shared UI, model and coding-tool regression suites
  still run; production has no remote SuperAgent dependency.
- Identity: a READY Project has exactly one `session-<projectID>`, which
  StartProjectSession starts once, also across a repeated setup commit and a
  retried start, and a start with another RequestID conflicts; deletion before
  READY leaves no Session. Duplicate start retains FlowID/request identity;
  Step/RPC does not manufacture a new Run. The same main
  commit reuses its Release; a new deployment request creates a new deployment. A new
  Sandbox instance has a new WorkspaceRequestID and its own provisioning; a
  repeated start of the same request joins that provisioning, also a completed
  one, and provisions nothing again.
- Map keys: current/archive sequence, 10-message chunks, plan revision, CallID,
  recovery and batch instances; archive RPC exact-loads only its target chunk.
- Channels: answer/approval/steering/stop/timeout races, duplicate decisions,
  late generations, parallel join validation, and terminal leftover handling.
- Streams: trimming, disconnects and shared-budget pressure cannot corrupt
  canonical state. Reconnect reloads Snapshot rather than inventing completion.
- Persistence: cold BlobCache, server/Worker replacement, retained
  archived-session/Release reads, and the inherited
  approximately 1,000-message trimming behavior.
- Authorization: two users isolated across all read/write/stream/proxy paths;
  administrator can operate both with correct actor attribution. Wrong password,
  expired/revoked login and forged session identity fail.
- Concurrency: duplicate email/project creation, repository claim, release
  preparation, Live deployment, configuration revision and credential refresh.
- Agent: FIFO, steering, atomic answer batches, plan, approval, unknown tool
  recovery, bounded parallel order, Git conflicts and rechecking.
- Subagents: a repeated start joins the one execution and a late admission is
  denied; the running limit and the per-message budget refuse a start without an
  effect; a subagent reports or reaches its duration limit; a merge applies, discards
  or leaves a content conflict and repeats its receipt; `stop_task`, the Stop
  button and the Project's deletion each stop a running subagent, which saves its worktree; a Worker replacement during a subagent's
  command runs the command once.
- Preview: the Project's one Preview starts without a lease
  (`VerifyPreviewSource`); a reclamation removes its route, after which the
  local gateway answers 404 for the Project's host; the provider tests reconcile a route
  whose spec changed, remove it at a stop and create it again under the same
  host. StartPreview on a Session
  without a running READY instance is accepted, and the Preview acquires the
  instance once the Session announces it; a reclamation of the instance a READY
  Preview serves on stops the Preview first; a Preview configuration read with
  a reclaimed Pod reads the prepared HEAD from storage, starts no provisioning
  and leaves the workspace unchanged, and a stopped Session engine does not
  gate it. Sandbox UID checks, dirty workspace protection, image replacement
  and cleanup remain correct.
- Preview engine: a start runs the prepared commit on the Sandbox-local engine
  although the workspace moved on, and Runs reaches it through the Sandbox
  Service; a reclamation and the next instance on the retained volume show the same
  executions; a full or inconsistent state directory, a crashing and a
  never-healthy application, and an unreachable supervisor at reclamation
  (recorded as a forced stop) each end as specified; an unchanged restart
  reuses the grant generation and a changed one revokes the superseded grant.
- Preview auto-deploy: a turn that pushes hands its commit to the Preview,
  which deploys exactly that commit from its exported tree and serves it, and
  the ready notification holds the instance for the Preview hold without a
  conversation entry; a deployment whose application fails reports a failure
  owned by the application, which opens an automatic turn that pushes a fix,
  and the Preview serves the fix; a retried deployment request is a duplicate,
  a newer one queues, also while the running deployment's provider Steps
  save, and the newest queued one wins; a deployment refused for its
  definitions keeps the prepared commit with that commit's AppManifest, and a
  start then serves it; a deployment refused for the configuration it lacks
  started nothing and leaves the Preview as it found it, with its refusal and
  gaps, and the Preview starts once configured; a deployment on a reclaimed or
  released instance ends
  `SKIPPED`, tells nobody and wakes no Sandbox.
- Zero-Flow-RPC tool path: after the workspace is READY, read, search, patch,
  command, `git_diff` and `commit_and_push` calls, including a parallel
  read-only wave, produce no `session workspace cold path` SessionFlowService log line
  and no new provisioning or Preview history event; the Session's start creates
  no provisioning.
  A verified push updates the Session HeadCommit in the Step commit of its tool
  result without a HEAD RPC, and a later Preview preparation binds the real
  workspace HEAD.
  Mutating tools stay serial and parallel waves stay dynamic without a Runner
  mutex.
- Sandbox deadline and reuse: with a short `sandbox.hold` the Session revokes
  its idle instance's lease at the deadline and sends one reclaim command, and
  its SessionWorkspaceFlow reclaims the instance and reports it reclaimed, with
  no PreviewFlow call unless a Preview holds it; a running tool or background
  command postpones it; the
  Preview engine stops and the Pod is deleted while Volume, Namespace and the
  Session's Preview data remain, and the Session stays ACTIVE. The next demand
  provisions a new instance with a new WorkspaceRequestID that adopts the
  retained volume, including uncommitted changes. The workspace Flow reclaims
  the previous instance before a replacement and runs one command at a time,
  so one volume never carries two Pods.
  `RenewSessionWorkspace` moves the deadline seven days ahead, answers false
  without a READY workspace, and the deadline timer armed for the earlier
  deadline waits again instead of reclaiming.
- Workspace recovery: a Pod replaced or deleted under a usable lease leaves the
  call undispatched and pending, re-initializes the workspace and resumes the
  call without consuming Dex retries; more than two losses for one call end in
  the known failure `workspace_unavailable`. A provisioning outcome that is not
  delivered is read when the Session's bounded wait ends, a repeated report is
  idempotent, and a late outcome after a replacement is declined and reclaimed. Worker
  replacement during the cold path, a tool wave and a reclamation keeps the
  lease, the instance and the request counter.
- Workspace cleanup: Project deletion, the Session's only close, during a
  provisioning, a READY instance, a reclamation, a FAILED instance and a
  Failed, Canceled or Terminated Session engine leaves no Namespace, Volume,
  route object or Preview data and acknowledges only the Session membership; a release
  failure keeps the Project DELETING until a retried deletion completes.
- Pooled transport: connections and Local Native forwards are reused across
  tool calls, stay within their bounds, are evicted when idle and are rebuilt
  from the lease after a Worker replacement or an exited forward.
- OAuth: callback duplicate/expiry, single refresh, rotation, secret-write and
  metadata-commit crash windows, and explicit reauthorization-required outcomes.
- Release: exact artifact/configuration/digest matching; no missing-definition
  fallback; config edits require redeployment and credential refresh does not.
- Business E2E: the three Studio journeys above, with direct Live deployment
  rather than staging promotion.
- Isolation: Terraform plans, IAM, DNS records, Kubernetes resources, local
  ports and state paths do not mutate V1.

Use real Dex integration for cross-boundary durability and deadline-based
polling. Local helpers verify checkout path and source fingerprint, not just
port health. Do not weaken a failing test or call a partial chain E2E.

Deploy the same accepted commit/image digest to the isolated test environment.
Repeat all three complete browser scenarios in hosted AWS test. Then verify
hosted-only Pod Identity, KMS/IAM, gVisor, DNS/TLS, provider OAuth, refresh only
on expired use, restart and cleanup behavior. Do not automatically deploy platform staging/prod.

## Documentation

Keep repository documentation and code comments in English. Maintain this
architecture specification, Flow/Step graphs,
the proto/API reference, the state-owner table, isolation resource inventory,
and Local Kind instructions.

The owning architecture, API, and topic documents describe current behavior and verification boundaries.

### Mandatory Flow design review checklist

Every subsequent Dex Plan/Design must list, for each Flow:

1. **Identity:** Flow type, exact FlowID format/example, business-ID source,
   creation trigger, Start RequestID, reuse policy, retry/recovery/rerun Run
   behavior, and business versus engine completion.
2. **Attributes:** every primitive name/type, singleton versus AttributeMap,
   exact instance-key construction/example, authoritative owner, index,
   exact load/lock/CAS, growth bound, retention, deletion, and secret boundary.
3. **Channels:** every Channel/ChannelMap name and payload, instance keys,
   producers/consumers, wait/consumption, duplicate/rejected-message behavior,
   expiry, cancellation, and terminal cleanup.
4. **Streams:** names, payloads, producers/consumers, actual budget scope,
   retention/trim behavior, resume, best-effort semantics, and canonical
   recovery source. Do not confuse shared capacity with per-Flow retention.
5. **Steps/RPCs:** graph, RPC inputs/permissions, primitives read/written,
   timeouts, heartbeat, retries, recovery, cleanup, open-execution compatibility,
   and real-Dex integration/E2E acceptance.

Write `None` explicitly for unused primitive categories. A statement such as
"state is saved in Attributes" without this inventory does not satisfy review.

## Implementation order and acceptance

1. Freeze source snapshots, import complete source/rules and reusable IaC, and
   establish isolated builds.
2. Implement the merged Go binary, single business proto/gRPC API, entity
   ownership, Flow graphs, auth, and provider interfaces.
3. Integrate inherited Studio/BFF, Agent UI, Preview, Release/Live, connector
   setup and OAuth through the native Go host, with application SDK refresh.
4. Complete the approved refactors and Local Native on Docker. Run the three
   real journeys on the lanes `make required-lanes` names, then complete Local Kind acceptance from final source
   and images. Preserve distinct receipts and resolve remaining business gaps.
5. Publish the accepted commit/digests to the independent test environment and
   verify hosted-only controls.

V1 cutover, data migration, and retirement are outside this specification. The user
decides those only after V2 is stable.

## Credential reuse clarification

The user explicitly requires V2 to reuse all existing V1 credentials, including
the GitHub App and installation. An independent App or newly issued credentials
are not a prerequisite. V2 resource names, state, deployment ownership and data
remain independent. The existing GitHub installation owner is `superdurable-apps`;
new managed repositories are named `<prefix>-<name slug>-<projectID>` (see
deployment names) and carry the retained
ownership marker; repositories created earlier keep the names they have. Provider permission failures must be diagnosed against that existing
configuration rather than asking the user to create a replacement integration.

### Template automation boundary

New projects receive the committable files of `app-template/` with Git file modes preserved; the template contains no repository automation, so the reused GitHub App permissions are unchanged. `go generate ./internal/app/apptemplate` packs them into the embedded archive, and `make check-app-template` fails on drift. Every new repository receives this template.

Workspace account display is read once by the server layout per new document and shared with all headers via the generated AccountSnapshot context. Menu toggles and client navigation do not fetch /api/auth/session. This display is not authentication authority; see [API ownership](../api/resource-grpc.md#workspace-account-display).

### Acceptance-only starts

CreateProject, PrepareRelease, StartReleaseBuild and StartDeployment
return only the canonical ID of the resource they started; the Project's
Session starts inside ProjectFlow, never through the API. Successful StartFlow
has no immediate snapshot read; resource Get/Watch owns current state. Release
Start RequestID binds logical command plus unchanged immutable fingerprint so
only a typed AlreadyStarted branch invokes its existing recovery RPC. Repository
claim and durable on-demand workspace preparation retain actual business
milestone checks. See [StartFlow admission](start-flow-admission.md) for the
admission rules of each Flow.

### Editor readiness and Preview manifest preparation

An `ensure` command reaches READY after checkout and infrastructure admission,
without registering the source manifest: MarkSandboxReady only commits
READY/ExpiresAt and completion. Invalid or incomplete source
must remain editable by the original Session. Explicit prepare-preview-source
registers and verifies the exact manifest, preserving all clean/pushed HEAD,
instance, configuration and definition checks; a Preview start requires that
prepared HEAD and uses the manifest its preparation registered. No primitive,
object format, Flow identity, Channel, Stream or database is added.
Recovery preserves the original workspace volume and existing command identity;
a failed retained ensure can resume at its existing Step boundary after external
resource reconciliation. Preview admission errors do not grant Preview readiness.
