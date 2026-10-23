# Superverse V2 Studio

Studio is the single Next.js browser application for Projects, coding Sessions,
Preview, Releases and Live deployments. Its BFF calls the merged Go server's
business gRPC services defined in `api/superverse/v2/superverse.proto`
([the current contract](../../docs/api/resource-grpc.md)).

Home (`/`) lists the account's Projects; an account without one sees the start
form there, which also lives at `/projects/start`. An account without a Teamspace gets one,
named from its email. The start form asks what process Dex should run: the description is the
first Agent message, a template from the gallery fills it in, and the project name (derived from
the description) sits under Advanced. Agent and model selection use the composer's
shared authenticated provider catalog and key/endpoint controls. Provider keys stay
in client memory until the accepted Project admits them, before its first message;
failed admission leaves an editable draft, and reload requires re-entry. Mock and
local-agent creation retain their existing paths. A new Project opens on its
conversation alone and switches once to the Builder when the agent's first requirements card
arrives. Under the composer a grid of starting points lists the built examples on every model, from
the catalog Studio bundles ([`lib/examples`](lib/examples/README.md), the same index the server
embeds): a few of each category before the person types, the six closest to the description while
they type, ranked by the search the server shortlists with and re-ranked at word boundaries, and
Browse all examples to search and filter the whole catalog. Build with no example opened sends the
description. When Mock carries it in CreateProject, a server whose closest-example pick is on
may start that Project from the example it finds clearly closest; provider-backed creation
starts from the default template unless the Builder explicitly opens an example. Studio then names no example, and its Flow tab draws that example's real
Flow definitions from the start until the agent pushes, then waits for that push's processes, with
the stage still Building. Studio shows the real flow whenever one exists: it draws a card's Flow
design only before the first build of a Project that started from the default template, and once a
Project has built processes a change keeps the last built flow while its push builds, then marks
the Steps the new build added or changed for a moment. Open example creates the Project from that example as it is, in one
click: CreateProject carries the example and no message, and what the person typed waits in the
Project's chat composer as an unsent draft (per browser, sent only when the person sends it). Its
Session starts with the example's card as version 1, built at the setup commit, so the Project
opens in the Builder at once: the Flow tab shows the real flow, the example's own Flow definitions
while the Preview builds the setup commit's processes (a change pushed before they are built waits
for its own), with no design or overview, and the
chat opens with the agent's greeting and its "Recorded version 1" line. Studio bundles every
example's definitions, generated with the template (`lib/generated/template-design/`), and the
Builder of an example's Project imports only that example's, on first use. CreateProject always sends the required
managedRepository field; Setup creates a private repository from the embedded template's default or
opened example. Pending creation and Agent intent session-storage
keys are v3, with stable request IDs, cancellation, retry and stale-response
protection. Publishing remains available after creation.

Authoritative references:

- [Resource gRPC and BFF contract](../../docs/api/resource-grpc.md)
- [Agent protobuf projection](../../docs/api/agent-protobuf-contract.md)
- [Architecture](../../docs/architecture/superverse-v2.md)
- [Real-dependency acceptance](tests/e2e/README.md)

## Commands

The project `.npmrc` disables npm audit and funding requests for local, CI and
image dependency installation.

```sh
cd web/studio
npm ci
npm run check            # check:source, lint, check:build; make check-web from the root
npm run check:source     # generation, pins, format, typecheck, unused, modules and READMEs
npm run check:build      # next build, then built CSS/style validation
npm run generate:check   # generated protobuf types match api/superverse/v2/superverse.proto
npm run format           # the pinned Prettier; npm run format:check verifies
npm run typecheck        # next typegen, then tsc for the application, the tests and scripts/
npm run lint             # ESLint: layering zones, typed rules, imports and test selectors
npm run check:pins       # every example's generated design, UI boundary and consumed pins
npm run check:unused     # knip: unused files, dependencies and exports
npm run check:modules    # directory and file limits, names, import cycles, client reachability
npm run check:readmes    # directory README summaries; npm run generate:readmes (make generate-studio-readmes) refreshes them
npm run build
npm run mock             # local in-memory presentation mode, see mock/README.md
```

