# Browser state

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Browser-only utilities: storage with every stored key, the theme preference, single-flight user actions, URL query updates and paged lists.

Importers: `app`, `components/build`, `components/flow/connections`, `components/flow/connections/row`, `components/flow/diagnostics`, `components/flow/frame`, `components/flow/scope`, `components/frame`, `components/frame/status`, `components/project`, `components/project/activity`, `components/project/setup`, `components/publishing`, `components/publishing/progress`, `components/publishing/publish`, `components/session`, `components/session/executor`, `components/shell`, `components/teamspace`, `components/teamspace/connections`, `components/ui/pane`, `components/ui/reveal`, `lib/project`, `lib/project/stage`, `lib/session/design` and `lib/teamspace` import this directory.

Imports: it imports `lib/api/transport` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/browser"]
  c0["app"] --> self
  c1["components/build"] --> self
  c2["components/flow/connections"] --> self
  c3["components/flow/connections/row"] --> self
  c4["components/flow/diagnostics"] --> self
  c5["components/flow/frame"] --> self
  c6["components/flow/scope"] --> self
  c7["components/frame"] --> self
  c8["components/frame/status"] --> self
  c9["components/project"] --> self
  c10["components/project/activity"] --> self
  c11["components/project/setup"] --> self
  c12["components/publishing"] --> self
  c13["components/publishing/progress"] --> self
  c14["components/publishing/publish"] --> self
  c15["components/session"] --> self
  c16["components/session/executor"] --> self
  c17["components/shell"] --> self
  c18["components/teamspace"] --> self
  c19["components/teamspace/connections"] --> self
  c20["components/ui/pane"] --> self
  c21["components/ui/reveal"] --> self
  c22["lib/project"] --> self
  c23["lib/project/stage"] --> self
  c24["lib/session/design"] --> self
  c25["lib/teamspace"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
