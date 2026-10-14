---
name: dex-sdk
description: Implement, debug, test, and operate Go applications with the Superdurable Dex SDK. Load it from dex-app-builder when implementing an application backend. Do not use as the primary workflow for Dex product, application, or business-process requests.
---

# Dex SDK

Build reliable applications through Dex's public programming model. Keep the user's experience in Dex terms and APIs.

## Stay at the application boundary

- Model behavior with Flows, Steps, Waits, Attributes, Channels, Streams, RPCs, Timers, SubFlows, Workers, and the Client.
- Use Studio Runs and Diagnostics, dexcli, SDK errors, and application logs for inspection and recovery.
- Use typed Flow RPCs as the application boundary for reading and writing Attributes, AttributeMaps, Channels, and ChannelMaps. Do not use removed Client state APIs. Attribute match remains the blocking observation API.
- Do not expose Dex Server internals as application requirements. Discuss them only when the user explicitly asks to develop Dex itself.
- Diagnose read-only by default. Do not stop, time travel, publish, invoke, delete, edit, or otherwise mutate a Flow unless the user authorizes it.

## Establish source authority

Before writing code, identify the `go.mod`-selected Dex SDK version, the registry, Worker and Client bootstrap, and the repository's make targets.

Dex Server is backward compatible with every earlier SDK release. The application is not pinned to a platform-chosen Dex Go SDK, Connector SDK, connector, or Dex CLI version; `go.mod` records minimum versions. Move to a newer stable release when useful (`go get <module>@latest`) after reading that release's "Breaking Changes" section, and use released versions only. The `go.mod`-selected module source and the compiler are the API authority: source excerpts in this bundle show API shapes, and when an excerpt and the selected source disagree, the selected source wins; never invent a Dex API. New application indexes must follow the generic numbered-slot policy in [core primitives](references/core/primitives.md#attribute).

## Load references progressively

Always read the [Go entry page](references/go/go.md) first.

Before writing or reviewing any Client boundary—including Flow start, RPC, cleanup, admission, waits, or external Stream writes—read [Error handling](references/core/error-handling.md) and [Go error handling](references/go/error-handling.md). This is implementation guidance; do not defer it until a failure needs troubleshooting.

For mutations followed by reads or completion checks, also read [Read-after-write consistency](references/core/read-after-write.md). Classify the write/read pair. A typed read-only RPC after a successful direct-state RPC write observes the write; search/projections and triggered business work have separate confirmation boundaries. Do not add polling or callbacks to strong write/read pairs.

Then load only the references required by the task:

