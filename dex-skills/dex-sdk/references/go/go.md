# Go handbook

Use this page first for a Go application. It describes the application boundary and points to focused pages; always check the version selected by `go.mod` and `go.sum`.

## Version and source authority

Dex Server is backward compatible with every earlier SDK release, and the platform keeps its Dex Server at the newest stable release. The application is not pinned to a platform-chosen Dex Go SDK, Connector SDK, connector or Dex CLI version; `go.mod` records the minimum versions it builds with. Run `go list -m github.com/superdurable/dex/sdk-go` to see the selected SDK. Move a module to a newer stable release when useful (`go get <module>@latest`) after reading that release's "Breaking Changes" section, and keep `go.mod`, `go.sum` and `dex-app.yaml` connector versions consistent. Use released versions only: no prereleases, pseudo-versions or `replace` directives. If a newer SDK makes the Connector SDK or a connector fail to compile, keep the newest SDK that compiles with them. The selected module source (its directory: `go list -m -f '{{.Dir}}' <module path>`) and the compiler win over any excerpt here. Names on these pages are those of current SDK releases, such as `*dex.FlowNotActiveOrNotFoundError`; use the names the selected SDK defines.

## Project shape and local run

Keep Flow and Step definitions in application packages, compose them in one Registry, and create one shared Registry and BlobCache for Client and Worker. Inject business services into Flow constructors. In the template, each Flow lives in its own `internal/process/<name>_flow.go`, `internal/worker/worker.go` registers every Flow and owns the Registry, BlobCache, Worker, and Client, and `cmd/server/main.go` serves the API and UI.

Studio Preview runs the application and its Worker on the Sandbox's Preview engine, and Live runs the build of the committed source; Superverse sets `DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, and `DEX_WORKER_TARGET` for both. In the Sandbox the application runs only as the Preview ([local environment](../core/getting-started.md#local-environment)). `DEX_WORKER_TARGET` is the address advertised to Dex and can differ from the bind address; with `DEX_WORKER_HEADLESS=true`, which Live sets, the template advertises it as headless (`dex.WorkerTarget{Headless: true}`) so Dex follows it to each new Live version ([operations](../core/operations.md#deploy-dex-server-components)).

## Connector configuration

Studio Configuration owns connector connections and credentials. Preview and Live supply the project configuration (`DEX_PROJECT_*`), which the template's `internal/connectorconfiguration` loads with the Connector SDK's `sdkgo/projectconfig` before any client or Worker starts. Open each connection declared in `dex-app.yaml` with the connector's generated `NewProjectConnection(configuration.Project(), connectionName)`, and set the same static `ConnectionName` on every operation-specific factory config; it is required. Applications never write credential codecs or credential providers.

Configuration is captured when it loads. Credentials are reread for each provider call, so reauthorization does not require a restart; configuration changes do. Keep secret values outside Flow state and logs.

Connector Step branch targets receive only the current generated result alias. Query aliases resolve to `sdkgo.QueryResult[OUT]`; Mutation aliases resolve to `sdkgo.MutationResult[OUT]`. `MapToOperationInput` is a pure mapping from the current application Step input into provider input; it has no error result, and the application input is not present in the operation result. Persist business context with an application Step and Attribute before the Connector call, then reload it in success, failure, RPC, and recovery Steps. Use `Annotations` for graph group and explanation metadata; do not use it for visual styling.

`ResultAttribute` is optional for every Query and Mutation. Omit it when only the branch target needs the operation result. Configure and register the precisely typed Attribute when an RPC, display, audit, recovery operator, or another path must read the raw result outside the transition chain.

Provider-neutral Triggers run outside Steps. Give each generated Trigger factory a static binding name, then pass an application-owned target. `NewDexFlowTriggerTarget` takes the typed Flow, a `TriggerFilter`, a stable `FlowIDResolver`, and a `FlowInputMapper`. `NewDexRPCTriggerTarget` takes the application's typed RPC, a filter, the same identity resolver, and an `RPCInputMapper`; no string RPC name is configured.

