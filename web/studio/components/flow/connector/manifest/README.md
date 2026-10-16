# Connector manifest fields

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Form controls and rules for the fields a connector manifest declares: inputs and defaults, authorization methods, and JSON Pointer reads and merges of unit values.

Importers: `components/flow/connections/model`, `components/flow/connections/row`, `components/flow/connector/native`, `components/flow/connector/team-space`, `components/flow/run/start` and `components/flow/run/tryit` import this directory.

Imports: it imports `components/flow/frame`, `components/ui`, `components/ui/menu`, `lib/api/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/connector/manifest"]
  c0["components/flow/connections/model"] --> self
  c1["components/flow/connections/row"] --> self
  c2["components/flow/connector/native"] --> self
  c3["components/flow/connector/team-space"] --> self
  c4["components/flow/run/start"] --> self
  c5["components/flow/run/tryit"] --> self
  self --> d0["components/flow/frame"]
  self --> d1["components/ui"]
  self --> d2["components/ui/menu"]
  self --> d3["lib/api/flow"]
  self --> d4["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
