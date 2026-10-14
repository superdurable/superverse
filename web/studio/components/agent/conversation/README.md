# Agent conversation

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Renders an Agent conversation from committed messages, activity and live text: turns, messages, notices, markers, Markdown, the work log with compact tool rows and model metrics, and the live clock.

Importers: `components/agent` imports this directory.

Imports: it imports `components/ui`, `components/ui/problem`, `lib/session`, `lib/shared` and `lib/shared/problem`.

```mermaid
flowchart LR
  self["components/agent/conversation"]
  c0["components/agent"] --> self
  self --> d0["components/ui"]
  self --> d1["components/ui/problem"]
  self --> d2["lib/session"]
  self --> d3["lib/shared"]
  self --> d4["lib/shared/problem"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
