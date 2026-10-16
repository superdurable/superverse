# Kubernetes API client

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Gives every Kubernetes provider one HTTP client of the Kubernetes API and the unstructured-object toolkit it builds and reads resources with.

Callers: `internal/comm/platform/kubeapi/kubeapitest`, `internal/dexai/agents/sandbox/kube`, `internal/dexai/bootstrap`, `internal/dexai/delivery/deployments/kube`, `internal/dexai/projects/infra/kube`, `internal/dexai/projects/infra/kube/connect`, `internal/dexai/projects/infra/kube/hosted`, `internal/dexai/projects/infra/kube/hosted/identity` and `internal/dexai/projects/infra/kube/local` import this package.

Dependencies: it imports `internal/comm` and `internal/comm/platform/deploymentnames` and calls the Kubernetes API.

```mermaid
flowchart LR
  self["internal/comm/platform/kubeapi"]
  c0["internal/comm/platform/kubeapi/kubeapitest"] --> self
  c1["internal/dexai/agents/sandbox/kube"] --> self
  c2["internal/dexai/bootstrap"] --> self
  c3["internal/dexai/delivery/deployments/kube"] --> self
  c4["internal/dexai/projects/infra/kube"] --> self
  c5["internal/dexai/projects/infra/kube/connect"] --> self
  c6["internal/dexai/projects/infra/kube/hosted"] --> self
  c7["internal/dexai/projects/infra/kube/hosted/identity"] --> self
  c8["internal/dexai/projects/infra/kube/local"] --> self
  self --> d0["internal/comm"]
  self --> d1["internal/comm/platform/deploymentnames"]
  self --> e0(["Kubernetes API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

The shared Kubernetes API boundary: one HTTP client and the unstructured-object
toolkit that every provider builds and reads resources with. It knows
Kubernetes conventions and declares the keys of the owner labels (`labels.go`),
which the providers and Local Native share. `Matches` compares an object as the
API serves it with the one a caller wants, ignoring the fields the API server
defaults, for a caller that replaces what no longer matches its spec. Each caller
keeps its own resource ownership: which owner labels it stamps and requires, UID
and generation admission, and the recovery of uncertain effects.