To change Studio against the real backend instead of the mock, run
`./infra-as-code/sv2 native start` from the root: it serves `next dev` from this
directory on Studio's address with the deployed Studio configuration, next to the
host Go server ([host Studio](../../docs/runbooks/local-native.md#studio-modes)).

These are static checks. Acceptance runs only against a real environment
through `./infra-as-code/sv2 kind e2e` (see [tests/e2e](tests/e2e/README.md)).

The standalone type check includes tests and scripts. Keep it as well as Next's
build-time type check: the pinned Next build excludes some test files and does
not provide the same coverage. CI uses the same three check groups as local
`check`, adding only `--cache --cache-strategy content --cache-location
.cache/ci-eslint` to lint. Ordinary `npm run lint` performs the complete check
without persistent lint caching.

CI restores npm downloads, not `node_modules` or the full `.next` directory.
A separate check cache stores only `.cache/ci-eslint`, `tsconfig.tsbuildinfo` and
`.next/cache/.tsbuildinfo`, with a combined save limit of 10 MiB. Its key includes
OS/architecture, Node version, cache format, the complete Studio/API Git trees
and validation workflow identity, with no restore prefix. Any source, exported
type, configuration or dependency change therefore invalidates all typed lint
results, including unchanged consumers. Every validation still runs on a cache
hit. See [Actions usage and approval](../../internal/dexai/agents/README.md)
before adding work or exceeding an authorized remote run budget.

## Modules

Every directory with source files has a README whose generated summary names
its Purpose, its importers and what it imports, including the remote APIs it
calls; `make generate-studio-readmes` refreshes them and this table. `lib/` is
split by side, then by domain; nothing in `lib/` imports `components/`, `app/`
or `mock/`, and only `app/` route files select `mock/`. `lib/generated/` is
generated (`protobuf/`, `template-design/`) and never edited by hand. The
layering, limits and naming rules are in
the current contract.

<!-- BEGIN GENERATED MODULE TABLE (scripts/check-readmes.mjs; edit the README Purpose) -->

| Directory | Purpose |
| --- | --- |
| [`app`](app/README.md) | The Next.js routes: signed-in and sign-in pages, the BFF route handlers and the `/dex` Flow API routes, and the only files that choose mock or production code. |
| [`components/agent`](components/agent/README.md) | The shared Agent UI's single entry `index.ts`; Studio reaches the conversation and interaction presentation only through this entry. |
| [`components/agent/attachments`](components/agent/attachments/README.md) | The files a sent or queued message attached, as chips with image thumbnails, reached only through the links the page injects, so the shared Agent UI stays transport-free. |
| [`components/agent/conversation`](components/agent/conversation/README.md) | Renders an Agent conversation: turns, messages, notices, Markdown, and each recorded requirements version and turn's work as one plain line, with its log and metrics folded, on a live clock. |
| [`components/agent/interaction`](components/agent/interaction/README.md) | What the Agent asks of and shows the reader: question batches, an old waiting requirements card, each recorded version's line, queued messages, todo progress, timers and background tasks. |
| [`components/build`](components/build/README.md) | The Flow and Interface surfaces: the Flow Definitions of the version shown, the Preview with its source, Start and Stop, problem, phases and history, and the automatic-builds switch. |
| [`components/flow/canvas`](components/flow/canvas/README.md) | Draws a Flow Definition on a React Flow canvas, by stage or by Step, with its controls, legend and Step panel, optionally overlaid with one run and its failure. |
| [`components/flow/canvas/camera`](components/flow/canvas/camera/README.md) | Frames what the canvas shows: a Step with the Steps around it, legibly; the entrance trace of a new drawing; one eased move per change. |
| [`components/flow/canvas/layout`](components/flow/canvas/layout/README.md) | Places a decoded Flow into one Scene: stages or Steps, the main lines drawn and the jumps docked as chips, ELK placement and group bands. |
| [`components/flow/canvas/model`](components/flow/canvas/model/README.md) | Decodes a Flow Definition into the canvas model and derives what the canvas shows: guards, branches, Step groups and context, actionable Steps and a run overlay. |
| [`components/flow/canvas/render`](components/flow/canvas/render/README.md) | Renders a laid-out Scene with React Flow: Step and stage cards, routed lines, stage boxes with their ⊕ and ⊖, the zoom bar and opening the view readable. |
| [`components/flow/connections`](components/flow/connections/README.md) | Tools, Setup's table: a row per connection with its settings under it, then the variables, in a Preview column and a Next publish column, each autosaved. |
| [`components/flow/connections/model`](components/flow/connections/model/README.md) | What Setup's table reads and how it names things: each column's catalog and sessions, the rows' items and tones, the grid and the reply channel default. |
| [`components/flow/connections/row`](components/flow/connections/row/README.md) | One connection's row of Setup's table: its connection on each side, its status chips, and the body with connection settings, a new connection and each setting. |
| [`components/flow/connector/manifest`](components/flow/connector/manifest/README.md) | Form controls and rules for the fields a connector manifest declares: inputs and defaults, authorization methods, and JSON Pointer reads and merges of unit values. |
| [`components/flow/connector/native`](components/flow/connector/native/README.md) | The native units a connector manifest declares, the host that runs their declared provider commands and saves their values, model ID rules and picker options. |
| [`components/flow/connector/native/model`](components/flow/connector/native/model/README.md) | Projects each model provider's listing into the model picker's options, one module per provider, with the listing notices they share. |
| [`components/flow/connector/team-space`](components/flow/connector/team-space/README.md) | A Teamspace connection's editor with the connector's own form, and the picker that chooses one for a Project's connection name. |
| [`components/flow/design`](components/flow/design/README.md) | Draws an agent's Flow design with the Flow canvas and its heading, compares it with the current source for marks and change lines, and holds its Process Brief. |
| [`components/flow/design/brief`](components/flow/design/brief/README.md) | The Process Brief on the Flow tab while a design is drafted or built: the summary line under the canvas with one More details, and the draft's Step names. |
| [`components/flow/diagnostics`](components/flow/diagnostics/README.md) | The Diagnostics pages of a Flow and one run: overview, live state, Streams, the run record, and the Stop, Time Travel and channel operation dialogs. |
| [`components/flow/diagnostics/event`](components/flow/diagnostics/event/README.md) | Renders the details of one history event: its Step method sections, effects, failure and the Step options it ran with. |
| [`components/flow/diagnostics/graph`](components/flow/diagnostics/graph/README.md) | Reconstructs and draws a run's execution graph from its history events, without applying a newer Flow Definition. |
| [`components/flow/diagnostics/timeline`](components/flow/diagnostics/timeline/README.md) | A run's Timeline of history events, the selected event's sidebar and the dashed connector that joins the selected card to it. |
| [`components/flow/diagnostics/value`](components/flow/diagnostics/value/README.md) | Renders the values a run's history carries: typed fields, structured and JSON values, Step options and decisions, waiting conditions and persistence groups. |
| [`components/flow/frame`](components/flow/frame/README.md) | What every Flow view shares: the FlowFrame root, links to a Flow target, pane presets with their storage keys, and the Runs workspace class constants. |
| [`components/flow/map`](components/flow/map/README.md) | The Flow tab's project map: a card per valid Flow Type, named as people say it, with its stages, apps, child Flows and change markers. |
| [`components/flow/run`](components/flow/run/README.md) | The Runs and Inbox views: the run list column with its health strip, Filter and events slots, run groups, the run search and the hook that routes Flow types. |
| [`components/flow/run/detail`](components/flow/run/detail/README.md) | The selected run's drawer: header, Display and Actions, QR code input, typed values, UI slots, run copy and the liveness of what the reader holds. |
| [`components/flow/run/failure`](components/flow/run/failure/README.md) | A failed Step or run at the top of the run drawer in business terms: what happened, what happens now, the engine's words behind More details. |
| [`components/flow/run/start`](components/flow/run/start/README.md) | A process's start form in business words, inline or in its dialog, with typed start input and recovery of a start whose response was uncertain. |
| [`components/flow/run/step`](components/flow/run/step/README.md) | What a reader may do at a run's clicked Step: open its Inbox item, ask the agent, retry a failed Step, have the agent fix it, or Time travel. |
| [`components/flow/run/tryit`](components/flow/run/tryit/README.md) | Runs' first view before Preview has a run: what the version still needs, connected in place with Setup's controls, then the process's start form inline. |
| [`components/flow/scope`](components/flow/scope/README.md) | Studio's only entry into the Flow UI: binds it to a Project-scoped Flow API, supplies navigation and the catalog notice, and loads its views on demand. |
| [`components/frame`](components/frame/README.md) | The Project Builder's frame: the bar, the chat column with its rail, Tools' list, the links between surfaces and versions, and the surface registry. |
| [`components/frame/bar`](components/frame/bar/README.md) | The Builder's one 48px bar: the Project switch, the Preview | Live switch, the surface tabs, the actions, and how far the bar gives way when narrow. |
| [`components/frame/status`](components/frame/status/README.md) | The bar's environment tabs: one status dot each, and a word only when something is off, which links to its fix or, for a stopped Preview, opens Start. |
| [`components/project`](components/project/README.md) | The Project pages: the start form, Project creation, Project details, the Project's one Session provider, the surfaces and tools inside the Builder frame, and Setup on either tab. |
| [`components/project/activity`](components/project/activity/README.md) | The per-environment timeline and the recent trigger events: Preview's starts, Live's publishes and deployments, each with version, outcome and problem. |
| [`components/project/creation`](components/project/creation/README.md) | Connects a local agent using inline risk acknowledgement and an expiring pairing code. |
| [`components/project/goals`](components/project/goals/README.md) | The Builder's Goals tab: the Live version's goals with their values and trends, the agent's proposals, the reviews that measured them, Review now and Set goals. |
| [`components/project/proposal`](components/project/proposal/README.md) | The agent's proposal as the chat's card and the Goals tab's decision footer: Approve, Decline with a reason, and where it goes until it ships. |
| [`components/project/setup`](components/project/setup/README.md) | Setup's pieces around the table: why it is not shown yet, the automatic apply of Preview's saved changes, and the Live tab's banner. |
| [`components/project/templates`](components/project/templates/README.md) | The start page's starting points: the bundled built examples and the placeholder templates, their gallery, the suggestions while typing, and the Project name a description gives. |
| [`components/publishing`](components/publishing/README.md) | Tools, Publishing: the publish in progress, what is Live with Stop Live, every version with its builds, rollback after its consequences, and Live's history. |
| [`components/publishing/progress`](components/publishing/progress/README.md) | One publish in Tools ▸ Publishing: its header with Cancel, the five-stage bar, a failure's problem, and the inline build log. |
| [`components/publishing/publish`](components/publishing/publish/README.md) | The bar's Publish button and its popover: the version, what blocks it, what to check, what happens, the skip reason, and Publish. |
| [`components/publishing/timings`](components/publishing/timings/README.md) | Where the time of a Release build, a source preparation or a deployment went: one row per measured phase, with the labels of the phase codes. |
| [`components/session`](components/session/README.md) | Binds the page's Session to the Agent UI: the chat and its rail, composer or mock composer, model picker, pending action dock, status, workspace renewal and startup phases. |
| [`components/session/composer`](components/session/composer/README.md) | The one message input, large on the start page and compact in the chat, with its About chip and the element picker that fills it. |
| [`components/session/composer/attachments`](components/session/composer/attachments/README.md) | The composer's attachment controls: Attach (+) with its file picker, the chips with their progress and reasons, and the drop zone that takes files dropped on the chat. |
| [`components/session/executor`](components/session/executor/README.md) | Shows the composer's Agent picker, SQL-derived remote liveness, offline notice, pairing entry and epoch-fenced return to Superverse. |
| [`components/session/mock`](components/session/mock/README.md) | The mock model's composer, in a Session and on the creation page: it picks a category, a scenario and its arguments, and sends the next input as its kind arrives. |
| [`components/session/remoteagent`](components/session/remoteagent/README.md) | Shows Builder connection risk acknowledgement, one-time pairing and safe connection metadata through Studio's typed business API. |
| [`components/session/start`](components/session/start/README.md) | The composer around a Session's turns: the starting model the creation page preselects, the composer's text per Project and its send. |
| [`components/shell`](components/shell/README.md) | The app frame: the sign-in and registration form, the signed-in sidebar with its rail, Projects, Team, theme and account, the account pages' bar, and the brand marks. |
| [`components/shell/palette`](components/shell/palette/README.md) | Jump to (⌘K or Ctrl K): Home, New project, every Project, and the Processes, surfaces and tools the open Builder offers. |
| [`components/teamspace`](components/teamspace/README.md) | The Teamspace switcher, creation and landing, page gate, Home header, People, a Project's Permissions and Share, and the Project role chip. |
| [`components/teamspace/connections`](components/teamspace/connections/README.md) | The Teamspace's Connections page: its connections by connector and major version, the Projects using each, and their creation, editing and deletion. |
| [`components/ui`](components/ui/README.md) | Product-free UI primitives: the class joiner `cx`, Button, Dialog, Popover, Banner, Chip and Eyebrow, icons and glyphs, the phase timing list and the not-ready placeholder, with resizable panes in `pane/`. |
| [`components/ui/badge`](components/ui/badge/README.md) | The one small pill for a state, a count or a label beside a name, in the five tones the palette licenses. |
| [`components/ui/menu`](components/ui/menu/README.md) | The one dropdown for every single choice, on the popover card, with its rows and listbox keyboard handling. |
| [`components/ui/pane`](components/ui/pane/README.md) | Resizable panes: the pixel resize handle with its rail and remembered width, the list column, and the drag and keyboard gestures they share. |
| [`components/ui/problem`](components/ui/problem/README.md) | The one problem panel every failure uses: what happened, why, one remedy button and the folded technical details. |
| [`components/ui/reveal`](components/ui/reveal/README.md) | The "More details" fold every surface uses to keep engine detail out of its default view, remembered open per surface in this browser. |
| [`components/ui/timings`](components/ui/timings/README.md) | The list of the phases a Preview start, a build or a deployment measured, each with its label and duration. |
| [`components/ui/wait`](components/ui/wait/README.md) | The running Dex mark for a long wait: the brand tile GIF shown after a few seconds, with its reduced-motion still and polite status words. |
| [`lib/account`](lib/account/README.md) | `AccountProvider`, which gives the signed-in pages the account their layout read once. |
| [`lib/api`](lib/api/README.md) | Browser clients of the BFF, one module per browser service with functions named after its RPCs, and the account client that reaches AccountService through `/api/auth`. |
| [`lib/api/flow`](lib/api/flow/README.md) | Flow API documents and calls: definition and run wire types, status and history readers, blobs, and one module per Flow API service, so no component builds a Flow API path. |
| [`lib/api/flow/draft`](lib/api/flow/draft/README.md) | The autosave queue of one configuration scope per tab: one compare-and-swap write at a time, each on the revision the previous returned. |
| [`lib/api/tenancy`](lib/api/tenancy/README.md) | Browser clients of TeamSpaceService, TeamSpaceMemberService and ProjectMemberService over the business RPC route. |
| [`lib/api/transport`](lib/api/transport/README.md) | The browser network layer owns RPC posts, bounded read retries, snapshot Watches, command recovery, the Flow API client and application URL probes. |
| [`lib/browser`](lib/browser/README.md) | Browser-only utilities: storage with every stored key, the theme preference, single-flight user actions, URL query updates and paged lists. |
| [`lib/examples`](lib/examples/README.md) | The examples of the application template this Studio was built with and the example search, which ranks them in the browser exactly as the server shortlists them. |
| [`lib/flow`](lib/flow/README.md) | The providers the Flow UI reads: the scoped Flow API client, Flow view navigation and the Flow catalog, with the definition revision binding and enum labels. |
| [`lib/project`](lib/project/README.md) | Project resources: the Project Watch provider and list, resource Watches, release versions, Process definitions, waiting runs, configuration reads, requirement gap names, the pending creation and the generated designs. |
| [`lib/project/goals`](lib/project/goals/README.md) | A Project's goal measurement and agent proposals for the Builder: the shared read, Review now, decisions, proposal documents and backtests, and goal values in their unit. |
| [`lib/project/publishing`](lib/project/publishing/README.md) | A publish's five stages from its phase timings, the publish in progress with its Watches, the build log read, and the Publish popover's version check. |
| [`lib/project/setup`](lib/project/setup/README.md) | Setup's shared state: each environment's status, the Preview draft, Live's next-publish layer with its items, and the gap links. |
| [`lib/project/stage`](lib/project/stage/README.md) | How far a Project has come, from its first conversation to built and Live, and the Builder tabs it has shown in this browser. |
| [`lib/server`](lib/server/README.md) | The server-only BFF core: the gRPC client and its `RpcBackend` seam, the Server-Sent Events relay, `/api/auth`, the caller cookie, same-origin checks and bounded request bodies. |
| [`lib/server/connector-oauth`](lib/server/connector-oauth/README.md) | Starts a Teamspace connection's OAuth authorization and completes its provider callback, binding the two with a signed route cookie. |
| [`lib/server/flow`](lib/server/flow/README.md) | Serves the `/dex` Flow API routes and the Session source catalog: maps each URL to a named Flow API RPC and checks CSRF. |
| [`lib/server/rpc`](lib/server/rpc/README.md) | Serves `/api/rpc/<Method>` for the six browser services through a fail-closed allowlist and a strict descriptor-driven request decoder. |
| [`lib/session`](lib/session/README.md) | The page-owned Session: `SessionProvider`, its owner hook with the single Watch and the Agent commands, the reducer, the Agent protobuf projection, message merging and the mock scenarios. |
| [`lib/session/attachments`](lib/session/attachments/README.md) | The composer's attachments: the accepted formats and limits, image normalization and hashing, the upload through a presigned PUT, the chip state machine, its tray hook and the history download links. |
| [`lib/session/credentials`](lib/session/credentials/README.md) | Owns the Project page’s non-secret credential status and focus refresh. |
| [`lib/session/design`](lib/session/design/README.md) | Reads which Flow design the Flow tab draws from the Session Snapshot, reads its body once per digest, remembers which card version it showed, and whether its Brief shows. |
| [`lib/session/draft`](lib/session/draft/README.md) | The requirements card the agent is still writing, as the Session Watch streams it, and when the Snapshot says its card call is over. |
| [`lib/session/model`](lib/session/model/README.md) | Maps every catalog AgentModel enum explicitly to its Studio literal and back, preserving historical values and recognizing unavailable identities. |
| [`lib/shared`](lib/shared/README.md) | Pure code both sides use: protobuf and JSON types, formatting, abort and delay, RPC method types, Session command errors, Project availability, Flow Definition catalogs and Studio routes. |
| [`lib/shared/problem`](lib/shared/problem/README.md) | The one problem format's copy: remedy buttons, what-happened phrases, gap phrases, refusal texts and the builders that turn a failure, an error or a notice into a problem. |
| [`lib/shared/version`](lib/shared/version/README.md) | How Studio names a commit: "V12 · subject" from the version the server froze with each record, never a cut hash. |
| [`lib/teamspace`](lib/teamspace/README.md) | The current Teamspace of the signed-in pages with its one read, the Teamspace list, and the closed sets of Teamspace and Project roles. |
| [`mock`](mock/README.md) | Mock mode for local presentation: the mock switch, the `/dex` Flow API mock and the mock application page, selected only by `app/` route files. |
| [`mock/backend`](mock/backend/README.md) | Provides explicit development business-service fixtures through generated protobuf codecs and store Watches, without production fallback or acceptance evidence. |
| [`mock/backend/catalog`](mock/backend/catalog/README.md) | Supplies development-only provider model metadata to the mock Agent service. |
| [`mock/fixture`](mock/fixture/README.md) | The Flow Definition fixtures of mock mode and their classification, the Flow runs recorded from Local Kind that the `/dex` mock serves, and the mock Agent's questions and requirements card. |
| [`mock/store`](mock/store/README.md) | The mock's in-memory store and its clock: seed data, accounts, the mock Agent's state and scripts, configuration drafts, Releases, builds and Live, phase timings and resource snapshots. |
| [`mock/store/attachments`](mock/store/attachments/README.md) | Development-only attachment staging, where uploads PUT to the mock route are prepared at once and the attachments a Project's messages carry have downloads. |
| [`mock/store/delivery`](mock/store/delivery/README.md) | Live delivery in Studio mock mode: publish runs, environment events, the Agent's delivery feedback phrases and the goal measurement of Live runs. |
| [`mock/store/scenarios`](mock/store/scenarios/README.md) | The mock model's scenario catalog of Studio mock mode, whose inputs drive the mock Agent's scripts through the mock composer, and the markers its phrases ignore. |
| [`mock/store/setup`](mock/store/setup/README.md) | The mock's Setup state: the Preview's applied configuration and starts, Live's next-publish layer resolved against current Live, and the Slack and Stripe fixture connections. |
| [`mock/store/tasks`](mock/store/tasks/README.md) | The mock Agent's background commands and subagents in Studio mock mode: their task scripts, the live line of a running subagent and the Stop button that stops them. |
| [`mock/store/tenancy`](mock/store/tenancy/README.md) | The Teamspaces, their members and the Projects' members of Studio mock mode, with the rules TeamSpaceFlow and ProjectFlow keep and the caller's role in each. |
| [`mock/store/validation`](mock/store/validation/README.md) | Checks Preview evidence when the mock store admits a release build. |
| [`mock/surface`](mock/surface/README.md) | The fixture Project surface inside the Builder frame with its role lens and the named Setup fixtures, which mock routes render in place of the production surface. |
| [`scripts`](scripts/README.md) | Generators and checks of the Studio source: protobuf types, consumed pins, the module graph, the module and styling rules, the example search's data and these directory READMEs. |
| [`tests/e2e`](tests/e2e/README.md) | The three required acceptance specs of the local lanes: registration, a Project edited and released through Studio, and an Agent-added Flow approval. |
| [`tests/e2e/integration`](tests/e2e/integration/README.md) | Focused Studio scenarios that run against an existing environment and private fixtures; they are run one at a time and are not acceptance. |
| [`tests/e2e/integration/catalog`](tests/e2e/integration/catalog/README.md) | Exercises catalog admission, existing model mappings, persisted Claude selection and mock continuation through the real Studio, BFF, gRPC, Dex and Sandbox. |
| [`tests/e2e/integration/goals`](tests/e2e/integration/goals/README.md) | The Goals tab specs: a never-published Project's empty state, and an agent review's proposal approved from the Goals tab or the chat card, then applied on Preview. |
| [`tests/e2e/integration/modelcredential`](tests/e2e/integration/modelcredential/README.md) | Verifies Project credential entry and composer admission through real dependencies. |
| [`tests/e2e/integration/publishing`](tests/e2e/integration/publishing/README.md) | The Publish popover and cancellation on a local lane: the Preview validation or a skip reason, the stated consequences, Live's waiting settings, and a cancelled build. |
| [`tests/e2e/integration/remoteagent`](tests/e2e/integration/remoteagent/README.md) | Exercises real Studio pairing, public remote replies, and epoch-fenced takeover through the lane's existing BFF, gRPC, remote HTTP listener, SQL and Dex. |
| [`tests/e2e/integration/remoteagent/help`](tests/e2e/integration/remoteagent/help/README.md) | Exercises the local agent help route, copyable setup commands, troubleshooting, permissions and narrow-screen layout against a real Studio server. |
| [`tests/e2e/integration/setup`](tests/e2e/integration/setup/README.md) | Focused Tools, Setup scenarios on a local lane: the Live tab's banner and Next publish controls over two versions the mock model pushes. |
| [`tests/e2e/integration/timeline`](tests/e2e/integration/timeline/README.md) | Exercises Preview history and Process navigation, including refused-push versions and query preservation across the shipped newsletter's two real Flows. |
| [`tests/e2e/journey`](tests/e2e/journey/README.md) | Retained end-to-end journeys through the real Studio, model, GitHub, Sandbox and application APIs that keep the resources they create. |
| [`tests/e2e/manual`](tests/e2e/manual/README.md) | Holds opt-in provider E2E cases that require private credentials and operator consent outside the required local lanes. |
| [`tests/e2e/support`](tests/e2e/support/README.md) | Modules the specs share: Studio BFF calls, accounts, Projects, configuration, publishing, Flow runs, Session transport, repository host reads, private fixtures and JSON assertions. |
| [`tests/e2e/support/attachments`](tests/e2e/support/attachments/README.md) | The E2E files a first message attaches on the start page, uploaded through presigned PUTs, and the checks of their delivery, downloads and chips once the Session shows them. |
| [`tests/e2e/support/configuration`](tests/e2e/support/configuration/README.md) | Opt-in configuration helpers: the Native read-fault recovery case and the pieces of a two-version Live fixture for Setup's Live tab specs. |
| [`tests/e2e/support/coverage`](tests/e2e/support/coverage/README.md) | Records, for each passing E2E spec, the Studio source files whose code ran in its browser pages, for the committed coverage map. |
| [`tests/e2e/support/credentials`](tests/e2e/support/credentials/README.md) | Sets real acceptance credentials through the business API without recording keys. |
| [`tests/e2e/support/mock`](tests/e2e/support/mock/README.md) | Drives the mock model's composer: whether the lane offers it and the Session runs it, the first input chosen on the creation page, and each later input in order. |
| [`tests/e2e/support/preview`](tests/e2e/support/preview/README.md) | The specs' Preview steps: starting it from the Interface, awaiting a push's automatic deployment or its configuration notice, and previewing the template's heading edit. |
| [`tests/e2e/support/teamspace`](tests/e2e/support/teamspace/README.md) | E2E steps of a Teamspace's people: create or open a Teamspace, add a member in People, give a Project role in Permissions and open a Project as an operator. |

<!-- END GENERATED MODULE TABLE -->

## Session ownership

A Project has exactly one Session, which its ProjectFlow starts when the
Project becomes READY and which ends only with the Project's deletion; the
Project ID addresses it. Project pages are `/p/{projectId}` and surfaces are
`/p/{projectId}/{surface}`, with no Session in the URL. A surface page owns its
Project's Session once the Project is READY: `SessionProvider` holds the
Session's complete protobuf `SessionSnapshot` and its single `WatchSession`
connection; the conversation, workspace, Preview and indicators read it, and
the conversation alone also receives streamed text. While Setup runs, the chat
shows "Preparing project…"; once the Project is READY it shows "Starting the
agent…" and repeats a not-found Session read every second for at most 30
seconds; a Project whose Session start failed (`session_start_failure`) shows
that failure there and on its Project page instead. The chat's heading shows how long the READY
workspace stays, a button that opens "Keep running for 7 days", which `RenewWorkspace` turns into a Sandbox
deadline seven days ahead; after a reclamation the chat reads that the
workspace paused and kept its files. The Session is archived only by the
Project's deletion, so an ARCHIVED Session reads "This Project is being
deleted.", and a Session whose engine execution stopped reads "The agent
stopped unexpectedly. An operator must recover it; your conversation and code
are kept."; both disable the composer.

Project creation goes straight to the Builder's Flow. The creation page shows the
Session's own model picker and composer, so the person chooses the model and the first
message there: the free-text composer with a Claude model, or, where `ListMockScenarios`
lets the account select the mock model, the mock composer, whose first scenario input
becomes the message. The picker starts on Sonnet, or for the platform administrator
(`useAccount().isAdmin`) on the mock model where `ListMockScenarios` offers it
(`components/session/use-starting-model.ts`). The creation page sends the message, its
message ID and the model with `CreateProject` (`components/project/ProjectCreationPage.tsx`).
The server admits them before the Project starts, and the Project's Session starts on that
model with the message admitted once, whether or not the tab stays open; a retry of the
same request repeats the same message ID, and a changed one is refused.

- Every Watch connection, including a reconnect, starts with a snapshot. Get is
  used for the first load, recovery and command reconciliation; a Get response
  never replaces a newer Watch snapshot.
- Session revision and committed message sequence reject older snapshots.
  Committed and archived messages are retained for the Session's lifetime. Readiness uses
  Agent history and status, never a RunID.
- Agent commands return an empty response after typed admission and do not
  trigger a full Get; the Watch reports their effect. An uncertain command is
  never replayed automatically.
- Every Session request names its Project explicitly. A Project or Session change
  cancels the previous page's reads and commands.

## Commands and retries

Preview commands and Publishing builds, recoveries and deployments keep their
request ID across retries until accepted, or until the server definitely rejects
(400, 401, 403, 404 or 409) a command whose attempts were all definite. After
one uncertain attempt the ID is kept, so the next retry repeats that request.
Project creation, with its first message's ID, and Release preparation keep their
stored request ID until accepted. Each component runs at most one user action at a
time (`useSingleFlight`), and unmounting aborts it.

## Browser boundary

`/api/rpc/<Method>` serves exactly the RPCs of twelve business services (the
`BROWSER_SERVICES` allowlist in `rpc-handler.ts`, which the compiler checks
against the `BrowserService` type in both directions): a unary RPC is a
same-origin POST of its generated request as JSON, and a Watch is a GET whose
query names its IDs and resume cursor and whose response is Server-Sent
Events. Any other method name, including the
account and Flow API services and the two OAuth RPCs of TeamSpaceConnectionService,
which only `/api/connector-oauth/start` and its callback call, is 404, and the
wrong verb is 405. The decoder
walks the request descriptor and rejects unknown fields, a browser-supplied
`caller`, null, mistyped or out-of-range values and undeclared or zero enum
values; the BFF then sets only the caller and adds no ID, fence or default.
AccountService is reached only through `/api/auth` and the Flow API services
only through `/dex/...`. See the
[resource gRPC and BFF contract](../../docs/api/resource-grpc.md#apirpcmethod).

The BFF derives caller context from the Go-issued login envelope, and Go
authenticates and authorizes every call. Mutations require same origin and a
body of at most 1 MiB. Browser cookies and authorization headers are never
forwarded to project applications. Configure `SUPERVERSE_GRPC_ADDRESS`,
`BROWSER_SIGNING_KEY` and `SUPERVERSE_STUDIO_PUBLIC_ORIGIN` from the private
environment, plus the TLS settings it requires. `BROWSER_SIGNING_KEY` is an
unpadded base64url encoding of 32 random bytes used only for Flow API CSRF
and connector OAuth route-cookie integrity; replacing it
requires reloading configuration pages and restarting pending OAuth.

Configuration and Runs reach project Dex through the named Flow API RPCs that
Go authorizes per resource. Missing Release, Flow Definition or backend data stays
unavailable; Studio never substitutes a sample or another App's Flow.

## The Builder frame

A Project page is the Builder ([the current contract](../../docs/architecture/studio-setup/studio.md)):
the sidebar, one 48px bar, the Project's chat in a column on every surface, and
one surface. The surfaces are `flow`, `runs`, `inbox`, `interface` and `tools`
(`/p/{projectId}/{surface}`), declared once in `components/frame/surface-registry.ts`;
`?v=live` sets the version every surface shows, and `?tool=` the open tool. The
earlier `/build` and `/publishing` URLs redirect there. The route group
`app/(workspace)/p/[id]/(builder)` holds the frame, the Project and its Session
in a layout, so they stay mounted while only the surface page below changes. The
sidebar and the chat fold to rails when dragged narrow, and every width is a
per-browser preference.

The bar's Preview | Live switch has one status dot per environment
(`lib/project/setup/environment-status.ts`) and says a word only when something is
off: _starting_ or _updating_, _failed_, _stopped_ or _needs setup_ on Preview,
_publishing_ or _publish failed_ on Live. A word with a fix links to it: the
Interface, Setup at the first missing item, or Tools ▸ Publishing. Unapplied changes
say no word; while Setup's Preview column lacks items or has changes to apply, the Tools tab
and its Setup entry carry an amber "Setup needs you" mark. The
switch has no menus: the Interface holds Start and Stop, the start's phases, its
problem and the Preview's History; Runs ▸ Events what the triggers received; the chat
header's workspace timer "Keep running for 7 days"; Project details the automatic
builds and deletion; Tools ▸ Publishing (and the Live Interface) Live's history. The
selected environment tab, the process picker and the open tab's underline are grey on
Preview and green on Live (`--color-env`). The Live status's hover hint says how many
versions (main-branch commits, from ListReleaseCandidates) Live is behind. While the person views Live
the chat stays live, with a note above the composer that changes are built on
Preview. The process picker at the head of the tabs (`?flowType=`, kept on
every surface) lists the Processes of the version shown, read by
`lib/project/ProcessDefinitionsProvider.tsx` (GetPreviewDefinitions on Preview,
GetRelease on Live), which also compares their digests: the Flow's project map
and the picked Process's canvas mark a Process that changed since Live. Without a picked Process
the Flow is the project map (`components/flow/map`), and Runs and Inbox merge
every Process's runs. A selected run is framed around its current Step, ringed
"now", with the Steps around it, or drawn whole when that still reads; a Step click opens the drawer. `lib/flow/FlowHostProvider.tsx` tells the
Flow views the version shown, whether the reader may stop and rerun, and how to
hand a Step to the chat, which `lib/session/ComposerDraftProvider.tsx` turns into
the composer's "About" chip, put in front of the message when it is sent. The
bar's Publish (`components/publishing/publish`) opens a popover: the version (the
newest ahead of Live, others folded, each with its Preview validation), what blocks
it with a link to each Setup row, what to check, what happens, a reason when the
version is not in the Preview record, and Publish; Tools, Publishing then follows
the build, deployment and health check in its stage bar.

## Resource pages

One page-level Project Watch supplies the frame, Publishing and configuration
validation receipts. Resource Watches are snapshot-first on every connection.

Tools, Publishing shows the publish in progress or the last one, with the stage bar
Check → Lock settings → Build → Deploy → Go live, Cancel and the inline build log
(`components/publishing/progress`), then what Live serves, with Stop Live, then
the newest 100 main-branch commits and older Releases 50 at a time, each with its
builds and how each met the Preview rule, then Live's history. A version ahead of
Live opens the Publish popover on it, Live's own version opens it on Live's waiting
settings, and an older one rolls Live back; Stop Live and a rollback each state
their consequences in a confirmation first. Accepted build and deployment IDs and
explicit Release URLs are read directly, never inferred from a listing.

Production surfaces derive availability and editability from authorized Project,
Session and Live snapshots. Role lenses, fixtures and sample Dex data exist only
in `mock/`.

## Local presentation mode

`npm run mock` runs Studio against an in-memory store behind the same
`/api/rpc` checks, protobuf codecs and components; `app/` route files alone
choose the mock or the production backend. It is not acceptance evidence. See
[the local mock guide](mock/README.md).

## Real acceptance

The acceptance gate is `./infra-as-code/sv2 kind e2e`. It runs exactly the
three required specs at the root of `tests/e2e` against a running environment:
registration, a real Agent edit with Preview, Release, Live and cleanup, and an
Agent-added Flow approval executed through Studio Actions. `npm run test:e2e`
runs every spec under `tests/e2e`, including fixture-only scenarios and
retained journeys, so its result is not acceptance; see
[tests/e2e](tests/e2e/README.md). Missing prerequisites fail explicitly; a
build or lint result is never reported as acceptance.
