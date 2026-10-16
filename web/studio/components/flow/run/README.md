# Runs and Inbox

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The Runs and Inbox views: the run list column with its health strip, Filter and events slots, run groups, the run search and the hook that routes Flow types.

Importers: `components/flow/run/tryit` and `components/flow/scope` import this directory.

Imports: it imports `components/flow/canvas`, `components/flow/canvas/model`, `components/flow/frame`, `components/flow/run/detail`, `components/flow/run/start`, `components/ui`, `components/ui/badge`, `components/ui/menu`, `components/ui/pane`, `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/run"]
  c0["components/flow/run/tryit"] --> self
  c1["components/flow/scope"] --> self
  self --> d0["components/flow/canvas"]
  self --> d1["components/flow/canvas/model"]
  self --> d2["components/flow/frame"]
  self --> d3["components/flow/run/detail"]
  self --> d4["components/flow/run/start"]
  self --> d5["components/ui"]
  self --> d6["components/ui/badge"]
  self --> d7["components/ui/menu"]
  self --> d8["components/ui/pane"]
  self --> d9["lib/api/flow"]
  self --> d10["lib/api/transport"]
  self --> d11["lib/flow"]
  self --> d12["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
