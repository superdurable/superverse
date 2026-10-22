# Model credentials

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Defines model credential envelopes, metadata and revision admission.

Callers: `internal/dexai/api` imports this package.

Dependencies: it imports `gen/superverse/v2`, `internal/comm`, `internal/comm/modelendpoint`, `internal/dexai/agents`, `internal/dexai/agents/modelcredential`, `internal/dexai/agents/session/conversation`, `internal/dexai/agents/session/modelprovider/openaicompat` and `internal/dexai/api/boundary`.

```mermaid
flowchart LR
  self["internal/dexai/api/modelcredential"]
  c0["internal/dexai/api"] --> self
  self --> d0["gen/superverse/v2"]
  self --> d1["internal/comm"]
  self --> d2["internal/comm/modelendpoint"]
  self --> d3["internal/dexai/agents"]
  self --> d4["internal/dexai/agents/modelcredential"]
  self --> d5["internal/dexai/agents/session/conversation"]
  self --> d6["internal/dexai/agents/session/modelprovider/openaicompat"]
  self --> d7["internal/dexai/api/boundary"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

The provider namespace is derived from the canonical model catalog, sorted and
deduplicated into ten credential instances. Mock has none. Metadata/writer
operations and hard-delete storage support that closed set; execution and new
key validation follow the server-admitted provider profiles. No key is stored
in the provider catalog, holder record, public snapshot or operation identity.

`Info.Endpoint` owns the canonical custom API root, requested model ID (possibly
empty), resolved effective model ID and conservative profile version. The
secret `Envelope` remains exactly revision and key. Endpoint/key/model changes
reserve one provider revision and follow the existing direct write, exact
readback and finalization path; the bounded previous metadata record supports
recovery. Frozen segment and child selections are historical execution choices,
not additional owners of current configuration.

The four Builder-authorized RPCs read metadata, set/delete one provider revision
and discover custom model IDs. Discovery accepts a transient redacted key or
requires the exact active provider revision and identical stored canonical base
URL before reading the stored key. It never writes credentials or discovery
responses to a Flow. The BFF excludes the complete service from request logging;
protobuf key, endpoint and model-ID fields are debug-redacted.
