# Start page attachments

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The E2E files a first message attaches on the start page, uploaded through presigned PUTs, and the checks of their delivery, downloads and chips once the Session shows them.

Importers: no production directory imports this directory.

Imports: it imports `lib/shared` and `tests/e2e/support` and calls the object storage presigned URL API.

```mermaid
flowchart LR
  self["tests/e2e/support/attachments"]
  self --> d0["lib/shared"]
  self --> d1["tests/e2e/support"]
  self --> e0(["object storage presigned URL API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