| Task | Core reference | Go reference |
| --- | --- | --- |
| First application or architecture | [Getting started](references/core/getting-started.md) and [Error handling](references/core/error-handling.md) | [Go entry](references/go/go.md) and [error handling](references/go/error-handling.md) |
| Go FDG, Summary/Display, Start forms or Actions | [Go FDG authoring](../dex-app-builder/references/fdg-authoring.md) | Read this focused syntax reference before parser source |
| Flow boundary or state model | [Modeling](references/core/modeling.md) | [primitives](references/go/primitives.md) |
| Primitive selection or exact API | [Primitives](references/core/primitives.md) | [primitives](references/go/primitives.md) |
| StepOptions, durability, timeout, heartbeat, retry, loads, locks, or failure route | [StepOptions](references/core/step-options.md) | [advanced features](references/go/advanced-features.md) |
| Design-pattern choice | [Patterns](references/core/patterns.md) | [patterns](references/go/patterns.md) |
| Client calls, admission, RPC, cleanup, waits, or external Streams | [Error handling](references/core/error-handling.md) and [Read-after-write consistency](references/core/read-after-write.md) | [error handling](references/go/error-handling.md) |
| Integration or failure-path tests | [Testing](references/core/testing.md) | [testing](references/go/testing.md) |
| Failure diagnosis | [Troubleshooting](references/core/troubleshooting.md) and [Error handling](references/core/error-handling.md) | [error handling](references/go/error-handling.md) and [gotchas](references/go/gotchas.md) |
| Production inspection or mutation | [Operations](references/core/operations.md) | [observability](references/go/observability.md) |
| Worker, Client, and Dex Server topology | [Getting started](references/core/getting-started.md#required-topology) | [Go entry](references/go/go.md) for Client and Worker targets |
| Connector selection or named connections | [Connector architecture](../dex-app-builder/references/connector-architecture.md); credentials stay in Studio Configuration | [Go entry](references/go/go.md) for runtime support |
| Large state, maps, or projections | [Data handling](references/core/data-handling.md) | [data handling](references/go/data-handling.md) |
| Open-Flow compatibility or upgrade | [Versioning](references/core/versioning.md) | [versioning](references/go/versioning.md) |
| Cancellation, BlobCache, or other advanced behavior | relevant core topic | [advanced features](references/go/advanced-features.md) |
| Durable AI agent | [AI agents](references/core/ai-agents.md) | [primitives](references/go/primitives.md), [data handling](references/go/data-handling.md), [patterns](references/go/patterns.md), and [advanced features](references/go/advanced-features.md) |

## Model before implementation

For non-trivial work, first state the Flow identity and lifecycle, typed start input and completion output, Steps and transitions, Flow-level durability default, method-level StepOptions, durable state, messages, synchronous RPCs, best-effort Streams, timers, retries, timeouts, recovery, and SubFlow boundaries.

Name every RPC handler and explicit RPC with a concrete action verb. Use complete domain names such as `get_queued_messages` or `move_queued_message_to_prioritized_messages`, not noun-only names, placeholders, or generic names such as `Get`, `Update`, `Manager`, `Data`, or `Handler`. Apply the same preference for complete, precise names to APIs, interfaces, classes, types, methods, functions, fields, variables, and constants. Brevity is not a goal; a name should communicate its operation and subject at the call site or registration boundary.

Use **Execute** for work-oriented transitions and **WaitFor** for durable waiting transitions. This is a modeling convention, not an SDK capability boundary: either phase may query or mutate an external provider when that is necessary to establish or reconcile its transition. Keep every external mutation idempotent or compensatable, including mutations made from `WaitFor`. Never poll a provider from `WaitFor` or with a Timer loop: waiting for an external system to become ready or finish uses the [Polling pattern](references/core/patterns.md#polling), one long-running Execute that loops and heartbeats while its StepOptions method timeout and retry total duration bound the wait. A long poll that runs beside other ongoing work in the same Flow stalls that work once the Flow reaches its operation limit, so it runs in [its own SubFlow](references/core/patterns.md#a-long-poll-beside-other-work).

Treat each WaitFor, Execute, and RPC invocation as a separate commit boundary. Split a provider action into its own Step when its successful completion deserves an independent checkpoint, retry/timeout policy, failure-recovery route, or audit boundary. For example, an idempotent Kafka or SQS send often merits its own Step so later failures do not resend it. Do not split solely because there is another API call: keep consecutive work in one Step when it shares one meaningful recovery boundary.

For a product mutation that entails multiple actions, cross-service calls, durable waits, retries, reconciliation, or cleanup, prefer starting one domain-named Dex Flow directly at the API boundary. Let that Flow own admission, orchestration, recovery, completion, and the durable process state that Dex can model. Never chain dependent StartFlow calls for that operation in the API handler, including a read/status branch between starts; follow [durable downstream-start ownership](references/core/modeling.md#own-downstream-starts-durably). Do not introduce a database outbox plus dispatcher, polling command queue, or generic event-driven coordinator solely to start or sequence the Flow. A single bounded local operation can remain synchronous, and independently owned external integrations may still require explicit events.

### Flow boundary default

Default a new design to no SubFlows. Use parallel Steps when work shares one
Flow identity and lifecycle. Create another top-level Flow only for a distinct
data owner and retention/cleanup lifecycle with its own waits, Timers, and
terminal outcomes; start it independently and coordinate through typed RPCs or
Channels. A separate top-level Flow is not a SubFlow.

Before proposing any SubFlow, read [Flow
modeling](references/core/modeling.md) and [pattern
selection](references/core/patterns.md). SubFlows are an explicitly confirmed
evolution after concrete single-Flow complexity or a fan-out beyond the
200-concurrent-Step architecture-review threshold. Code reuse, provider
abstraction, retry policy, or ordinary parallelism is not sufficient.

### Dex-first state ownership

Default durable application state to typed Flow Attributes and AttributeMaps,
not to a new database dependency. Prefer a stable domain/entity Flow when Dex
can own facts shared by multiple processes; cross-Flow reuse is an
ownership-design question, not by itself a storage gap. Use indexed Attributes
and Dex search for supported lookup paths, typed RPCs for reads and mutations,
Channels for queued intent, bounded AttributeMap chunks or partitions for
growing collections, and Dex blob storage for large values. Assign application indexes explicit
generic typed slots shared across Flow types; keep business names on Attributes.
Application run searches constrain FlowType;
see [core primitives](references/core/primitives.md#attribute).
For map-wide write invariants, follow the singleton coordination-lock pattern in
[data handling](references/core/data-handling.md#whole-map-coordination).

Introduce an external database or search store only for a confirmed access or
scale requirement Dex cannot reasonably satisfy: complex/ad-hoc indexes,
full-text or vector search, sustained high-contention reads and writes to one
hot record, relational joins or multi-record transactions, or large analytical
scans. First consider a Dex Attribute Store projection when Dex can remain the
authority and only the query shape is missing. Never create ambiguous dual
authority: identify the source of truth per fact and define synchronization,
failure, and reconciliation behavior.

Call StartFlow first; never add a preflight read RPC, search, or status lookup solely to avoid retry/AlreadyStarted edge cases. Follow the [start identity and error-handling rule](references/core/error-handling.md#start-first-reconcile-only-after-an-error), including a stable Request ID plus the ignore-already-started option for attachable retries. On success, return the accepted response from known validated request fields; do not reread identity or initial Attributes, or use an immediate snapshot to choose the next start.

Deduplicate root Flow starts with Dex start identity, not an application-owned database mechanism. Derive a stable Flow ID for the logical operation, derive the start Request ID from the complete logical request, choose the explicit ID reuse policy, and handle the SDK's typed already-started result. Never add a table, row, outbox, lease, lock, cache, or generic admission projection solely to deduplicate or serialize `startFlow`. If the API must confirm durable admission, wait for the admission Step or Flow result and read accepted business state from its owning domain record.

At application boundaries, preserve typed Dex failures until domain policy can distinguish business rejection, a closed-Flow race, a retryable service failure, and a local defect. Reconcile a not-active result from existing authoritative state; inspect the Flow only when an otherwise unknown terminal distinction changes the outcome. Use retained Streams only for best-effort observation, never as authoritative business state.

For terminal business reads, apply the [terminal read RPC rule](references/core/error-handling.md#terminal-read-rpc-rule): inspect RPC options and handler effects, read retained state through a typed read-only RPC, and never replace that snapshot with historical Step-output decoding. When tests are requested, verify this path with the [terminal entity read integration scenario](references/core/testing.md#terminal-entity-reads).

Prefer the nearest official pattern to an ad hoc coordination loop. Preserve its Flow shape while replacing the domain and integrations. Once the shape is explicit, list the Flow source in `dex-app.yaml` and run `make check-fdg-v2`, which renders strict FDG 2.0 with `dexcli visualize`. Studio Design shows those definitions and Studio Preview runs the Worker on the Sandbox's Preview engine at 127.0.0.1:8801; never start `dexcli dev` or another engine ([local environment](references/core/getting-started.md#local-environment)).

## Complete the vertical slice

Application changes should include typed inputs, outputs, and failures; Flow and Step definitions; every persistence schema entry; constructor-injected dependencies; registry wiring; Worker and Client wiring when absent; and an application boundary. Add a real Dex Server integration test when the user asks for tests; the template adds no test suites by default.

Keep stable Flow, Step, Attribute, Channel, Stream, and RPC names compatible with open executions unless the user has an explicit migration, including across an SDK upgrade that changes default type names ([Go type names](references/go/versioning.md#default-flow-and-step-type-names)). Use unique Flow IDs and convergence polling rather than fixed sleeps.

Before handoff, review registration of every durable primitive, retry and timeout recovery, duplicate requests, terminal behavior, heartbeat cadence, Stream best-effort semantics, and compatible Worker/Client registry and payload configuration. `commit_and_push` runs the template's `make check-static` gate (generation, strict FDG 2.0, formatting, modules, vet, and builds); when the user requested tests, also run the relevant integration scenario.
