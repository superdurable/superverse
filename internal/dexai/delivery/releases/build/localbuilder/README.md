# Local build service

`server.py` is the explicit development build service of the local lanes, Local Kind and Local Native, with five typed endpoints: `/releases/inspect`, `/releases/start`, `/releases/cancel`, `/releases/cleanup-release`, `/releases/log-read`. It is not a business API or a generic command endpoint. The merged Go server remains the business owner through ProjectReleaseFlow and the `Builder` interface.

| File | Responsibility |
| --- | --- |
| `server.py` | The fixture: request validation, source preparation, image build and publication (Kind load or a push to the Native registry), inspection, cancellation and Release cleanup |
| `../build-profiles/go-react-v1/` | The `bp1` profile it accepts: `profile.json` and the product-owned `Dockerfile` |
| `../build-profiles/go-react-v1/local/` | The local `bp3` profile, the default of new local builds: `bp1` with the module redirect |

## Per-commit Release protocol

ReleaseID equals the main commit. Every request carries exactly the typed key
set, including `contractVersion: 2` and `buildRevisionId`, and, for an image
build, `configurationDigest` (`sha256:<64 hex>`) and `configurationRevision`
(uint64).
The frozen configuration reference remains owned by the Release; the builder
binds its digest/revision into the strict definition bundle and image labels.
Configuration bytes and credentials are never embedded in the image.

The preparation request has `buildRevisionId: source` and `sourceOnly: true`,
omitting configuration fields. It uses the exact accepted profile, clones the
exact readable commit of the repository host and performs strict FDG/manifest validation, then
returns READY with four payloads and empty image fields. It never invokes image
build/load. A shared four-minute work deadline bounds its subprocesses; owned
analysis containers are stopped, observed and removed with a separate bounded
cleanup budget. Native process cancellation stops, then kills, the exact
observed child tree of the running command and confirms the build `CANCELLED`
once every observed child is gone; it retains UNKNOWN only when the tree cannot
be established or stopped under the existing launcher ownership contract.
Native child admission tolerates the spawn/exit observation race and validates
the direct child's parent rather than assuming its process group. A failed
command records a bounded exception class and trusted command kind in the job
log, without exception text, arguments, environment or credentials. Container
cleanup queries exact owned names even when `docker rm` times out; proven
absence settles FAILED. Inspect reconciles UNKNOWN source jobs once the job
has ended, the child is proven settled and every container of its exact
attempt prefix is absent. Persisted child settlement survives builder restart;
historical UNKNOWN jobs without that proof remain unresolved after restart.
Failed source inspections also check that prefix: a Docker create can finish
after its CLI was cancelled. For an ended job with settled-child proof,
inspection continues its admitted cleanup, removing at most one exact observed
container per call and rechecking absence before reporting FAILED. Explicit
cancellation and Release cleanup also reconcile names of ended jobs, including
after a builder restart; no newer attempt starts while their outcome is unknown.
Each SDK metadata probe has up to 120 seconds for tool-container startup under
load, within the same shared four-minute source deadline.
A cancellation of a build revision that has no state yet records the attempt
FAILED `CANCELLED`, so a Start of that attempt arriving later is refused.

Each Save and Build uses `build-<UUIDv4>`. Private source/build artifacts reside at
`<environment>/<project>/<commit>/<buildRevisionId>/`; image tags append that
revision, so rebuilding one commit cannot replace an earlier Live image.
`/releases/cleanup-release` takes a normal request, durably fences all
new revisions, and settles/removes at most 64 exact revision directories per call.
MISSING means all directories have been reclaimed; RUNNING means more cleanup is
needed, which the Go client reports as `build.ErrPending`; UNKNOWN means provider
termination remains unproven. A cancelled build that still runs is also
`build.ErrPending`. A rejected request returns the fixture's fixed error text with
its HTTP status. Exact per-revision and whole-commit fencing receipts survive
cleanup. Kind images retain the existing owned-cluster cache lifetime, and the Native
registry keeps its images with the lane's registry volume.

Use a dedicated 0600 token file of at least 32 characters. Start with `python3 internal/dexai/delivery/releases/build/localbuilder/server.py --token-file <private-file> --tool-image <immutable-image-reference> --app-manifest-tool <trusted-compiled-app-manifest-tool> --git-base-url <origin> [--target docker --registry localhost:<port> --state-dir <lane-build-state>]`. The default bind is 127.0.0.1:29792, Kind's; `native start` passes `--port 27792`, the port of `native.yaml`, so the builders of both lanes run at the same time.

