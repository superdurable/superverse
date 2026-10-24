# Local Native

The Native lane runs the merged Go server with its Worker, Studio and the Release builder on the host, and everything
else on the local Docker Engine: platform Temporal, MinIO, PostgreSQL, Dex and Keycloak (the OAuth provider of the
connection tests, recreated on every start with its secrets in the instance's private `keycloak-credentials.json`), the
Preview and Live gateway and a local registry
as one Docker Compose project, and every Sandbox, Preview and Live workload as containers that the server's `docker`
runtime creates by the Docker runtime. It uses no Kind cluster.
It is the lane of every change that touches no Kind path; `make required-lanes` names when Local Kind runs it instead
([local lanes](../../internal/dexai/agents/README.md)), and only the Local Kind acceptance on `local-kind-acceptance.yaml`, with the mock model by default
(or a real model under the approved strong-reason exception), satisfies the AWS publication gate. It serves ports of its own and takes no lock of Kind's, so it runs
[beside Local Kind](#beside-local-kind).

## Prerequisites

- Docker Desktop or OrbStack on macOS, with the containerd image store (Docker Desktop: *Use containerd for pulling
  and storing images*; OrbStack uses it by default): the Sandbox runs the coding image by the digest that the store
  records. Containers reach the host processes through `host.docker.internal`, which both provide.
- On OrbStack, run `orb config set network_bridge false`, then restart OrbStack after all lanes stop.
  Direct Mac bridge mirroring has an observed eight-network ceiling ([upstream report](https://github.com/orbstack/orbstack/issues/2728));
  Native creates Project and Session networks, and exceeding it caused 30-second container-start waits.
  Host-side lane processes use published loopback ports, so direct container-IP/domain access is unnecessary.
- `docker` with Compose, `helm` (it renders the coding profile from the chart), `go`, `aws` (it creates the platform
  buckets), Node/npm with `npm --prefix web/studio ci` done once, and the pinned `dexcli` from `make install-dexcli`
  (or the binary that `DEXCLI` names).
- The pinned Python packages of `infra-as-code/validation-requirements.txt` in the environment that runs `sv2`.
- Node 22.22.0 or 24.19.0 first on `PATH` for browser acceptance; Node 26.5.0 stalls the pinned Playwright's trace
  merge. The E2E preflight records the executable, version and SHA-256 and checks them before each case.
- The lane's fixed ports (below) unoccupied. Local Kind may run at the same time on its own ports; the launcher
  refuses an occupied port and never stops a process it does not own.

## Start

```sh
./infra-as-code/sv2 native start                 # Studio with hot reload
./infra-as-code/sv2 native start --studio build  # production Studio build, which native e2e requires
./infra-as-code/sv2 native start --studio build --config <profile>  # a test profile layered over native.yaml
# A second isolated instance can stay open for interactive Studio work.
./infra-as-code/sv2 native start --instance view
# Keep this foreground owner alive. It prints the run ID when ready.
./infra-as-code/sv2 native status
```

`native start`, in order:

1. takes the selected instance's owner lock and refuses a retained run of that instance, a missing tool, an occupied port or a port of the
   Kind lane;
2. prepares Studio: `next dev` from the checkout, or `npm run build` copied into the run directory with `--studio
   build`;
3. builds the coding image under the tag `docker.io/superverse-v2/coding-sandbox:native` (`:native-<name>` for a named
   instance, so one instance's build never moves the tag whose digest another running instance pinned) when its source changed
   (the shared build of `infra-as-code/scripts/coding_image.py`, with its tool smoke test), pins it by the digest the
   engine records and renders the chart's coding profile with it into `sandbox-profiles/<profile>.yaml`;
4. compiles the host server and validates its configuration: `native.yaml` and a last layer of the run with
   `native.instance`, `native.runId`, `native.stateDir`, `native.dockerSocket` (the socket of the current Docker context),
   `localgit.dir`, `server.blobCacheDir`, `sandbox.profile` and the `live` images, through the server's own loader;
5. starts the Compose project `superverse-v2-native` (or `superverse-v2-native-<instance>`) (`infra-as-code/scripts/native/compose.yaml`): Temporal,
   MinIO, the gateway and the registry first, the versioned buckets `superverse-v2-blobs` and
   `superverse-v2-artifacts`, then Dex. A published port accepts connections before its container listens, so
   readiness is by protocol: the Temporal UI, Dex's `GetServerInfo` through the pinned CLI, the gateway's API and the
   registry's `/v2/`;
6. starts the builder (`--target docker`), the server and Studio as children in the owner's process group and
   waits for each: a child that exits fails the start at once with the name of its log;
7. proves that platform Dex reaches the Worker through an existing Flow (below) and records the run as ready.

The server's environment holds only secrets, all generated and retained in the lane's state directory: the
platform MinIO credentials, the cookie signing key, the build token file and the local Git signing key. No `ANTHROPIC_API_KEY` is passed from
the host environment or the main checkout's `.env`. `native.yaml` offers the mock model and
starts new Sessions on it ([mock model](../../internal/dexai/agents/README.md)). To use a Claude model, set the
Project’s Anthropic key through the Agent picker; no Session uses a platform environment key. Studio receives the lane's own browser signing key, its public origin and the server's address.

| Boundary | Address |
| --- | --- |
| Studio (host process) | `http://studio.native.localhost:21000` |
| Shared login (same host Studio process, only with `native-login.yaml`) | `http://login.native.localhost:21000` |
| Business gRPC, health | `127.0.0.1:26080`, `127.0.0.1:26082`; platform Worker streams are outbound to Dex |
| Local Git host | `127.0.0.1:26085`; a Sandbox clones from `http://host.docker.internal:26085` |
| Remote agent Bridge origin | `127.0.0.1:26089`, shifted by the named-instance offset; see [connection operation](remote-agent.md) |
| Routes the server serves the gateway | `127.0.0.1:26086` |
| Platform Dex API / Web | `127.0.0.1:26811` / `http://localhost:26812/` |
| Platform MinIO, also the browser's presigned uploads and downloads (`storage.browserEndpoint`) | `http://127.0.0.1:27000` |
| Platform Temporal / UI | `127.0.0.1:25233` / `http://localhost:26233/` |
| Preview/Live gateway / its API | `http://<scope>-<project>.localhost:21080` / `http://127.0.0.1:21081` |
| Local registry | `localhost:25000` |
| Release builder | `http://127.0.0.1:27792` |

Every port is on the loopback and is the lane's own: it is the port Kind serves for the same boundary, or Native's
former one, minus 2000, except the registry, whose address every Release image records.
`native.yaml` declares them, and `infra-as-code/scripts/native/lane.py` the Temporal, Temporal UI and Dex Web ports that no server
setting names; the Compose project publishes them from there. The launcher refuses a port that the Kind lane serves
and one from 32768 up, where the engine draws the ephemeral loopback ports of the project containers, which the
server looks up for each use. A connector OAuth app registered with Native's former callback
(`http://localhost:23000/api/connector-oauth/callback`) needs `http://studio.native.localhost:21000/api/connector-oauth/callback`;
that address is now Kind's. A Project created before the lane had ports of its own keeps the clone origin it was set
up with, `host.docker.internal:28085`, which is now Kind's local Git host, so its Sandbox cannot clone: delete it and
create it again. Accounts and every other Flow keep working, because the Worker kept its port.

### Named instances

`--instance <name>` accepts `[a-z0-9]{1,12}` and defaults to `default`. The default retains the paths, Compose
project, ports and `10.233.0.0/16` range listed above. A named instance uses `.local/superverse-v2/native-<name>`,
`superverse-v2-native-<name>`, and a stable offset of 100 for every listed port; its offset is allocated once in the
private state root and skips any offset whose ports reach Local Kind's or the engine's dynamic range. Its Docker
network is the next distinct `/16` (`view`, when first allocated, uses `10.234.0.0/16`). All platform and Project
Docker objects carry `superdurable.io/native-instance=<name>`; named Project and Session object names carry the same suffix, and every lookup and cleanup selects it through the owner
labels. Objects created before instances existed carry no instance label, and Docker cannot add one; the default
instance owns them, so its lookups select without that label and accept an object that lacks it, while a named
instance refuses it. The registry is intentionally per instance, avoiding cross-instance image repository or credential sharing.

For example, leave `native start --instance view` open on an interactive checkout, then run `native start --studio
build` and the E2E cases on another checkout's default instance. Stop, recover, status, restart-server and e2e must
receive the same `--instance`; stopping or deleting Projects in one instance cannot select the other's objects. Two
instances roughly double host load. Dex reads can time out under heavy load , so keep heavy checks off the host while both run.

### Studio modes

| `--studio` | Serves | Use |
| --- | --- | --- |
| `dev` (default) | `next dev` from the checkout; edits reload in place | Development. Never acceptance; `native e2e` refuses it. |
| `build` | The production build with browser source maps, copied into the run directory, laid out as in the Studio image | `native e2e`, which also refreshes the [Studio coverage map](../../internal/dexai/agents/README.md) from its three cases. Startup and acceptance bind the Studio source fingerprint and build ID. |

Go server changes need a new run: stop it and start again. The Compose project, the lane's data and the Projects
survive.

### Test inputs

A Dex integration test reaches Native through these inputs; the Kind lane's are its own addresses
([Local Kind](../../infra-as-code/README.md#local-kind)). `SUPERVERSE_TEST_NATIVE_INSTANCE` defaults to `default`;
the guard reads the selected instance's effective run layer, and
`dexflowtest` fails a test at once when `SUPERVERSE_TEST_LANE` is missing or any address belongs to
the other lane ([lane inputs](../../internal/dexai/agents/README.md)).

```sh
SUPERVERSE_TEST_LANE=native SUPERVERSE_TEST_NATIVE_INSTANCE=default SUPERVERSE_TEST_NATIVE_RUN_ID=<run-id> \
SUPERVERSE_TEST_DEX_ADDRESS=127.0.0.1:26811 SUPERVERSE_TEST_API_ADDRESS=127.0.0.1:26080 \
SUPERVERSE_TEST_PROJECT_ID_LENGTH=4 go test <packages> -run '<tests>' -count=1
```

When selecting a named instance, set `SUPERVERSE_TEST_NATIVE_STATE_DIR` to its matching
`.local/superverse-v2/native-<name>` directory whenever the test opens Project credentials or Docker objects. The
lane guard rejects a state directory that does not match `SUPERVERSE_TEST_NATIVE_INSTANCE` before it connects.

A Playwright spec run directly takes `SUPERVERSE_E2E_BASE_URL=http://studio.native.localhost:21000`.
The launcher checks that both canonical Studio and login names resolve only to
loopback; add them to the local hosts file if the OS does not resolve `.localhost`
subdomains. A named instance inserts its label into both names, for example
`studio.view.native.localhost` and `login.view.native.localhost`, and uses its
retained port offset. Cookie scope follows the hostnames, not the ports.

To exercise shared login explicitly, start with
`--config cmd/superverse-server/config/native-login.yaml`. Its account, product,
browser-cookie and issuer secrets are distinct private retained lane values.
Without this profile, password sign-in stays on Studio; the feature is disabled.

### Test settings

`--config <profile>` layers a server configuration profile over `native.yaml`, below the run's own layer, as
`kind deploy --config` does on Kind: a test that needs a setting other than the lane's default, such as a short
`sandbox.hold` or `sandbox.previewHold`, names it and repeats it as a test input
([integration verification](../../internal/dexai/agents/README.md)). Startup validates the layers with the server's own loader,
and the run binds the profile's digest like `native.yaml`'s; `native e2e` records both in its receipt.

## State

The default instance keeps everything in `.local/superverse-v2/native` of the main checkout; a named instance uses
its own `.local/superverse-v2/native-<name>`. Each has its own Compose volumes, and the Kind lane is separate.

| Path under `.local/superverse-v2/native` | Holds |
| --- | --- |
| `platform-credentials.json`, `cookie-signing-key`, `browser-signing-key`, `git-signing-key`, `release-build-token` | The lane's generated secrets, 0600 |
| `git/` | The local Git host's repositories of the lane's Projects |
| `credentials/<project>/` | The credentials the server generates for a project: the root key pair of its MinIO and one file per storage grant, 0600, created exclusively with their owner and removed with their scope or grant |
| `routes/` | The gateway's route files, one per public host with its owner and generation |
| `sandbox-profiles/` | The rendered coding profiles the server reads |
| `sandbox-build/`, `coding-image.json` | The coding image's build context, receipt and accepted record |
| `release-builds/` | The builder's private build state |
| `owner.json`, `owner.lock` | The lane's crash fence: the record of the retained run, or of the last released one, and the lock that one owner holds while it runs and `recover` while it cleans up |
| `runs/<run-id>/` | One run: its record `run.json` (with `exitedFirst`, the role, exit status and time of the first owned process found exited, when the owner stopped for that reason), the configuration layer, the server binary, the logs of the server, builder and Studio, the Studio build, and the E2E directories |
| `e2e-acceptance.json` | The receipt of the last passing `native e2e` |

The default volumes `superverse-v2-native_temporal`, `_minio` and `_registry` (and the corresponding named-project
volumes) hold its state; a named Compose project owns identically isolated volumes. Each Project's containers,
networks and volumes carry both the owner labels and the Native instance label, and are removed only by that
instance's Project deletion.

## Project workloads on Docker

The server creates every project workload itself, with the identities of the
decision:

- **Project scope.** The network `sv2-project-<project>` with the containers `sv2-project-<project>-storage`,
  `-temporal` and `-dex`, reachable on the network as `storage`, `temporal` and `dex`. The storage also holds the
  fixed address `.2` of the network's subnet, through which every Live application and Preview loads its
  configuration (the Connector SDK accepts only a local endpoint such as a private address). A project created
  before that address existed is refused with the advice to delete it and create it again.
- **Sandbox.** The Session network `sv2-session-<project>-<session>` with the container `<network>-coding` (alias
  `coding`), its workspace volumes `<network>-workspace-repository` and `-preview-state`, and the project network,
  where its Preview reads the project storage. It runs the coding profile's container hardened as the Pod is and
  never restarts by itself.
- **Live.** One network per generation, `sv2-project-<project>-app-<deployment>-<generation>`, and an application
  container of the same name that runs the Release image from the local registry and joins the project network.

Read them with Docker, without changing them:

```sh
docker ps --filter label=superdurable.io/project-id=<projectID> --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
docker network ls --filter label=superdurable.io/project-id=<projectID>
docker volume ls --filter label=superdurable.io/project-id=<projectID>
```

## Dex pull delivery proof

Before marking the owner ready, the launcher uses the pinned `dexcli` to discover an open platform Flow, requires
that its Worker target has `pull: true`, invokes only its read-only `GetDexSummary` RPC and confirms
the run is still current; reads have eight-second deadlines within two minutes. The run records
`callbackReadiness.level=CALLBACK_VERIFIED` with the Flow type, RunID, a hash of the FlowID and the CLI identity,
never the CLI output. A lane with no open platform Flow, such as one whose Projects are all deleted, records
`TRANSPORT_ONLY_NO_OPEN_FLOW`, which is weaker than a callback proof; the browser cases remain the authority.
Platform Dex Web on 26812 serves the flow list, graph and timeline for read-only diagnosis.

Real integration tests may retain Flows on temporary Workers. Discovery counts these as
`otherWorkerCandidates` and continues to a Flow whose target selects the platform pull Worker;
it never invokes their RPCs or changes their state. Open platform Flows of which none matches
still fail readiness. A candidate with missing Worker identity also fails readiness.

## Local Git host

The lane's Projects keep their repositories in the local Git host inside the server
([the current contract](../../internal/comm/platform/repohost/localgit/README.md),
[package](../../internal/comm/platform/repohost/localgit/README.md)), selected by `repohost: localgit` of `native.yaml`.
The bare repositories `superverse-local/localnative-<name slug>-<projectID>.git`, such as
`localnative-myapp-a7k2`, are in `.local/superverse-v2/native/git`; a deleted
Project's repository is archived, not removed. Read a Project's repository name from its snapshot (`fullName` of
`repository`), never by composing it: a repository created before the name carried its prefix and slug is named
`sv2-test-<projectID>`. A repository credential is minted per use and signed with the lane's
`git-signing-key`. The E2E runner writes a fresh administrator token to `git-administrator-token` before each case,
for example to read the head of a Project's default branch:

```sh
curl -H "Authorization: Bearer $(cat .local/superverse-v2/native/git-administrator-token)" \
  "http://127.0.0.1:26085/_api/v1/repositories/<fullName of the Project's repository>/branch?name=main"
```

## Preview engine diagnostics

A Preview's Flows run on the engine inside its Session's Sandbox
([the current contract](../architecture/agent-harness-design/sandbox.md)). Read them with the pinned CLI
through the port that the coding container publishes for 8801:

```sh
container=sv2-session-<projectID>-coding
docker port "$container" 8801/tcp          # prints 127.0.0.1:<local port>
dexcli --server 127.0.0.1:<local port> --timeout 15s flow search \
  --query 'FlowType = "<application Flow type>"' --no-hydrate --page-size 50
dexcli --server 127.0.0.1:<local port> --timeout 15s flow state <flow-id> --no-hydrate
```

The engine and application logs are in `/var/lib/superverse/preview/logs` of the container (`supervisor.log`,
`temporal.log`, `dexcli.log`, `build.log`, `app.log` and the Dex Server logs); read them with `docker exec
"$container" tail`. The Preview's own state, including `PreviewEnvironment` and the phase timings, is on platform
Dex (`flow state preview-<projectID> --no-hydrate` on 26811), and an instance's provisioning with its
phase timings is `sandbox-provision-<projectID>-<workspaceRequestID>`. Mutating the Preview engine
follows the same authorization rule as any Flow mutation.
--no-hydrate` on 26811). Mutating the Preview engine follows the same authorization rule as any Flow mutation.

## Restart the server

```sh
./infra-as-code/sv2 native restart-server --run-id <run-id>
```

The owner replaces its host server, and with it the Dex Worker, as a Pod replacement does in Kind, and the command
returns once the new server is ready. Platform Dex reconnects to the new Worker some seconds later. The Worker restart
tests call the command through `dexflowtest.RestartServerForTest` with `SUPERVERSE_TEST_LANE=native` and
`SUPERVERSE_TEST_NATIVE_RUN_ID`, which then waits until platform Dex invokes the new Worker.

## Stop and recover

```sh
./infra-as-code/sv2 native stop --run-id <run-id>
# Only after a crashed owner is absent; never signals a reused owner PID:
./infra-as-code/sv2 native recover --run-id <run-id>
```

`stop` signals the owner, which stops every owned process with exact PID, start-time and process-group identities and
bounded TERM, KILL and reap checks, then stops the Compose project and releases the run. The containers and volumes of
the platform and of every Project stay. `recover` holds the owner lock, requires that the owner is gone, proves the
group's ownership through a recorded surviving child, stops it and the Compose project and releases the run. If
ownership or exit cannot be established the run stays retained in `owner.json` for explicit reconciliation; do not
delete the record or start another Native owner meanwhile. Kind is not affected either way.

A checkout from before parallel lanes kept its Native run in the shared mode file `.local/superverse-v2/execution-mode.json`
and held the shared lock `execution.lock` for every Kind command and Native run. The owner lock includes that lock, so
this launcher neither starts nor recovers while such a checkout runs a Kind command or a Native owner, or while the
mode file retains a Native run; stop or recover that run with the launcher of its checkout. The former Kind-based
Native's cluster switch is undone by the next `kind deploy`, whose chart no longer has the bridge.

Native build commands inherit the launcher group. Per-build cancellation stops, then kills, only the exact observed
child identities of the running command and never the outer group, and confirms the build `CANCELLED` once every
observed child is gone (the current contract): the build commands, git and the Docker CLI, do not daemonize, and an image a
stopped push may still leave sits under the cancelled build revision's own tag, which nothing deploys. A build whose
child tree cannot be established or stopped keeps its attempt `UNKNOWN` until its effects are reconciled. Whole-group
shutdown proves local exit, not build rollback or completed Project deletion.

## Beside Local Kind

Kind and Native run at the same time, so a change that `make required-lanes` sends to Kind runs there while others run
on Native. Neither lane takes a lock of the other's, and they share no port, state directory, Git repository, builder
state or Docker object:

| Kept apart | Local Kind | Local Native |
| --- | --- | --- |
| Fixed loopback ports | 23000, 23080, 27233, 28080, 28085, 28190, 28233, 28432, 28811, 28812, 29000, 29792 | 21000, 21080, 21081, 25000, 25051, 25233, 26080, 26082, 26085, 26086, 26087, 26170, 26233, 26432, 26811, 26812, 27000, 27792 |
| Exclusivity | The action lock `kind-action.lock` and the E2E fence `kind-e2e/` | The owner lock and record `native/owner.lock` and `owner.json`, and the run's own E2E fence |
| State | `.local/superverse-v2`: kubeconfig, cluster state, build contexts, receipts | `.local/superverse-v2/native`, which Kind never reads |
| Git repositories, signing key | `git/`, `git-signing-key` | `native/git/`, `native/git-signing-key` |
| Builder | port 29792, `release-builds/`, `release-build-token`, images loaded into the cluster | port 27792, `native/release-builds/`, its own token, images in the registry on 25000 |
| Docker objects | the cluster node `superverse-v2-control-plane` on the network `kind`; images under the Kind tags | the Compose project `superverse-v2-native`, the `sv2-project-*` and `sv2-session-*` networks, containers and volumes on `native.networkPrefix`, and the coding tag `:native` |

They share the Docker Engine, its image store and build cache, the host's CPU, memory and disk, and the trusted
AppManifest tool of `.local/superverse-v2/app-manifest-tools`, which each builder compiles under a name of its own
and renames into place. A parallel run costs both lanes at once: on the engine, the Kind node with the platform, its
Sandboxes and Live scopes, beside the Native Compose project and Native's project and Sandbox containers, each
limited as on its own (a project component to 2 CPUs and 2 GiB, a Sandbox to its coding profile's limits); on the
host, Native's server, Studio and builder. A browser acceptance on each lane at once doubles the build and browser
load: give the engine the memory of both, or run the two acceptances one after the other.

A browser keeps one cookie jar per host name, not per port, so one browser profile holds a Studio login of one lane
at a time: sign in to the other lane in another profile or a private window. The browser cases run in fresh contexts
and are not affected.

## Reclaiming Docker disk

The lanes add images and build cache to the shared engine with every build, deploy and Release. `sv2 kind e2e`,
`sv2 native e2e` (passed or failed) and `sv2 native stop` end with the same reclamation that this command runs by hand:

```sh
./infra-as-code/sv2 local prune --dry-run   # list what it would remove; takes no lock
./infra-as-code/sv2 local prune             # --image-age-hours 72 and --cache-age-hours 24 by default
```

The automatic run is best effort: when it cannot complete, it prints one line and leaves the test's result unchanged.
It runs beside running lanes because it judges each image on its own: it removes only images that no container uses,
no lane records and that are older than the image age. While a Release build of either builder may still run it keeps
every image, because a build uses base images that no container holds, and removes only the build cache, whose records
an active build holds and BuildKit therefore keeps. A build still `RUNNING` or `UNKNOWN` after two hours belongs to a
builder that stopped mid-build (one build command is bounded to 30 minutes and ProjectReleaseFlow waits at most 60), so
it does not keep the images. In order, it removes:

1. host images older than the image age that no container uses and no lane records: dangling images, and images whose
   every name is a lane's own (`superverse-v2/*`, the Kind Release tags `sv2-<environment>-<project>` and the Native
   registry's `localhost:<port>/sv2-*`). The images `.local/superverse-v2/state.json` records (the Kind deployment and
   its dependency tags) and every Native instance's coding image stay;
2. the build cache unused for the cache age (`docker builder prune --filter until=<h>h`). Old images pin the cache
   records they reference, so the cache goes after them, and a size limit alone frees nothing;
3. images in the Kind node that no container uses and the Kind state does not record, whose every name belongs to an
   earlier lane deployment and which `kind load` imported on a day before the image age. Release images stay. The node
   is pruned only under the Kind action lock and skipped while a Kind action or acceptance holds it.

It prints the counts, sizes and the host's free space before and after; the engine may return freed space with a
delay. It removes no Project container, volume or network: the Project's deletion owns them. A recorded Kind dependency
tag that an earlier manual prune removed makes `kind deploy` refuse with the tag's name; `kind up` loads it again from
its digest-pinned source.

An instance you no longer need is deleted with its data; a running one is stopped first:

```sh
./infra-as-code/sv2 native delete --instance <name> --confirm <name>
```

It refuses while the instance retains a run. It removes every container (with its anonymous volumes), volume and
network that the engine label `superdurable.io/native-instance=<name>` selects, which covers the platform services and
every Project and Session the instance created, then its coding image tag and its state directory, and it releases
the instance's topology index for a later instance. The default instance keeps index 0 and starts empty next time.

## Reclaiming other local disk

`scripts/clean-local-disk.py` applies fixed rules, the same on every run, to the disk that the lanes and the
sessions that drive them leave behind; `--dry-run` lists what it would remove. It takes a lock, so two runs never
overlap, and it removes only what no process uses:

1. Worktrees of this repository under `.claude/worktrees` or a session scratchpad that are clean, whose work reached
   `origin/main` (or whose pull request is merged or closed) and that nothing changed for six hours.
2. In every other idle worktree (Codex's included, which its tool owns), the regenerable `node_modules` and `.next`
   of Studio, once nothing changed for 48 hours. `~/orca/workspaces` is never touched: Orca's workers run there and a
   worktree can look idle between steps.
3. Dormant scratchpads (nothing changed for 72 hours) and `sv2-*` temporary directories older than 72 hours.
4. Kind E2E artifacts older than 72 hours.
5. The Go build caches (the default one and the Makefile's `/tmp/superverse-v2-go-cache`) above 30 GiB, down to what was
   used within 24 hours (all of it only when the disk is low and no `go` command runs); the npm cache above 5 GiB.
6. Docker, through `sv2 local prune` (it needs a Python environment with `infra-as-code/validation-requirements.txt`)
   and dangling images.

Below 40 GiB free the age limits shrink to one hour, the Go caches are cleared whole and the Go module cache too. Native instances are
never deleted by the script: they hold accounts, Projects and demo data, so it only lists them with the
time of their last change; delete one with `sv2 native delete --instance <name> --confirm <name>` when its owner
agrees.

`scripts/clean-idle-containers.py` (`--dry-run` lists) releases the memory of containers that nothing serves. A Native
instance is idle when nobody holds its `owner.lock`; the script then stops (never removes) the instance's running
`sv2-project-*` and `sv2-session-*` containers that started more than an hour ago, and removes those that exited more
than 24 hours ago, with their volumes kept. The next `native start` starts them again. It also stops the Kind node when
no `sv2 kind` command, Kind port-forward, action lock or acceptance fence is in use. Containers of a running
instance and containers without an instance label are never touched.

The Kind node runs every platform Pod and holds several GiB, so it runs only while a command needs it: every `sv2 kind`
command except `delete` starts a stopped node and waits for its API server, and the node's Docker restart policy is `no`,
so the engine does not start it either. `./infra-as-code/sv2 kind stop` stops it (its state, images and data stay); the
long-running `sv2 kind ports` ends with it, and `sv2 kind builder` keeps the node counted as in use until it ends.
`sv2 kind e2e` stops the node itself when it ends, unless another `sv2 kind` command or Kind port-forward runs or a
failed acceptance keeps its fence for reconciliation.

## Actual acceptance

Application connectors follow the same mock default as the Agent. `connections.mockLLM`
is enabled by the local profiles and the platform injects its reserved mock endpoint into
Preview and Live. The template opens the released LLM connector with a guarded local HTTP
transport; it accepts only the fixed synthetic key, calls the platform's deterministic
Responses endpoint, and refuses every real provider request. Hosted connection opening
is unchanged. No OpenAI key or private key file is required or read by the default cases.

With an owner started with `--studio build` and ready, run all three browser cases:

```sh
env -u SUPERVERSE_E2E_OPENAI_FILE ./infra-as-code/sv2 native e2e --run-id <run-id>
```

The cases still configure the summary connection through Setup, execute Preview and Live
summaries, and assert the returned model, deterministic text and positive token counts.
A real LLM variant needs the platform developer's explicit approval before running:
start a separate run with a local configuration layer `connections.mockLLM: false`,
set `SUPERVERSE_E2E_LLM=real` and `SUPERVERSE_E2E_REAL_LLM_APPROVAL` to the recorded
approval reason, then supply the private OpenAI key file. The same approval rule applies
to real Agent models; a Session switched to Claude needs its Project key. Neither
opt-in grants approval by itself.

For credential-bearing runs, set `SUPERVERSE_E2E_CAPTURE=off` to disable browser traces and screenshots and
`PLAYWRIGHT_NO_COPY_PROMPT=1` to suppress Playwright's failure-page snapshot. Assertions, JSON results and
coverage remain enabled. When the capture override is unset, traces retain failures and screenshots capture
failures as before; video remains off.

The runner takes the run's exclusive `e2e.lock` and persists `runs/<run-id>/e2e-admission.json` before spawning a
browser. Only normal completion of all three cases, their cleanup, the final identity checks and the receipt clear
it; a failure, an interruption or an uncertain launch keeps it for explicit reconciliation, as the Kind lane's does.
Before and after the cases the runner requires the Native source, the server binary, the Studio build, the platform
containers, the coding image and the rendered profile to be the run's. The receipt `e2e-acceptance.json` (environment
`local-native`) records them, the callback proof, the cases with their reports, and the startup and case durations. It
never replaces a Kind receipt.

The retained journey `web/studio/tests/e2e/journey/new-project.spec.ts` runs directly with Playwright against the ready
Native Studio, as the [E2E contract](../../web/studio/tests/e2e/README.md#retained-journeys-no-cleanup) shows; it is
not acceptance. Its Preview and Live URLs answer while the run's gateway runs.

The Native owner allows the server 120 seconds after SIGTERM before forcing exit, including
server restarts and recovery. This covers the validated platform Worker drain (at most 60 seconds)
and listener shutdown. A restart command also allows two minutes for replacement registration;
the stopping server reports not ready before draining.
