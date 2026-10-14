# Build, test, and handoff

For write/read boundaries, follow the shared [SDK consistency matrix](../../dex-sdk/references/core/read-after-write.md). Assert direct-state RPC reads on the first read; confirm triggered business work separately. Do not add callbacks or polling to strong write/read pairs.

## Implementation loop

Use the template's commands. Change `openapi/openapi.yaml`, then regenerate
with `make generate`; never hand-edit generated Go or TypeScript clients. The
generated directories (`internal/api/generated`, `web/src/api/generated`) exist
only as ignored local build outputs. Never stage or commit them.

The template starts with the reference `TextSummaryFlow`. When the request does
not need text summarization, remove it with its worker registration,
`summary-model` connection, text-summary endpoints and form, as `AGENTS.md`
lists, rather than keeping it beside the requested Flow. Preserve unrelated
business Flows in imported or already-developed applications.

Read `.superverse/template.json` for the exact command surface. In a fresh
checkout, restore its locked dependencies through the declared bootstrap
command before generation or checks (normally `make bootstrap`, which runs
`go mod download`, `go -C tools/openapi mod download` and
`npm --prefix web ci`). A missing generator executable requires dependency
restoration, not repeated failing builds or a package upgrade. Reuse the
installed dependencies until their manifests or lockfiles change.

Generate ignored API packages before resolving their imports with `go mod tidy`.
`commit_and_push` runs the declared `checkStatic` source gate
(`make check-static`): it regenerates the Go and TypeScript contracts,
validates every strict FDG 2.0 definition (`make check-fdg-v2`), checks gofmt,
`go mod tidy -diff` and `go vet` (also with `-tags apptest`), and builds the
production backend and frontend. Then the platform renders every Flow and runs
the declared `testApplication` (`make test-app`) against a fresh test engine
([API tests](api-tests.md)). Use focused checks during editing and do not repeat that full gate
immediately before calling it. Do not invent a removed test target or hand-edit
generated outputs.

A source handoff and real business acceptance are separate milestones. Every
application tests each Flow through its API in `internal/apptest` with connector
mocks ([API tests](api-tests.md)); it adds no browser test suites, fixtures or
other test-framework dependencies. The source gate covers generation, strict
FDG, formatting/modules/vet, types, production builds and those tests. Preserve
unrelated tests in imported applications.

The coding sandbox hands off clean, pushed, source-verified code for Studio
Configuration and Preview without a private Connector key or isolated test
stack. Missing configuration does not make otherwise complete source blocked.
Record real execution as pending: the user configures connections and runs
Preview or Live in Studio. Never call compilation a provider or deployment
success. An explicitly requested executed test remains incomplete until its
real dependencies pass. Repair actual source-gate failures before handoff.

Keep the template's generators, Make targets and Go toolchain. Dependency
versions follow the [version policy](workspace-bootstrap.md). Released connector
modules declare the minimum Connector SDK and Dex SDK they need, and Go builds
with the highest version any module requires. If moving the Dex Go SDK forward
makes the Connector SDK or a connector fail to compile, keep the newest Dex SDK
that compiles and report the incompatibility instead of patching module source.

For **No custom UI**, reduce OpenAPI to `getHealth` (the `healthPath` in
`dex-app.yaml`) plus confirmed integration ingress, remove the reference
text-summary endpoints and form, keep a minimal page without business or
management controls, and regenerate both clients.

For **Custom UI**, implement the requested pages, fields and actions directly.
Only when a consequential interaction choice remains unresolved, use the
optional static checkpoint in
[UI workflow](ui-workflow.md-resolve-material-interaction-choices); the user
opens the pushed pages in Studio Preview, not through a sandbox dev server. Do
not introduce a mock server, model lifecycle states, generate visual assets, or
polish the surface before the page inventory is confirmed.
The confirmed discovery decision must name the Studio capability gap that
requires each custom management surface. Keep management operations covered by
Summary RPC, Display RPC, Action RPC, Indexed Attributes, Inbox, editable
fields, timeline, or graph inspection out of the custom backend and UI.

