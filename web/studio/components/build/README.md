# Build surface

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The Flow and Interface surfaces: the Flow Definitions of the version shown, the Preview with its source, Start and Stop, problem, phases and history, and the automatic-builds switch.

Importers: `components/project`, `components/project/setup` and `mock/surface` import this directory.

Imports: it imports `components/flow/scope`, `components/frame/status`, `components/project/activity`, `components/ui`, `components/ui/problem`, `components/ui/timings`, `components/ui/wait`, `lib/api`, `lib/api/transport`, `lib/browser`, `lib/flow`, `lib/generated/protobuf`, `lib/project`, `lib/project/setup`, `lib/project/stage`, `lib/session`, `lib/session/design`, `lib/shared`, `lib/shared/problem` and `lib/shared/version`.

```mermaid
flowchart LR
  self["components/build"]
  c0["components/project"] --> self
  c1["components/project/setup"] --> self
  c2["mock/surface"] --> self
  self --> d0["components/flow/scope"]
  self --> d1["components/frame/status"]
  self --> d2["components/project/activity"]
  self --> d3["components/ui"]
  self --> d4["components/ui/problem"]
  self --> d5["components/ui/timings"]
  self --> d6["components/ui/wait"]
  self --> d7["lib/api"]
  self --> d8["lib/api/transport"]
  self --> d9["lib/browser"]
  self --> d10["lib/flow"]
  self --> d11["lib/generated/protobuf"]
  self --> d12["lib/project"]
  self --> d13["lib/project/setup"]
  self --> d14["lib/project/stage"]
  self --> d15["lib/session"]
  self --> d16["lib/session/design"]
  self --> d17["lib/shared"]
  self --> d18["lib/shared/problem"]
  self --> d19["lib/shared/version"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
