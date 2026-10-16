# More details

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The "More details" fold every surface uses to keep engine detail out of its default view, remembered open per surface in this browser.

Importers: `components/agent/conversation`, `components/agent/interaction`, `components/flow/canvas`, `components/flow/design`, `components/flow/design/brief`, `components/flow/diagnostics`, `components/flow/run/detail`, `components/flow/run/failure`, `components/flow/run/start`, `components/flow/run/step`, `components/project`, `components/project/activity` and `components/session` import this directory.

Imports: it imports `components/ui` and `lib/browser`.

```mermaid
flowchart LR
  self["components/ui/reveal"]
  c0["components/agent/conversation"] --> self
  c1["components/agent/interaction"] --> self
  c2["components/flow/canvas"] --> self
  c3["components/flow/design"] --> self
  c4["components/flow/design/brief"] --> self
  c5["components/flow/diagnostics"] --> self
  c6["components/flow/run/detail"] --> self
  c7["components/flow/run/failure"] --> self
  c8["components/flow/run/start"] --> self
  c9["components/flow/run/step"] --> self
  c10["components/project"] --> self
  c11["components/project/activity"] --> self
  c12["components/session"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/browser"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
