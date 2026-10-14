# Diagnostic values

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Renders the values a run's history carries: typed fields, structured and JSON values, Step options and decisions, waiting conditions and persistence groups.

Importers: `components/flow/diagnostics`, `components/flow/diagnostics/event` and `components/flow/diagnostics/graph` import this directory.

Imports: it imports `components/flow/frame`, `components/ui`, `lib/api/flow`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/diagnostics/value"]
  c0["components/flow/diagnostics"] --> self
  c1["components/flow/diagnostics/event"] --> self
  c2["components/flow/diagnostics/graph"] --> self
  self --> d0["components/flow/frame"]
  self --> d1["components/ui"]
  self --> d2["lib/api/flow"]
  self --> d3["lib/flow"]
  self --> d4["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
