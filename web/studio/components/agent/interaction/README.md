# Agent interaction

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: What the Agent asks of and shows the reader: question batches, an old waiting requirements card, each recorded version's line, queued messages, todo progress, timers and background tasks.

Importers: `components/agent` and `components/agent/conversation` import this directory.

Imports: it imports `components/agent/attachments`, `components/ui`, `components/ui/reveal`, `lib/session` and `lib/shared`.

```mermaid
flowchart LR
  self["components/agent/interaction"]
  c0["components/agent"] --> self
  c1["components/agent/conversation"] --> self
  self --> d0["components/agent/attachments"]
  self --> d1["components/ui"]
  self --> d2["components/ui/reveal"]
  self --> d3["lib/session"]
  self --> d4["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
