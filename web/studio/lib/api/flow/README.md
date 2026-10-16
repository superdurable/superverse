# Flow API documents and calls

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Flow API documents and calls: definition and run wire types, status and history readers, blobs, and one module per Flow API service, so no component builds a Flow API path.

Importers: `components/flow/**` (22), `components/teamspace/connections`, `lib/api/flow/draft`, `lib/flow`, `lib/project` and `mock/**` (2) import this directory.

Imports: it imports `lib/api/transport`, `lib/generated/protobuf` and `lib/shared` and calls the `TeamSpaceConnectionService` and Flow APIs.

```mermaid
flowchart LR
  self["lib/api/flow"]
  c0["components/flow/** (22)"] --> self
  c1["components/teamspace/connections"] --> self
  c2["lib/api/flow/draft"] --> self
  c3["lib/flow"] --> self
  c4["lib/project"] --> self
  c5["mock/** (2)"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/shared"]
  self --> e0(["TeamSpaceConnectionService API"])
  self --> e1(["Flow API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
