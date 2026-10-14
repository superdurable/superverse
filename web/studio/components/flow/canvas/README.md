# Flow canvas

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Draws a Flow Definition on a React Flow canvas, by stage or by Step, with its controls, legend and Step panel, optionally overlaid with one run.

Importers: `components/flow/design`, `components/flow/run`, `components/flow/run/detail` and `components/flow/scope` import this directory.

Imports: it imports `components/flow/canvas/camera`, `components/flow/canvas/layout`, `components/flow/canvas/model`, `components/flow/canvas/render`, `components/flow/frame`, `components/ui`, `components/ui/pane`, `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/canvas"]
  c0["components/flow/design"] --> self
  c1["components/flow/run"] --> self
  c2["components/flow/run/detail"] --> self
  c3["components/flow/scope"] --> self
  self --> d0["components/flow/canvas/camera"]
  self --> d1["components/flow/canvas/layout"]
  self --> d2["components/flow/canvas/model"]
  self --> d3["components/flow/canvas/render"]
  self --> d4["components/flow/frame"]
  self --> d5["components/ui"]
  self --> d6["components/ui/pane"]
  self --> d7["lib/api/flow"]
  self --> d8["lib/api/transport"]
  self --> d9["lib/flow"]
  self --> d10["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
