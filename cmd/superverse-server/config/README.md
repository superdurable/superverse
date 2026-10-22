# Server configuration

This package is the one place the Superverse server's settings are defined, defaulted,
loaded and validated. `cmd/superverse-server` loads it, `internal/dexai/bootstrap` composes the
server from it, and `make check-config` loads and validates every profile that is checked
in next to `config.go`. Only those three, `cmd/tools/check-config` and the layering rule
in `scripts/check-package-layers.py` may import it.

Every setting that is not a secret is a key of the YAML document below. New server
settings are added here with a default constant, a range in `validate.go` and a row in
this reference, never as a new environment variable. Secrets stay environment variables
of the server Secret and are never in a configuration file.

## Loading

`SUPERVERSE_CONFIG_FILE` names the configuration files, comma separated. The server loads
them in order on top of the defaults, so a later file overrides the keys it sets in an
earlier one, then validates the result. Parsing is strict: an unknown key, a value of the
wrong type or any violation of the rules below stops the server before it opens a listener,
and the error names the key.

`SUPERVERSE_CONFIG_FILE` is the only setting the server reads from its environment.
Durations are Go duration strings such as `90s`, `10m` or `720h`.

A key that no file sets keeps its default. A key that a file sets to an empty string is an error where the
setting is required, and no value is trimmed: `error ` is not `error`.

## Profiles

The checked-in profiles are layers. `local-kind.yaml` is the base of the Kind profiles, which override it;
`native.yaml` is complete on its own.

| File | What it configures |
| --- | --- |
| `local-kind.yaml` | Local Kind with the mock model offered and new Sessions on it (`agent.mockModel: true`, `agent.model: mock`) and the local Git host (`repohost: localgit`). `sv2 kind deploy` renders it into the server ConfigMap and adds the project image digests and the coding profile name that it computes. |
| `local-kind-acceptance.yaml` | The final acceptance before an AWS wave: the same, with `agent.mockModel: true` still offering the mock model and `agent.model: anthropic/claude-opus-5-5` starting new Sessions on a Claude model, and `repohost: github` with the GitHub App IDs and `github.connectors` naming the connectors test repository. It layers over `local-kind.yaml`, so `localKind: true` applies and any account may select the mock model, as in every Local Kind profile. Whether the acceptance runs the mock model or a real Claude model, and which, is the platform developer's decision (`SUPERVERSE_E2E_MODEL_TARGET` of `kind e2e`, which the receipt records as `modelTarget`). `release attest` accepts only a receipt that was deployed from this unchanged file and copies its `modelTarget`. |
| `native.yaml` | Local Native ([the current contract](../../../docs/runbooks/local-native.md)), `runtime: docker`: the mock model offered and new Sessions on it, the local Git host, and the default instance's loopback addresses. `sv2 native start` adds a last file with the selected `native.instance`, its shifted addresses and network prefix, and what only the run knows: `native.runId`, `native.stateDir`, `native.dockerSocket` of the Docker context, `localgit.dir`, `server.blobCacheDir`, `sandbox.profile` of the coding profile it rendered and the `live` images. |

The hosted environments set `agent.model`, `agent.mockModel` and `agent.closestExamplePick` through the
Terraform variables `agent_model`, `agent_mock_model` and `agent_closest_example_pick`, which every
environment's `kubernetes.tfvars` names explicitly: test and staging offer the mock model to the
administrator, prod never does, and all three start a Project created from a first message from the
closest example.

`sv2 kind deploy --config cmd/superverse-server/config/<profile>.yaml` (the default is
`local-kind.yaml`) chooses the profile. A deployed profile must configure Local Kind: the deploy refuses
one that does not set `localKind`, that selects `runtime: docker` or that has a `native` section, such as
`native.yaml`, which only `sv2 native start` runs. The deployment inputs and the E2E receipt record the profile
name, the digest of each layer and the document that the server loaded.
A hosted environment has no profile file: its Terraform wiring renders the same document from chart
values (`server.config`) into the ConfigMap, and the chart fills the addresses it owns, such as the
Dex Service name, and fixes `environment`, `localKind` and `sandbox.profile`.

## Reference

### Top level

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `environment` | The [deployment-name catalog](../../../internal/comm/platform/deploymentnames/README.md) entry of this server | `test` | `test`, `staging` or `prod`; any other name is an error |
| `localKind` | The local lanes, Local Kind and Local Native | `false` | `true` only with `environment: test` |
| `runtime` | Where project workloads run: `kubernetes`, the server's cluster (Local Kind and the hosted environments), or `docker`, the local Docker Engine of Local Native, where the server starts without a Kubernetes API | `kubernetes` | `kubernetes` or `docker`; `docker` only with `localKind` and `environment: test`, and the `native` keys only with `docker` |
| `repohost` | The Git host of every Project repository: `github`, or the [local Git host](../../../internal/comm/platform/repohost/localgit/README.md) inside the server | `github` | `github` or `localgit`; any other name is an error; `localgit` only with `localKind` |

A new Project's repository is named `<prefix>-<name slug>-<projectID>`. No key sets the prefix: server
composition derives it from `runtime`, `localKind` and `environment`, whichever `repohost` is selected.
`runtime: docker` (Local Native) is `localnative`, `localKind: true` on the `kubernetes` runtime (Local Kind,
also `local-kind-acceptance.yaml`) is `localkind`, and any other server takes the name of its `environment`:
`test`, `staging` or `prod`. The [deployment names](../../../docs/architecture/superverse-v2.md)
define the slug.

