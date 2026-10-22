# Agent model view

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Maps every catalog AgentModel enum explicitly to its Studio literal and back, preserving historical values and recognizing unavailable identities.

Importers: `components/project`, `components/session`, `components/session/executor` and `lib/session` import this directory.

Imports: it imports `lib/api`, `lib/api/transport`, `lib/generated/protobuf` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/session/model"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  c2["components/session/executor"] --> self
  c3["lib/session"] --> self
  self --> d0["lib/api"]
  self --> d1["lib/api/transport"]
  self --> d2["lib/generated/protobuf"]
  self --> d3["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
