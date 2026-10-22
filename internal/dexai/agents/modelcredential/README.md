# Model credentials

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Defines model credential envelopes, metadata and revision admission.

Callers: `internal/dexai/agents`, `internal/dexai/agents/session`, `internal/dexai/agents/session/agenttasks`, `internal/dexai/agents/session/subagent`, `internal/dexai/api` and `internal/dexai/api/modelcredential` import this package.

Dependencies: it imports `internal/dexai/agents/session/conversation`.

```mermaid
flowchart LR
  self["internal/dexai/agents/modelcredential"]
  c0["internal/dexai/agents"] --> self
  c1["internal/dexai/agents/session"] --> self
  c2["internal/dexai/agents/session/agenttasks"] --> self
  c3["internal/dexai/agents/session/subagent"] --> self
  c4["internal/dexai/api"] --> self
  c5["internal/dexai/api/modelcredential"] --> self
  self --> d0["internal/dexai/agents/session/conversation"]
```
<!-- END GENERATED PACKAGE SUMMARY -->
