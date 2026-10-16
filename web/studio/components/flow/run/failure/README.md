# Run failure

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: A failed Step or run at the top of the run drawer in business terms: what happened, what happens now, the engine's words behind More details.

Importers: `components/flow/run/detail` and `components/flow/run/step` import this directory.

Imports: it imports `components/flow/canvas`, `components/ui`, `components/ui/reveal` and `lib/api/flow`.

```mermaid
flowchart LR
  self["components/flow/run/failure"]
  c0["components/flow/run/detail"] --> self
  c1["components/flow/run/step"] --> self
  self --> d0["components/flow/canvas"]
  self --> d1["components/ui"]
  self --> d2["components/ui/reveal"]
  self --> d3["lib/api/flow"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
