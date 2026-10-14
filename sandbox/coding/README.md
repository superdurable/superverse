# Coding sandbox image

This image is the tool execution environment inside each Agent Sandbox
`Sandbox`. It implements sandbox image contract revision 8 (coding profile
`coding-v4`) and combines the pinned Agent Sandbox daemon (`sandboxd`) with
`tini` as PID 1, Git, Go, Node.js, npm, Python 3, ripgrep, Dex CLI 1.5.0,
Temporal CLI 1.8.1, Chromium Headless Shell, the image's own commands
`superverse-exec`, `superverse-files` and `superverse-previewctl`, and a
prewarmed proxy of the module graph of the repository's `app-template/` (its
Dex Go SDK, Connector SDK, llm connector and ogen). The Go and gofmt entry
points live in `/usr/local/bin`, so login shells keep them even when the base
image resets `PATH`.

The platform's Session Agent, the provisioning of its instances and its
Preview run in the trusted platform namespace and reach this container only
through `sandboxd`, bound to one
instance's workspace (`internal/agents/sandbox/workspace`). Generated code,
file changes and build commands run here.

## Toolchain and caches

The build copies `app-template/go.mod`/`go.sum` and the ogen tool module
`app-template/tools/openapi/go.mod`/`go.sum`, so the prewarmed graph is exactly
what a new project starts with. Applications may select newer releases, which
download through the public proxy fallback. The read-only proxy is
`/opt/superverse/go-module-proxy`.

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

The three commands are built from the repository's own Go module (the build
stage's `go.mod.imported` names it), so they share `internal/supervision`
(process identities, process-group termination, detached starts, locks and
atomic JSON records) and `internal/confinement` (the path rules), and use only
the standard library besides them.

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

Kind, Native and hosted builds use the single `coding-base` target. The
application's own `go.mod` selects every Dex and Connector module; no workspace
overlay replaces them. Dex CLI is installed by the repository's
`scripts/install-pinned-binary.sh` with its pinned platform checksums. Immutable
Release build profiles keep their admitted historical metadata; updating this
image does not rewrite old build identities.

The Local Kind profile is a functional test lane. Production additionally runs
the image under the `coding-v4` profile's gVisor RuntimeClass and production network
policies. Pull requests that change this image run a native ARM64 container
build on the private Docker runner and verify the complete login-shell toolchain.
