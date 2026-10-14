# Flow diagnostics

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The Diagnostics pages of a Flow and one run: overview, live state, Streams, the run record, and the Stop, Time Travel and channel operation dialogs.

Importers: `components/flow/run/detail`, `components/flow/run/step` and `components/flow/scope` import this directory.

Imports: it imports `components/flow/diagnostics/event`, `components/flow/diagnostics/graph`, `components/flow/diagnostics/timeline`, `components/flow/diagnostics/value`, `components/flow/frame`, `components/ui`, `components/ui/menu`, `components/ui/pane`, `lib/api/flow`, `lib/api/transport`, `lib/browser`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/diagnostics"]
  c0["components/flow/run/detail"] --> self
  c1["components/flow/run/step"] --> self
  c2["components/flow/scope"] --> self
  self --> d0["components/flow/diagnostics/event"]
  self --> d1["components/flow/diagnostics/graph"]
  self --> d2["components/flow/diagnostics/timeline"]
  self --> d3["components/flow/diagnostics/value"]
  self --> d4["components/flow/frame"]
  self --> d5["components/ui"]
  self --> d6["components/ui/menu"]
  self --> d7["components/ui/pane"]
  self --> d8["lib/api/flow"]
  self --> d9["lib/api/transport"]
  self --> d10["lib/browser"]
  self --> d11["lib/flow"]
  self --> d12["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
