# Flow providers

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The providers the Flow UI reads: the scoped Flow API client, Flow view navigation and the Flow catalog, with the definition revision binding and enum labels.

Importers: `components/build`, `components/flow/canvas`, `components/flow/canvas/model`, `components/flow/connections`, `components/flow/connections/model`, `components/flow/connections/row`, `components/flow/connector/native`, `components/flow/diagnostics`, `components/flow/diagnostics/event`, `components/flow/diagnostics/graph`, `components/flow/diagnostics/timeline`, `components/flow/diagnostics/value`, `components/flow/frame`, `components/flow/map`, `components/flow/run`, `components/flow/run/detail`, `components/flow/run/start`, `components/flow/run/step`, `components/flow/run/tryit`, `components/flow/scope`, `components/project`, `components/project/activity` and `components/project/goals` import this directory.

Imports: it imports `lib/api/flow`, `lib/api/flow/draft`, `lib/api/transport` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/flow"]
  c0["components/build"] --> self
  c1["components/flow/canvas"] --> self
  c2["components/flow/canvas/model"] --> self
  c3["components/flow/connections"] --> self
  c4["components/flow/connections/model"] --> self
  c5["components/flow/connections/row"] --> self
  c6["components/flow/connector/native"] --> self
  c7["components/flow/diagnostics"] --> self
  c8["components/flow/diagnostics/event"] --> self
  c9["components/flow/diagnostics/graph"] --> self
  c10["components/flow/diagnostics/timeline"] --> self
  c11["components/flow/diagnostics/value"] --> self
  c12["components/flow/frame"] --> self
  c13["components/flow/map"] --> self
  c14["components/flow/run"] --> self
  c15["components/flow/run/detail"] --> self
  c16["components/flow/run/start"] --> self
  c17["components/flow/run/step"] --> self
  c18["components/flow/run/tryit"] --> self
  c19["components/flow/scope"] --> self
  c20["components/project"] --> self
  c21["components/project/activity"] --> self
  c22["components/project/goals"] --> self
  self --> d0["lib/api/flow"]
  self --> d1["lib/api/flow/draft"]
  self --> d2["lib/api/transport"]
  self --> d3["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
