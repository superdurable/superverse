# Superverse V2 infrastructure

This directory owns V2 resources only. V1 repositories, clusters, state and DNS
ownership are unchanged. Existing V1 credentials and GitHub
App installation are reused; no new App or credentials are required by this layout.

## Local entry and deployment source

`./infra-as-code/sv2` is the operator entry for the Local Kind and Local Native lanes. Both use the platform Helm chart at `kubernetes/charts/superverse-platform` and the pinned local inputs. The platform runs one Go server, Studio and platform Dex; Preview uses the Sandbox-local engine and Live runs in its own Project scope. Streams are best effort and do not own durable business state.

Hosted deployment is described in private operations material.
The optional monitoring installation is also maintained as private operations material.

## Local Kind

Required tools: Docker, Kind 0.33.0, kubectl, Helm, Go, Node/npm, Git and AWS CLI.
Kind commands verify the installed version against `source-lock.json` before
admitting work; status remains read-only and available without that check. New
clusters pass the exact `kindNodeImage` from that lock, taken from Kind 0.33.0's
released default. Existing owned clusters are never recreated or upgraded by
this pin change.
Browser acceptance requires Node 22.22.0 (CI baseline) or 24.19.0 (verified local
runtime) first on `PATH`. Both Kind and Native reject other Node versions before
browser admission and record/check its executable, version and SHA256 through the
run. Node 26.5.0 stalls the pinned Playwright 1.55 final trace merge even after
browser cleanup. Keep full traces; do not reduce evidence to work around it.
Selecting an existing compatible Node for this command does not require changing
the global runtime. Stop/recover/status remain available with any host Node.
Chart validation and deployment/acceptance image verification also need the
pinned Python packages from `validation-requirements.txt`; install them in a
disposable virtual environment and use that environment for `sv2` commands.
Only cluster `superverse-v2`, context `kind-superverse-v2`, and the private
`.local/superverse-v2/kubeconfig` are used. The cluster belongs to the repository:
its main checkout and random cluster owner must agree, and every worktree uses the
main checkout's `.local/superverse-v2`, building and deploying its own source. An
existing unowned cluster is never adopted. Generated credentials and state stay
mode 0600 under the main checkout's ignored `.local/superverse-v2`; never upload it.

```sh
./infra-as-code/sv2 kind up
./infra-as-code/sv2 kind build
# Separate terminal; starts the actual Docker release builder using the accepted coding image:
./infra-as-code/sv2 kind builder
# The private server env file (required) supplies the secrets: release-build.env's token and, for a profile with
# repohost github, the GitHub App private key. It may hold other keys, which are ignored; every other server setting
# is in the configuration profile (cmd/superverse-server/config/README.md).
./infra-as-code/sv2 kind deploy \
  --secret-env-file /private/path/v2-server.env \
  --published-dependencies infra-as-code/dependencies/published/dex-1.10.1-llm-0.2.0.json
./infra-as-code/sv2 kind ports
# Separate terminal, with real provider prerequisites:
./infra-as-code/sv2 kind e2e
```

`up` creates the cluster with a mount of the local Git host's repository directory, the main checkout's
`.local/superverse-v2/git`, into the node, and creates that directory writable by the server Pod's non-root user. A
cluster created before the local Git host has no such mount, and the Local Kind chart mounts the directory into the
server Pod whatever the profile's repository host: `kind deploy` refuses that cluster and names the recreation, which
is needed once: `kind delete --confirm superverse-v2`, then `up`, `build` and `deploy` again. Projects of the old
cluster go with it; delete `.local/superverse-v2/git` with the cluster, because its repositories and their IDs belong to
those Projects (a repository ID must never be reused in a cluster whose Dex already knows it).

