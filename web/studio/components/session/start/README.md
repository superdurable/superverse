# components/session/start

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: A new Session's first turn: the model the creation page starts on and the creation intent the chat applies and sends once the Session waits.

Importers: `components/project` and `components/session` import this directory.

Imports: it imports `lib/account`, `lib/project` and `lib/session`.

```mermaid
flowchart LR
  self["components/session/start"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  self --> d0["lib/account"]
  self --> d1["lib/project"]
  self --> d2["lib/session"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