### `accounts`

| Key | Meaning | Default | Valid range |
| --- | --- | --- | --- |
| `platformAdminEmails` | Canonical emails of ACTIVE ordinary accounts with platform-wide authority | `[admin@superverse.test]` | 1–20 unique lowercase plain email addresses; hosted environments set their own list; the singular key is rejected |

### `server`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `grpcAddress` | Listener of the business gRPC API | `:8080` | |
| `healthAddress` | Listener of `/healthz` and `/readyz` | `:8082` | |
| `blobCacheDir` | Dex payload blob cache, exclusive to this process | `/tmp/superverse-blob-cache` | |
| `projectIDLength` | Length of a new Project ID | `4` | `1` to the catalog's maximum for the environment |
| `rpcCoverageFile` | Private file of business RPC methods served by a local test server | none | Absolute path in Local Native or Local Kind only; disabled in hosted profiles |

### `dex`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `serverAddress` | Platform Dex Flow API | `127.0.0.1:8801` | |
| `pull.maxConcurrentTasks` | Maximum simultaneous platform Worker handlers | `256` | `1` to `256` |
| `pull.idleStreamsPerQueue` | Idle stream target per registered Flow type and process queue | `1` | `1` to `8` |
| `pull.stopDrainTimeout` | Drain before cancelling active handler contexts | `60s` | `1s` to `60s`; fits the 120-second Pod grace with preStop and listener shutdown |

The platform registers 16 Flow types. SDK 1.14.2 retires surplus idle streams after load subsides. Its steady-state stream bound is `maxConcurrentTasks + (F + 1) * idleStreamsPerQueue`: 273 at the defaults and 392 at the supported maxima. Bootstrap refuses a registry/options combination above `PlatformDexWorkerStreamCap` (512). Platform Dex must set `matching.maxConcurrentTasksCap: 512` and `matching.maxStreams: 100000`; these count registered streams, including idle streams. The 120-stream margin above the supported maximum accommodates transient bursts but is unmeasured. Budget every server replica and rollout overlap against one matching instance, plus other pull Workers; 195 Workers at the per-Worker cap consume 99,840 streams. Review both settings before increasing replicas or adding Flow types.

The 256 slots are shared by Steps and RPCs, with no reserved RPC capacity. Size each replica for active Session handlers (up to four during parallel tools), background watches, subagents, provider/build/cleanup polling and RPC bursts. A workload of 32 Session handlers, 32 background watches, 32 subagent handlers, 64 pollers and 32 RPCs uses 192 slots; simultaneous four-way tool waves can exceed this envelope. A finite cap cannot match uncapped push under arbitrary load. Engine-side Timer, Channel and SubFlow waits release their handler after `WaitFor` returns; `PollUntil` holds one for its full attempt.

Each open stream uses SDK goroutines, contexts, control channels and gRPC stream state; a busy stream adds a task goroutine and its loaded payload/state. The matching cap bounds registered streams during bursts, while the SDK retires surplus idle streams after load subsides. Two initial Go stacks alone cost at least 4 KiB per stream; 16–64 KiB per stream per endpoint is a planning estimate, not measured memory or a payload bound. Keep application payload limits and container memory limits in the capacity budget. Matching sends idle Ping/Pong traffic every 20 seconds by default; the default idle target of 17 streams produces about 0.85 exchanges per second per Worker after all tasks finish.

The Worker opens outbound streams to `serverAddress`; it has no listener or advertised address.
Readiness waits for every queue's acknowledged registration. A permanent rejection with no remaining registered stream on its queue stops the Worker; rejected surplus streams can log and retry. Initial readiness does not prove the server accepts the full stream envelope.

