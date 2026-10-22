# Provider catalog integration

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Exercises catalog admission, existing model mappings, persisted Claude selection and mock continuation through the real Studio, BFF, gRPC, Dex and Sandbox.

Importers: no production directory imports this directory.

Imports: it imports `lib/api`, `tests/e2e/support`, `tests/e2e/support/coverage`, `tests/e2e/support/mock` and `tests/e2e/support/teamspace`.

```mermaid
flowchart LR
  self["tests/e2e/integration/catalog"]
  self --> d0["lib/api"]
  self --> d1["tests/e2e/support"]
  self --> d2["tests/e2e/support/coverage"]
  self --> d3["tests/e2e/support/mock"]
  self --> d4["tests/e2e/support/teamspace"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The spec runs inside `TestProjectAnthropicCredentialDexIntegration` with `SUPERVERSE_E2E_BYOK_FIXTURE=true`. The production-composed server uses real Native dependencies and only the existing mock model endpoint for credential validation. Capture is disabled.

`model-catalog.spec.ts` creates its own account, TeamSpace and disposable Project, sets its own synthetic key through `SetModelCredential`, selects Sonnet without calling it, reloads, then runs the explain scenario on mock. New provider selections must be refused without changing the revision.
