# Coding sandbox image

This image is the tool execution environment inside each Agent Sandbox
`Sandbox`. It implements sandbox image contract revision 8 (coding profile
`coding-v4`) and combines the pinned Agent Sandbox daemon (`sandboxd`) with
`tini` as PID 1, Git, Go, Node.js, npm, Python 3, ripgrep, Dex CLI 1.15.1,
Temporal CLI 1.8.1, Chromium Headless Shell, the image's own commands
`superverse-exec`, `superverse-files`, `superverse-previewctl` and
`superverse-evolve`, and a
prewarmed proxy of the module graphs of the repository's `app-template/` and
each of its examples (the Dex Go SDK, Connector SDK, the connectors the
examples use and ogen). The Go and gofmt entry
points live in `/usr/local/bin`, so login shells keep them even when the base
image resets `PATH`.

The platform's Session Agent, the provisioning of its instances and its
Preview run in the trusted platform namespace and reach this container only
through `sandboxd`, bound to one
instance's workspace (`internal/dexai/agents/sandbox/workspace`). Generated code,
file changes and build commands run here.

## Toolchain and caches

The `app-dependencies` stage downloads the module graphs
(`go mod download all`) of `app-template/go.mod`/`go.sum`, of the ogen tool
module `app-template/tools/openapi/go.mod`/`go.sum` and of the same manifests
of every listed example's composed tree, so the prewarmed graph is exactly what
a new project starts with, from a description or from an example. The downloads
go through a module cache mount that the builds on one engine share, so a
module an earlier build fetched needs no network; the stage then fills a module
cache of its own from that mount and copies only that, so the proxy holds these
graphs and no module another build left in the mount. Measured on 2026-10-16 it
holds 150 MB (142 MB compressed), 14 MB of it (13 MB compressed) for the
examples' own modules. Applications may select newer releases, which download
through the public proxy fallback. The read-only proxy is
`/opt/superverse/go-module-proxy`.

The `template-source` stage reads the server's embedded catalog,
`internal/dexai/app/apptemplate/examples/catalog.json`, and unpacks the
composed tree of each example it lists, in its order: the default from
`app-template.tar`, every other one from `examples/<id>.tar`. These are the
trees setup commits, so the seeds compile exactly what a new project holds; an
archive of that directory that the catalog does not list is never seeded.

The `app-build-cache` stage seeds the examples' build caches. It starts from
`coding-toolchain`, the image's packages, Go, Temporal and Dex CLI, module
proxy, environment and user, so it builds with the final toolchain. For each
example in turn it replaces `/workspace` with the example's Go sources, OpenAPI
document and npm manifests, as a project created from it has them, and runs the
commands of the template's gate into one Go build cache: the Go half of
`make generate` (ogen), both `go vet` runs, `go build ./...` and the API test
compile. Then it runs `npm --prefix web ci` once for each distinct npm
lockfile. Go keys a build cache entry by the compiler, the package directory
and its sources, and the module cache path is the runtime one, so the entries
match a new project's, and a package that several examples import at the same
release is stored once. The final `coding-base` stage builds on the same
toolchain. It copies the Go build cache to
`/opt/superverse/go-build-cache-seed` (497 MB for the four examples) and the
npm cache to `/opt/superverse/npm-cache-seed` (32 MB; the examples share one
lockfile), root-owned and read-only, names them in
`SUPERVERSE_GO_BUILD_CACHE_SEED` and `SUPERVERSE_NPM_CACHE_SEED`, and only then
adds `sandboxd`, the image's commands, the skills and the shims. A change to
one of those therefore rebuilds and republishes only the small layers from its
own on, never the seeds; a change to the proxy rebuilds the seeds but not the
Temporal and Dex CLI layer below it. The template's `make bootstrap` copies the
build cache once into the Pod's `$GOCACHE` (`cp -R -n`, fresh modification
times, so Go's trimming keeps them; an entry Go cannot verify is a miss) and
the npm cache before an `npm ci`. The image's smoke test builds every listed
example from the frozen context with the seed restored and the image's module
proxy as the only one, and fails when a module is missing from the proxy or
`go build -v ./...` compiles a package outside the template module. A change to
an example therefore rebuilds the image: the coding fingerprint names the
catalog and the archive of each example it lists, which stand for every file of
its composed tree.