The filter runs before identity resolution, input mapping, or any Dex call. Return false to consume an event that does not satisfy the application's channel, sender, message, tenant, authorization, routing-field, or other domain rule. Filter, resolver, and mapper callbacks are deterministic, side-effect-free pure functions without error results. A blank or invalid resolved Flow ID is an application defect. Provider binding matchers reduce traffic but do not replace this application admission boundary.

For RPC delivery, register the application's bound method with application-owned `dex.RPCOptions`, then pass that method directly to `NewDexRPCTriggerTarget`. The application decides whether stable provider event IDs need deduplication, stores only bounded domain state, and locks only the business state or effect that must commit atomically.

For complete Trigger wiring, read `examples/thread-approval` in the Slack connector module and `examples/thread-reply` in the Gmail connector module (directory: `go list -m -f '{{.Dir}}' <connector module>`). They load a local development store; a Superverse application keeps the template's project configuration loader.

## Hosted configuration and recovery boundary

Preview and Live follow the [Core native project configuration contract](../core/operations.md#native-hosted-project-configuration). Keep project scope and credentials behind that runtime boundary; a browser, Flow input, or ordinary Attribute cannot select secret storage. For recovery after a Worker fix, preserve the original resource and Flow ID, reconcile external effects, and use an explicitly authorized, recorded Step boundary as described in [safe recovery](../core/operations.md#safe-recovery).

## FDG 2.0 management metadata

The FDG 2.0 management-interface analyzer (`dexcli visualize`) reads Go source. Go Flow definitions can publish Indexed Attribute presentation, Summary and Display RPC fields, Action metadata, typed Action forms, conditions, permissions, groups, explanations, and user-string `capture:qr-code` hints. Superverse Studio consumes these definitions for Design, Start Flow, Display, Actions, and Inbox. `make check-fdg-v2`, part of `make check-static`, validates every Flow listed in `dex-app.yaml`; see [FDG authoring](../../../dex-app-builder/references/fdg-authoring.md).

## Minimal Flow

Declare schema at package scope, embed defaults, register Step types, and return a decision from every Execute method. Keep the default Flow and Step type names (the Go type name without its package); implement `GetFlowType` or `GetStepType` only for the [documented exceptions](versioning.md#default-flow-and-step-type-names).

[Runnable example in the Dex repository](https://github.com/superdurable/dex/blob/main/examples/go/primitives/flow/workflow.go)
```go
var (
	Status = dex.DefineAttribute[string]("status")
	Notify = dex.DefineChannel[dex.None]("notify")
)

type ExampleFlow struct {
	dex.FlowDefaults
}

func NewExampleFlow() *ExampleFlow {
	return &ExampleFlow{}
}

func (*ExampleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(ExampleStep{}),
		dex.DefineStep(Finish{}),
	}
}
```

Use `dex.None` for nil-only input/output and concrete structs for durable payloads.

A no-wait Step from the same example embeds the input-typed default and returns a durable decision:

```go
type Finish struct {
	dex.StepDefaultsNoWaitFor[int]
}

func (Finish) Execute(ctx dex.Context, input int) (*dex.StepDecision, error) {
	if err := Status.Set(ctx, "done"); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(input + 1), nil
}
```

## Registry, Worker, and Client

Construct every Flow once, then pass the same definitions into the Registry used by Worker and Client. The template's `internal/worker/worker.go` shows the bootstrap: one `dex.NewRegistry` call, one BlobCache, and a Worker and Client built from both. Controllers start with `client.StartFlow(ctx, flow, flowID, input, options)` and retain the run ID for diagnostics.

## Route by task

- Syntax/state semantics: [primitives](primitives.md)
- Application shape: [patterns](patterns.md)
- Real-server verification: [testing](testing.md)
- Client boundary design and retry ownership, before implementation: [error handling](error-handling.md)
- Persistence/maps/Streams: [data handling](data-handling.md)
- Histories/progress: [observability](observability.md)
- Open-Flow compatibility: [versioning](versioning.md)
- Go traps: [gotchas](gotchas.md)
- Locks/loading/heartbeat/cancellation: [advanced features](advanced-features.md)

## Completion checklist

Confirm every Step type and persistent definition is registered, errors are returned, Client calls have deadlines, tests use unique IDs and a real Dex Server, and application code does not depend on Dex Server internals.
