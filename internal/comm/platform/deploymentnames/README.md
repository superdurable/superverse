# Deployment names

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Declares the deployment environments and owns their static, operator-selected names (prefixes, clusters, registries, IAM paths and domains) that Go, Terraform, Helm and the infra scripts share.

Callers: `cmd/dex-debug-tunnel`, `cmd/superverse-server/config`, `internal/comm/platform/connectorlibrary`, `internal/comm/platform/githubissues`, `internal/comm/platform/kubeapi`, `internal/comm/platform/repohost`, `internal/comm/platform/repohost/github`, `internal/comm/platform/repohost/localgit`, `internal/dexai/agents/platformissue`, `internal/dexai/agents/sandbox/docker`, `internal/dexai/agents/sandbox/preview`, `internal/dexai/agents/sandbox/provision`, `internal/dexai/bootstrap`, `internal/dexai/delivery/deployments/docker`, `internal/dexai/delivery/releases`, `internal/dexai/delivery/releases/build`, `internal/dexai/delivery/releases/build/hosted`, `internal/dexai/projects/infra`, `internal/dexai/projects/infra/docker`, `internal/dexai/projects/infra/docker/dockertest`, `internal/dexai/projects/infra/kube`, `internal/dexai/projects/infra/kube/hosted`, `internal/dexai/projects/infra/kube/kubetest` and `internal/dexai/studio/runs` import this package.

Dependencies: it imports `internal/comm`.

```mermaid
flowchart LR
  self["internal/comm/platform/deploymentnames"]
  c0["cmd/dex-debug-tunnel"] --> self
  c1["cmd/superverse-server/config"] --> self
  c2["internal/comm/platform/connectorlibrary"] --> self
  c3["internal/comm/platform/githubissues"] --> self
  c4["internal/comm/platform/kubeapi"] --> self
  c5["internal/comm/platform/repohost"] --> self
  c6["internal/comm/platform/repohost/github"] --> self
  c7["internal/comm/platform/repohost/localgit"] --> self
  c8["internal/dexai/agents/platformissue"] --> self
  c9["internal/dexai/agents/sandbox/docker"] --> self
  c10["internal/dexai/agents/sandbox/preview"] --> self
  c11["internal/dexai/agents/sandbox/provision"] --> self
  c12["internal/dexai/bootstrap"] --> self
  c13["internal/dexai/delivery/deployments/docker"] --> self
  c14["internal/dexai/delivery/releases"] --> self
  c15["internal/dexai/delivery/releases/build"] --> self
  c16["internal/dexai/delivery/releases/build/hosted"] --> self
  c17["internal/dexai/projects/infra"] --> self
  c18["internal/dexai/projects/infra/docker"] --> self
  c19["internal/dexai/projects/infra/docker/dockertest"] --> self
  c20["internal/dexai/projects/infra/kube"] --> self
  c21["internal/dexai/projects/infra/kube/hosted"] --> self
  c22["internal/dexai/projects/infra/kube/kubetest"] --> self
  c23["internal/dexai/studio/runs"] --> self
  self --> d0["internal/comm"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

The static, operator-selected names of each deployment environment: product and
namespace prefixes, clusters, registries, IAM paths and domains.
`environments.json` is the catalog that Go, Terraform, Helm and the infra
scripts read; it holds no business state and a resource request never selects a
name from it.

`Environment` is the closed set of the catalog's keys, declared once as
`EnvironmentTest`, `EnvironmentStaging` and `EnvironmentProd`. It is a struct
that string literals cannot convert to: operator input enters Go through
`ParseEnvironment`, which reports an `unknownEnvironmentError` for another
name, and `For` and `MustFor` take the typed value. Its JSON and text spelling
is the catalog key, so persisted and wire documents keep their names. The
package panics at start unless the declared environments and the catalog's keys
are the same set.

`RepositoryPrefix` is the first part of a managed repository's name. The two local
lanes run as the test environment, so the catalog cannot hold it: it is the closed set
`RepositoryPrefixLocalNative`, `RepositoryPrefixLocalKind` and
`HostedRepositoryPrefix(environment)`, the environment's name, a struct that string
literals cannot convert to. `internal/dexai/bootstrap` chooses among them from the server
configuration. The
[managed repository names](../repohost/README.md#managed-repositories)
define the rest of the name.