## Publication targets

`--target` selects where a build publishes its image; every other step is the same.

| Target | Lane | Publication | READY recheck |
| --- | --- | --- | --- |
| `kind` (default) | Local Kind | `kind load` into every node of the owned cluster, then an alias by the loaded descriptor digest in every node; startup requires the cluster | The descriptor every node holds for the build's tag, which must be the recorded one |
| `docker` | Local Native | `docker push` of `<registry>/<imageTag>` to the lane's local registry, then the digest the registry holds under that tag (an index or a manifest) pins `artifactReference` as `<registry>/<repository>@<digest>`; no Kind is involved, and startup requires the registry to answer | A HEAD of the tag's manifest at the registry, whose `Docker-Content-Digest` must be the recorded digest |

The registry is `localhost:<port>`: the engine pushes to it and pulls from it under the same
address, so the Live application of a Release runs exactly the pushed digest. ProjectReleaseFlow's
exact comparison of the observed and the rechecked reference and digest holds for both targets.
Each lane has its own builder state and token: Kind keeps `.local/superverse-v2/release-builds`
and `release-build-token`, and `native start` passes `--state-dir
.local/superverse-v2/native/release-builds` and the Native lane's own token file. `--git-base-url` is the origin that the builder fetches Project source from: `https://github.com` (the default) or the lane's local Git host, `http://127.0.0.1:28085` in Kind and `http://127.0.0.1:26085` in Native, which `kind builder` and `native start` pass from the deployed profile. The builder fetches the exact commit without a depth, because the local Git host does not serve shallow requests. Docker Desktop forwards the owned Kind node's `host.docker.internal:29792` traffic to this localhost listener; no all-interface listener is needed. Do not expose this fixture publicly. The Go client only accepts reviewed local endpoints, rejects redirects and uses a 30-second request deadline.