`up` installs dependencies and the controller through the same chart; server and
Studio start after build/deploy. The dependencies include the platform PostgreSQL: `up` keeps its role passwords in
the private `postgres-credentials.json`, adding a password for a role the source added, and, on every run, creates the
missing roles and sets their passwords through the container's socket; `deploy` does the same before the server Pod's
init container applies the migrations, which grant to those roles. It also writes the platform Dex configuration, which re-resolves the headless
Worker Service every two seconds, and restarts Dex when that configuration changed (`dex.configRevision`). Dex refuses to
start while a table of its read model Attribute Store is missing, so the first `deploy` adds the store
(`scripts/local_platform.py`, as the `superverse_dex` role) only after the server's rollout migrated, then restarts Dex;
until then that deploy's server cannot start the Flows that select the store. Later `up` runs keep the store definitions that the last deploy rendered, which its migrations made tables for, and a later `deploy` renders the checkout's after its own migrations; an `up` from a checkout with a new store or AttributeMap therefore never points Dex at a missing table. Native
applies the migrations before it starts its Dex, which it recreates on every start and so always has the store. A server
deployed before the server configuration profiles has no configuration to install, so `up` keeps it scaled down until the next `kind deploy` renders a profile; run `kind build` and `kind deploy`
once after updating a cluster that predates them. Existing versioned MinIO and Temporal PVCs remain
in place. A fresh cluster uses pinned dependency images. Existing owned clusters
retain their accepted MinIO identity rather than silently selecting another image.
SQLite Temporal is a local development dependency, not hosted persistence. The dependencies also include Keycloak, the
OAuth provider of the connection tests: it imports the realm of the chart's `files/keycloak-realm.json` into a new
database whenever its Pod starts, and `up` keeps its administrator, client and user secrets in the private
`keycloak-credentials.json`. Its tokens carry the browser's issuer, `localDependencies.keycloakOrigin`, while the server
and applications call it through its Service. A cluster brought up before Keycloak needs `up` again before `deploy`.

`kind ports` handles INT, TERM and HUP and cleans only its own returned child
processes. Cleanup attempts every child: TERM with a shared 20-second grace,
then KILL and a shared 5-second reap bound. A remaining child is an explicit
cleanup failure. No process is selected by port, executable name or parent group.
Common operator commands default to 60 seconds; image pulls, new-cluster setup
and host tool compilation allow 15 minutes, image save/load and cluster deletion
10 minutes, Docker component builds 60 minutes, Helm install 11 minutes, and
300-second rollouts 330 seconds. The builder service is explicitly long-lived;
its existing per-build deadlines remain authoritative. Cancellation is checked
every 250 ms while waiting on these commands, then uses the same owned-child
cleanup. A timeout or interruption does not establish rollback of Docker/Kind/
Kubernetes effects or exit of detached descendants, and never triggers an
automatic mutation retry. Reconcile retained ownership and provider state first.

`build --component server|studio|coding` builds only the selected component; default
`all` reuses unchanged accepted images. Frozen Docker contexts exclude npm caches,
outputs, tests and unrelated documentation. Images are loaded directly into Kind,
verified against Docker filesystem/config identity, and deployed by exact manifest
alias. No local registry, registry mirror, push or image publication is involved.

`builder` uses the accepted Docker **root descriptor ID** directly, so an absent Kind-only
manifest alias or a mutable tool tag cannot silently change its toolchain. Restart
an old builder after a coding-image change; the command does not kill an unrelated
process occupying port 29792. Include the generated private `release-build.env`
when assembling the server env file; it holds only the token, because the builder's address is the
`builds.localURL` setting. The entry also compiles the trusted one-shot
`cmd/tools/app-manifest` tool; it calls the same Go AppManifest extractor as
the server. Release connector/environment artifacts therefore use canonical
sorting/defaults without duplicating that parser in Python or running repository
scripts. The executable source and binary digest are recorded privately.

