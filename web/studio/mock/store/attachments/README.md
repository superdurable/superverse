# Mock attachments

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Development-only attachment staging, where uploads PUT to the mock route are prepared at once and the attachments a Project's messages carry have downloads.

Importers: `app` and `mock/backend` import this directory.

Imports: it imports `lib/generated/protobuf`, `lib/server` and `mock/store`.

```mermaid
flowchart LR
  self["mock/store/attachments"]
  c0["app"] --> self
  c1["mock/backend"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/server"]
  self --> d2["mock/store"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
