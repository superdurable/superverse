# E2E support

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Modules the specs share: Studio BFF calls, accounts, Projects, configuration, publishing, Flow runs, Session transport, repository host reads, private fixtures and JSON assertions.

Importers: no production directory imports this directory.

Imports: it imports `lib/api/flow`, `lib/generated/protobuf` and `lib/shared` and calls the GitHub REST, Local Git host JSON and Studio BFF APIs.

```mermaid
flowchart LR
  self["tests/e2e/support"]
  self --> d0["lib/api/flow"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/shared"]
  self --> e0(["GitHub REST API"])
  self --> e1(["Local Git host JSON API"])
  self --> e2(["Studio BFF API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