The coding toolchain owns npm install policy through `NPM_CONFIG_AUDIT=false`
and `NPM_CONFIG_FUND=false`. Cache seeding, workspace bootstrap, Preview builds,
connector contribution builds and the image smoke test inherit these settings,
including applications created before this policy. Installs therefore do not
depend on npm audit or funding services. The smoke test checks the effective npm
configuration. Bootstrap keeps `--prefer-offline` and the existing cache seed.
Release builds use immutable, versioned profiles outside this image; existing
profiles retain their recorded npm settings.

### Seed budget

`make bootstrap` copies both seeds into the Pod's `/tmp`, an `emptyDir` with a
2 GiB `sizeLimit` (`_coding-profile.tpl`) that also holds the module cache and
every later build; kubelet compares the blocks its files allocate with the
limit and evicts a Pod that passes it. The seed stage therefore fails when the
Go build cache seed and the npm cache seed together take more than 1 GiB,
counted in 4 KiB blocks as the node's filesystem stores them (each file rounded
up to whole blocks, each directory one block), whatever the build host's
filesystem allocates; the build log prints the count. Most seed files are
small, so the count is about 6% above their apparent size.

Measured on 2026-10-16 (`linux/arm64`, in 4 KiB blocks with the apparent size
in parentheses) after each example of the seed stage, in catalog order:
`text-summary`, the default, 400 MB (375 MB); `github-releases-newsletter`
92 MB more (88 MB), most of it the gRPC, x/net, x/text and genproto releases it
selects besides the base's, then its three connectors; `slack-thread-approval`
23 MB more (22 MB), for its connector and another Connector SDK release;
`summary-approval`, which keeps the default's `go.mod`, 12 MB more, its own
packages. The Go build cache seed takes 527 MB (497 MB) and the npm cache seed
34 MB (32 MB), 560 MB together. Compressed, the build cache layer grows from
89 MB to 116 MB. The seed stage compiles the four examples in 30 s on 10 CPUs,
against 21 s for the default alone.

An example adds the packages that no earlier example compiled: its own; those
of every module the base does not compile, its connectors and their
dependencies; those of modules it shares with the base that the base does not
import (for the Slack connector, two Connector SDK packages); and every shared
module again at each release it selects besides the base's. With the base's
release of every module it shares, `slack-thread-approval` adds 19 MB to a seed
of the default alone instead of 24 MB: about 12 MB of its own packages, as
`summary-approval` adds, and about 7 MB for the Slack connector,
gorilla/websocket and the two Connector SDK packages. An example with an npm
lockfile of its own adds the packages that no other lockfile installs, up to
the 34 MB of today's whole lockfile.

After a first gate in a container of the image, its `/tmp` held 0.90 to 0.95
GB: the copied seeds, the module cache (334 MB for
`github-releases-newsletter`, 383 MB for `text-summary`) and the gate's own
builds. With the seeds at their 1 GiB budget and the module cache of
`text-summary`, the Pod keeps about 690 MB for its later builds. The budget
leaves 640 MB for the examples besides the default and the npm seed: about 15
examples of today's average (42 MB), 7 that each select their own releases as
`github-releases-newsletter` does, or about 33 that keep the base's releases
and add one Slack-sized connector each. The connectors' packages alone, at
about 7 MB each, fill it at about 90 distinct Slack-sized connectors.

The image ENV owns the toolchain cache paths: Go modules under
`/tmp/superverse-tool-execution/go/pkg/mod`, the Go build cache and the npm
cache under `/tmp`, XDG config and cache under `/tmp/superverse-tool-execution`.
The image HOME is the workspace volume `/workspace`, so every process the
platform starts for repository code also sets `HOME=/tmp`; nothing a tool writes
under HOME lands in the repository. The system Git configuration marks
`/workspace` as a safe directory for processes started without the platform's
own Git environment.

The repository's [Dex application skills](../../dex-skills/README.md) are copied
read-only to `/opt/superverse/dex-skills`, the `dex-connector-contributor` skill of
connector development among them since revision 7. The image's source fingerprint
identifies this Superverse source; there is no separate skills tag or label
([the current contract](../../docs/architecture/superverse-v2.md)).

