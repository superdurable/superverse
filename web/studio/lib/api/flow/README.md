# Flow API documents and calls

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Flow API documents and calls: definition and run wire types, status and history readers, blobs, and one module per Flow API service, so no component builds a Flow API path.

Importers: `components/flow/canvas`, `components/flow/canvas/model`, `components/flow/connections`, `components/flow/connections/model`, `components/flow/connections/row`, `components/flow/connector/manifest`, `components/flow/connector/native`, `components/flow/connector/team-space`, `components/flow/design`, `components/flow/diagnostics`, `components/flow/diagnostics/event`, `components/flow/diagnostics/graph`, `components/flow/diagnostics/timeline`, `components/flow/diagnostics/value`, `components/flow/map`, `components/flow/run`, `components/flow/run/detail`, `components/flow/run/start`, `components/flow/run/step`, `components/flow/scope`, `components/teamspace/connections`, `lib/api/flow/draft`, `lib/flow`, `lib/project`, `mock` and `mock/fixture` import this directory.

Imports: it imports `lib/api/transport`, `lib/generated/protobuf` and `lib/shared` and calls the `TeamSpaceConnectionService` and Flow APIs.

```mermaid
flowchart LR
  self["lib/api/flow"]
  c0["components/flow/canvas"] --> self
  c1["components/flow/canvas/model"] --> self
  c2["components/flow/connections"] --> self
  c3["components/flow/connections/model"] --> self
  c4["components/flow/connections/row"] --> self
  c5["components/flow/connector/manifest"] --> self
  c6["components/flow/connector/native"] --> self
  c7["components/flow/connector/team-space"] --> self
  c8["components/flow/design"] --> self
  c9["components/flow/diagnostics"] --> self
  c10["components/flow/diagnostics/event"] --> self
  c11["components/flow/diagnostics/graph"] --> self
  c12["components/flow/diagnostics/timeline"] --> self
  c13["components/flow/diagnostics/value"] --> self
  c14["components/flow/map"] --> self
  c15["components/flow/run"] --> self
  c16["components/flow/run/detail"] --> self
  c17["components/flow/run/start"] --> self
  c18["components/flow/run/step"] --> self
  c19["components/flow/scope"] --> self
  c20["components/teamspace/connections"] --> self
  c21["lib/api/flow/draft"] --> self
  c22["lib/flow"] --> self
  c23["lib/project"] --> self
  c24["mock"] --> self
  c25["mock/fixture"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/shared"]
  self --> e0(["TeamSpaceConnectionService API"])
  self --> e1(["Flow API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
