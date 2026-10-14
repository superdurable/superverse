# Execution graph

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Reconstructs and draws a run's execution graph from its history events, without applying a newer Flow Definition.

Importers: `components/flow/diagnostics` imports this directory.

Imports: it imports `components/flow/diagnostics/value`, `components/flow/frame`, `components/ui`, `lib/api/flow`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/diagnostics/graph"]
  c0["components/flow/diagnostics"] --> self
  self --> d0["components/flow/diagnostics/value"]
  self --> d1["components/flow/frame"]
  self --> d2["components/ui"]
  self --> d3["lib/api/flow"]
  self --> d4["lib/flow"]
  self --> d5["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
