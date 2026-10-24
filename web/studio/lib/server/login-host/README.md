# lib/server/login-host

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Handles shared login forms, browser handoffs, code exchange, host-only transient cookies, and server credentials for first-party products.

Importers: `app` imports this directory.

Imports: it imports `lib/generated/protobuf`, `lib/server` and `lib/shared` and calls the `AccountService` API.

```mermaid
flowchart LR
  self["lib/server/login-host"]
  c0["app"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/server"]
  self --> d2["lib/shared"]
  self --> e0(["AccountService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