During Flow and Connector design, finalize `openapi/openapi.yaml`, run
`make generate`, and implement the generated Go server interfaces before wiring
the generated TypeScript client into the UI. Keep visual polish until a real Dex
and Connector journey passes in Studio Preview. The API tests use the
connectors' mock packages and nothing else: no component mocks, fake providers
or intercepted API responses. Trigger cases through the actual API, or record
the unverified invariant. Check both production artifacts
after an API change; a successful Go compile does not prove that the frontend's
generated imports still exist. Run relevant source checks after each edit batch
and keep the requested real acceptance result separate from source readiness.

Before installing a database, cache, ORM, or separate read model, review the
storage decision matrix and name the exact query, concurrency, transaction, or
analytics requirement that Dex Attributes, AttributeMaps, indexes/search,
typed RPCs, partitioning/chunking, blob storage, and Attribute Store projection
cannot satisfy. Remove an unneeded dependency when Dex meets the requirement.
When external storage is justified, test its declared authority boundary,
projection/synchronization lag, retry behavior, outage handling, and
reconciliation instead of treating a successful happy-path write as proof.

Only actual dependency calls establish Dex durability, Worker replacement,
Timer, RPC, retry, provider or application E2E behavior.

The user follows the work in Superverse Studio: Build Design renders the
definitions prepared from the pushed commit, Configuration holds connections
and credentials, Preview runs the application and its Worker on the Sandbox's
Preview engine, and Runs and Inbox show Preview or Live. That engine at
127.0.0.1:8801 is the only Dex in the Sandbox: never run `dexcli dev` or
`temporal server start-dev`. See the application run through the Preview, and
read its Flows and logs as the
[local environment](../../dex-sdk/references/core/getting-started.md#local-environment)
describes.

## Local CLI

The template's FDG check runs the `dexcli` on `PATH` (or `$DEXCLI`). The coding
sandbox provides it, and Superverse runs the same `dexcli visualize` rendering
when it prepares Preview and Live definitions. Do not install another CLI.

Validate every Flow file listed in `dex-app.yaml`; see [FDG focused verification](fdg-authoring.md#focused-verification).

## Durable verification

This section guides requested runtime acceptance, not a requirement to generate
test scaffolding in every application. Use the real Studio Preview and Live
path. Use a real Dex Server when behavior crosses a Client, Worker, wait, RPC,
Channel, Timer, Stream, retry, provider, or process boundary.

Cover:

- start and typed terminal output;
- duplicate start/request behavior and [StartFlow-first ordering](../../dex-sdk/references/core/testing.md#startflow-ordering-and-retry-identity), with reconciliation reads confined to relevant error branches;
- durable wait and Worker replacement;
- accepted-start responses without defensive identity/Attribute rereads, and [durable downstream start recovery](../../dex-sdk/references/core/testing.md#downstream-start-recovery) with no dependent API-side second start;
- Action eligibility, valid action, duplicate/late action, and terminal rejection;
- role-to-permission mapping, unauthorized Action rejection, and multi-permission work discovery at the application boundary;
- concurrent Action-source writes without projection-only locks and cumulative permission history after state changes and completion;
- retry and exhausted-recovery behavior;
- provider idempotency and unknown-outcome reconciliation;
- summary/display reads before, during, and after terminal completion;
- terminal-readable entity snapshots through typed `Get*` RPCs after closure, following the SDK's [terminal entity read scenario](../../dex-sdk/references/core/testing.md#terminal-entity-reads) and [terminal read RPC rule](../../dex-sdk/references/core/error-handling.md#terminal-read-rpc-rule);
- connector Trigger Flow/RPC routing and correlation when used;
- RPC-to-Connector-Step routing when an application RPC requests provider work.

For a No custom UI application, also assert that no approval, display, status,
list, search, detail, retry, escalation, Action-proxy, or Attribute-proxy HTTP
route remains. Test each retained webhook independently from Studio management.

Use deadline-based polling and report Flow IDs and status on failure. Do not hide or skip a failing check.

## Handoff

Ensure:

- `.superverse/template.json` remains valid;
- application commands still match the template, and `go.mod`, `go.sum` and
  `dex-app.yaml` agree on every connector version;
- every top-level Flow is justified by its authoritative owner,
  retention/cleanup, independent waits or Timers, and terminal lifecycle;
- no two Flows exchange RPCs or messages on frequent events, such as every
  operation, tool call or poll;
- parallel work remains in Steps by default, and every SubFlow has concrete
  evolution evidence, rejected Step/batching/RPC alternatives, a defined
  parent-child lifecycle, and explicit user confirmation, except that a
  SubFlow isolating a long poll beside other work needs only the lifecycle and
  the confirmation;
- no long-running Execute, such as a long poll, runs beside other ongoing
  work in the same Flow;
- temporary validation, failure, and expiry state cannot pollute a long-lived
  authoritative Attribute or AttributeMap owner;
- the authorization model starts with `admin`, keeps Action permissions
  granular, and justifies every additional role with a distinct authenticated
  membership and visibility or operation boundary;
- the confirmed UI mode is recorded;
- the management UI capability mapping was completed before the UI-mode
  decision and designs Summary, Display, Action, Indexed Attribute, Inbox,
  and permission coverage;
- every Custom UI surface names a specific remaining Studio capability gap;
- a No custom UI shell contains no business controls or management routes;
- a Custom UI has a requested or confirmed page inventory, navigation, fields,
  and actions before backend work, with no early visual-polish artifacts;
- its OpenAPI contract was designed with the Flow and Connector boundaries,
  both generated clients and production bundles reproduce locally, neither generated directory is
  tracked, and the Go HTTP boundary implements the generated server interfaces;
- its dynamic UI uses only the generated TypeScript client, and visual polish
  followed a passing real Dex and
  Connector end-to-end journey;
- secrets are absent from files, logs, generated values, and archives;
- dependencies and connectors use exact released versions, or a connector release
  candidate the platform published (no replace or pseudo-versions);
- every connector capability, by exact kind and name, comes from the published
  catalog through `connector_catalog` and was confirmed in that exact release's
  `connector.yaml` and generated factory;
- every public-provider boundary reuses an available released connector
  capability or records the missing capability as a blocker;
- every internal-service boundary uses a released official connector
  capability or records the missing capability as a blocker;
- each configurable Connector Step has a static connection name matching its generated Connection;
- each Connector Step branch receives only its current operation result, while application Attributes retain domain context;
- every Connector factory uses pure `MapToOperationInput` and graph-only `Annotations`;
- each Connector Trigger binding has a static binding name, application-owned Flow ID resolver, and typed target;
- `dex-app.yaml` lists every Release Flow source and static connector
  connection, including its exact published `modulePath`, without configuration
  values or secrets; local source validators and generated artifacts preserve
  the same deployment schema;
- `make check-fdg-v2` passes: every declared Flow renders as strict FDG 2.0 and
  every connector version matches `go.mod`. Publishing renders the same
  definitions and validates `dex-app.yaml` again from the committed source;
- Live Publishing accepts an eligible main-branch commit regardless of author,
  prepares its exact FDG/manifest, configures that source and builds the whole
  application with a frozen configuration reference before deployment; a Flow
  Type is never the deployment unit. Preview uses the current clean pushed
  Sandbox source through Build Configuration and Preview controls;
- a Live deployment uses the configuration frozen into its build, with an exact
  object version and digest, and fails closed when any identity mismatches;
- hosted bootstrap uses the official `projectconfig.LoadFromEnvironment` with
  trusted `DEX_PROJECT_*` scope, canonical key, exact version/digest and scoped
  AWS identity; it never substitutes latest, a mounted configuration file or
  a credential broker. Resolve/apply application environment before Workers
  start, and keep provider tokens entirely inside the official SDK boundary;
- requested acceptance for known token expiry, concurrent calls, credential rotation,
  lost exchange responses and Worker restart records actual results or explicit
  gaps, without recreating the released Connector's test suite in the app;
  retries join an admitted exchange and only an immutable result authorizes
  recovery. Unknown expiry or an unclassified 401 never triggers refresh;
- any missing connector capability or release blocker is explicit;
- no `go.work`, local `replace`, branch, pseudo-version, or commit SHA remains
  in the production dependency graph;
- the repository has a clean, reviewable commit pushed by `commit_and_push`;
- limitations and unimplemented integrations are explicit.

Live deployment is a Studio Publishing operation. Report only observed
identities, such as the source commit, Release, configuration, deployment and
real E2E result. Never substitute local mocks or an image-only build for those
identities, and never invent a command, URL, or success result.
