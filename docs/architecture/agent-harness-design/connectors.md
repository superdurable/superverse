# Connectors

Generated applications reach external products through connectors from the public connectors
library. This document owns:

- the facts about the library that the platform depends on;
- how an application discovers, declares and wires a connector, and how the configuration host
  and Studio use its release;
- the connector subagents that choose connectors and build missing ones in the library;
- how every contributed head is published as a release candidate that the application uses in the
  Preview and Live before the connector's release;
- the GitHub App setup these need, and the connector mocks used by application tests.

Other topics own what this one uses:

| Subject | Owner |
|---|---|
| Subagent Flow, worktrees, reports, limits | [subagents](subagents.md) |
| Notification path, `connector` notifications, automatic turns | [notifications and feedback](notifications-and-feedback.md) |
| Requirements card that lists connections | [session](session.md#requirements-cards) |
| `commit_and_push`, dexcli checks, application tests that use mocks | [commit gate and application tests](commit-gate-and-app-tests.md) |
| Preview engine and the Preview deployment chain | [sandbox](sandbox.md), [notifications and feedback](notifications-and-feedback.md#the-deploy-preview-chain) |

## The connectors library

`superdurable/dex-connectors-library` is a public repository. These are the facts the platform
relies on.

| Fact | Detail |
|---|---|
| Layout | `sdkgo/` is the Connector Go SDK, its own module. `connectors/<company>/<slug>/` is one Go module per connector: the manifest `connector.yaml`, the generated `zz_generated_connector.go`, a README, provider tests, a runnable example under `examples/`, the generated mock package `<slug>mock` where the connector declares mocks, and optional Studio UI sources under `ui/`. A company with one connector has no slug directory (`connectors/newsapi`). `catalog.yaml` at the root is the sorted allowlist of connector directories. `cmd/connectorctl` validates, generates and releases. |
| Module path and tag | `github.com/superdurable/dex-connectors-library/connectors/<path>`, for example `.../connectors/superdurable/llm` and `.../connectors/google/gmail`. Tags are `connectors/<path>/vX.Y.Z` and `sdkgo/vX.Y.Z`. |
| Slug | The connector ID, `metadata.name` of the manifest: the last element of the path (`llm`, `gmail`, `newsapi`). |
| Independent versions | Each connector module has its own `metadata.version` and is released on its own: a pull request that should release moves only its connector's version, one step from its latest tag (patch for a fix, minor for an addition such as an operation, a Trigger, a UI unit or mocks; a breaking change before v1 is marked `(breaking)` with a minor bump), and a new connector starts at `v0.1.0`. `connectorctl catalog --check` requires stable semver, never behind the module's latest tag, a large enough bump for a breaking change and the connector listed in the catalog. The manifest version is not the SDK version; the connectors share the one `sdkgo` module. |
| Release workflows | `Release Connectors and Catalog` runs on every push to `main`. It compares each manifest version with the reachable tags and publishes the pending ones in parallel: compatibility gate against the latest Dex CLI, generated-code and `GOWORK=off` module checks, dependency rules, then `connector-release.json` with its digest, an optional UI artifact, a module-download check and the marker `connector-release.complete`. It then publishes `catalog.yaml` and the directory site to GitHub Pages (`https://superdurable.github.io/dex-connectors-library/`). `Release Connector Go SDK` does the same for `sdkgo`. Manual runs only repair an incomplete release. |
| Merge is release | A merge publishes every version it newly declares, so a contribution that moves its connector's version is released a few minutes after the merge, and a merge that moves no version releases nothing. |
| SDK first | An SDK change is merged and released (an `sdkgo:` pull request) before any connector requires it. A connector `go.mod` requires an exact published SDK tag, never a `replace`, a pseudo-version, a branch or a commit. `sdkgo` v0.24.0 ships the mock contract `connectormock`. |
| Pull request rules | One released module per pull request. The title starts `sdkgo:`, `connector(<slug>):`, `tooling:` or `docs:`. The token `(breaking)` marks a breaking change. `main` is protected by organization rules: pull requests only, no force push, no deletion. CI runs on branches of the same repository. |
| Authorship | An AI tool is never the Author, the Committer or a `Co-authored-by`, and hooks are never bypassed. A human reviews and merges every pull request. |
| Authoring requirements | The manifest is the only source of generated code. Each connector has a README, provider tests and a runnable example, and a new connector adds one `catalog.yaml` entry. Fields visible in the configuration UI need connector-owned guidance. |

Consequences for Superverse:

- Commits that Superverse makes in the library are authored and committed as the GitHub App's bot
  identity (or `Superverse` where the server has no upstream library), never as the model, the user
  or any AI tool. The platform never merges a pull request.
- A change that needs an SDK change is outside what a connector subagent can deliver: it reports the
  blocker, and the main agent tells the user.
- A contribution becomes releasable once its pull request, which moves its connector's version, is
  merged. Until then it can only be used in Preview ([below](#using-a-connector-before-its-release)).

## Discovery: the `connector_catalog` tool

The agent selects connectors from the published catalog, which lists released connectors only: the
catalog matches the Session's first segment lists ([below](#catalog-matches-of-the-first-request)),
and `connector_catalog` for every need they do not cover. `connector_catalog` searches the
catalog.

- **Input** (strict schema): `query`, 1 to 160 characters of provider, connector ID or capability
  terms, all of which must match; optional `operation`, an exact operation name to verify; optional
  `limit`, 1 to 8 (default 4).
- **Source.** `https://superdurable.github.io/dex-connectors-library/catalog.yaml`, fetched inside the
  Sandbox with `curl` (HTTPS only, 15 seconds, at most 1 MiB), checked for the expected document type,
  at most 512 connectors and valid directory and version identities, then kept in a process-local cache
  for five minutes. No private scope or credential enters the cache. Raw catalog bytes never become
  model or UI progress.
- **Matching.** A connector matches when every term occurs in its ID, name, description, version,
  directory, or in the name, kind and description of any operation or trigger. With `operation`, only
  connectors that publish that exact operation match, and only that operation is returned.
- **Result.** Compact JSON: the catalog source and SHA-256, the observation time, then per match the
  connector ID, name, description, version, directory, operations (name, kind, description) and
  triggers, the `install_command` (`go get <module>@<version>`) and the names of the manifest and
  factory files inside the module. At most 16 KiB: matches are dropped from the end until it
  fits, and a single match that still does not fit fails with `catalog_result_too_large`, asking for one
  exact operation.
- **Failures.** `catalog_unavailable` (fetch failed, or the response was truncated) and
  `catalog_invalid` (unexpected document) are platform-owned, so the agent stops connector-dependent
  work and reports the blocker; it never falls back to the library's `main` branch.
- **Execution.** Read-only, parallel-capable, 60 seconds ([tool orchestration](tool-orchestration.md)).

After `install_command`, the agent locates the module directory with `go list -m -f '{{.Dir}}'
<module>` and reads `connector.yaml` and `zz_generated_connector.go` with the file tools; the module
cache is a read-only root of those tools. It confirms the configuration fields, authentication
methods, input and output types, branches, idempotency, execution policy, trigger schema and generated
package before writing code, and matches each requirement to an exact operation name and kind. A
capability that does not exist is recorded as a gap, never assumed.

### Catalog matches of the first request

The server finds the released connectors that the Session's first request names before the first
model call, so the agent can write the requirements card at once, without a Sandbox, a subagent or
a catalog search.

- **Copy.** Each server replica keeps one copy of the same published catalog document
  (`connectorlibrary.PublishedCatalog`). The composition root refreshes it in a goroutine it owns
  and cancels at shutdown: one HTTPS GET at start and every five minutes, a minute after a failed
  fetch, each within 15 seconds; the document is decoded like the tool's (at most 1 MiB and 512
  connectors, valid identities) and the last good copy stays until a newer one replaces it. Studio's
  TeamSpace connection catalog reads the same copy and fetches only when it is older than five
  minutes. The copy holds only public data.
- **Matching.** On the warm copy, with no network call, the Step that opens the first segment
  matches the user's own words of the request, by whole words and ignoring case, against each
  connector's display name, its ID with hyphens read as spaces, its directory's last element and a
  small alias table (`Sheets`, `Google Mail`, `Outlook`, `Teams`, `Excel`, `SharePoint`, `Entra`,
  `S3`, `Postgres`, `Zendesk`, `Twilio`, `Hacker News`). A display name or alias that is also an
  everyday word (`Front`, `Linear`, `LLM`, `Webhook`, `Zoom`, `Notion`, `Teams`, ...) matches only
  as capitalized, and such an ID or directory element never matches. Overlapping names resolve to the
  leftmost and then the longest, so `Outlook Calendar` names the calendar connector.
- **Result.** The `<catalog_matches>` section of the first `segment_start`, in the tool's result
  shape: the catalog source and SHA-256, the fetch time, at most six connectors in the order the
  request first names them, each with its operations, triggers, module and `install_command`, the
  total and whether entries were left out, at most 8 KiB
  ([context management](context-management.md#segments)). A cold copy or no match leaves the
  section out, and the agent searches with `connector_catalog` as before.

The catalog names no authentication method, so the card's "what the user must set up" may stay
general (connect the account in Studio) until the build reads the connector's manifest.

## Declaring and wiring a connector

An application declares each connection in three places that must agree.

- **`dex-app.yaml`**, entry in `connectors`:

  | Field | Meaning |
  |---|---|
  | `connectionName` | A static name the agent chooses. It is also the connection's identity in Studio Configuration. |
  | `connectorId` | The slug, for example `llm`. |
  | `modulePath` | The exact module. The short connector ID does not determine it. |
  | `version` | The version `go.mod` selects. |
  | `authMethodId` | The one authentication method: `auth.defaultMethod` of the manifest unless the user asked for another listed method, and `""` when the manifest has no `auth.methods`. |
  | `operations`, `triggerBindings` | The operations and the `{triggerName, bindingName}` pairs the application uses. |

- **`go.mod`** requires the module at that version (`go get <module>@<version>`).
- **Worker code** opens each connection with the connector's generated
  `NewProjectConnection(configuration.Project(), name)` and sets the Step's `ConnectionName` to the
  same static name; the generated factory panics on a mismatch. Credential codecs and providers are
  never written by hand.

Rules the platform enforces:

- `commit_and_push` rejects a missing field or a module outside
  `github.com/superdurable/dex-connectors-library/connectors/`, and `make check-fdg-v2` rejects a
  `dex-app.yaml` version that differs from `go.mod`.
- Credentials exist only in Studio Configuration: never in source, Flow state or logs, and their
  absence does not block a complete source commit. The agent never asks the user which key or account
  to use; it declares the connection and the user configures it.
- Without a fitting connector the agent does not call the provider directly from the application. For
  a public product with a documented API it starts a connector subagent ([below](#connector-subagents));
  for an internal service it checks for a generic connector such as `webhook`, and otherwise reports the
  gap.
- Applications are not pinned to a connector or SDK version: `go.mod` records minimums and the agent may
  move to newer stable releases after reading their breaking changes
  ([the current contract](../superverse-v2.md)).

## How the configuration host resolves releases

The configuration host (`internal/dexai/studio/confighost`) validates a Project's configuration against the
release each declared connection names. It resolves only exact official releases.

1. **Input.** The AppManifest registered for a source commit lists each connection as a
   `connectormanifest.Module{ConnectorID, ConnectionName, ModulePath, ModuleVersion}`. Registration
   rejects a declaration that is not an exact official release: a module path under the library's
   `connectors/` directory, a version of the form `vX.Y.Z`, a connector ID that starts with a letter and has 2 to 63 lowercase letters,
   digits and hyphens.
2. **Download.** `ReleaseResolver.Resolve` reads two assets of the GitHub release whose tag is the module
   path without the repository prefix plus the version, for example `connectors/superdurable/llm/v0.21.0`:
   `connector-release.json.sha256` (at most 1 KiB) and `connector-release.json` (at most 1 MiB).
3. **Verification.** The metadata must match its published SHA-256; the connector ID, manifest name,
   module path, version and tag must match the module; the manifest digest and source commit must be
   present and well formed; the Studio command declarations must be valid.
4. **Cache.** Verified releases are kept by connector ID and tag, at most 128, oldest evicted first. The
   data is public.
5. **Use.** The verified manifest drives the connection form, authorization (including OAuth),
   brokered provider commands, and readiness: the stored connection must match the module, provider and
   authentication method, its credential must be READY, its configuration and credentials must be
   complete, and every declared trigger binding must be configured. The stored connection records no
   release, so upgrading a connector keeps a saved connection ready.
6. **Failure.** A release that cannot be downloaded or verified is `CONNECTOR_RELEASE_UNAVAILABLE`
   (HTTP 502). It is a platform failure, not a configuration gap
   ([gap report](notifications-and-feedback.md#configuration-gap-report)).

No remote UI bundle is downloaded or run. A release candidate `vX.Y.Z-rc.N` takes the same steps: the
host downloads it from where this server's contributions are published
([below](#using-a-connector-before-its-release)).

## Studio's native UI units

Studio renders connector configuration with eight built-in units and plain fields. A manifest declares
the units it uses in `spec.studio.units` and binds them to fields with `studioUnit: {unit, port}`.
Provider reads that a unit needs run as brokered, read-only commands declared in `spec.studio.commands`
with the connection's credential.

| Unit | Value port |
|---|---|
| `modelPicker` | `model` |
| `channelPicker` | `channelId` |
| `memberPicker` | `memberIds` (several) |
| `spreadsheetPicker` | `spreadsheetId` |
| `sheetTabPicker` | `tab` |
| `textInput` | `text` (multi-line) |
| `emailListInput` | `emails` (several) |
| `searchQueryInput` | `query` (multi-line) |

A release that declares any other unit shows "a configuration control that Studio does not support", so
a new connector may use only these units and plain fields. The connector subagents' prompts state this
([units source](../../../web/studio/components/flow/connector/native/NativeConnectorUnit.tsx)).

## Connector subagents

When a requirement has no released connector or operation, the agent contributes it upstream instead of
working around it. Two subagent kinds, both background subagents with their own context
([subagents](subagents.md)):

| Kind | Purpose | Effort | Model-call limit | Time limit | Tools | Workspace |
|---|---|---|---|---|---|---|
| `connector_select` | Choose connectors for the requirements | `medium`, fixed | 25 | 10 minutes | `bash`, `read`, `grep`, `glob`, `connector_catalog`; no edit, write or platform readers | Application worktree, which it does not change |
| `connector_develop` | Build one missing connector or operation, or fix a defect of an existing one, for a draft pull request | `agent.subagentConnectorDevelopEffort`, default `high`; `set_effort` | 80 | 45 minutes | `bash`, `read`, `edit`, `write`, `grep`, `glob`, `connector_catalog`, `set_effort` | A worktree of the connectors library on the connector's branch |

How they fit the requirements flow:

1. For a new application or large change the main agent takes the card's
   connections from the catalog matches the first segment lists
   ([above](#catalog-matches-of-the-first-request)) and calls `connector_catalog` only for a need
   they do not cover. It starts `connector_select` with the user's requirements before the card only
   when several connectors could meet a need or none does.
2. The main agent puts those connectors, or the `connector_select` report, into the `connections` of
   the requirements card and, when several choices exist, asks with `request_user_input` first
   ([session](session.md#requirements-cards)).
3. Once the card is recorded, the main agent starts one `connector_develop` per gap, in parallel up to the
   subagent limits, and keeps working on the rest of the application.
4. When a develop subagent completes, its notification carries the contribution (the module, the
   branch and its head commit), the Session Service submits it to the connector's
   `ConnectorContributionFlow`, which opens the draft pull request ([Contributions](#contributions)),
   and the main agent uses the connector in Preview ([below](#using-a-connector-before-its-release)).

A defect found in an existing connector follows the same path without a new requirements card: the main
agent explains the defect in business terms and asks with `request_user_input` whether to submit a fix,
since the fix opens an upstream pull request. After the user agrees it starts one `connector_develop`
with the contribution `fix`.

### `connector_select`

- **Prompt.** A dedicated system prompt shared by every Session, so it is cached: how to use the
  catalog, what a manifest and an operation mean, the authentication types, the Studio units, and the
  release rules of the library.
- **Tools.** Read-only Sandbox tools, `connector_catalog` and module source reading (`go mod download`,
  then `read` and `grep` on the module cache). It edits no file.
- **Report** (fixed structure, at most 24 KiB). One block per requirement, in the order of the
  requirements:
  - `need`: the requirement in business terms;
  - `recommended`: connector ID, module, version, kind and operation names, and the authentication
    method;
  - `alternatives`: other connectors or operations that could serve it, with the trade-off;
  - `gaps`: for each missing capability, `new connector` or `new operation`, the provider and API
    endpoint, input and output, authentication, idempotency and uncertainty needs, and the Studio units
    it can use;
  - `user setup`: what the user prepares (an API key, an account, an OAuth application).

  The fields map one to one onto the requirements card (`need`, `connector`, `operations`, `status`
  existing or new, `user_setup`).

### `connector_develop`

- **Input.** `start_agent` for this kind names the connector `slug` (the manifest's `metadata.name`,
  `^[a-z][a-z0-9-]{1,62}$`) and the `contribution` (`new`, `operation` or `fix`); the `description`
  also titles the commit and the pull request. The call fingerprint and the Session's record carry
  both.
- **Admission** (`RouteTool`). Besides the subagent limits, a `connector_develop` call is refused
  `connector_contribution_running` while another unfinished develop subagent works on the same slug,
  and `connector_contribution_limit_reached` for a 17th distinct slug of the Session, counted over
  `AgentState.ConnectorSlugs`, which keeps every slug an admitted develop call named.
- **Workspace.** One full clone of the library per Sandbox inside the workspace's Git directory,
  `/workspace/.git/superverse/connectors/library`, where the application's Git commands and the main
  agent's file tools never reach it, and one worktree per subagent,
  `/workspace/.git/superverse/connectors/worktrees/<agentID>`, which the Session's `ExecuteTool`
  creates with `start_agent` (`CreateContributionWorktree`). The branch is
  `superverse/<lane>/<projectID>/<slug>`, where the lane is the server's repository prefix (`test`,
  `staging`, `prod`, `localkind`, `localnative`), for example `superverse/test/a7k2/newsapi`, so the
  lanes that share the test environment never share a branch. The worktree checks the branch out from
  the remote branch when an earlier contribution pushed it, otherwise from `main`, which every new
  branch fetches first, so no connector is developed on a stale library. A full clone, not a partial
  one: the lazy fetches of a partial clone would run in the model's own commands, which hold no
  credential.
- **The server's library** (`connectorlibrary.Library`):

  | Server | Fetch from | Pushes to | Commits as |
  |---|---|---|---|
  | `github.connectors` set, GitHub repository host | The upstream library | The upstream library | The GitHub App's bot |
  | Local lanes (local Git host) | The local Git host's copy, refreshed from GitHub before every new branch | The local Git host's copy | The bot with an upstream, else `Superverse` |
  | No `github.connectors`, GitHub repository host | The public library, anonymously | Nowhere: the branch stays in the Sandbox's clone | `Superverse` |

  In the local lanes ([composition](composition.md#repository-host)) the Sandbox rewrites the module's
  URL to the local host, so an application's `go get <module>@<branch>` resolves locally; only the
  pull request uses GitHub.
- **Prompt.** Superverse's own skill `dex-connector-contributor`, adapted from the library's public
  contributor skill and shipped in the coding image (`/opt/superverse/dex-skills`), which the system
  prompt of the kind ends with. It carries the library's authoring rules, the Studio unit limits, the
  release and versioning rules, the pull request title rule and the rule that every new connector has
  a runnable example and mocks. Its first segment names the contribution (slug, kind, branch, base
  commit) instead of the application's repository context.
- **Scope.** One module per subagent. A new connector adds its one `catalog.yaml` entry. A change that
  needs an SDK change is not attempted; the subagent reports it as a blocker.
- **Checks.** The library's own: component validation of the module, `go test -race` and `go vet` with
  `GOWORK=off`, `connectorctl validate` and `connectorctl generate --check` on the manifest, and
  `connectorctl catalog --check` ([library rules](#the-connectors-library)). A check that needs real
  provider credentials (a live test) is recorded as unverified in the report and the pull request, never
  claimed.
- **Report.** What was built, the checks that ran and their results, and what is unverified. A develop
  subagent leaves nothing to `merge_agent_changes`.

### The contribution branch and pull request

`FinishSubagent` saves the work of every develop subagent through the contribution helper
(`git.GitOperations.FinishContribution`, one fixed helper program per operation; 5 attempts of 5
minutes for this kind), and only a COMPLETED one contributes. Each step checks its effect before it
repeats, and the receipt `.git/superverse/connectors/contributions/<agentID>.json` makes each happen
once:

1. **Commit** the worktree as one commit on its base, by the committer the library names, with the
   subject `connector(<slug>): <description>` and no trailer naming an AI tool. A retry reuses the
   saved result.
2. **Verify** that the change touches exactly one module below `connectors/` whose `connector.yaml`
   declares `metadata.name` the slug and whose path ends with it: a `new` contribution adds its
   `catalog.yaml` entry and nothing else, an `operation` or `fix` contribution changes an existing
   module and leaves `catalog.yaml` alone. A change outside the module, or no change, contributes
   nothing, and `Outcome.Refused` says why.
3. **Push** the branch without force where the library takes pushes, reading the remote branch before
   (a branch that already holds the commit is published; one that moved elsewhere is refused) and
   after, so a lost response is reconciled by reading the remote. The token is minted for the one
   operation and exists only in the environment of the helper's Git process, never in a tool result,
   log, notification or Attribute, and the model never sees it.
4. **Record the outcome**: `Outcome.Contribution` (module, kind, branch, head commit, location), which
   the `task` notification carries to the Session.

The draft pull request never blocks the subagent, the main agent's turn or Preview: the
`ConnectorContributionFlow` opens it ([below](#contributions)). Its `OpenConnectorPullRequest` is a
polling Step (`dexflow.PollUntil` within `PollingStepOptions(5 minutes)`) whose every round first looks
for the pull request by head branch (which also reconciles a lost response), pushes a branch of the
local Git host to the upstream library without force in the local lanes, and otherwise opens a draft
against `main` with the upstream installation: title `connector(<slug>): <description>`, a body with
the report's summary, the checks that ran and what is unverified. When the wait limit passes, the
contribution records `pull_request_failed` and one `connector` notification
(`connector/<slug>/pull_request_failed/<head12>`) lets the agent tell the user in business terms; a
later head of the same connector retries the pull request.

The pull request stays a draft for the maintainers, who review and release the connector; the
platform neither marks it ready nor deletes the branch.

After recording a head in the upstream library, the Flow uses a durable timer to read the
head's aggregate GitHub Actions check suites about 15 minutes after the head was recorded.
If pending or unknown, it waits about 30 more minutes and checks once more, then records the
result as still running if unresolved. A failed result records at most 12 failed job names and
a redacted 3 KiB tail from the first failed step, and sends
`connector/<slug>/checks_failed/<full-head>` to the Session. A newer head cancels the old timer
and starts its own; no workflow is started or rerun. `read_connector_checks` is a read-only
platform tool scoped by the Session's Project and slug; it reads the current GitHub result and
failed-job log tail on demand, including before a notification arrives. The remote inbox carries
the same connector notification to Codex.

### Contributions

Each contributed connector of a Project is one `ConnectorContributionFlow` execution,
`connector-contribution-<projectID>-<slug>`, for example `connector-contribution-a7k2-newsapi`, which
owns the contribution record; the Session keeps no copy, and the Snapshot reads the contributions
through the Service. The per-Flow contract is in the
[contribution README](../../../internal/dexai/agents/session/contribution/README.md).

- **Start.** The Session Service, in `ReportSubagentOutcome`, once the Session admitted the report of a
  COMPLETED develop subagent that carries a contribution, with the Start RequestID
  `flowid.Fingerprint{SessionFlowID, slug, branch, head commit}` under `dex.IDReuseDisallow` and
  same-request IgnoreError; the client confirms every submission with the RPC
  `UpdateConnectorContribution`, which a later head of the same connector also uses (idempotent by head
  commit). A contribution that ended is final for that slug in that Project.
- **Admission.** The first Step asks the Session through the Service before any effect; a deleting
  Project's Session denies it, and the Flow closes without an effect.
- **Record.** `Contribution{Module, Kind, Branch, HeadCommit, Location, PullRequest{Number, URL,
  NodeID, State}, Status, ReleasedVersion, ReleaseCandidate, Failure, Description, Report}`, at most
  about 20 KiB.

| Field | Meaning |
|---|---|
| `Module` | The module path in the library |
| `Kind` | `new` (a new connector), `operation` (a new operation of an existing connector) or `fix` (a defect fix in an existing connector) |
| `Branch`, `HeadCommit`, `Location` | The contribution branch, its latest commit, and where it was pushed: the upstream library, the local Git host's copy or only the Sandbox's clone |
| `PullRequest` | `{Number, URL, NodeID, State}`; `State` mirrors GitHub: open as draft, open and ready, merged, closed |
| `Status` | `pull_request_pending`, `unpublished` (a server without an upstream library, an explicit state, never a skip), `pull_request_failed`, `draft` or `closed` |
| `ReleasedVersion` | Reserved for the `vX.Y.Z` that contains the work; nothing sets it yet |
| `ReleaseCandidate` | The candidate of the latest head, `{Directory, Version, Tag, Commit, Status published\|failed, Failure, UpdatedAt}` ([below](#using-a-connector-before-its-release)) |
| `Checks` | The current head's `{Head, State pending\|passed\|failed, SuiteCount, Jobs, Step, LogTail, UpdatedAt}`; failure evidence is redacted before retention and cleared by a later head |

`Status` moves only forward: a contribution with a pull request never returns to a status without
one, and a later head moves `pull_request_failed` back to `pull_request_pending`, which retries the
pull request. Only the close of a deleting Project moves a contribution to `closed`.

## Using a connector before its release

Every head that a Project contributes to a connector is published as a release candidate of the
library, and the application uses that candidate in the Preview and in Live like any release
(the current contract). Maintainers still review the
draft pull request and release the connector; how an application moves from a candidate to the
release, and dependency upgrades in general, are a later design.

### Versions and tags

- **The connector's version.** The develop subagent moves the manifest's `metadata.version` after the
  connector's latest release, as the library's versioning rules say: a new connector `v0.1.0`, an
  addition (an operation, a Trigger, mocks) the next minor, a fix the next patch, a change that breaks
  existing connections the next major, `v1.0.0` from v0. A later head of the same branch keeps a
  version whose candidate was not released.
- **The candidate.** `vX.Y.Z-rc.N` under the tag `<directory>/vX.Y.Z-rc.N`, for example
  `connectors/newsapi/v0.1.0-rc.1`: `N` counts the candidates of that version, and a tag that already
  names the head is reused, so a head has at most one candidate. A candidate is a GitHub pre-release
  with the assets of a release, `connector-release.json`, its `.sha256` and
  `connector-release.complete`, uploaded last; its manifest's version is the candidate's. Tags never
  move. The public catalog lists releases only; the agent knows the module it built.

### Publication

The library's own tooling of `main` validates and builds every candidate, so a branch cannot change
how it is checked:

- **Upstream library.** `ConnectorContributionFlow`'s `PublishReleaseCandidate` sends the repository
  dispatch `connector-release-candidate` with `{directory, commit, version}`, which needs only the
  App's contents write permission. The library's Release Connector Candidate workflow, whose definition
  comes from the default branch, validates that the commit changes only the connector's module (a new
  connector also its `catalog.yaml` entry) relative to its merge base with `main` and that the version
  is after the connector's latest release, runs the checks of a release, creates the tag and the
  pre-release with its assets, and downloads the module at the candidate. It refuses a tag that names
  another commit and repeats a publication at the same commit. The Step reads the candidate before it
  requests it in every round, and polls until the release holds the metadata and the complete marker.
- **Local lanes.** Without GitHub, the develop subagent's finish builds the candidate in the Sandbox:
  the contribution helper runs the library's `connectorctl` of `main` in a worktree of `main`
  (`release-candidate`, `ui-artifact` for a Studio UI, `release-artifact`), pushes the tag to the
  local Git host's copy, and the server stores the assets in the host's release store, which serves
  them at GitHub's download path
  ([local Git host](../../../internal/comm/platform/repohost/localgit/README.md#release-assets)). The
  Flow's Step then only confirms them.
- **Refusal.** A candidate that the library's rules refuse, such as a version that is not after the
  latest release, is `release_candidate_refused` in the finish, or a failed candidate in the Flow; the
  contribution still records the head, and the agent is told to fix it.

### Using the candidate

- **Notification.** On the local Git host the subagent's task notification names the published
  candidate. Otherwise the Flow's `RecordReleaseCandidate` sends a `connector` notification:
  `connector/<slug>/release_candidate_published/<version>`, or
  `connector/<slug>/release_candidate_failed/<head12>` with the reason when the library refused it or
  did not publish it within 25 minutes.
- **Switching.** The agent runs `go get <module>@<version>`, writes the version into the connection of
  `dex-app.yaml`, and commits and pushes; the Preview deployment, the Release and the Live deployment
  then run as for a release.
- **Accepted versions.** dexcli `cli-v1.10.1`, AppManifest registration and the configuration host
  accept `vX.Y.Z-rc.N` beside `vX.Y.Z`; pseudo-versions, branches and `replace` stay refused. The
  configuration host reads official releases from the connectors library and candidates from
  `connectorlibrary.Library.ReleaseCandidateDownloadURL`: the upstream library's releases, or the
  local Git host's copy.
- **Module downloads.** Hosted, the module proxy serves the upstream library's public candidate tag.
  In the local lanes the Sandbox's tool environment and the local builder redirect the library's Git
  fetches to the copy, and the module skips the checksum database; go.sum still verifies it. The local
  builder's recipe `bp3` is `bp1` with that redirect, and the local lanes select it for new Releases.

### Fixing a candidate

A problem found later, by a delivery notification or by the user, makes the main agent start another
`connector_develop` subagent for the same slug and branch. Its head reaches the same
`ConnectorContributionFlow` through `UpdateConnectorContribution`, which starts that head's candidate,
the next `N`; a candidate of an older head is never recorded over the current one.

## GitHub App setup

The platform's existing GitHub App is extended; no new App and no fork are created. Only a GitHub
organization owner can change it.

1. **Permissions.** Pull requests (read and write) and Issues (read and write) beside Contents write.
   No Workflows permission.
2. **Installation.** The App is installed on the `superdurable` organization for exactly
   `superversev2` (platform issues), `dex-connectors-library` (branches and pull requests) and
   `dex-connectors-library-test`, a copy of the library for the integration tests, so tests never open
   pull requests in the real library.
3. **Settings.** The server configuration keys `github.connectors.repository` and
   `github.connectors.installationId` name the upstream library and its installation, both or neither
   ([server configuration](../../../cmd/superverse-server/config/README.md#github)); without them a
   contribution stays `unpublished`. A further `githubapp.App` installation serves the upstream; the
   repository host's client stays bound to the installation of the managed application repositories.
   The manual GitHub integration test points at the test repository.
4. **Tokens.** Every operation mints its own token with `githubapp.App.OperationToken`, restricted to
   the one repository and to the permission it needs: contents write for a push, pull requests write
   for the pull request calls, metadata read for the bot's user. No token is cached, logged, stored or
   put in an error.

Accepted risk: a repository-scoped token cannot be limited to one branch, so in theory it can write other
branches. `main` is protected by organization rules (pull request required, no force push, no deletion),
the token appears only in the platform's Git process, and every merge is reviewed by a maintainer.

## Connector mocks

Generated applications are tested against connector mocks, never real providers
([commit gate and application tests](commit-gate-and-app-tests.md)). The mock contract is a library
feature, released with the connectors.

- **`sdkgo/connectormock`.** `Query[IN,OUT]` and `Mutation[IN,OUT]` implement the real SDK interfaces with
  the real definitions, so undeclared branches still fail.
  - Scripting: `Respond(cases...)` in order, `When(func(IN) bool, case)`, `ForFlow(idOrPrefix)`,
    `Default(cases...)` and `Strict()`.
  - Cases: `Branch(id, value, failure)`, `Retry(failure, after)`, `Uncertain(value, failure)` and
    `Paged(page func(IN) int, pages...)`.
  - A repeated idempotency key returns the first result without consuming a case. `Calls()` records
    input, call ID, key, branch and Flow ID, and a process-wide recorder feeds branch coverage. An
    unscripted call with no default returns `defect` and fails the test.
- **Generated seam.** `connectorctl generate` emits an `Operations` interface with one accessor per
  operation, an adapter over the real client, and `NewConnectionWithOperations(Operations, ref)`;
  `NewConnection(*Client, ref)` is unchanged.
- **Generated `<module>/<pkg>mock`** (for example `llm/llmmock`): `New(t, connectionName)`,
  a scripting accessor per operation, a constructor per branch (`GenerateTextProviderRejected(...)`), one
  per manifest use case, and the manifest default.
- **Manifest.** `pagination: {inputField, nextField}` and `mocks: [{name, branch, description, output |
  failure | retry | uncertain, default}]`.
- **Validation.** `connectorctl validate` requires at least one mock per declared branch, exactly one
  default per query, a paged default of at least three pages (unordered, with boundary and filtered items,
  when the provider documents no order), outputs that decode strictly and pass the connector's own
  validation, and an `uncertain` case for a mutation that declares the branch. `connectorctl mocks
  scaffold` writes the failure fixtures.
- **Ownership.** Connector maintainers write the mocks; the agent only scripts them. A connector built by
  `connector_develop` ships with its mocks. Mocks release with the connector: adding one is a minor
  change, changing its data a patch, renaming or removing one a breaking change.
- **Waiver.** The commit gate counts a connector as mocked when its module has a `<name>mock` package;
  a connection of a connector without mocks does not block coverage and is listed as uncovered in the
  gate's report ([commit gate](commit-gate-and-app-tests.md#stage-4-checks)).
- **Rollout.** `sdkgo` v0.24.0 ships `connectormock`, and llm, github and gmail ship their generated
  mock packages; validation is enabled connector by connector until all have mocks, and each connector
  releases its mocks on its own version. Each library merge is a release and is confirmed with the
  owner first. Superverse's template and server require `sdkgo` v0.26.0, which includes `connectormock`;
  a connector's mock package brings the SDK release it needs.

## Dex Flow Changes

### SessionAgentFlow (parts owned here)

The Flow's full contract is in [session](session.md#sessionagentflow-contract). This topic adds no
StartFlow, FlowID, Step, AttributeMap, Channel or Stream to the Session:

- **Attributes.** `AgentState.ConnectorSlugs`, every slug an admitted `connector_develop` call named
  (at most 16), which `RouteTool` counts for the 17th-slug refusal. The Session keeps no contribution
  record: the Snapshot reads the contributions of the Project through the Service, from their Flows.
- **Steps.** `RouteTool` admits `start_agent` of kind `connector_develop` (the slug and limit checks
  above); `ExecuteTool` creates the library worktree on the connector's branch instead of an
  application worktree. `SettleForegroundCommand` settles only bash calls and builds their
  Sandbox context without resolving a file root, so recovery loads neither worktree AttributeMap;
  this changes no Session primitive.
- **Channels.** `connector` notifications (`connector/<slug>/<event>/<head12 or version>`) reach the Session on
  `SessionNotifications` through `ReportSessionNotification`, like every notification
  ([notifications](notifications-and-feedback.md)), and open automatic turns within `agent.taskTurns`.

### ConnectorContributionFlow

The per-Flow contract is owned by the [contribution README](../../../internal/dexai/agents/session/contribution/README.md);
its role is described under [Contributions](#contributions).

- **Identity.** FlowID `connector-contribution-<projectID>-<slug>` (`flowid.ConnectorContributionFlowID`),
  one execution per Project and connector, started by the Session Service with
  `dex.IDReuseDisallow`, same-request IgnoreError and the RequestID
  `flowid.Fingerprint{SessionFlowID, slug, branch, head commit}`; every submission is confirmed by the
  RPC `UpdateConnectorContribution`, which a closed execution refuses. An independent top-level Flow,
  not a Project child; the deleting Project's cleanup finds the open ones by FlowType, `ProjectID` and
  status and closes them.
- **Attributes.** `Identity`, `ProjectID` (indexed `keyword2`), `ContributionStatus` (indexed
  `keyword3`, the status lock Attribute), `Submission`, `Contribution` and Boolean `Closing`, the authority of the
  contribution. No AttributeMap. Channel `ContributionEnd`, which only `CloseConnectorContribution`
  publishes. No Stream.
- **Steps and RPCs.** `AdmitConnectorContribution`, `AwaitConnectorContributionEnd`,
  `OpenConnectorPullRequest`, `RecordConnectorPullRequest`, `WaitConnectorChecks` (durable
  `WaitFor` timers at about 15 and 45 minutes, at most two short reads),
  `RecordConnectorChecks`, `PublishReleaseCandidate` (a polling Step
  within `PollingStepOptions(25 minutes)`, no lock), `RecordReleaseCandidate`, `ReportConnectorEvent`
  and `DropConnectorEvent`. RPCs `UpdateConnectorContribution`, `CloseConnectorContribution`,
  `GetConnectorContributionSnapshot`, `GetDexSummary` and `GetDexDisplay`. The graph is
  `docs/flows/sv2-connector-contribution-flow.json`.
- **External effects.** The push of a local branch to GitHub (no force; a branch that already holds the
  commit is published); the pull request, found by its head branch before it is created, so a lost
  answer, a Worker restart or a retry opens no second one; and the release candidate, read before it is
  requested, whose tag names one commit only. GitHub check reads add no Actions usage.

### SubagentFlow (connector kinds)

The Flow is owned by [subagents](subagents.md); this section states the connector differences.

- **Identity.** `subagent-<projectID>-<agentID>` as for every subagent, with the kind, slug and
  contribution inside the call fingerprint of the Start RequestID. The kinds are values of `Config`,
  so no Flow type or Step type is added.
- **Attributes.** `Start.Call.Connector` and `Start.Branch` name the contribution; `Config` carries the
  kind's effort, model-call and time limits; `Outcome.Contribution` holds what a completed develop
  subagent contributed and `Outcome.Refused` why it contributed nothing. `ConnectorCheckFailures`
  is an integer Attribute, initially absent and read as zero, incremented once per failed finish
  check, and retained until this subagent Flow ends.
- **Steps.** `FinishSubagent` of `connector_develop` saves the work through the contribution helper
  ([the contribution branch](#the-contribution-branch-and-pull-request)). The helper runs the
  library's selected connector checks before branch movement or push. On failure it keeps the
  worktree editable and writes no finish receipt; `ResumeConnectorAfterCheckFailure` appends the
  bounded output as a user input, returns the model to its loop and rearms the original deadline.
  The third check failure ends the task FAILED, saves its work without contributing and reports the
  last failure to the parent. A retry or Worker restart after a passed gate reuses the same commit
  and reconciles the push by reading the remote.
- **Cleanup.** The worktree is removed as for every subagent; the library clone stays with the Sandbox
  volume. A deleting Project stops its subagents; a branch pushed earlier stays for its pull request.

No other Flow changes. `ProjectFlow`, `PreviewFlow` and the delivery Flows treat a release candidate as
any release.

### Pre-push connector check (CONNGATE)

The shared `FinishContribution` Sandbox helper checks the staged connector with the library's
`catalog-check`, `studio-bundle-theme`, `react-connectors`, `test-dex-compat-latest-go-sdk` and
`test-dex-compat-current` targets, selecting exactly the changed module from the base and staged
commit. It fetches official release tags through public HTTPS first, because the local Git host's
copy contains `main` without tags and the library's dependency-release validator needs them.
In place of `test-connectors`, the gate runs the same dependency-release validator, module tests,
vet, manifest validation and generation-drift commands; only `go test -race` becomes `go test`: the coding
image has no C compiler and CGO is disabled. Race detection runs only in the library CI; CONNCI
notifies the agent of a CI failure. The current-Dex target also checks each runnable
example's Flow layout and Dex compatibility. Each target has a deadline; the current-Dex target's
deadline is 30 minutes. Only a passed gate may move or push the branch and request a release
candidate. A failure returns the last 4 KiB of output and preserves the worktree for repair. Go
module, Go build and npm caches live in the Sandbox workspace volume; the library's current-Dex
script redownloads Dex CLI and Dex Web on every run. Its disposable source copies use the coding
profile's 2 GiB `/tmp` volume, outside the application repository; the real Sandbox test verifies
that bound with the current Stripe checks. Provider live tests still require their own
credentialed acceptance.

**Per-Flow contract delta.** The unchanged complete `SessionAgentFlow` contract is in
[session](session.md#dex-flow-changes) and the remote contribution fields and key rules are in
[remote executor](../remote-agent/executor.md#connector-contribution-contract). Its FlowID
`session-<projectID>` (for example `session-project-42`), Project-readiness trigger, Start
RequestID, Run reuse, business and engine completion, retention, independent top-level relation,
Attribute/AttributeMap keys and ownership are unchanged. No new Attribute, AttributeMap, Channel,
ChannelMap, Stream, index or secret is added there. Remote `submit_connector_contribution` still
runs in `ExecuteTool` under its admitted workspace lease without a normal-path Flow read or RPC;
a failed check returns `connector_check_failed` to the current call, keeps its slug's
`ConnectorContributions` record OPEN and its worktree editable, and starts no contribution Flow.
The same call can retry after repair; a lost successful answer still reconciles its finish receipt
and pushed head before the existing `SubmitConnectorContribution` dependency call. The existing
tool deadline grows to 70 minutes to contain the library checks, and cancellation keeps the
existing Sandbox process-group behavior. The remote endpoint admits the report within 15 seconds;
the Bridge answers `submit_connector_contribution` after admission instead of holding its MCP call
past the 1,770-second relay wait or 1,800-second Codex timeout, and delivers the eventual result as a
late-result notification. No change is made to the Session's existing Channel
admission, ordering, stale-generation, pending-message cleanup or best-effort Stream recovery.

The unchanged complete `SubagentFlow` contract is in [subagents](subagents.md#dex-flow-changes)
and its [package README](../../../internal/dexai/agents/session/subagent/README.md). Its FlowID
`subagent-<projectID>-<agentID>` (for example `subagent-a7k2-k3n8q5r1`), call-fingerprint Start
RequestID, nonreuse, independent top-level relation, business and engine completion and retention
remain as documented. `ConnectorCheckFailures` is the sole new durable primitive; no new map,
map key, index, Channel, ChannelMap or Stream is introduced. `FinishSubagent` runs the Sandbox gate
without a status or message lock and sends a failure to the new
`ResumeConnectorAfterCheckFailure` Step. That Step locks `SubagentStatus` and
`MessageWindowLock`, increments the counter, writes a new `CurrentMessages` user entry using the
existing sequence key and archive rules, and resumes `CallSubagentModel` and the original deadline
unless Stop, expiry or the third failure ends the task. The new input is bounded and contains no
repository token. The existing `StopSubagent` RPC records a Stop requested during finishing;
the resume Step honors it. A successful finish retains its query-before-push receipt recovery;
failed checks write no finish receipt, and a final failed task saves its work off-branch. Existing
open executions read the absent counter as zero and retain their Flow and prior Step identities.
Tests cover gate refusal, repair, retry bound, remote tool delivery, local Git branch identity
and the real library targets; no external effect is inferred from a missing answer.

## Database Schema Changes

None. A contribution record is an Attribute of its `ConnectorContributionFlow`; a release candidate's
assets are a GitHub pre-release of the library, or files of the local Git host's release store; the
catalog and pull requests are read from their public owners and cached for minutes only, the catalog
as one in-memory copy per server replica and one per Sandbox Runner.
The new CI result belongs to that same Attribute; there is no table or migration.

## UI/UX

- The requirements card lists each connection as existing or new. A new one reads "needs a new connector,
  built and reviewed upstream" with what the user must prepare.
- The tasks panel shows connector subagents like any other, with their one-line status.
- Connector notifications (`pull_request_failed`, `release_candidate_published`,
  `release_candidate_failed` and `checks_failed`) appear as notices in the conversation
  ([notifications](notifications-and-feedback.md#uiux)).
- The Session Snapshot lists the contributions (`ConnectorContribution`: slug, module, kind, status,
  branch, head commit, location, pull request number and link, failure, and the release candidate's
  version, status and failure).
- `read_connector_checks` lets the agent refresh the result for its own contributed head; the
  failure notice labels the excerpt as untrusted CI output and names the failed jobs.
- Connection forms for a release candidate work as for a release, and show its version.
- Accessibility, keyboard behavior, cancellation and stale-response protection are unchanged. The Studio
  mock lists no contribution, so it shows none of these fields.

## Tests

Agent paths are Dex integration tests with the mock model
([context management](context-management.md#mock-model-for-tests)); Dex, Kind or Docker and the Sandbox
are real. End-to-end coverage stays at the three happy paths. The GitHub path of a contribution runs in
the manual `TestConnectorContributionGitHubIntegration` against `superdurable/dex-connectors-library-test`;
missing credentials or the test repository make the result incomplete, never a pass.
The read-only `TestConnectorHeadChecksGitHubIntegration` checks an existing passing and failing
head in the official public library without triggering a workflow. Dex integration tests exercise
the production CI Steps and cancellation on a newer head and Project close with test RPCs that
install observed results; Session tests drive the mock notification and read tool and the remote
HTTP endpoint's tool and inbox delivery. The local Git host has no Actions and supplies no CI result.

- **Catalog.** A query returns bounded compact matches; an exact operation filters; an oversized single
  match fails with `catalog_result_too_large`; an unreachable catalog is `catalog_unavailable`.
- **Selection.** The report has the fixed structure for an existing connector and for a gap; the main agent
  puts it on the requirements card.
- **Catalog matches.** `TestSessionMockCatalogPrefetchDexIntegration` (scenario `catalog-prefetch`):
  a request that names Stripe and Slack opens the first segment with both in `<catalog_matches>`,
  read from the server's copy of the real published catalog, and the card is recorded from them
  with no `connector_select` or `connector_catalog` call while the workspace request is in flight.
- **Contribution.**
  - The contribution helper in a real coding container creates the library worktree. A Stripe example
    with two Flow sources fails the library check before push; after repair, finish and replay push
    the accepted head once. An unreachable remote is transient.
  - The remote HTTP Dex integration test reports the failed check to Codex, preserves its editable
    worktree and OPEN contribution, and submits the repaired head. The platform mock scenario drives
    three failed finish checks and verifies a FAILED task with no contribution or pushed branch.
  - `ConnectorContributionFlow` on the default lane (no upstream library) records a submission
    `unpublished`, joins a repeated submission, applies a later head and closes with the Project's
    deletion, after which every submission is refused.
  - Against the GitHub test repository: the branch name, the bot as Author and Committer, no AI
    trailer, the `connector(<slug>):` subject, one draft pull request against `main` after a repeated
    opening, and no token in the clone.
  - A subagent that fails or times out creates no contribution; a 17th slug is refused; two develop
    subagents use separate worktrees of one clone.
- **Release candidates.**
  - `TestSessionMockConnectorReleaseCandidateDexIntegration` (scenario `connector-release-candidate`) on
    the default lane: the develop subagent builds a complete new connector; its finish publishes the
    head's candidate on the local Git host's copy with the library's tooling; the contribution records
    it published without a second notification; the main agent switches the application to it and
    pushes; the Preview deployment asks only for the connection; once configured, the Preview serves
    the commit, and a Release built by the local builder serves in Live.
  - `TestPublishedReleaseCandidateManifestAdmission` resolves a real candidate of the GitHub test copy
    and admits an AppManifest that declares it; a pseudo-version stays refused.
  - The library's Release Connector Candidate workflow, run in the test copy, publishes a candidate,
    repeats a publication at the same commit, and refuses a change outside the module and a version
    that is not after the latest release.
- **Mock scenarios** (each with ordered inputs and a Dex integration test that runs it):
  `connector-select-existing` (describe; the card follows and the agent goes on),
  `connector-select-choice` (describe, choose among candidate connectors; the card follows),
  `connector-develop-new`, `connector-develop-operation` (describe; the card, then the branch and
  contribution), `connector-develop-fix` (describe the defect, agree to submit a
  fix, branch and contribution) and `connector-pr-retry` (pull request creation fails until
  `pull_request_failed`), `connector-ci-failure` (CI notice, then read-only status read), and
  `connector-release-candidate` (describe; the card, then a complete new
  connector whose candidate the application uses).
- **Release resolver.** A real released connector resolves, caches and survives eviction; a missing asset,
  a wrong digest and a mismatching identity are `CONNECTOR_RELEASE_UNAVAILABLE`.

Not verified without the real library: that maintainers merge and release, and a hosted contribution's
candidate end to end on AWS, from the dispatch to the module proxy's download in the Live build. Live
provider behavior of a new connector is recorded as unverified.

## Documentation

- Skills: `dex-connector-contributor` (Superverse-owned, in `dex-skills/`, shipped in the coding image);
  `dex-app-builder` guidance on confirming requirements and contributing a missing connector;
  each example's `AGENTS.md` (`app-template/examples/<id>/`) and `dex-skills` read module source through `go list -m -f '{{.Dir}}'`
  (`make generate-app-template` after changing the template).
- Package READMEs: [contribution](../../../internal/dexai/agents/session/contribution/README.md) (the Flow),
  [connectorlibrary](../../../internal/comm/platform/connectorlibrary/README.md) (the server's library and the
  upstream client), the [workspace Git package](../../../internal/dexai/agents/sandbox/workspace/git/README.md)
  (the contribution helper) and the [local Git host](../../../internal/comm/platform/repohost/localgit/README.md)
  (the library's copy).
- `docs/api/resource-grpc.md` documents the contributions in the Snapshot, with their release
  candidates. the current contract records the release
  candidates; the [local build service](../../../internal/dexai/delivery/releases/build/localbuilder/README.md#release-candidates)
  owns the `bp3` redirect.

## Resource Scalability

CONNCI adds no cloud, Kubernetes, Temporal namespace, Dex Flow, or database resource. Each current head adds one bounded timer/check Step within the existing contribution Flow; a newer head or Project close cancels it. The result and Step history follow that Flow's existing retention. Each head incurs at most two scheduled aggregate GitHub reads, about 15 minutes and 45 minutes after it was recorded, with no recurring per-round cost or tenant-wide scan. A rate limit consumes a check point as unknown without a retry loop. GitHub's installation REST rate limit has a 5,000 requests/hour floor and 12,500/hour cap ([GitHub documentation](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api), checked 2026-10-16). A simultaneous burst above roughly 2,500 heads at one timer point would consume that floor; admission shaping or installation sharding is required before that scale. Normal 10,000-Project and 100,000-deployment growth creates no per-Project recurring CI read. The failure path reads at most four check-run pages, one job and 512 KiB of one log per failed head; agent-initiated on-demand reads consume additional calls only when invoked. The CONNGATE pre-push gate runs inside the existing
Sandbox and its existing Session or subagent Flow. One connector worktree and one cache tree grow on
the workspace volume per active Session; the cache is shared by its connector contributions and is
deleted with that Sandbox volume at Project deletion. The worktree is removed after an accepted
finish or failed subagent outcome. Go module, build and npm caches accumulate downloads and build
artifacts meanwhile, bounded by the workspace disk quota and the existing low-disk admission check;
the gate creates no per-request cloud resource. Concurrent gates are bounded by the Session's
existing tool concurrency and subagent limit. No cloud control-plane call or tenant scan is made
per gate. Existing Sandbox, Flow, Git and storage quotas and the 10,000-Project / 100,000-deployment
capacity target are unchanged; their limits and dated sources remain in the owning architecture
documents.
