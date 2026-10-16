# Provider outcome classification

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Classifies the outcomes of Kubernetes, AWS, S3, GitHub and gRPC provider calls for every adapter and business package, without reading error text.

Callers: `internal/comm/platform/githubapp`, `internal/comm/platform/objectstore`, `internal/dexai/agents/sandbox/preview`, `internal/dexai/delivery/releases/build/hosted`, `internal/dexai/delivery/releases/build/local`, `internal/dexai/projects`, `internal/dexai/projects/infra`, `internal/dexai/projects/infra/kube/hosted/identity` and `internal/dexai/projects/infra/kube/hosted/storage` import this package.

Dependencies: it imports no other package of this module and calls no remote API.

```mermaid
flowchart LR
  self["internal/comm/platform"]
  c0["internal/comm/platform/githubapp"] --> self
  c1["internal/comm/platform/objectstore"] --> self
  c2["internal/dexai/agents/sandbox/preview"] --> self
  c3["internal/dexai/delivery/releases/build/hosted"] --> self
  c4["internal/dexai/delivery/releases/build/local"] --> self
  c5["internal/dexai/projects"] --> self
  c6["internal/dexai/projects/infra"] --> self
  c7["internal/dexai/projects/infra/kube/hosted/identity"] --> self
  c8["internal/dexai/projects/infra/kube/hosted/storage"] --> self
```
<!-- END GENERATED PACKAGE SUMMARY -->

Classifies the outcome of calls to external providers: Kubernetes, AWS and
S3-compatible storage, GitHub and gRPC services. It holds only error typing and
classification shared by several packages and imports no Superverse package, so
every provider adapter can use it. An adapter exposes what classification needs
through a method its error type implements, never through error text. Business
packages decide what a classified error means for their own durable state.
