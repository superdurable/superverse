# components/session/start

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The composer around a Session's turns: the starting model the creation page preselects, the composer's text per Project and its send.

Importers: `components/project`, `components/project/creation` and `components/session` import this directory.

Imports: it imports `components/ui`, `components/ui/menu`, `lib/account`, `lib/browser` and `lib/session`.

```mermaid
flowchart LR
  self["components/session/start"]
  c0["components/project"] --> self
  c1["components/project/creation"] --> self
  c2["components/session"] --> self
  self --> d0["components/ui"]
  self --> d1["components/ui/menu"]
  self --> d2["lib/account"]
  self --> d3["lib/browser"]
  self --> d4["lib/session"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
