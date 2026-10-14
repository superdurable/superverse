# Resizable panes

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Resizable panes: the pixel resize handle with its rail and remembered width, the list column, and the drag and keyboard gestures they share.

Importers: `components/flow/canvas`, `components/flow/diagnostics`, `components/flow/frame`, `components/flow/run`, `components/frame` and `components/shell` import this directory.

Imports: it imports `components/ui` and `lib/browser`.

```mermaid
flowchart LR
  self["components/ui/pane"]
  c0["components/flow/canvas"] --> self
  c1["components/flow/diagnostics"] --> self
  c2["components/flow/frame"] --> self
  c3["components/flow/run"] --> self
  c4["components/frame"] --> self
  c5["components/shell"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/browser"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