The trusted executable is built from `cmd/tools/app-manifest`. The Local Kind CLI
compiles it into `.local/superverse-v2/app-manifest-tools/<source-fingerprint>/`,
named by its transitive source fingerprint. A running helper must be restarted
from the accepted source when its command path changes. The executable emits
the Release artifacts whose retained names
[appmanifest](../../../../app/appmanifest/README.md#retained-contract-names) documents.

The fixture accepts only the complete trusted fingerprint of the `bp1` or `bp3` profile, exact 40-character source commit, isolated V2 project/image identity and owned `superverse-v2` Kind cluster. Git receives an ephemeral repository-scoped credential through its process environment/helper. Source code never executes on the host. FDG analysis runs inside the immutable tool container without network, Docker access or host credentials. Image creation uses the product-owned digest-pinned Dockerfile, never a project-supplied Dockerfile; the recipe runs code generation, tests, vet and compilation. The `kind` target reads the exact OCI descriptor from every Kind node and publishes an immutable digest alias; the `docker` target publishes the digest its registry holds.

Source preparation uses `go mod download` for the application and generator
modules before offline strict FDG analysis, from the image's module proxy and the
public proxy with the checksum database. The accepted source is mounted read
only. Do not use `go mod download all`: under lazy loading it may add checksums
for dependency-only packages and fail by trying to rewrite the accepted
`go.sum`. This does not authorize editing lockfiles during Release preparation;
missing required checksums still require a corrected source commit.

## Release candidates

A Release may depend on a release candidate of a connector that the lane contributed, a
tag only the local Git host's copy of the connectors library holds
([connectorlibrary](../../../../../comm/platform/connectorlibrary/README.md#release-candidates)).
`--module-redirect MODULE=CLONE_URL`, which `release_builder.py` passes on a lane whose
repository host is the local Git host, redirects that module as the Sandbox's
`sandboxd.ToolProcessEnvironment` does: `url.<clone URL>.insteadOf` sends Git's fetches of
the module's repository to the copy, as a container of this machine reaches it
(`http://host.docker.internal:<port>/superverse-local/dex-connectors-library.git`), and
the module skips the checksum database, which knows no such version; go.sum still
verifies every module. Source preparation then adds the `direct` source after the
proxies, so a version the proxies lack comes from the copy; every other module still comes
from the proxies. An image build of `bp3` passes the redirect as the build arguments of its
`go-modules` stage; a `bp1` build, which predates it, gets none. Without the argument, as
with GitHub, nothing is redirected.

Local image builds pass `--no-cache-filter go-build`. Docker's shared layer cache
reuses the `go-modules` stage, whose online downloads persist both the OpenAPI
tool's and application's complete module caches in immutable image layers.
The `go-build` stage always executes generation, tests, vet and compilation
without network, with `GOPROXY=off` and `GOSUMDB=off`. Its compiler cache mount
is only an accelerator: eviction causes recompilation, never a missing module.
The frontend install layer remains cacheable, as does its npm download mount.
Hosted builds use a fresh isolated BuildKit instance and do not share this cache.

After replacing the local coding image or trusted AppManifest executable,
reconcile every build receipt and stop the exact owned helper only when its
jobs are settled. Restart through `sv2 kind builder` (or a new `sv2 native start`)
so its fixed tool image and executable match the accepted build. Never delete a failed receipt to force a
MISSING result: retry the same Release through its explicit preparation
admission, which advances the attempt after known failure.

Private local state is under `<state-dir>/<environment>/<project>/<release>/<buildRevisionId>/`. Inspect reconciles the request, published image and four payloads, and returns the bounded failure classification of a FAILED or UNKNOWN build (`BUILD_FAILED`, `CANCELLED`, `CANCEL_OUTCOME_UNKNOWN` or `NATIVE_CANCEL_OUTCOME_UNKNOWN`), never provider output. Running state without a live owning helper thread is UNKNOWN and requires inspection; automatic restart never assumes it succeeded or starts duplicate work. A failed build can be rerun only with an explicitly higher attempt. Cancel confirms process termination before reporting completion. Release cleanup removes private fixture files after Flow-owned S3 artifact cleanup. Immutable node images are an owned-cluster cache and are removed with that disposable cluster, and registry images stay in the Native registry's volume; neither is an application serving resource. SIGTERM/interrupt cancels child process groups.

Inspect also returns the `phaseTimings` that the job measured and that
`state.json` keeps with its terminal state, as `build.BuildPhaseTiming`
entries: QUEUE from start admission until the job thread begins,
HOST_PREPARATION for the Git fetch and checkout, DEPENDENCY_PREPARATION for the
module downloads, ANALYSIS for the trusted AppManifest tool and the FDG
analysis, COMPILATION for `docker build` and IMAGE_PUBLICATION for the Kind load
and the digest tags, or the push and the digest the registry holds. ANALYSIS is entered twice, around the module downloads, and
reports its first start with the summed work. Source preparation ends with
ANALYSIS. A failed build reports the phases it finished and a running build
none. Durations are whole milliseconds, at least one, so a measured phase never
reads as running.

`/releases/log-read` answers `{"text": ..., "next": <offset>, "complete": <bool>}`:
one page of the build's private `build.log`, which keeps the last 256 KiB of
the combined output of the build's commands (mode 0600, inside the revision
directory, so Release cleanup removes it), from the absolute byte offset in the
`X-Superverse-Log-Offset` header, or its end with `X-Superverse-Log-Tail: true`,
at most `X-Superverse-Log-Limit` bytes (1 to 64 KiB). `build.log.start` records
the absolute offset of the first byte kept, so a cursor survives the trim; a
cursor before it resumes at it. While the build runs, or while more follows, a
page ends at a whole line; a single longer line is cut at the limit. `complete`
is a build that no longer runs and whose log holds nothing after `next`. A
build without a log, or a revision closed by cleanup, answers an empty,
complete page. The log grows by whole commands, so a running `docker build`
shows its output when it ends. The log is build output: the server reads it
only on demand, redacts it and never stores it.

Hosted CodeBuild/ECR transport is implemented separately in `../hosted/README.md` behind the same `Builder` interface.

## Application source

Builds use the application's own `go.mod` with `GOWORK=off` and the published
`go-react-v1` recipe, `bp3` for a new local build ([the current contract](../../../../../../docs/architecture/superverse-v2.md)).

Live source preparation resolves `github.com/superdurable/dex/sdk-go` with the isolated Go toolchain after replacements. The selected version must be at least `v1.13.0`; missing, local-path, malformed and older selections produce the typed application rejection `unsupported-dex-sdk` with an upgrade hint. Preliminary module metadata rejects invalid replacements before downloads; the offline selected module check follows dependency preparation. No selected version is retained in state, artifacts or logs.
