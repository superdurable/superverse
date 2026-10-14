# components/ui/problem

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The one problem panel every failure uses: what happened, why, one remedy button and the folded technical details.

Importers: `components/agent/conversation`, `components/build`, `components/project`, `components/project/activity`, `components/publishing`, `components/publishing/progress`, `components/publishing/publish`, `components/session` and `components/teamspace/connections` import this directory.

Imports: it imports `components/ui` and `lib/shared/problem`.

```mermaid
flowchart LR
  self["components/ui/problem"]
  c0["components/agent/conversation"] --> self
  c1["components/build"] --> self
  c2["components/project"] --> self
  c3["components/project/activity"] --> self
  c4["components/publishing"] --> self
  c5["components/publishing/progress"] --> self
  c6["components/publishing/publish"] --> self
  c7["components/session"] --> self
  c8["components/teamspace/connections"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/shared/problem"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
