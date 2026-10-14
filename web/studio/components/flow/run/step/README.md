# Run step actions

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: What a reader may do at the clicked Step of a run: open its Inbox item, Time travel, ask the agent, or fix a Live failure on Preview.

Importers: `components/flow/run/detail` imports this directory.

Imports: it imports `components/flow/diagnostics`, `components/flow/frame`, `components/ui`, `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/run/step"]
  c0["components/flow/run/detail"] --> self
  self --> d0["components/flow/diagnostics"]
  self --> d1["components/flow/frame"]
  self --> d2["components/ui"]
  self --> d3["lib/api/flow"]
  self --> d4["lib/api/transport"]
  self --> d5["lib/flow"]
  self --> d6["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