The server's non-secret settings are one YAML document, the
[server configuration](../cmd/superverse-server/config/README.md): `agent.model` selects
`anthropic/claude-opus-5-5`, `anthropic/claude-sonnet-5-5` or `anthropic/claude-haiku-4-5`.
`kind deploy --config <profile>` (default `cmd/superverse-server/config/local-kind.yaml`) layers a checked-in
profile over `local-kind.yaml`, merges the image digests and the coding profile name that the deploy computes, validates
the result with the server's own loader and renders it through the chart into the `superverse-v2-server-config` ConfigMap
that the Pod mounts. A change of the document rolls the server. `--secret-env-file` is required, and deploy
takes only the secrets from it (`AWS_*`, the GitHub App private key forms, the cookie signing key and the build token). It
ignores every other key, so one private file can serve every layer; one notice names the ignored keys, never their values, and the
configuration profile stays the only source of every other setting. Deploy refuses a file that carries `ANTHROPIC_API_KEY`, which
comes from the main checkout's `.env`. Because deploy replaces the server Secret as a whole, it also refuses,
before it applies or prunes anything, a Secret that lacks the cookie signing key, the build token or, for `repohost: github`,
the GitHub App private key (a `repohost: localgit` deploy generates the signing key of the local Git host itself), and a
profile that does not configure Local Kind (`native.yaml` is for `native start`). Deploy also removes keys from the live server Secret that it no longer
sets, so a setting of an earlier deploy cannot survive beside the configuration document. `local-kind.yaml` offers the mock model and starts new Sessions on it;
`local-kind-acceptance.yaml` offers the mock model beside the Claude models and starts new Sessions on a Claude model. The deployment inputs and the receipt record the profile name, the
digests of its layers and the document; the browser cases follow the accepted deployment. Whether the final acceptance runs the mock model or a real
Claude model, and which, is the platform developer's decision: ask before the run and never choose it yourself. `kind e2e` on the acceptance
profile refuses to run without `SUPERVERSE_E2E_MODEL_TARGET` (`mock`, or a model such as `anthropic/claude-opus-5-5`) and records it as
`modelTarget` in the receipt. `release attest` accepts only a receipt deployed from the unchanged `local-kind-acceptance.yaml`, requires
its `modelTarget` and copies it into the attestation, so the release record shows which model the accepted run used
([mock model](../internal/dexai/agents/README.md)).
Every deployment takes `ANTHROPIC_API_KEY` from the ignored `.env` file at the root of the
main checkout, which every worktree shares, whenever it is there, and a profile whose new Sessions
start on a Claude model requires it; `make test-claude-live` and `make test-integration` read the same file unless
the key is exported. Access denial never silently selects another model.
New Session context budgets follow the [model limits](../docs/architecture/agent-harness-design/context-management.md#models).
`mock/` is rejected. Browser
acceptance uses the repository host of the deployed profile, the model and the provider APIs; no simulated
OAuth/Stripe/Gmail runtime is installed. The local Git host of `local-kind.yaml` is a real Git service (the real `git`
command clones from and pushes to it, as MinIO is the S3 service of the local lanes) and needs no GitHub access;
the final acceptance profile `local-kind-acceptance.yaml` uses real GitHub, and the E2E reads the
pushed commits through the `gh` CLI there. The former `provider_fixture.py` and fake tests were removed.

The local Git host keeps its repositories in the main checkout's `.local/superverse-v2/git` and signs repository
credentials with `.local/superverse-v2/git-signing-key`, which the deploy generates and puts in the server Secret as
`SUPERVERSE_LOCAL_GIT_SIGNING_KEY`. A profile with `repohost: github` needs the GitHub App private key instead, in
the server env file. The end-to-end runner writes a fresh administrator token, derived from the same key, to the
private file `git-administrator-token` for the specs.

Ports: Studio 23000, gRPC 28080, local Git host 28085, platform Dex API 28811, Dex Web 28812, MinIO 29000,
PostgreSQL 28432 (for the integration tests), Keycloak 28190 (for the connection tests), Temporal 27233, Temporal UI
28233, Preview/Live gateway 23080, release builder 29792. The Sandbox clones from the
Service port 8085 in the cluster; `kind ports` forwards 28085 to it for the host builder, which `kind builder`
starts with the origin of the deployed profile's host, and for the E2E.
`ports --dependencies-only` forwards Dex API/Web, MinIO, PostgreSQL, Keycloak and Temporal. Use the pinned
Dex CLI from `make install-dexcli` for runtime inspection; Native readiness uses the
same binary, or the one named by `DEXCLI`. Local Native serves ports of its own (each of these minus 2000; see its
[runbook](../docs/runbooks/local-native.md#beside-local-kind)), so the lanes run at the same time.

The Local Kind project-resource admission policy keeps only ownership labels
immutable on UPDATE: `app.kubernetes.io/managed-by`, `app.kubernetes.io/part-of`,
`superdurable.io/project-id`, `superdurable.io/component`, and the presence/value
of optional `superdurable.io/scope`. Deployment and generation labels may
change when a new deployment updates the Project's existing `live` HTTPRoute.
Namespace identity remains immutable. The hosted project activation policy has
no whole-label-map UPDATE equality rule and does not require this correction.

Do not deploy during an ongoing E2E. Old content-addressed profile ConfigMaps and
their exact read Roles are retained for existing Sessions. New profiles are immutable
and use the accepted coding digest. Cluster deletion remains explicit:
`kind delete --confirm superverse-v2`.

## Local Native

[Local Native](../docs/runbooks/local-native.md) runs the merged Go server, its Worker, Studio and the Release builder
on the host and everything else on the local Docker Engine, without Kind
([the current contract](../docs/runbooks/local-native.md)). Platform Temporal, MinIO, PostgreSQL,
Dex and Keycloak, the gateway and a local registry run as the Docker Compose project of `scripts/native/compose.yaml`, with Kind's pinned
images and configuration (`scripts/local_platform.py`; the static check compares the Compose arguments with the
chart's). The Native gateway is the Traefik image that `source-lock.json` pins. The server's `docker`
runtime runs every Sandbox, Preview and Live workload as containers. Go and Studio changes need no image build or
deploy; the coding image is built with `docker build` when its source changes, and Release images go to the local
registry.

```sh
./infra-as-code/sv2 native start [--studio build]
./infra-as-code/sv2 native restart-server --run-id <run-id>   # the Worker restart tests
SUPERVERSE_E2E_OPENAI_FILE=<private file> ./infra-as-code/sv2 native e2e --run-id <run-id>
./infra-as-code/sv2 native stop --run-id <run-id>             # or recover after a crash
```

It is the lane of every change that touches no Kind path; `make required-lanes` names when Kind runs it instead. The lane keeps
its own state in `.local/superverse-v2/native` and its Compose volumes, serves fixed ports of its own and holds only
its own owner lock, so it runs beside Kind. Its receipt never authorizes AWS publication.

Both lanes share the engine's images and build cache; `./infra-as-code/sv2 local prune` reclaims them between lane runs
([Reclaiming Docker disk](../docs/runbooks/local-native.md#reclaiming-docker-disk)).

## Build and acceptance identity

| Identity | Exact inputs |
| --- | --- |
| Server | Linux/host-architecture transitive local Go sources and embedded files from `go list`; root module sums, Dockerfile, dockerignore, release profiles. GOWORK is disabled. |
| Studio | `web/studio`, its package lock/build configuration, proto and dockerignore. |
| Coding | `sandbox/coding`, `scripts/install-pinned-binary.sh`, the go-react-v1 `profile.json` that selects its base images, Dex skills, the template module manifests, the embedded example catalog and the archive of every example it lists (`app-template.tar` for the default, `examples/<id>.tar` for each other one), and the exact agent-sandbox archive commit and per-file hashes. |
| Dependency contract | Per-component `build-inputs.json` entry, target architecture, digest-pinned base images and language dependency locks. |
| Common acceptance | Studio/coding inputs, real browser E2E sources/config, actual builder/transitive host AppManifest tool sources, every top-level script module plus the `acceptance/` package, dependency pins and their Python requirements, Helm source and local values (Native renders its coding profile from them). |
| Kind acceptance | Common inputs plus Linux server component inputs, the `kind/` package and the published dependency approvals. |
| Native acceptance | Common inputs plus actual host-OS server Go inputs, the `native/` package with its Compose file and `native.yaml`, with the host Studio build ID. The server Dockerfile, the server and Studio container builds, the `kind/` package and the published dependency approvals are not Native requirements. |

Both lanes record the common fingerprint and their complete mode fingerprint.
Script inputs are whole packages, so a new or renamed module cannot drop out of
the identity. The modules both lanes run are top-level modules, which both
identities cover. The `hcp/` and `release/` packages, Terraform
and prose do not execute in these local lanes; the release boundary binds hosted
plans separately.

Component input manifests and Docker IDs are saved under `.local/superverse-v2/build`
and `sandbox-build`. This is a direct three-component collector, not a generic
build system. The Go dependency list is evaluated for the actual Linux target;
Darwin-only source selection cannot accidentally omit Linux runtime code.

`deploy` verifies current component fingerprints and stores exact runtime resource
UIDs, Deployment generations/template digests, secret resource versions, profile
identity, project Dex/storage/Temporal images and a private configuration digest.
A secret change changes the Pod template checksum and rolls the application.
`e2e` verifies behavior and runtime identity before and after all three cases.
Zero, skipped, flaky, missing or failed cases cannot emit an acceptance receipt.
Readiness and static validation are not business acceptance.

### Script layout

`sv2` dispatches a lane and is the one error boundary: it prints the message of an
operator-facing `RuntimeError` and replaces any other failure with one fixed
sentence, because parser and provider errors may quote private data. Lanes never
print raw provider output.

| Path under `scripts/` | Owns |
| --- | --- |
| `paths.py`, `pins.py` | Repository and private-state locations; `source-lock.json` pins and the static deployment names. |
| `commands.py` | The one bounded command runner and child-process cleanup (see below). |
| `private_files.py`, `operator_errors.py` | Atomic mode-0600 files, retained private secrets and locks; operator-facing failures. |
| `credential_http.py` | The opener for requests that carry a credential: redirects are refused, never followed with the credential; an expected redirect target is checked before it is followed without it. |
| `build_inputs.py` | Component build inputs and the Kind/Native acceptance identities above. |
| `git_host.py`, `config_files.py` | The local Git host's files of a lane (repositories, signing key, administrator token); the server configuration layers both lanes merge and preflight with the server's own loader. |
| `coding_image.py`, `release_builder.py`, `local_platform.py` | The coding image build under a lane's tag; the release builder's command and trusted AppManifest tool; the platform Dex configuration and the versioned buckets both lanes use. |
| `image_identity.py`, `published_dependencies.py` | OCI and Docker image identity; the reviewed published dependency approval. |
| `chart_validation.py` | The static chart render and rejection checks run by `make -C infra-as-code check`. |
| `local_prune.py` | `sv2 local prune`: the lanes' superseded images, old build cache and the Kind node's unused images, under every lane lock ([Reclaiming Docker disk](../docs/runbooks/local-native.md#reclaiming-docker-disk)). |
| `kind/` | The owned cluster (`cluster.py`: identity, kubeconfig-scoped commands, port catalog, the lane's Git host files and node mount) and the Kind lane: images, chart installation, deployed identity, the local release builder, Kind acceptance and the lane's action lock and acceptance fence (`locks.py`). |
| `native/` | Native supervision (`cli.py`), the lane's state, ports and secrets (`lane.py`), its owner lock and run record (`owner.py`), the Compose platform (`compose.yaml`, `services.py`), the coding image and profile (`coding.py`), the host Studio (`studio.py`) and the Dex readiness and callback proof (`callback.py`). |
| `acceptance/` | The one registry of browser cases, their runner and reports, and exact process ownership, shared by both lanes. |
| `release/` | Sanitized release evidence and exact-image promotion. |
| `hcp/` | The one HCP API client, workspace bootstrap (`workspaces.py`), the repository-owned Terraform inputs and their static check (`inputs.py`), their read-only workspace and plan review (`input_review.py`) and reviewed run approval (`runs.py`). |

Existing `runtimeIdentity` fields remain part of the private state and receipt
format: Kind installation produces them, Kind acceptance and release evidence
consume them, and Native records its own platform containers, coding image and profile. All Kubernetes
operations keep the explicit kubeconfig and ownership guard; Native makes none. Project IAM
permissions live in `terraform/cloud/project-permissions.tf`.

`scripts/check-dex-versions.py` cross-checks the root SDK, the dexcli version,
source commit and four archive checksums in `scripts/install-pinned-binary.sh`
(the one installer for the host and the coding image), the coding image and
hosted toolchain copies, and the platform Dex chart version/digest against
`source-lock.json`. The installer verifies the downloaded binary's version and
commit before installing it. The root and IaC static checks run this
comparison; `--dexcli-binary /absolute/path/to/dexcli` also checks an existing
binary without downloading anything or contacting Dex.

Use `scripts/update-dex-versions.py` with the explicit `--sdk-go-version`,
`--dexcli-version`, `--dexcli-commit` reported by the reviewed release binary, and
optional `--checksums-file` to update all of those copies together. A platform
image change adds `--server-version` with either the reviewed `--server-digest`
or `--resolve-server-digest`, which reads the release's multi-platform index
digest (never a per-platform manifest or Docker config ID). The updater stages
every edit before writing and does not edit immutable Release BuildProfiles.
Publishing new accepted profiles and runtime rollout remain separate reviewed
operations.

Kind and Native take no lock of each other's: they serve separate ports and state and
run at the same time. The Kind action lock serializes `up`, `build`, `deploy`, `delete`
and `e2e`; long-lived `ports` and `builder` take no lock and can serve acceptance.
Kind E2E also records `.local/superverse-v2/kind-e2e/e2e-admission.json` before any
browser spawn. A failed/interrupted run retains that fence and blocks later Kind
mutations/E2E; Native is not affected. Reconcile exact process identities and incomplete
business effects, then archive the marker privately under its exclusive `e2e.lock`
and the Kind action lock. No automatic clear/retry assumes rollback. Status and
existing diagnostic helpers remain available. The same process/failure rules are
described in [Native acceptance](../docs/runbooks/local-native.md#actual-acceptance). Python validation compiles in
memory; the CLI disables bytecode files. Disposable tool caches stay outside the
checkout.
