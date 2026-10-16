# Start Flow

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: A process's start form in business words, inline or in its dialog, with typed start input and recovery of a start whose response was uncertain.

Importers: `components/flow/run` and `components/flow/run/tryit` import this directory.

Imports: it imports `components/flow/connector/manifest`, `components/flow/frame`, `components/ui`, `components/ui/menu`, `components/ui/reveal`, `lib/api/flow`, `lib/api/transport`, `lib/flow` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/run/start"]
  c0["components/flow/run"] --> self
  c1["components/flow/run/tryit"] --> self
  self --> d0["components/flow/connector/manifest"]
  self --> d1["components/flow/frame"]
  self --> d2["components/ui"]
  self --> d3["components/ui/menu"]
  self --> d4["components/ui/reveal"]
  self --> d5["lib/api/flow"]
  self --> d6["lib/api/transport"]
  self --> d7["lib/flow"]
  self --> d8["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
