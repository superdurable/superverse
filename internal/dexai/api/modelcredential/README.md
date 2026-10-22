# Model credential API

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Authorizes Project credential mutations and returns non-secret metadata.

Callers: `internal/dexai/api` imports this package.

Dependencies: it imports `gen/superverse/v2`, `internal/comm`, `internal/dexai/agents`, `internal/dexai/agents/modelcredential`, `internal/dexai/agents/session/conversation`, `internal/dexai/agents/session/modelprovider/openaicompat` and `internal/dexai/api/boundary`.

```mermaid
flowchart LR
  self["internal/dexai/api/modelcredential"]
  c0["internal/dexai/api"] --> self
  self --> d0["gen/superverse/v2"]
  self --> d1["internal/comm"]
  self --> d2["internal/dexai/agents"]
  self --> d3["internal/dexai/agents/modelcredential"]
  self --> d4["internal/dexai/agents/session/conversation"]
  self --> d5["internal/dexai/agents/session/modelprovider/openaicompat"]
  self --> d6["internal/dexai/api/boundary"]
```
<!-- END GENERATED PACKAGE SUMMARY -->
