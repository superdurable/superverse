# Message attachments

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The files a sent or queued message attached, as chips with image thumbnails, reached only through the links the page injects, so the shared Agent UI stays transport-free.

Importers: `components/agent`, `components/agent/conversation` and `components/agent/interaction` import this directory.

Imports: it imports `components/ui` and `lib/session`.

```mermaid
flowchart LR
  self["components/agent/attachments"]
  c0["components/agent"] --> self
  c1["components/agent/conversation"] --> self
  c2["components/agent/interaction"] --> self
  self --> d0["components/ui"]
  self --> d1["lib/session"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
