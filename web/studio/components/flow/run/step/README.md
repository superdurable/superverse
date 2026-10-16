# Run step actions

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: What a reader may do at a run's clicked Step: open its Inbox item, ask the agent, retry a failed Step, have the agent fix it, or Time travel.

Importers: `components/flow/run/detail` imports this directory.

Imports: it imports `components/flow/canvas`, `components/flow/diagnostics`, `components/flow/frame`, `components/flow/run/failure`, `components/ui`, `components/ui/reveal`, `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/run/step"]
  c0["components/flow/run/detail"] --> self
  self --> d0["components/flow/canvas"]
  self --> d1["components/flow/diagnostics"]
  self --> d2["components/flow/frame"]
  self --> d3["components/flow/run/failure"]
  self --> d4["components/ui"]
  self --> d5["components/ui/reveal"]
  self --> d6["lib/api/flow"]
  self --> d7["lib/api/transport"]
  self --> d8["lib/flow"]
  self --> d9["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
