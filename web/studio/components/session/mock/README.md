# components/session/mock

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The mock model's composer, in a Session and on the creation page: it picks a category, a scenario and its arguments, and sends the next input as its kind arrives.

Importers: `components/project` and `components/session` import this directory.

Imports: it imports `components/session/composer/attachments`, `components/ui`, `lib/session` and `lib/session/attachments`.

```mermaid
flowchart LR
  self["components/session/mock"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  self --> d0["components/session/composer/attachments"]
  self --> d1["components/ui"]
  self --> d2["lib/session"]
  self --> d3["lib/session/attachments"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
