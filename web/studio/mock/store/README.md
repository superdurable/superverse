# Mock store

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The mock's in-memory store and its clock: seed data, accounts, the mock Agent's state and scripts, configuration drafts, Releases, builds and Live, phase timings and resource snapshots.

Importers: `app`, `mock`, `mock/backend`, `mock/store/attachments`, `mock/store/delivery`, `mock/store/tasks`, `mock/store/tenancy` and `mock/surface` import this directory.

Imports: it imports `lib/generated/protobuf`, `lib/server`, `lib/shared`, `lib/shared/problem`, `mock/fixture`, `mock/store/scenarios`, `mock/store/setup` and `mock/store/validation`.

```mermaid
flowchart LR
  self["mock/store"]
  c0["app"] --> self
  c1["mock"] --> self
  c2["mock/backend"] --> self
  c3["mock/store/attachments"] --> self
  c4["mock/store/delivery"] --> self
  c5["mock/store/tasks"] --> self
  c6["mock/store/tenancy"] --> self
  c7["mock/surface"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/server"]
  self --> d2["lib/shared"]
  self --> d3["lib/shared/problem"]
  self --> d4["mock/fixture"]
  self --> d5["mock/store/scenarios"]
  self --> d6["mock/store/setup"]
  self --> d7["mock/store/validation"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
