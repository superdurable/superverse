# components/project/activity

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The per-environment timeline and the recent trigger events: Preview's starts, Live's publishes and deployments, each with version, outcome and problem.

Importers: `components/build`, `components/project`, `components/publishing` and `mock/surface` import this directory.

Imports: it imports `components/publishing/timings`, `components/ui`, `components/ui/problem`, `components/ui/reveal`, `components/ui/timings`, `lib/api`, `lib/api/transport`, `lib/browser`, `lib/flow`, `lib/generated/protobuf`, `lib/project/setup`, `lib/shared`, `lib/shared/problem` and `lib/shared/version`.

```mermaid
flowchart LR
  self["components/project/activity"]
  c0["components/build"] --> self
  c1["components/project"] --> self
  c2["components/publishing"] --> self
  c3["mock/surface"] --> self
  self --> d0["components/publishing/timings"]
  self --> d1["components/ui"]
  self --> d2["components/ui/problem"]
  self --> d3["components/ui/reveal"]
  self --> d4["components/ui/timings"]
  self --> d5["lib/api"]
  self --> d6["lib/api/transport"]
  self --> d7["lib/browser"]
  self --> d8["lib/flow"]
  self --> d9["lib/generated/protobuf"]
  self --> d10["lib/project/setup"]
  self --> d11["lib/shared"]
  self --> d12["lib/shared/problem"]
  self --> d13["lib/shared/version"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
