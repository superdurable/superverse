# internal/dexai/projects/infra/kube/hosted/storage

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Creates, verifies, and deletes hosted project buckets and exposes their object clients.

Callers: `internal/dexai/projects/infra/kube/hosted` imports this package.

Dependencies: it imports `internal/comm/platform`, `internal/dexai/projects/infra`, `internal/dexai/projects/infra/kube` and `internal/dexai/projects/infra/kube/hosted/identity` and calls the AWS S3 API.

```mermaid
flowchart LR
  self["internal/dexai/projects/infra/kube/hosted/storage"]
  c0["internal/dexai/projects/infra/kube/hosted"] --> self
  self --> d0["internal/comm/platform"]
  self --> d1["internal/dexai/projects/infra"]
  self --> d2["internal/dexai/projects/infra/kube"]
  self --> d3["internal/dexai/projects/infra/kube/hosted/identity"]
  self --> e0(["AWS S3 API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

`Account` owns the S3 bucket lifecycle and object access. It uses the identity package for the resource tags and bucket policy, while the hosted backend keeps the `kube.Backend` methods.
