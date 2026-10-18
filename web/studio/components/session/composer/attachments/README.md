# Composer attachments

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The composer's attachment controls: Attach (+) with its file picker, the chips with their progress and reasons, and the drop zone that takes files dropped on the chat.

Importers: `components/project`, `components/session`, `components/session/composer` and `components/session/mock` import this directory.

Imports: it imports `components/ui` and `lib/session/attachments`.

```mermaid
flowchart LR
  self["components/session/composer/attachments"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  c2["components/session/composer"] --> self
  c3["components/session/mock"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/session/attachments"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
