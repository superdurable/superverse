# Stage 3: design the Flow, Connectors, and OpenAPI contract, then implement the backend

Start from the template's reference Flow named in `AGENTS.md`. For anything it does not show, follow the sibling [Dex SDK skill](../../dex-sdk/SKILL.md) routing with [Go](../../dex-sdk/references/go/go.md) as the only language, and load only the Core and Go references required by the approved design.

Dex SDK supplies the public SDK guidance. The platform constraints in this skill are stricter and take precedence: use only the Go SDK, keep external effects in `Execute`, keep `WaitFor` free of provider or Dex mutations, and require strict FDG 2.0 rendering.

Read [Flow modeling](../../dex-sdk/references/core/modeling.md), [pattern
selection](../../dex-sdk/references/core/patterns.md), and [Go FDG
authoring](fdg-authoring.md) before editing a Flow. State the Flow identity,
input/output, Steps, transitions, Attributes, Channels, RPCs, timers, retries,
recovery, and connector boundaries before code. New applications default to no
SubFlows: use parallel Steps inside one lifecycle, and use independently
started top-level Flows plus typed RPCs or Channels when the confirmed data
lifecycle requires another owner. Implement a SubFlow only after the Core gate
is satisfied and the user explicitly confirms it.

### Dex-first backend storage

Use Dex Flow state as the default durable application store. Read the shared
[data-handling guidance](../../dex-sdk/references/core/data-handling.md) and the Go
[data-handling](../../dex-sdk/references/go/data-handling.md) and
[entity-state patterns](../../dex-sdk/references/go/patterns.md) before choosing a
database, cache, or ORM.

- Keep cohesive Flow-owned current state in typed Attributes.
- Use AttributeMaps with exact instance loads for independently accessed
  records, bounded chunks, partitions, and keyed entities.
- Use indexed Attributes and Dex search for supported list and lookup paths,
  typed RPCs for application reads and mutations, Channels for queued intent,
  and Dex's blob path for large durable values.