### `studio`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `publicOrigin` | Origin that OAuth providers return actors to; Studio reads its own `SUPERVERSE_STUDIO_PUBLIC_ORIGIN` and the two must agree | none | required; an `http` or `https` origin with a host and no path, query, fragment or user |
| `runsDexConnections` | Verified Dex connections that Studio Runs pools on each replica ([Runs target cache](../../../docs/architecture/runs-target-cache-and-watch-authorization.md#dex-connection-pool)); a replica with more targets in use re-verifies the least recently used | `128` | 1 to 4096 |

### `agent`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `model` | Model a new Session starts with | none | required; `anthropic/claude-opus-5-5`, `anthropic/claude-sonnet-5-5`, `anthropic/claude-haiku-4-5`, or `mock` with `mockModel` in Local Kind and Native only |
| `effort` | Base effort of every model call, clamped per model | `medium` | `low`, `medium`, `high`, `xhigh` or `max` |
| `subagentInvestigateEffort` | Base effort of the model calls of an `investigate` subagent, clamped per model | `low` | `low`, `medium`, `high`, `xhigh` or `max` |
| `subagentChangeEffort` | Base effort of the model calls of a `change` subagent, clamped per model; the subagent may raise it with `set_effort` | `medium` | `low`, `medium`, `high`, `xhigh` or `max` |
| `subagentConnectorDevelopEffort` | Base effort of the model calls of a `connector_develop` subagent, clamped per model; the subagent may raise it with `set_effort`. A `connector_select` subagent runs at `medium`, fixed | `high` | `low`, `medium`, `high`, `xhigh` or `max` |
| `compactionTokens` | Context size that triggers a compaction, capped per model | `200000` | `20000` to `800000` |
| `thinkingPrefixMismatch` | What the API does with a replayed thinking block that fails its conversation check | `drop_block` | `drop_block` or `error` |
| `taskTurns` | Consecutive turns that task notifications open before automatic continuation pauses | `5` | `0` to `100` |
| `platformTurns` | Consecutive turns that application-owned delivery failures of the Session's HEAD, such as a Preview whose application failed, open before automatic continuation pauses; `0` opens none | `3` | `0` to `100` |
| `mockModel` | Start the [mock model](../../../internal/dexai/agents/session/modelprovider/mockmodel/README.md) in this process and offer it to every Session as the model `mock`, besides the Claude models: any account selects it in Local Kind and Native, only the administrator elsewhere | `false` | refused in `prod` |
| `closestExamplePick` | Start a Project created from a first message, with no example picked, from the [example the server finds closest](../../../app-template/examples/README.md#closest-example) to that message instead of the default; the Session's agent adapts it to the message without announcing it | `false` | on in `native.yaml` and `local-kind.yaml` (and so `local-kind-acceptance.yaml`), and in test, staging and prod through Terraform's `agent_closest_example_pick` |

### `sandbox`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `hold` | How long the Sandbox stays after the main agent or a Preview command last used it; the Session reclaims the idle Pod at that deadline and keeps the volume | `1h` | `1m` to `168h` |
| `previewHold` | How long the Sandbox stays after a Preview became READY on it, which the Preview's ready notification tells the Session | `168h` | `1m` to `168h` |
| `profile` | Name of the installed immutable coding profile | `coding-sandbox-profile-coding-v4` | not empty; the chart and `sv2 kind deploy` supply the content-addressed name |

### `storage`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `s3Endpoint` | S3 endpoint of its own, such as MinIO | none (AWS S3) | only with `localKind` |
| `browserEndpoint` | Storage endpoint that browsers reach, which presigned uploads and downloads name; shared by attachments and knowledge documents | none (the bucket's regional S3 endpoint) | an `http` or `https` origin, `http` only with `localKind`; required with `s3Endpoint`. `local-kind.yaml` names the `kind ports` MinIO forward, `http://127.0.0.1:29000`; `native.yaml` the default instance's MinIO and the Native launcher each instance's |
| `artifactBucket` | Bucket of the release artifacts | none | required |
| `artifactKMSKeyID` | KMS key of the artifact bucket | none | required outside Local Kind |

### `attachments`

The bounds of the files people attach to messages.
Sizes are bytes.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `maxPerMessage` | Attachments of one message | `10` | 1 to 10, the Session's bound |
| `imageLongEdge` | Long edge of an image in pixels, which Studio normalizes to | `2000` | 256 to 2000 |
| `imageMaxBytes` | Size of an image | `1048576` (1 MiB) | 64 KiB to 4 MiB; one message of `maxPerMessage` attachments must fit `segmentBytes` (below) |
| `documentMaxBytes` | Size of a Markdown, text, Word or PDF document | `20971520` (20 MiB) | 1 MiB to 30 MiB, below the 32 MiB object limit |
| `jsonMaxBytes` | Size of a JSON file | `4194304` (4 MiB) | 64 KiB to 4 MiB, the extractor's JSON limit |
| `messageInlineText` | Document text one message renders inline | `122880` (120 KiB) | 16 KiB to 256 KiB; within one message's bound (below) |
| `segmentBytes` | Rendered attachment blocks of one context segment, which every model call of the segment resends | `20971520` (20 MiB) | 1 MiB to 24 MiB, and at least the rendered blocks of one message (`conversation.MessageAttachmentBytes`): of its `maxPerMessage` attachments, any number images of `imageMaxBytes` as base64 in their blocks, the rest documents and JSON within `messageInlineText` or their 8 KiB briefs; about 14 MB by default. A fresh segment then always shows a whole message |
| `segmentImages` | Images of one context segment | `60` | 1 to 90, at least `maxPerMessage` |
| `uploadWait` | How long a registered upload waits for its PUT | `1h` | `1m` to `24h` |
| `stagingRetention` | How long a prepared attachment stays staged | `24h` | `1h` to `72h` |
| `teamSpaceStagedFiles`, `teamSpaceStagedBytes` | A TeamSpace's staged attachments | `200`, `2147483648` (2 GiB) | 10 to 2000; 100 MiB to 20 GiB |
| `projectLifetimeFiles`, `projectLifetimeBytes` | The attachments a Project's messages send over its lifetime | `1000`, `2147483648` (2 GiB) | 10 to 10000; 100 MiB to 20 GiB |
| `renditionCacheBytes` | This replica's cache of rendition objects | `268435456` (256 MiB) | 16 MiB to 1 GiB |

The byte quotas hold at least the largest file a kind admits.

### `postgres`

The PostgreSQL database of the accounts ([accounts](../../../docs/architecture/teamspace/accounts.md)) and of the read
model, which platform Dex writes as the role `superverse_dex`. The server connects as the role `superverse_server` and
`superverse-server migrate` as `superverse_owner`; each environment creates the three roles with their passwords, which
are secrets.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `host` | Host of the database server | none | required |
| `port` | Port of the database server | `5432` | 1 to 65535 |
| `database` | Database name | `superverse` | required |
| `sslMode` | `disable`, `require` or `verify-full` | `verify-full` | `disable` only with `localKind` |
| `maxConnections` | Bound of the server's connection pool | `16` | 1 to 100 |

Hosted environments connect to the RDS instance of the cloud Terraform layer: Terraform renders `host`, `port` and
`database` from its outputs and fixes `sslMode: require`. The server image does not trust the RDS certificate
authority, so `verify-full` is not available there, and the instance refuses connections without TLS
(hosted PostgreSQL).

### `connections`

The TeamSpace connections ([connections](../../../docs/architecture/teamspace/connections.md)): the credential read
endpoint that applications call, where the platform's provider calls may connect, and the OAuth applications the
platform registered. Their client secrets are the secret `SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS`.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `readListen` | Address of the credential read endpoint's own listener | `:8086` | a host:port apart from every other listener; port 8086 with runtime kubernetes, the port the chart, its `<product>-credential-read` policy and the projects' `allow-application` policies admit (the chart sets it); `127.0.0.1:<port>` with runtime docker (`127.0.0.1:26087` in `native.yaml`) |
| `mockLLM` | Serve the local application summary mock on the credential listener and inject its reserved workload URL; requires a local lane and `agent.mockModel` | `false` | Local Native and Local Kind only |
| `readURL` | The endpoint as applications reach it; configuration documents and frozen Live versions carry it, so it is fixed per environment | none | required: an absolute URL of `/internal/connection-credentials/read`, http only to a loopback, private, single-label or cluster-local host; the chart sets the server Service's cluster address on every Kubernetes environment; with runtime docker `http://host.docker.internal:<readListen port>/…` |
| `oauthCallbackOrigin` | The origin of the OAuth redirect URI `<origin>/api/connector-oauth/callback` that providers return actors to | `studio.publicOrigin` | only with `localKind`, for a developer's manual test whose callback proxy redirects to the local Studio ([Slack relay manual test](../../../docs/runbooks/slack-relay-manual-test.md)) |
| `allowInsecureOAuthEndpoints` | Provider calls may use plain http to loopback and private addresses, such as the lanes' local OAuth server; otherwise only https to public addresses | `false` | only with `localKind` |
| `platformClients` | The platform's OAuth applications by `<connector>/<method>` (`default` for a connector without methods): `clientId` and the `scopes` the provider verified | none | each with a client ID and distinct scopes without spaces, and a secret |
| `slackRelay.listenAddress` | The Slack event relay's public listener, which serves only `POST /api/slack/events` and only with `SUPERVERSE_SLACK_SIGNING_SECRET` | `:8087` | a host:port apart from every other listener; port 8087 with runtime kubernetes, the port the chart and its load balancer route; `127.0.0.1:<port>` with runtime docker (`127.0.0.1:26088` in `native.yaml`) |
| `slackRelay.queue.implementation` | The relay's queue | `memory` | only `memory` |
| `slackRelay.queue.maxEvents` | Queued events in all scopes of one replica | `10000` | 1 to 1000000 |
| `slackRelay.queue.maxBytes` | Queued bytes in all scopes of one replica | `67108864` (64 MiB) | `maxEventBytes` to 1 GiB |
| `slackRelay.queue.maxEventBytes` | One event, and the events endpoint's body limit | `1048576` (1 MiB) | 1 KiB to 16 MiB |
| `slackRelay.queue.maxScopeEvents` | Queued events of one Project scope | `100` | 1 to `maxEvents` |
| `slackRelay.delivery.timeout` | One POST to an application | `10s` | 1s to 1m |
| `slackRelay.delivery.concurrency` | Deliveries in flight across scopes; one per scope | `16` | 1 to 256 |

### `builds`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `localURL` | Host build service of Local Kind and Native (`http://127.0.0.1:27792` in `native.yaml`); it builds only with its token | `http://host.docker.internal:29792` | used only with `localKind` |
| `hosted.project` | The environment's CodeBuild project | none | required outside `localKind`; the catalog's `build_project` |
| `hosted.repository` | ECR repository of the built applications | none | `<registry>/<ecr_prefix>/applications` |
| `hosted.bucket` | Versioned S3 bucket of build artifacts | none | |
| `hosted.prefix` | Artifact key prefix | none | the catalog's `artifact_prefix` |
| `hosted.kmsKeyARN` | KMS key of the build artifacts | none | a key in the configured account and region |
| `hosted.toolImage` | Reviewed immutable build-tools image | none | `<registry>/<ecr_prefix>/build-tools@sha256:...` |
| `hosted.hostImage` | Reviewed immutable CodeBuild host image | none | `<registry>/<ecr_prefix>/build-host@sha256:...` |
| `logReadsPerSecond` | Build log reads one replica starts per second (`ReleaseService.GetReleaseBuildLog` and the Agent's log tail), a token bucket with a burst of twice the rate; the viewers of one page share a read for two seconds, and a deferred read answers an empty page with `retry_after_ms`. Each hosted read makes one CloudWatch Logs `GetLogEvents` call, at most two, against the account's 10 per second | `2` | 0.1 to 10 |

Every `hosted` key is required outside Local Kind; the hosted builder also checks each against the catalog when it starts.

### `github`

`owner`, `appId` and `installationId` are used with `repohost: github`, which hosted environments and
`local-kind-acceptance.yaml` set. The other host leaves them unused and needs no GitHub App private key.

`issues` names the repository where the platform files its incidents, a platform failure of a Session reported
once per signature as a GitHub issue, and the App's installation on it: a second installation of the same App, so
`appId` and the App private key are needed whatever the `repohost`. Without it, the default, the server only logs
each incident. Every local profile in this directory leaves it empty. The hosted test and staging
Terraform inputs select the configured issue repository through its private installation ID; prod leaves it empty.

`connectors` names the upstream connectors library that `connector_develop` subagents contribute to
([connectorlibrary](../../../internal/comm/platform/connectorlibrary/README.md)) and the App's installation on it, a further
installation of the same App, so `appId` and the App private key are needed whatever the `repohost`. Hosted environments
name `superdurable/dex-connectors-library` (Terraform, `github_app.connectors_installation_id`), and
`local-kind-acceptance.yaml` names its GitHub test copy `superdurable/dex-connectors-library-test`. Without it, the
default and the setting of `local-kind.yaml` and `native.yaml`, no contribution reaches GitHub, an explicit recorded
state: in the local lanes the branch is pushed to the local Git host's copy of the library, otherwise it stays in the
Sandbox's clone, and the contribution records `unpublished` with no pull request.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `owner` | Account that owns the managed repositories | `superdurable-apps` | required with `repohost: github` |
| `appId` | ID of the GitHub App; not a secret | none | required with `repohost: github`, `issues` or `connectors`; positive |
| `installationId` | Installation of the App; not a secret | none | required with `repohost: github`; positive |
| `issues.repository` | The repository of the incident issues, `owner/name`; the App needs its Issues permission there | none: incidents are only logged | set together with `issues.installationId` or neither; a GitHub owner of letters, digits and hyphens and a repository name of letters, digits, `.`, `_` and `-` |
| `issues.installationId` | Installation of the App on that repository; not a secret | none | set together with `issues.repository` or neither; positive |
| `connectors.repository` | The upstream connectors library, `owner/name`; the App needs Contents and Pull requests write there | none: contributions stay unpublished | set together with `connectors.installationId` or neither; a GitHub owner of letters, digits and hyphens and a repository name of letters, digits, `.`, `_` and `-` |
| `connectors.installationId` | Installation of the App on that repository; not a secret | none | set together with `connectors.repository` or neither; positive |

### `localgit`

The local Git host, used with `repohost: localgit` in Local Kind and Native. It serves bare repositories of
`superverse-local/<prefix>-<name slug>-<projectID>`, such as `superverse-local/localkind-myapp-a7k2`, over the standard smart HTTP protocol, with credentials signed by the
`SUPERVERSE_LOCAL_GIT_SIGNING_KEY` secret.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `dir` | Directory of the bare repositories. Local Kind mounts the main checkout's `.local/superverse-v2/git` there from the Kind node; Native's launcher sets its own `.local/superverse-v2/native/git` | none | required with `repohost: localgit`; an absolute path |
| `listen` | Address of the Git HTTP server: `0.0.0.0:8085` in the Pod, `127.0.0.1:26085` on the Native host | none | required with `repohost: localgit`; `host:port` |
| `cloneBaseURL` | Origin a Sandbox clones from: the `git-http` port 8085 of the server Service in Kind, `http://host.docker.internal:26085` in Native | none | required with `repohost: localgit`; an `http` or `https` origin with a host and no path, query, fragment or user |

### `live`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `dexImage` | Dex engine image of every project scope | none | required; `<name>@sha256:<64 hex>` |
| `temporalImage` | Temporal image of a project scope in the local lanes | none | required with `localKind`; pinned by digest |
| `s3Image` | MinIO image of a project scope in the local lanes | none | required with `localKind`; pinned by digest |
| `retirementDrain` | How long a replaced Live generation keeps running after the new one committed, before its grant is revoked and its objects are removed ([the current contract](../../../docs/architecture/studio-setup/flow-changes.md)). Ten minutes cover the ALB's 300-second target deregistration, the seconds the read model lags and in-flight requests and Runs reads. The local profiles set `30s` | `10m` | 30s to 24h |

### `routes`

The public routes of the local lanes: every application at `<scope>-<project>.localhost:<localPort>`, through one
gateway.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `localPort` | Port of the public application URLs: 23080 in Kind, 21080 in Native | `23080` | a TCP port; with `localKind` |
| `gatewayURL` | Where the server probes a route with the route's Host header: the gateway Service in Kind, `http://127.0.0.1:21080` in Native | none | required with `localKind`; an `http` or `https` origin, on 127.0.0.1 with `runtime: docker` |

### `aws`

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `region` | Region of the AWS clients | none | required |
| `accountId` | The V2 AWS account; hosted startup refuses credentials of any other account | none | required outside Local Kind; 12 digits |

### `projectCloud`

The project cloud account of every environment but Local Kind, where every key below is required. The hosted
provider also checks each value against the catalog when it starts.

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `eksClusterName` | The environment's cluster | none | the catalog's `cluster` |
| `bucketPrefix` | Prefix of the project buckets | none | |
| `kmsKeyARN` | KMS key of the project storage | none | |
| `permissionsBoundaryARN` | Permissions boundary of project roles | none | |
| `temporalAddress` | Shared private Temporal frontend for hosted Project namespaces | `xtemporal-frontend.temporal.svc.cluster.local:7233` | must equal this in-cluster endpoint; plaintext, port `7233` |
| `temporalRetentionDays` | Retention of project Temporal namespaces | `30` | `1` to `90` |

### `evolve`

Goal measurement: the bounds of the run export of each review, the list prices of its AI usage
and where agent reviews run ([the package](../../../internal/dexai/delivery/evolve/README.md)).

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `maxRunsPerExport` | Most runs one review exports; the export is marked truncated beyond | `5000` | 1 to 20000 |
| `maxHistoryEventsPerRun` | Most history events read per run; a longer run's record is marked truncated | `2000` | 100 to 10000 |
| `maxReadsPerExport` | Most reads of the Live engine one export makes | `40000` | 100 to 200000 |
| `maxConcurrentExports` | Exports one server replica runs at once; the others wait, heartbeating. Every running or waiting export Step holds one of the replica's `dex.pull.maxConcurrentTasks` slots | `4` | 1 to 8 |
| `keepExports` | A Project's newest exports kept in storage; a review deletes older ones when it begins | `4` | 1 to 25 |
| `modelPrices` | List prices `{model, inputPerMTok, outputPerMTok}` in dollars per million tokens, keyed by the API model ID a model connector reports; a model without a price shows tokens and no dollars | empty | each model once; 0 to 10000 |
| `priceTableAsOf` | The date of `modelPrices`, which each export's manifest carries | empty | `YYYY-MM-DD`, required with `modelPrices` |
| `reviewAccess` | Which Projects get agent reviews after measurement: `none`, `listed` (the TeamSpaces below) or `all`. Measurement runs for every Project with valid goals | `listed` | one of the three |
| `reviewTeamSpaces` | TeamSpace IDs with agent reviews under `listed`, kept in private environment configuration | empty | at most 100 canonical TeamSpace IDs, each once |

### `native`

Local Native, `runtime: docker`: the server, Studio and the Release builder run on the host and every project
workload on the local Docker Engine, beside the platform Temporal, MinIO and Dex, the gateway and the registry that
the launcher runs with Docker Compose ([runbook](../../../docs/runbooks/local-native.md)).

| Key | Meaning | Default | Range or rule |
| --- | --- | --- | --- |
| `runId` | The launcher's run identity, which labels the health answers | none | set by the launcher; 8 to 64 lowercase letters, digits or hyphens |
| `stateDir` | The lane's private state directory: the credentials the server generates, the gateway's route files and the rendered coding profiles | none | set by the launcher; an absolute path |
| `dockerSocket` | Unix socket of the Docker Engine API | none | an absolute path; the launcher sets the socket of the current Docker context |
| `networkPrefix` | Private IPv4 network from which every Docker network of the lane takes a /24 | none | a private IPv4 network from /8 to /24, such as `10.233.0.0/16` |
| `registry` | The local registry, under which the engine pushes and pulls Release images alike | none | `localhost:<port>` |
| `gatewayAPI` | Origin of the gateway's API, which confirms that a route is loaded | none | an `http` origin on 127.0.0.1 |
| `routesListen` | Loopback address where the server serves the gateway its routes | none | `127.0.0.1:<port>` |

`native.yaml` gives every address a port of the lane's own, apart from the Kind lane's, so the lanes run at the same
time; the launcher refuses a port that Kind serves ([runbook](../../../docs/runbooks/local-native.md#beside-local-kind)).

With `runtime: docker` every listener of the server (`server.grpcAddress`, `server.healthAddress`,
`dex.serverAddress`, `native.routesListen` and, with `repohost: localgit`, `localgit.listen`)
is `127.0.0.1:<port>`, `routes.gatewayURL`, `native.gatewayAPI`, `storage.s3Endpoint`, `storage.browserEndpoint` and `builds.localURL` are
`http` origins on 127.0.0.1, `localgit.cloneBaseURL` is `http://host.docker.internal:<port of localgit.listen>`, where containers reach the host
processes.

## Secrets (environment only)

The server reads these from its environment and from nowhere else. They never appear in a configuration file,
an error, a log, a receipt or the chart values.

**Local Kind.** `sv2 kind deploy` writes the server Secret from the generated values, from the required
`--secret-env-file` and from the main checkout's `.env`. The Secret is replaced as a whole, so the deploy refuses to
start unless it holds every secret below that is required, names a missing one and never prints a value.

The env file may be shared with other layers and may hold any other keys too: the deploy takes only the secrets of the
first four rows from it. Every other key is ignored, so it reaches neither the server Secret nor the configuration, and one
notice names the ignored keys, never their values. The deploy refuses a file that carries `ANTHROPIC_API_KEY`:
model keys are Project-owned RPC inputs. Local launchers never read them for the server environment.

**Local Native.** `sv2 native start` passes the host server its secrets in the environment: the generated and
retained values of the lane's state directory (the platform MinIO credentials, the cookie signing key, the build
token file, the local Git signing key and the server’s database password). It takes no env file. Before the server it runs `superverse-server migrate` with only the owner's password.

| Variable | Meaning | Source in Local Kind |
| --- | --- | --- |
| `GITHUB_APP_PRIVATE_KEY_BASE64`, `GITHUB_APP_PRIVATE_KEY`, `GITHUB_APP_PRIVATE_KEY_PATH` | The GitHub App private key as base64, as PEM or as a file path; the path wins, then base64, then PEM. One is required with `repohost: github`, `github.issues` or `github.connectors` and unused otherwise | the env file |
| `SUPERVERSE_LOCAL_BUILD_TOKEN`, `SUPERVERSE_LOCAL_BUILD_TOKEN_FILE` | Shared token of the Local Kind build service, as a value or as a file path (preferred). One is required; without it Local Kind has no builder | the env file (`release-build.env` holds the token) |
| `SUPERVERSE_COOKIE_SIGNING_KEY` | Standard base64 key, at least 32 bytes, that signs Account session tokens and Watch cursors | generated and retained by the deploy; the env file may replace it |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | MinIO's credentials, read by the AWS SDK default chain | generated by the deploy; the env file may replace them |
| `SUPERVERSE_LOCAL_GIT_SIGNING_KEY` | Standard base64 key, at least 32 bytes, that signs the repository credentials of the local Git host. Required with `repohost: localgit` and unused otherwise | generated and retained by the deploy as `git-signing-key` in the Kind state directory; the env file cannot replace it, because the end-to-end runner derives its administrator token from the same file |
| `SUPERVERSE_POSTGRES_PASSWORD` | Password of the server's database role `superverse_server` | generated and retained by `kind up` in `postgres-credentials.json` of the Kind state directory, which also sets the role's password; the deploy puts it in the server Secret and the env file cannot replace it |
| `SUPERVERSE_POSTGRES_OWNER_PASSWORD` | Password of the schema owner `superverse_owner`; only `superverse-server migrate` reads it | the same file; `kind up` puts it in the `superverse-v2-postgres` Secret, which only the server Pod's migration init container reads |
| `SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS` | A JSON object from each `connections.platformClients` key to its client secret; a configured client without a secret, or a secret without a client, refuses the start | the main checkout's `.env` for a developer's manual test ([Slack relay manual test](../../../docs/runbooks/slack-relay-manual-test.md)); otherwise none: the local lanes configure no platform OAuth application |
| `SUPERVERSE_SLACK_SIGNING_SECRET` | The platform Slack application's Signing Secret; the Slack events listener serves only with it, and a `connections.platformClients` key of `slack/...` without it refuses the start | generated and retained by the deploy as `slack-signing-secret` of the Kind state directory (Native: of the instance's state directory), which the relay tests sign events with; the main checkout's `.env` replaces it for a developer's manual test |
Project model credentials are entered through the Agent picker and `ModelCredentialService`.
The server does not read `ANTHROPIC_API_KEY` for Sessions; mock requests use a fixed placeholder.

**Hosted environments** (the pre-provisioned `<product>-server` Secret; Terraform neither reads nor writes it):
the chart uses explicit references to the documented keys below, not a blanket Secret import. Legacy
model key entries cannot enter the server environment; the next release removes them from existing
Secrets as the deployment notes require.
`SUPERVERSE_COOKIE_SIGNING_KEY`, one form of the GitHub App private key (hosted environments
always use `repohost: github`),
`SUPERVERSE_POSTGRES_PASSWORD`, the password of `superverse_server`, and, once the environment configures
`connections.platformClients`, `SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS`, and `SUPERVERSE_SLACK_SIGNING_SECRET`: without it the server
does not open its Slack events listener, so the platform ALB's `/api/slack/events` rule routes to a port nothing serves
and Slack's deliveries fail. The owner's password,
`SUPERVERSE_POSTGRES_OWNER_PASSWORD`, is not in that Secret: only the migration init container reads it, from the
pre-provisioned `<product>-postgres` Secret. Both passwords come from the private procedure of the
hosted PostgreSQL runbook.
The AWS SDK default chain reads its credentials from the pod identity (`AWS_ROLE_ARN`, `AWS_WEB_IDENTITY_TOKEN_FILE`
and what the identity webhook injects); they are not settings and not in the Secret.

## Environment that is not a setting

| Variable | Meaning |
| --- | --- |
| `SUPERVERSE_CONFIG_FILE` | The configuration files above |
| `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT_HTTPS` | Announced to every Pod by the kubelet; they locate the cluster API server |
| `SUPERVERSE_TEST_*` and the variables that a test names | Inputs of a test process, not of the server |

## Replaced environment variables

Every earlier environment variable of the server maps to one key. A name on the left that still exists in a Secret
or an env file has no effect: the server reads only the secrets above and `SUPERVERSE_CONFIG_FILE` from its environment,
the Kind deploy and Native ignore such keys, and a hosted Secret can drop them when convenient.

| Environment variable | Key |
| --- | --- |
| `SUPERVERSE_ENV` | `environment` |
| `SUPERVERSE_LOCAL_KIND` | `localKind` |
| `SUPERVERSE_PROJECT_ID_LENGTH` | `server.projectIDLength` |
| `SUPERVERSE_GRPC_ADDRESS`, `SUPERVERSE_HEALTH_ADDRESS` | `server.grpcAddress`, `server.healthAddress` |
| `SUPERVERSE_BLOB_CACHE_DIR` | `server.blobCacheDir` |
| `DEX_SERVER_ADDRESS` | `dex.serverAddress` |
| `SUPERVERSE_STUDIO_PUBLIC_ORIGIN` | `studio.publicOrigin` (Studio keeps its own variable) |
| `SUPERVERSE_AGENT_MODEL`, `_EFFORT`, `_COMPACTION_TOKENS`, `_THINKING_PREFIX_MISMATCH`, `_TASK_TURNS` | `agent.model`, `effort`, `compactionTokens`, `thinkingPrefixMismatch`, `taskTurns` |
| `SUPERVERSE_AGENT_SCRIPTED_MODEL` | none: `agent.mockModel` offers the mock model per Session instead of switching the whole server |
| `SUPERVERSE_SANDBOX_HOLD_SECONDS`, `SUPERVERSE_SANDBOX_PROFILE` | `sandbox.hold`, `profile` |
| `SUPERVERSE_SANDBOX_TTL_SECONDS`, `_DRAIN_SECONDS` | none: the Session reclaims its instance at its deadline and decides the drain itself ([the current contract](../../../docs/architecture/agent-harness-design/sandbox.md)) |
| `SUPERVERSE_S3_ENDPOINT`, `SUPERVERSE_ARTIFACT_BUCKET`, `SUPERVERSE_ARTIFACT_KMS_KEY_ID` | `storage.s3Endpoint`, `artifactBucket`, `artifactKMSKeyID` |
| `SUPERVERSE_LOCAL_BUILD_URL` | `builds.localURL` |
| `SUPERVERSE_HOSTED_BUILD_PROJECT`, `_REPOSITORY`, `_BUCKET`, `_PREFIX`, `_KMS_KEY_ARN`, `_TOOL_IMAGE`, `_HOST_IMAGE` | `builds.hosted.project`, `repository`, `bucket`, `prefix`, `kmsKeyARN`, `toolImage`, `hostImage` |
| `GITHUB_APP_ID`, `GITHUB_INSTALLATION_ID`, `SUPERVERSE_GITHUB_OWNER` | `github.appId`, `github.installationId`, `github.owner` |
| `SUPERVERSE_LIVE_DEX_IMAGE`, `_TEMPORAL_IMAGE`, `_S3_IMAGE` | `live.dexImage`, `temporalImage`, `s3Image` |
| `AWS_REGION`, `AWS_ACCOUNT_ID` | `aws.region`, `aws.accountId` |
| `SUPERVERSE_EKS_CLUSTER_NAME`, `SUPERVERSE_PROJECT_BUCKET_PREFIX`, `SUPERVERSE_PROJECT_KMS_KEY_ARN`, `SUPERVERSE_PROJECT_PERMISSIONS_BOUNDARY_ARN` | `projectCloud.eksClusterName`, `bucketPrefix`, `kmsKeyARN`, `permissionsBoundaryARN` |
| `SUPERVERSE_PROJECT_TEMPORAL_RETENTION_DAYS` | `projectCloud.temporalRetentionDays` |
| `SUPERVERSE_LOCAL_NATIVE`, `SUPERVERSE_NATIVE_RUN_ID`, `SUPERVERSE_KUBE_API_URL`, `SUPERVERSE_KUBE_CA_FILE`, `SUPERVERSE_KUBE_TOKEN_FILE`, `SUPERVERSE_NATIVE_KUBECONFIG` | none since [the current contract](../../../docs/runbooks/local-native.md): `runtime: docker` and `native.runId`; Native needs no Kubernetes credentials |

The duration keys replace whole seconds with Go durations: `SUPERVERSE_SANDBOX_HOLD_SECONDS=3600` is `hold: 1h`.

## Checking

`make check-config` loads and validates every profile in this directory with the real `Load` and
`Validate`: each Kind profile layered over `local-kind.yaml`, and `native.yaml` on its own. The deploy and the
Native launcher supply the project image digests and the run's values, so the check completes them with
shape-only values. It is a static check, not an integration test: the
server still validates the document it is given when it starts.

## Remote agent listener

| Key | Default | Boundary |
| --- | --- | --- |
| `remoteAgent.listen` | `:8088` | Independent Bridge HTTP listener; Kubernetes fixes 8088, Native requires its per-instance loopback port (default 26089), distinct from every other server listener. |
| `remoteAgent.maxLongPolls` | `1000` | Per-replica RAEXEC sync admission, integer 1..10000; RAPAIR creates no long polls. |
| `remoteAgent.maxConcurrentReports` | `16` | Per-replica RAEXEC report admission, integer 1..256; acquire before body/auth without waiters, return 429 with Retry-After when full. |
| `remoteAgent.maxConcurrentReads` | `4` | Per-replica exact-key RAEXEC message hydration, integer 1..64; released before a sync activity wait. |
| `remoteAgent.minimumBridgeVersion` | `0.1.0` | Semantic version without a v prefix; older Bridge versions get 403 bridge_outdated. |
| `remoteAgent.acceptDevBridge` | `false` | Exact 0.0.0-dev exemption only in explicit local Native/Kind profiles; hosted profiles reject it. |
| `remoteAgent.downloadsDir` | `/opt/superverse/bridge` | Absolute environment build root; only fixed manifest-listed artifacts are served. |

There is no `maxConnections` key: the approved cap is fixed at six rows per
Project, atomically enforced in PostgreSQL. See [the connection API](../../../docs/api/remote-agent.md)
and [operation runbook](../../../docs/runbooks/remote-agent.md). Native offsets
its configured port; Kind's ordinary ports command forwards 28089 to 8088.
Hosted routing adds `/remote-agent/` to the existing server Ingress/Studio ALB.

The `agent.model` default must be a selectable catalog model. A recognized but unavailable
provider/model fails configuration validation with its catalog reason. Registering new wire
identities does not change the existing Claude key requirement or enable another provider.
