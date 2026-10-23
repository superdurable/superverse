# Attachments

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The composer's attachments: the accepted formats and limits, image normalization and hashing, the upload through a presigned PUT, the chip state machine, its tray hook and the history download links.

Importers: `components/project`, `components/session`, `components/session/composer`, `components/session/composer/attachments`, `components/session/mock`, `components/session/start` and `lib/session` import this directory.

Imports: it imports `lib/api`, `lib/api/transport`, `lib/browser`, `lib/generated/protobuf` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/session/attachments"]
  c0["components/project"] --> self
  c1["components/session"] --> self
  c2["components/session/composer"] --> self
  c3["components/session/composer/attachments"] --> self
  c4["components/session/mock"] --> self
  c5["components/session/start"] --> self
  c6["lib/session"] --> self
  self --> d0["lib/api"]
  self --> d1["lib/api/transport"]
  self --> d2["lib/browser"]
  self --> d3["lib/generated/protobuf"]
  self --> d4["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
