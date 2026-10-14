# Versioning Go applications

## Check first

```bash
go list -m github.com/superdurable/dex/sdk-go
go list -m -json github.com/superdurable/dex/sdk-go
```

The selected SDK source is authoritative; see [version and source authority](go.md#version-and-source-authority).

## Server protocol compatibility

The Go Worker obtains its diagnostic artifact version from Go build information. On startup it calls `GetServerInfo`, negotiates the highest common protocol, synchronizes Attribute indexes, and then binds WorkerService. A missing RPC, invalid interval, or disjoint interval fails startup before the listener opens. Superverse operates Dex Server; the application never upgrades it.

## Default Flow and Step type names

A Flow or Step registers under its Go type name without package or pointer: `*orders.OrderFlow` registers as `OrderFlow`, and `shipOrder{}` as `shipOrder`. Embed `dex.FlowDefaults` and the appropriate Step defaults to use these names. The package does not disambiguate: Flow types must be unique within one Registry, and Step types must be unique within their Flow. Name each Flow type after its domain, such as `OrderFlow` or `ApprovalFlow`, never just `Flow`. `dex.GetFinalFlowType` and `dex.GetFinalStepType` return the registered names for searches, tests, and Step execution IDs.

Implement `GetFlowType` or `GetStepType` only to keep an existing durable name that the default would change, or to name a generic Flow or Step type, which has no default; `NewRegistry` rejects a generic or unnamed type without an override. Return one non-empty compile-time string, as a literal or constant; an empty string selects the default. Do not override for display labels, naming style, or speculative future refactors.

The registered name is durable identity. An open Flow keeps the type it started with, and a Worker that registers a different name cannot dispatch it; there is no compatibility shim. Renaming a Go type changes the name, and so does adopting an SDK release whose "Breaking Changes" change the default naming rule. To keep open executions running, override only the affected method and return the exact existing string, taken from real executions rather than from the new Go name. Preserve an existing override that already owns a durable identity, and verify that an old execution resumes on the new Worker before deploying it to Live.

`dexcli visualize` derives the same names, so the generated definitions and Studio Design match the Worker when the SDK and CLI follow the same naming rule. It reports `generic_flow_type_name`, `generic_step_type_name`, or `dynamic_type_name` when it cannot know a name. A definition mismatch is not a reason to override every type; compare the definitions with the registered names and diagnose the SDK and CLI releases.

## Open-Flow compatibility

An open Flow can resume on new Worker code. Preserve Flow type, reachable Step type strings, registered schemas, and decodable payloads. Do not rename a Step as a refactor. Add a new Flow/Step type for incompatible behavior and route new starts there.

Additive fields with defaults are usually safer. Removing registered Steps, changing state types, repurposing enum values, or changing retry/terminal invariants is risky.

## Rollout

Live keeps its Flow executions across deployments, so a new Live build serves the open Flows that earlier builds started. Commit only code that can serve old and new runs; updating a process does not replace durable execution state.

Choose ID reuse and already-started behavior as product semantics. Test active and closed prior runs. Before rollout, start representative Flows on old code, stop at durable boundaries, replace the Worker, and complete them on new code. Include maps, Channels, Timer, RPC, retry, and timeout handler.
