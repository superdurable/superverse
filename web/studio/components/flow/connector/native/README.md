# Native connector units

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The native units a connector manifest declares, the host that runs their declared provider commands and saves their values, model ID rules and picker options.

Importers: `components/flow/connections`, `components/flow/connections/model`, `components/flow/connections/row` and `components/flow/connector/team-space` import this directory.

Imports: it imports `components/flow/connector/manifest`, `components/flow/connector/native/model`, `components/flow/frame`, `components/ui`, `components/ui/menu`, `lib/api/flow`, `lib/api/flow/draft`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/connector/native"]
  c0["components/flow/connections"] --> self
  c1["components/flow/connections/model"] --> self
  c2["components/flow/connections/row"] --> self
  c3["components/flow/connector/team-space"] --> self
  self --> d0["components/flow/connector/manifest"]
  self --> d1["components/flow/connector/native/model"]
  self --> d2["components/flow/frame"]
  self --> d3["components/ui"]
  self --> d4["components/ui/menu"]
  self --> d5["lib/api/flow"]
  self --> d6["lib/api/flow/draft"]
  self --> d7["lib/api/transport"]
  self --> d8["lib/flow"]
  self --> d9["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
