# Dex Connector Architecture v0

Dex Connector is an open-source full-stack integration layer for Dex ecosystem process applications and durable agents. It is not a generic API wrapper or a node-based automation library.

Version 0 supports application integration only from a Go backend using the
Dex Go SDK and Connector Go SDK. The application's React frontend calls the
application API; it is not a Connector SDK backend. Dependency versions follow
the version policy in [workspace bootstrap](workspace-bootstrap.md).

## Capability model

A connector may expose five capabilities:

- **Auth**: authorize and identify a logical connection.
- **Trigger**: translate an external event into a provider-neutral typed event.
- **Query**: read external state from a Step or agent tool.
- **Mutation**: change external state with idempotency and recovery.
- **UI**: configuration units for non-secret Step or Trigger settings. Superverse Studio renders supported units itself; applications load no connector UI bundle.

Each connector declares its complete capabilities in a machine-readable
`connector.yaml` at its module root. Release automation aggregates the released
connectors into the published catalog at
`https://superdurable.github.io/dex-connectors-library/catalog.yaml`.

## Released capability discovery

Treat the `connector_catalog` tool as the mandatory first source for
application selection. It searches the published catalog, which contains only
released connectors, and returns each match's ID, directory, version, Query and
Mutation operations, Triggers, `install_command`, `module`,
`manifest_path` and `factory_path`. Do not select a connector from memory,
repository search, or the presence of a provider name alone.

For every external integration:

1. Search by capability or provider terms; pass `operation` to verify one exact
   operation name.
2. Match each application requirement to an exact catalog capability name and
   kind: Trigger, query, or mutation. Record missing capabilities separately;
   one matching operation does not imply that the rest exist.
3. Run the match's `install_command` (`go get <module>@<version>`), then read
   `connector.yaml` and `zz_generated_connector.go` in the directory that
   `go list -m -f '{{.Dir}}' <module>` prints for the match's `module`. Confirm the
   configuration fields, auth methods, input and output types, branches,
   idempotency, execution policy, Trigger schema, and generated Go package
   before writing application code.
4. Record the connector ID, module path, version, and exact capabilities in the
   connector capability matrix.

Declare each connection in the `dex-app.yaml` `connectors` list with
`connectionName` (a static name you choose), `connectorId`, `modulePath` (the
exact published module; the short connector ID does not determine it),
`version` (the version `go.mod` selects), `authMethodId`, and the `operations`
and/or `triggerBindings` (`triggerName`, `bindingName`) the application uses.
`commit_and_push` rejects a missing field or a module outside
`github.com/superdurable/dex-connectors-library/connectors/`, and
`make check-fdg-v2` rejects a version that differs from `go.mod`.

