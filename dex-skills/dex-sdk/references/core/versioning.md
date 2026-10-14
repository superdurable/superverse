# Versioning and open Flows

An open Flow is a durable contract between its recorded state and future Worker code. Treat stable Flow, Step, RPC, Attribute, Channel, Stream, and map names as data, not local refactoring details.

## Source authority

Read `go.mod` and `go.sum` first and follow the [version policy](../../SKILL.md#establish-source-authority). Validate exact signatures against the `go.mod`-selected SDK source. When an example in this bundle and the selected SDK disagree, the selected source and the compiler win.

## Server protocol compatibility

Server-to-SDK compatibility is based on inclusive protocol intervals, not artifact version ordering. Worker startup calls `GetServerInfo`, chooses the highest version in the interval intersection, synchronizes Attribute indexes, and only then binds WorkerService. A new SDK can use an older Server when it retains the old protocol. A new Server can use an older SDK while their intervals still overlap.

Treat `UNIMPLEMENTED`, invalid intervals, and disjoint intervals as startup failures. A breaking Server release raises its minimum protocol. Workers negotiate only at startup and do not renegotiate in place; in Superverse the platform operates and upgrades the Dex Server.

Protocol negotiation protects the Worker-to-Server API. It does not make open Flow type names, reachable Step graphs, schemas, or persisted payloads compatible. Apply both checks independently.

## Compatible changes

Prefer additive evolution:

- add a new Step type and route only new executions to it
- add optional Attributes, Channels, Streams, or RPCs
- retain old Step and RPC handlers while open executions can still reach them
- add a version Attribute or start-input field that selects the graph at a durable boundary
- deploy readers before writers when changing a serialized payload shape

Do not rename or remove a durable resource merely because application source no longer references it. Search for open executions and recorded paths first.

## Incompatible changes

When compatibility cannot be preserved, choose one explicit strategy: keep versioned handlers in one registry, migrate at a safe application boundary, or terminate/recover selected executions with user authorization. Superverse runs one Live build at a time and it serves every open execution, so there is no older Worker pool to finish them on. Do not add an unreviewed fallback that silently interprets old data as the new type.

Changing the SDK and changing the Flow model are separate decisions, but an SDK upgrade can change durable identity by itself: a release can change how default Flow and Step type names are derived (see [Go type names](../go/versioning.md#default-flow-and-step-type-names)). Read the release's "Breaking Changes", compare the registered type names with open executions, upgrade the dependency, regenerate or adapt compile-time code, and then test one execution started before deployment plus one started after deployment.

## Verification

- inspect open Flow types and active Steps before removing registrations
- compile or type-check every handler retained for old executions
- start on the old version, wait, replace the Worker, then complete on the new version
- verify payload and error compatibility through the actual codec
- verify timeout handlers, failure targets, and SubFlow options survive retry paths

Official operations guidance: https://docs.superdurable.io/production/application-operations#versioning-flow-code
