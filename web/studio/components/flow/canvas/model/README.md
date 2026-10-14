# Canvas model

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Decodes a Flow Definition into the canvas model and derives what the canvas shows: guards, branches, Step groups and context, actionable Steps and a run overlay.

Importers: `components/flow/canvas`, `components/flow/canvas/camera`, `components/flow/canvas/layout`, `components/flow/canvas/render`, `components/flow/design`, `components/flow/map`, `components/flow/run` and `components/flow/run/detail` import this directory.

Imports: it imports `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/canvas/model"]
  c0["components/flow/canvas"] --> self
  c1["components/flow/canvas/camera"] --> self
  c2["components/flow/canvas/layout"] --> self
  c3["components/flow/canvas/render"] --> self
  c4["components/flow/design"] --> self
  c5["components/flow/map"] --> self
  c6["components/flow/run"] --> self
  c7["components/flow/run/detail"] --> self
  self --> d0["lib/api/flow"]
  self --> d1["lib/api/transport"]
  self --> d2["lib/flow"]
  self --> d3["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
