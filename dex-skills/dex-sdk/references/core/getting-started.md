# Getting started

Use this guide for a new Dex application or when Worker and Client wiring is missing. The Superverse template already wires the registry, Worker, and Client in `internal/worker/worker.go`, and `internal/process/text_summary_flow.go` is a complete slice; register new Flows in that bootstrap instead of rebuilding it.

## Local environment

The coding Sandbox already provides **dexcli**; do not install it. Source work needs no running Dex: `commit_and_push` runs the template's `make check-static` gate, and Superverse builds Live from committed source.

The Sandbox runs exactly one Dex engine, the **Preview engine**: a local Temporal and Dex whose state lives on the workspace volume (Temporal namespace `default`; closed executions are kept 30 days). Studio Preview runs the prepared commit's application and Worker on it, and Studio Runs shows its executions.

- Its Dex listens on **127.0.0.1:8801**, the default of dexcli. A Preview start runs the engine; when it does not answer, for example after the workspace was reclaimed, `superverse-previewctl environment ensure` starts it and answers `starting` until it is `ready`; ask again until then. Its earlier executions stay readable.
- Never start another engine: no `dexcli dev` and no `temporal server start-dev`. The Sandbox's `dexcli` and `temporal` refuse both.
- See the application run through the Preview, which the user starts in Studio. Do not run the application or a Worker yourself: Sandbox commands have no project configuration (the trusted `DEX_PROJECT_*` environment that only a Preview start supplies), so the application stops at startup, and ports 3000 and 18811 belong to the Preview application. Never invent that environment or a credential.
- Read Preview Flows with `dexcli flow search`, `summary`, `state`, `history` or `inspect`, adding `--no-hydrate` unless you need payloads ([diagnostic order](operations.md#diagnostic-order)). The Sandbox's `dexcli` runs only reads (those, `flow watch`, `health`, `version` and `visualize`) unless given `--allow-mutation`; use that flag only when the user explicitly asks to change a Flow.
- The engine and Preview logs are in `/var/lib/superverse/preview/logs`: `supervisor.log`, `temporal.log`, `dexcli.log`, `build.log`, `app.log` and the Dex Server logs. File tools reach only `/workspace`, so read them with commands, for example `tail -n 200 /var/lib/superverse/preview/logs/app.log`.

## First vertical slice

Build the smallest end-to-end path in this order:

1. Define one typed start input and one start Step.
2. Make the Step return a terminal decision with a typed output.
3. Define the Flow's Step list.
4. Register the Flow in the application registry.
5. Create one Worker using that registry and a reachable bind/advertise address.
6. Create one Client using the same registry and blob/payload configuration.
7. Start the Worker before accepting application requests.
8. Read [error handling](error-handling.md) and [Go error handling](../go/error-handling.md); define stable request identity, duplicate-start behavior, uncertain acceptance reconciliation, and local-defect handling.
9. Start the Flow through an HTTP handler, CLI command, or service method.
10. When the user asks for tests, add an integration test that starts the Flow and waits for its result.

Add Attributes, Channels, RPCs, Streams, or SubFlows only when the first path runs successfully.

## Required topology

- Dex Server receives public Client calls and dispatches handler work.
- The Worker hosts registered Flow, Step, and RPC implementations.
- The Client starts and interacts with Flow executions.
- Superverse Studio shows Flow definitions (Design), runs, Display, Inbox actions, Flow state, and Step progress (Runs and Diagnostics); there is no Dex Web.
- The application owns its HTTP, messaging, database, and third-party integrations.

The Worker must be reachable from Dex Server. A bind address controls where the Worker listens; an advertised target controls the address Dex Server calls. These differ when containers, proxies, or load balancers are involved.

A Flow keeps the Worker target it started with unless an update to its Flow configuration changes it, and Dex invokes that target for the Flow's later Steps and RPCs, including read-only RPCs on a retained ended execution. Once that Worker is gone, the Flow's RPCs fail, including its typed read RPCs, while Dex's own Flow state, Attribute, and Channel reads still answer. A Flow started on a short-lived Worker therefore cannot serve its business reads afterwards. Start Flows that must stay readable on a long-lived Worker target, and let code that lists or reads many Flows report one unreadable Flow without failing the whole listing.

## Configuration checks

Before debugging code, verify:

- the application uses the Dex Server address from its environment (`DEX_FLOW_SERVICE_ADDRESS` in the template; Superverse sets it for Preview and Live)
- the Worker advertised target is reachable from Dex Server (the template reads `DEX_WORKER_BIND_ADDRESS` and `DEX_WORKER_TARGET`; Superverse sets both for Preview and Live)
- the Flow type is present in the Worker and Client registries
- the Worker and Client use compatible payload codecs and blob cache/storage
- all Attributes and Channels used by handlers are in the Flow persistence schema
- Indexed Attributes are synchronized before the Worker starts serving
- connector connections and credentials come from Studio Configuration through the template's `DEX_PROJECT_*` configuration loader (`internal/connectorconfiguration`), never from source or Flow state

## Source material

- Reference implementation: the template's `internal/process/text_summary_flow.go` and its wiring in `internal/worker/worker.go`
- Runnable SDK examples: `examples/` in the selected SDK module (directory: `go list -m -f '{{.Dir}}' github.com/superdurable/dex/sdk-go`)
- Primitive overview: [core primitives](primitives.md) and https://docs.superdurable.io/primitives

Copy API usage from these sources and the selected SDK source rather than from prose or memory.
