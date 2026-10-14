# Flow frame

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: What every Flow view shares: the FlowFrame root, links to a Flow target, pane presets with their storage keys, and the Runs workspace class constants.

Importers: `components/flow/canvas`, `components/flow/connector/manifest`, `components/flow/connector/native`, `components/flow/connector/team-space`, `components/flow/design`, `components/flow/diagnostics`, `components/flow/diagnostics/event`, `components/flow/diagnostics/graph`, `components/flow/diagnostics/timeline`, `components/flow/diagnostics/value`, `components/flow/run`, `components/flow/run/detail`, `components/flow/run/start`, `components/flow/run/step` and `components/flow/scope` import this directory.

Imports: it imports `components/ui`, `components/ui/pane`, `lib/browser`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/frame"]
  c0["components/flow/canvas"] --> self
  c1["components/flow/connector/manifest"] --> self
  c2["components/flow/connector/native"] --> self
  c3["components/flow/connector/team-space"] --> self
  c4["components/flow/design"] --> self
  c5["components/flow/diagnostics"] --> self
  c6["components/flow/diagnostics/event"] --> self
  c7["components/flow/diagnostics/graph"] --> self
  c8["components/flow/diagnostics/timeline"] --> self
  c9["components/flow/diagnostics/value"] --> self
  c10["components/flow/run"] --> self
  c11["components/flow/run/detail"] --> self
  c12["components/flow/run/start"] --> self
  c13["components/flow/run/step"] --> self
  c14["components/flow/scope"] --> self
  self --> d0["components/ui"]
  self --> d1["components/ui/pane"]
  self --> d2["lib/browser"]
  self --> d3["lib/flow"]
  self --> d4["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
