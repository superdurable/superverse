# Model credentials

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Defines model credential envelopes, metadata and revision admission.

Callers: `internal/dexai/agents`, `internal/dexai/agents/session`, `internal/dexai/agents/session/agenttasks`, `internal/dexai/agents/session/subagent`, `internal/dexai/api` and `internal/dexai/api/modelcredential` import this package.

Dependencies: it imports `internal/comm` and `internal/dexai/agents/session/conversation`.

```mermaid
flowchart LR
  self["internal/dexai/agents/modelcredential"]
  c0["internal/dexai/agents"] --> self
  c1["internal/dexai/agents/session"] --> self
  c2["internal/dexai/agents/session/agenttasks"] --> self
  c3["internal/dexai/agents/session/subagent"] --> self
  c4["internal/dexai/api"] --> self
  c5["internal/dexai/api/modelcredential"] --> self
  self --> d0["internal/comm"]
  self --> d1["internal/dexai/agents/session/conversation"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

The provider namespace is derived from the canonical model catalog, sorted and
deduplicated into ten credential instances. Mock has none. Metadata/writer
operations and hard-delete storage support that closed set; execution and new
key validation remain limited to available Anthropic profiles. No key is stored
in the provider catalog, holder record, public snapshot or operation identity.
