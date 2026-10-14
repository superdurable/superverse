# Composer

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The one message input, large on the start page and compact in the chat, with its About chip and the element picker that fills it.

Importers: `components/project` and `components/session` import this directory.

Imports: it imports `components/ui` and `lib/session`.

```mermaid
flowchart LR
  self["components/session/composer"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/session"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