The Go and React project template runs source checks, generation and
production builds without installing operating-system packages. The template
owns its exact npm dependencies and lockfile; the image installs no global
copies of `@hey-api/openapi-ts` or `@playwright/test`. Playwright should use
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` when set and add `--no-sandbox` and
`--disable-dev-shm-usage` only for that system browser; browser downloads are
disabled. Project commands may restore dependencies their manifests and
lockfiles declare, but there are no global npm packages, operating-system
package installs or remote installer scripts. Agent behavior is governed by
the Agent system prompt and the workspace `AGENTS.md`, not by this README.

## Command ledger and file helper

`superverse-exec` ([contract](cmd/superverse-exec/README.md)) is the command
ledger: every recoverable command the platform starts, the Agent's `bash` tool
included, runs under its own detached supervisor and is recorded under a stable
execution ID in `/tmp/superverse-exec`, with its lifetime and output limits
enforced in the Pod. `superverse-files` ([contract](cmd/superverse-files/README.md))
serves line reads, idempotent edits, writes and ripgrep searches under the
agent's path rules. Raising what either promises raises the image contract
revision and the coding profile's metadata revision.

The image's commands are built from the repository's own Go module (the build
stage's `go.mod.imported` names it), so `superverse-exec`, `superverse-files`
and `superverse-previewctl` share `internal/supervision`
(process identities, process-group termination, detached starts, locks and
atomic JSON records) and `internal/confinement` (the path rules), and use only
the standard library besides them.

`superverse-evolve` ([contract](cmd/superverse-evolve/README.md)) is the
agent's measurement in an agent review, which the platform never runs: it
checks a goals file and evaluates, compares and gates a proposed change over
the review's folder of Live runs that `place_evolve_export` placed, refusing
any other runs or goals. It is built in the same stage from the source tree's
goals evaluator and run record (`internal/dexai/delivery/evolve/goals` and
`runexport`), which import only the standard library, `gopkg.in/yaml.v3`
(`go.sum.imported`, from the image's module proxy, so the build needs no
network) and each other, as `scripts/check-package-layers.py` checks; a change
to them therefore runs on Local Kind (`scripts/kind-required-paths.txt`).

## Preview environment

`superverse-previewctl` (`cmd/superverse-previewctl`, whose README is the
command contract) runs the Preview inside the Sandbox: a local Temporal and
`dexcli dev` engine whose state lives on the workspace volume at
`/var/lib/superverse/preview`, and the Preview application, built from an
isolated export of a prepared commit. Its `apptest run` gives each application test
run a fresh in-memory Temporal and `dexcli dev` on the test ports, one run at a time. Its daemon is detached from sandboxd's
commands; `tini` reaps the processes it leaves behind, and the profile's
preStop hook stops it.

The Agent's `PATH` holds guarded shims of `dexcli` and `temporal`
(`shims/`). `dexcli` passes only read subcommands unless `--allow-mutation` is
given, and neither starts a second development environment; the real binaries
live in `/usr/local/libexec/superverse` for `superverse-previewctl`.

Storage credentials arrive as a private file under `.git`, never as
command-line arguments.

## Build

Build from the repository root and supply the Agent Sandbox checkout as a named
context:

```sh
docker build \
  --file sandbox/coding/Dockerfile \
  --build-context agent_sandbox=/path/to/agent-sandbox-v1.0.1 \
  --build-arg GO_IMAGE=public.ecr.aws/docker/library/golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd \
  --build-arg NODE_IMAGE=public.ecr.aws/docker/library/node:22.22.0-bookworm-slim@sha256:dd9d21971ec4395903fa6143c2b9267d048ae01ca6d3ea96f16cb30df6187d94 \
  --tag kind.local/superverse-coding-sandbox:local-kind-e2e \

```

Kind, Native and hosted builds use the single `coding-base` target, the last
stage; `sandboxd-build`, `template-source`, `image-commands`,
`app-dependencies`, `coding-toolchain` and `app-build-cache` are its build
stages. The
application's own `go.mod` selects every Dex and Connector module; no workspace
overlay replaces them. Dex CLI is installed by the repository's
`scripts/install-pinned-binary.sh` with its pinned platform checksums. Immutable
Release build profiles keep their admitted historical metadata; updating this
image does not rewrite old build identities.

The Local Kind profile is a functional test lane. Production additionally runs
the image under the `coding-v4` profile's gVisor RuntimeClass and production network
policies. Pull requests that change this image run a native ARM64 container
build on the private Docker runner and verify the complete login-shell toolchain.
