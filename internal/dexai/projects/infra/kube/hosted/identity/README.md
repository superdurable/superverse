# internal/dexai/projects/infra/kube/hosted/identity

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Owns hosted IAM roles, Pod Identity associations, policy documents, grant lifecycle, and cloud resource ownership tags.

Callers: `internal/dexai/projects/infra/kube/hosted` and `internal/dexai/projects/infra/kube/hosted/storage` import this package.

Dependencies: it imports `internal/comm`, `internal/comm/platform`, `internal/comm/platform/kubeapi`, `internal/dexai/projects/infra` and `internal/dexai/projects/infra/kube` and calls the Amazon EKS, AWS IAM and AWS S3 APIs.

```mermaid
flowchart LR
  self["internal/dexai/projects/infra/kube/hosted/identity"]
  c0["internal/dexai/projects/infra/kube/hosted"] --> self
  c1["internal/dexai/projects/infra/kube/hosted/storage"] --> self
  self --> d0["internal/comm"]
  self --> d1["internal/comm/platform"]
  self --> d2["internal/comm/platform/kubeapi"]
  self --> d3["internal/dexai/projects/infra"]
  self --> d4["internal/dexai/projects/infra/kube"]
  self --> e0(["Amazon EKS API"])
  self --> e1(["AWS IAM API"])
  self --> e2(["AWS S3 API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

`Account` owns AWS identity operations and the shared naming and tag rules. The hosted backend forwards its grant and Dex identity methods here; storage uses its ownership checks and bucket policy.