- When several processes need the same durable domain facts, prefer a dedicated
  stable domain/entity Flow that owns those Attributes or AttributeMaps and
  exposes typed application operations. Cross-Flow reuse alone is not a reason
  to introduce a database or duplicate state in each process Flow. A process
  Flow reads those facts in an `Execute` Step through the application's Dex
  Client and the owner's typed RPC; see
  [Read another Flow from a Step](../../dex-sdk/references/go/primitives.md#read-another-flow-from-a-step).

Do not add an external database, cache, ORM, outbox, or shadow read model merely
because the product has durable domain data or may need more queries later. Add
external storage only after a concrete confirmed requirement exceeds Dex's
storage and access model, such as complex or ad-hoc secondary indexes,
full-text/vector search, high-concurrency reads and writes against one hot
record that cannot accept Attribute-lock serialization, relational joins or
transactions across independently owned records, or analytics that require
large cross-Flow scans.

Before adding application-managed external storage, evaluate
Attribute/AttributeMap partitioning, bounded chunking, exact loads, indexed
Attributes, typed RPCs, and—when the state remains Dex-authoritative but needs
a separate query shape—Dex Attribute Store synchronization. If an external
store is still required, document the exact unsupported operation and expected
scale/SLO, assign one authoritative owner for every fact, define projection or
synchronization and recovery semantics, and test failure and reconciliation.
“Future flexibility” is not a sufficient reason.

### Runs that can be measured and improved

Build every Flow so that its own history answers which runs succeeded, for whom,
at what cost, and what a different decision would have done. These rules cost
little in the first version and are expensive to retrofit:

- **Outcomes are visible in the run.** Each business outcome the contract names
  (approved, paid, activated, replied, resolved) is a Step the run enters, an
  Action it accepts, or an Attribute value. When the outcome happens in another
  system, record it when the application learns it, through a Trigger, an Action
  or a Step, rather than leaving it to be inferred later.
- **Segment facts have their own scalar Attributes.** A fact the business would
  compare runs by, such as a plan, region, source, tier or customer size, is a
  scalar Attribute set once when it is known, not only a field inside a JSON
  value or a display string. Index it only when a list needs it; index slots are
  few.
- **Decisions are plain functions.** Routing, scoring, thresholds, lane choices
  and timer durations are computed by plain functions of the run's recorded facts
  and configuration, which the Step calls and records the inputs of. The same
  function can then be run over past runs to see what a changed rule would have
  done.
- **Model calls are their own Steps.** Each model call is an `llm` connector Step
  ([text generation](connector-architecture.md#text-generation)), so its result,
  with its token usage, is in the run's history.
- **Fields keep their meaning across releases.** A release adds a new Attribute
  for new data instead of changing what an existing one means, so runs from
  different releases stay comparable.

Keep personal data in these Attributes to what the process needs.

For Custom UI, design the application OpenAPI contract in the same pass as the
Flow and Connector boundaries. Map each requested or confirmed UI action to an
application-level operation backed by a Flow start, typed RPC, query, or
confirmed integration ingress. Define authentication and permission checks,
idempotency, asynchronous status, request and response types, validation,
errors, retry behavior, and terminal outcomes. Keep Dex Steps, Channels,
Attributes, and other execution internals out of the public HTTP contract.

Before implementing backend handlers, update `openapi/openapi.yaml` as the
contract source and run the template generation command. Implement the Go HTTP
boundary through the generated server interfaces and reserve the generated
TypeScript client for the later UI integration stage. Never hand-write parallel
request, response, or client types. Treat `internal/api/generated` and
`web/src/api/generated` as ignored local build outputs: regenerate them in the
workspace when needed, but never commit them.
Do not resume dynamic frontend work while the Go Flow, Connector, and
application boundary are being implemented.

Superverse Studio is the management surface: Build Design renders the strict
FDG 2.0 definitions prepared from the pushed commit, Configuration holds
connections and credentials, Preview runs the application and its Worker on the
Sandbox's Preview engine, and Runs and Inbox show Preview or Live
([Studio and FDG 2.0](studio.md)). That engine at 127.0.0.1:8801 is the only Dex
in the Sandbox: never run `dexcli dev` or `temporal server start-dev`. See the
application run through the Preview and read its Flows and logs as the
[local environment](../../dex-sdk/references/core/getting-started.md#local-environment)
describes. Run `make check-fdg-v2` after each Flow change; it renders every Flow
file in `dex-app.yaml` with `dexcli visualize --schema-version 2.0 --json`.

Model each human operation as a typed Go Action with one **ActionRequiresPermission** option. Use lowercase domain keys such as **refund.manage** or **refund.message**. An Action RPC rechecks current state and uses business locks when its state check and effect must commit atomically.

Do not add projection-only locks. The Server atomically adds newly matched Action permissions to the Flow execution's Work Queue permission history. A historical match supports discovery; it does not prove current eligibility or caller authorization.

Keep external effects in `Execute`. `WaitFor` only declares durable conditions and must not query or mutate providers or Dex state.

For connectors:

1. search the canonical published catalog
   (`https://superdurable.github.io/dex-connectors-library/catalog.yaml`) with
   `connector_catalog` before selecting a connector or writing provider
   integration code;
2. match every required Trigger, Query, and Mutation capability by exact
   kind and name; a provider or connector name alone is not a match;
3. run the match's `install_command` (`go get <module>@<version>`), then read
   that release's `connector.yaml` and `zz_generated_connector.go` in the
   directory that `go list -m -f '{{.Dir}}' <module>` prints, for auth, configuration, input/output,
   branches, idempotency, Trigger, and generated Go package contracts;
4. declare the connection in `dex-app.yaml` with its `connectionName`,
   `connectorId`, `modulePath`, `version` (the one `go.mod` selects),
   `authMethodId` and `operations` or `triggerBindings`. If the catalog or that
   release's manifest cannot be read and verified, stop connector-dependent
   implementation and report the blocker; do not infer support from memory,
   a branch, or a provider SDK;
5. compose the Flow from the latest compatible released capabilities wherever
   possible: Query/Mutation factories become Connector Steps, a Trigger target
   starts a typed Flow or invokes a typed RPC, and an application RPC that
   requests provider work commits its state and schedules a Connector Step
   instead of calling the provider itself;
6. do not add an application-local provider client, webhook adapter, or direct
   official SDK call when the connector library already supplies the required
   capability;
7. give every operation-specific factory a static `ConnectionName` equal to
   the `dex-app.yaml` `connectionName` (it is required), and open the
   Connection in `internal/worker/worker.go` with the connector's generated
   `NewProjectConnection(configuration.Project(), connectionName)`; never write
   credential codecs or credential providers;
8. for a public external product with a documented API or official SDK, require
   dedicated Connector Trigger, Query, and Mutation capabilities as needed;
9. when that public connector or required operation/Trigger is absent or
   defective, report the exact capability gap to the user as a blocker; never
   bypass it with direct provider code;
10. never commit a branch, commit SHA, pseudo-version, `go.work`, or local
   replacement as a dependency; use exact released connector versions, or the
   release candidate (`vX.Y.Z-rc.N`) the platform published for a connector
   this Project contributed;
11. for an organization-controlled internal service, use a released official
   capability when one fits, such as the official `webhook` connector's
   verified inbound requests and signed outbound events; otherwise report the
   gap. `dex-app.yaml` accepts only official
   `github.com/superdurable/dex-connectors-library/connectors/...` modules, and
   the official library no longer has a generic HTTP connector.

Each Connector Step passes only its current operation result to a branch target. Use generated result aliases such as `ListThreadMessagesResult` or `PostThreadReplyResult`. Persist thread identity, customer input, recovery context, and other business state in an application Step and Attribute before entering the Connector Step. Map the provider operation input with the pure `MapToOperationInput`; never recover upstream context from a result envelope. Use `Annotations` only for graph grouping and explanation metadata. Omit `ResultAttribute` unless a display, RPC, audit, recovery operator, or another path must read the raw result outside the transition chain.

For every Trigger binding, give the generated factory a static connection name and binding name. Keep its matcher configuration separate from connection credentials. Supply an application-owned `TriggerFilter` and `FlowIDResolver`, then choose either `NewDexFlowTriggerTarget` with a typed Flow and `FlowInputMapper` or `NewDexRPCTriggerTarget` with a typed RPC and `RPCInputMapper`. The filter is the final admission rule for channel, sender, text, tenant, required routing fields, and other domain constraints. Returning false consumes the event without calling Dex. Filter, resolver, and mapper callbacks are pure functions without error results. Do not add manifest-level Flow/RPC Trigger kinds or a configurable RPC-name string.

For an RPC target, register the application's bound method with application-owned `dex.RPCOptions`, then pass that method directly to the target. The application decides whether redelivery needs deduplication, which bounded domain state records it, and which business state or effect requires a lock. Do not add connector-owned persistence or locks to every Trigger RPC.

Make connector mutations idempotent and reconcile unknown outcomes query-first. A missing connector release blocks production handoff. Never invent an unreleased connector API.

Use an authorization method declared by the exact connector release.
`dex-app.yaml` names one `authMethodId` per connection (the empty string for a
single-method manifest), and Studio Configuration accepts credentials only for
that method; application code must not hard-code OAuth, API-key, or
service-account fields outside that contract. Preview and Live load the
accepted ordinary snapshot through the official `projectconfig` loader in the
template's `internal/connectorconfiguration` package and resolve connection
credentials separately when a Connector Step runs, letting the Connector SDK
perform supported refresh; business code never handles tokens. Rotation takes
effect on the next Connector call without changing Flow state or rebuilding the
app.