Superverse authorizes exactly one auth method per connection, and Studio
Configuration accepts credentials only for that method. Use `""` when the
manifest has no `auth.methods` list. Otherwise use one exact
`auth.methods[].id`: the manifest's `auth.defaultMethod` unless the user asked
for another listed method (for `llm`, the provider: `openai`, `anthropic`, or
`gemini`). Do not ask the user which account, mailbox, sender, or key to use:
the user enters credentials for that method in Studio Configuration, and the
connected account determines provider identity (Gmail, for example, sends from
the connection's verified primary address).

If `connector_catalog` or the module download fails, stop connector-dependent
implementation and report the blocker. Do not fall back to the repository's
`main` branch, install the provider's SDK, create an application-local webhook,
or claim that a connector capability exists.

## Runtime boundary

Credentials belong to Connector Runtime, never Flow state, IDs, logs, browser code, or generated values. A Flow stores a logical connection ID and domain correlation data.

Every operation-specific factory also declares a static `ConnectionName`. It must equal the `dex-app.yaml` `connectionName` and the runtime Connection's name; the generated factory panics on a mismatch. Studio Configuration identifies a connection by connector ID plus connection name.

A Connector Step emits only the current Query or Mutation result. Generated aliases such as `ListThreadMessagesResult` and `PostThreadReplyResult` are non-recursive aliases of `sdkgo.QueryResult` or `sdkgo.MutationResult`. `MapToOperationInput` is a pure mapping from the current application Step input into provider input; it does not return an error, and the application input is not copied into the result. Persist domain context in an application-owned Attribute before invoking the Connector Step. Use `Annotations` only for graph group and explanation metadata.

Give every wired branch a deliberate, visible outcome. A Step that receives several branches of one Connector Step switches on `result.Branch`, or failure branches route to their own Step, so a failure branch never looks like success; the template's `TextSummaryFlow` routes its failure branches to `RecordSummaryFailure`. A Query output with a next-page field, such as github `listReleases` and its `NextPage`, is one page: read every page with the [paging recipe](../../dex-sdk/references/go/patterns.md#page-a-connector-query), never only the first, and do not assume provider order.

`ResultAttribute` is optional for every Query and Mutation. Omit it when the branch target is the only consumer because the same result already crosses the durable Step transition. Configure and register the typed Attribute only when an RPC, display, audit, recovery operator, or another path must read the raw provider result outside that transition chain.

Trigger processing verifies and normalizes the inbound event, preserves its stable provider event ID, and delivers it at least once. The connector does not classify a Trigger as Flow-start or RPC delivery.

The application supplies a `TriggerFilter` and `FlowIDResolver`. It chooses `NewDexFlowTriggerTarget` with a typed Flow and `FlowInputMapper` to start a Flow, or `NewDexRPCTriggerTarget` with a typed RPC definition and `RPCInputMapper` to invoke an existing Flow. Never route through a configurable RPC-name string or require an application RPC to accept the raw provider event.

The application filter runs before Flow ID resolution, input mapping, or a Dex call. Returning false consumes an irrelevant event. Filter, resolver, and mapper callbacks are deterministic, side-effect-free pure functions without error results. Reject events missing routing fields in the filter; a blank or invalid resolved Flow ID is an application defect. Provider matcher configuration can reduce inbound traffic, but the application filter remains the final admission boundary for channel, sender, message content, tenant, authorization, and other domain rules.

Flow starts reuse the provider event ID as the request ID. The resolved Flow ID owns root-event deduplication. For RPC delivery, register the application's bound method with application-owned `dex.RPCOptions` and pass that method directly to the target. The application owns redelivery policy, bounded deduplication state when needed, and locks for the exact business state or effect that must commit atomically.

Query and Mutation run from `Execute`. Mutation supplies a stable idempotency key. If the result is uncertain, persist that outcome and query before retrying. Do not let Trigger source handlers mutate Dex primitives directly.

A Query can still be metered. When the provider offers neither idempotency nor
outcome reconciliation, use one dispatch attempt for a billable operation and
record an uncertain result as UNKNOWN. Do not blindly repeat a paid request
because its response was lost; an explicit rerun creates a new business
operation identity. Inspect the released factory's execution options instead
of assuming its retry defaults satisfy this boundary.

## Application composition

Prefer an existing released connector capability over application-local
provider code at every external boundary.

| Application need | Connector capability | Composition |
| --- | --- | --- |
| Provider read or write during a Flow | Query or Mutation | Use the operation-specific factory as a Connector Step; the provider call runs in `Execute`. |
| External event starts work | Trigger | Bind `NewDexFlowTriggerTarget` to the typed Flow, filter, stable Flow ID resolver, and input mapper. |
| External event updates existing work | Trigger | Bind `NewDexRPCTriggerTarget` to the registered typed RPC, filter, Flow ID resolver, and input mapper. |
| User/API RPC requests provider work | Query or Mutation plus application RPC | The RPC validates and commits application state, then returns a movement to a Connector Step; it does not call the provider. |
| Provider-aware setup | Auth and UI | Studio Configuration renders the manifest's connection form and the Step or Trigger configuration units the Flow declares; the application supplies static connection and binding names. |

Inspect the selected release's `connector.yaml` for the exact operation or
Trigger; the presence of the provider's connector alone does not prove that the
required capability exists. Do not bypass a suitable connector with a direct
provider SDK, custom webhook, generic HTTP call, or application-specific
credential store.

## Text generation

Every text generation Step uses the `llm` connector (module
`github.com/superdurable/dex-connectors-library/connectors/superdurable/llm`,
package `llm`), as the template's `TextSummaryFlow` does. One connection serves
one provider: Studio Configuration owns its `provider` (openai, anthropic,
gemini, qwen, deepseek, meta, mistral, kimi or xai), its `api_key` and its
default `model`, so a provider or model change is a configuration change, not a
code change. Its `dex-app.yaml` entry uses `"authMethodId": ""`. Open it like
any other connection, with `llm.NewProjectConnection(project, name)`, and build
Steps with `llm.NewGenerateTextStep`; the request and message types come from
`github.com/superdurable/dex-connectors-library/sdkgo/textgen`. Use OpenAI's or
Gemini's own connector only for their provider-native operations
(`createResponse`, `generateContent`).

| User intent | Composition |
| --- | --- |
| Generic, such as "summarize with an LLM" | Leave `Model` empty, as `TextSummaryFlow` does; the connection's model applies, or the provider's default when it has none. |
| Named model, such as "use Claude Opus" | Leave `Model` empty and tell the user to choose provider `anthropic` and that model in Studio Configuration. Set `Model` in code only when the user requires a fixed model, and then use the exact provider model ID, never prefixed with the provider. |
| Several providers in one application | One `llm` connection per provider, each with its own connection name. |

Run each model call as its own `llm` Step, never from inside another Step's
`Execute`: the Step's result carries the call's token usage, so the run's
history shows what each run spent on models.

Keep requests portable by leaving `Temperature` and `ReasoningEffort` unset and
`MaxOutputTokens` zero or generous. Do not add a per-run model to start input
unless the user asks, because anyone who can start the Flow could then choose,
and bill, any model of the connection's provider. Model provider failover as
Flow branches over separate connections. The selected `llm` module's
`connector.yaml` and README give the exact API; the README's Dex Web setup
steps correspond to Studio Configuration in Superverse.

## UI and live state

Superverse applications embed no connector UI: Studio Configuration renders connector forms and units. Browser code uses the application API, which calls typed Flow RPCs.

Use a durable snapshot RPC for canonical state. Streams may improve live presentation but are best-effort; reconnect through the durable snapshot rather than treating retained Stream data as authoritative.

## Hosted configuration and credential boundary

In Superverse, Studio Configuration and the Superverse configuration host own
project configuration and credentials. The host validates settings and
credentials against the exact released manifests and stores ordinary settings
separately from credentials with the Connector SDK's `projectconfig` protocol.
Dex Server runs only the engine (`api,interpreter`) and has no configuration
UI. `dex-app.yaml` holds the non-secret connection requirements, from which
Superverse derives the Release connector and environment contracts.
Configuration values, API keys, OAuth tokens, and webhook secrets never belong
in source, generated artifacts, or Flow state.

`projectconfig/environment.go` and `projectconfig/README.md` in the selected
Connector SDK module
(directory: `go list -m -f '{{.Dir}}' github.com/superdurable/dex-connectors-library/sdkgo`)
describe this boundary; that source is the authority for exact signatures.

For Preview and Live, Superverse sets the trusted `DEX_PROJECT_*` deployment
variables (project, scope, Preview Session, exact configuration key, version
and digest, and storage location) and `AWS_REGION`. They are deployment
identity, not application settings. The template's
`internal/connectorconfiguration.Load` calls `projectconfig.LoadFromEnvironment`,
which reads only the accepted ordinary snapshot without credential reads or
provider refresh, then resolves the accepted application environment with
`LoadedProject.ResolveApplicationEnvironment` and applies it before the Worker,
clients, HTTP server, or goroutines start. Keep that order.

Open every connection that `dex-app.yaml` declares with the connector's
generated `NewProjectConnection(project, connectionName)`, where `project` is
`configuration.Project()` of the template's `connectorconfiguration.Load`.
That one call decodes the connection settings,
binds the credentials and builds the client; it is all the connection code an
application needs. Run a connector's Trigger bindings with its one project
runner, such as Gmail's `NewProjectMessageTriggerRunner` or Typeform's
`NewProjectResponseSubmittedEndpointRunner` (its connector README names it): it
loads each binding's stored configuration, fails when one is missing, and keeps
acknowledged events in the binding's durable project inbox. A webhook runner is
also the `http.Handler` the application mounts at the provider's webhook URL. Set
every generated Step and Trigger config's `ConnectionName` to the same static
connection name: it is required, and a missing or different name fails when the
Flow is built. Load a Step's own configuration only with
`provider.LoadOperationConfiguration` for the exact Flow/Step operation
configuration declared in FDG; an absent value matches
`projectconfig.ErrObjectNotFound`. A Trigger binding is identified by connector
ID, connection name, Trigger name, and binding name; its provider matcher
belongs to that binding, not to the reusable connection.

Never write credential codecs, call credential provider constructors, decode
connection settings into a connector's `Config` yourself, read auth-method,
token-refresh, or provider-client internals, serialize `Credentials` (secret
wrappers reject it), invent field names, or treat a missing key as usable
credentials.

The `projectconfig/provider` adapter resolves current credentials at actual use
and performs any refresh under durable admission. Business code never reads
credential objects or implements refresh, token exchange, or token persistence.
No token or raw provider failure belongs in a Flow or log.

Ordinary settings, and the TeamSpace connection a connection name selects, are
fixed when the application loads them; an edit or a new selection takes effect at
the next Preview start or Live deployment. Credentials are reread at
actual use, so a replaced key or reauthorization needs no restart. Preview
credentials are Session-scoped; a Live key or OAuth change affects later uses by
every deployed build. Missing configuration or a terminal refresh failure
surfaces as deployment-blocked or reauthorization-required state. Missing
credentials do not block a complete source commit; real provider behavior is
verified in Preview after the user completes Studio Configuration.

## AI agents

Agents use the same typed Query and Mutation capabilities as deterministic Steps. Dex owns durable state, approval waits, retries, reconciliation, and cleanup. Connector code does not become a second workflow engine.

## Provider classification

Every interaction with an external provider uses a dedicated connector. This
includes Trigger, Query, and Mutation. The connector library no longer publishes
a generic HTTP connector, and Superverse accepts only official connector
modules, so there is no generic connector escape hatch for external SaaS APIs.

An application-owned local artifact, such as a generated HTML file, is not a
provider interaction and needs no connector. Write it from `Execute`
idempotently (a deterministic path that a retry can safely overwrite), keep the
durable copy in an Attribute (Dex offloads large values to blob storage), and
serve it through the application API or a Display field; a file path inside the
application's container is not reachable from Studio.

## Reuse and contribution

Search released connectors with `connector_catalog` first. Reuse the latest
compatible released dedicated connector and every matching operation and
Trigger before writing application-local integration code.

For a public external product with a documented API or official SDK, a missing
connector, operation, or Trigger is a connector contribution, not permission to
call the provider directly from the application. Report the exact capability
gap to the user as a blocker. Never commit a branch, commit SHA, pseudo-version,
`go.work`, or local replacement as a dependency.

## Internal connector library decision

Superverse's `dex-app.yaml` accepts only official modules under
`github.com/superdurable/dex-connectors-library/connectors/`, and Studio
Configuration configures only those, so a Superverse application cannot use an
internal or private connector library; the removed generic HTTP connector is
not in the catalog either. For an organization-controlled internal service,
check `connector_catalog` for an official connector that fits, such as
`webhook` for verified inbound requests and signed outbound events. Keep
credentials in Studio Configuration, provider calls in Connector Step
`Execute`, and the same idempotency, uncertainty, and retry requirements.
Otherwise report the gap to the user as a blocker and record the resulting
reuse limitation in the handoff.

## Released Trigger examples

Read `examples/thread-approval` in the Slack connector module (`github.com/superdurable/dex-connectors-library/connectors/slack`) for a Socket Mode root event, application-owned channel/poster/text filters, thread query, typed reply RPC, thread-reply Mutation, and composed configuration UI units.

Read `examples/thread-reply` in the Gmail connector module (`github.com/superdurable/dex-connectors-library/connectors/google/gmail`) for a polled root message, application-owned sender/text filters, message query, typed reply RPC, email-reply Mutation, and composed configuration UI units. Its polling transport is a local alpha path, not a production push-delivery design.

Find the module directory with `go list -m -f '{{.Dir}}' <module path>` after adding the module with `go get`. Both examples derive one stable Flow ID from provider thread identity. They absorb root redelivery through deterministic starts, handle reply redelivery in bounded application-owned thread state, and move uncertain or rejected external writes into explicit recovery instead of blind resend. They load the local development store and Dex Web binding configuration; a Superverse application keeps the template's project configuration loader.

## Optional application connections

AppManifest `connectors[].optional: true` permits Preview and Live without a
selection for that connection. Omitted or false remains required. Use optional
only when the rest of the application meaningfully works without the feature.
Selected optional connections still require valid configuration and authorization.
The released connector opener may require settings at startup: check typed
absence from the loaded immutable Project configuration before opening it, keep
a typed availability fact, and route the unavailable feature to a durable
`connection_not_configured` notice ("Connect <name> to use this feature") before
any provider call. Carry the typed absence cause through the existing durable
Step result; a result Step must not reinterpret it from replacement-Worker
configuration. Never branch on error text or suppress malformed selected
settings. Keep credentials in Studio. The text-summary template demonstrates
typed absence routing; generic generated SDK support is separate work.

An unrelated Studio configuration gap never replaces a requested source edit.
Finish the edit and its source gate, call commit_and_push with blocked=false when
source is complete, and report the exact connection and user action separately.
Do not claim configured provider or Preview acceptance until it succeeds.
