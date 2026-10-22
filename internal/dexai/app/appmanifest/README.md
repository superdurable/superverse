# Application manifest contracts

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Owns the application contracts a project's source declares, for Release, Preview and Studio: the dex-app.yaml manifest, its connector and environment requirements and strict Flow Definition Graphs.

Callers: `cmd/tools/app-manifest`, `internal/dexai/agents`, `internal/dexai/agents/sandbox/preview`, `internal/dexai/agents/sandbox/workspace/git`, `internal/dexai/api`, `internal/dexai/api/boundary`, `internal/dexai/delivery/deployments`, `internal/dexai/delivery/deployments/docker`, `internal/dexai/delivery/deployments/kube`, `internal/dexai/delivery/releases`, `internal/dexai/projects`, `internal/dexai/projects/config`, `internal/dexai/studio/confighost`, `internal/dexai/studio/confighost/configitems`, `internal/dexai/studio/flowapi/fdg` and `internal/dexai/studio/runs` import this package.

Dependencies: it imports `gen/superverse/v2`, `internal/comm`, `internal/comm/flowid` and `internal/comm/platform/objectstore`.

```mermaid
flowchart LR
  self["internal/dexai/app/appmanifest"]
  c0["cmd/tools/app-manifest"] --> self
  c1["internal/dexai/agents"] --> self
  c2["internal/dexai/agents/sandbox/preview"] --> self
  c3["internal/dexai/agents/sandbox/workspace/git"] --> self
  c4["internal/dexai/api"] --> self
  c5["internal/dexai/api/boundary"] --> self
  c6["internal/dexai/delivery/deployments"] --> self
  c7["internal/dexai/delivery/deployments/docker"] --> self
  c8["internal/dexai/delivery/deployments/kube"] --> self
  c9["internal/dexai/delivery/releases"] --> self
  c10["internal/dexai/projects"] --> self
  c11["internal/dexai/projects/config"] --> self
  c12["internal/dexai/studio/confighost"] --> self
  c13["internal/dexai/studio/confighost/configitems"] --> self
  c14["internal/dexai/studio/flowapi/fdg"] --> self
  c15["internal/dexai/studio/runs"] --> self
  self --> d0["gen/superverse/v2"]
  self --> d1["internal/comm"]
  self --> d2["internal/comm/flowid"]
  self --> d3["internal/comm/platform/objectstore"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

`appmanifest` owns the formats that a project's source declares and the Release
artifacts that carry them. `ExtractAppManifest` strictly decodes `dex-app.yaml`
and admits how the application is served (an unprivileged port, and an absolute health path without a host, query or
line break), which Flow sources it declares and which connector releases it
uses. It returns the canonical connector and environment requirements with
their digests. The Flow Definition helpers validate strict FDG 2.0 bundles and
describe how project Dex serves them. Release, Preview, Studio and the trusted
`cmd/tools/app-manifest` executable read and write these formats only through
this package, which imports no Flow package.

Content that the manifest checks reject is `ErrInvalidAppManifest`, wrapping the
reason: the application's own source, which Preview reports as an
application-owned failure. `RequirementGap` (`requirement_gaps.go`) is one reason
why a scope's stored configuration cannot start the application a source
declares: its kind (`CONNECTION_REQUIRED`, `AUTHORIZATION_REQUIRED`,
`CONNECTION_INCOMPLETE`, `TRIGGER_BINDING_REQUIRED`, `TRIGGER_BINDING_INVALID`,
`OPERATION_CONFIGURATION_REQUIRED` or `ENVIRONMENT_REQUIRED`, a struct-typed
closed set) and the names of the connection, connector, trigger, binding, Flow,
Step, operation, required Connector UI unit or environment variable it concerns,
never a value or credential. The configuration host computes the gaps,
Preview and the Live release report them, and `RequirementGapSnapshots` maps
them to the business API's `ConfigurationRequirementGap`, whose wire kind each
kind declares. A gap of Live's next publish may carry its item's
`FollowRefusal` (`SECRET` or `ENDPOINT_BOUND`, a struct-typed closed set whose
zero value is none): the item can never follow Preview and Live holds no value
of its own.

## Retained contract names

The connector and environment requirements travel under `contract` names that
are stored or exchanged across boundaries. They are compatibility boundaries,
not generic names for new code:

| Boundary | Producer and consumer | Retained representation |
| --- | --- | --- |
| Release artifacts | Trusted AppManifest executable and Release verifier | `connector-contract.json`, `environment-contract.json`; `superverse.dev/connector-contract/v1`, `superverse.dev/environment-contract/v1`; envelope keys `connectorContract`, `environmentContract`, `connectorContractDigest`, `environmentContractDigest` |
| Stored Release references | Release Flow and its typed readers | Serialized `ArtifactRefs.ConnectorContract` and `.EnvironmentContract` in `PreparedSource` and `ReleaseBuildRevisions` |
| Accepted configuration metadata | Project/Sandbox owners, project Dex configuration API and deployment readers | `ReadyConfiguration` and `PreviewConfiguration` fields `ConnectorContractDigest`, `EnvironmentContractDigest`; Dex JSON `connectorContractDigest`, `environmentContractDigest` |
| Public resource protobuf | Merged server, generated clients and Studio | `AppManifestSnapshot` fields `connector_contract_digest` (3), `environment_contract_digest` (4), `connector_contract_object` (9), `environment_contract_object` (10); `ProjectConfigurationSnapshot` digest fields (16, 17), with their existing JSON names |

Some persisted Go fields have no explicit JSON tag, so their exported spelling
is part of the stored representation. A rename must preserve these bytes and
the canonical connector and environment serialization and digests.

## File map

| File | Responsibility |
| --- | --- |
| `manifest.go` | `Manifest`, strict `dex-app.yaml` decoding, application and Flow source rules, connector requirements, `ExtractAppManifest` |
| `environment.go` | Declared application environment variables and their rules |
| `requirements.go` | Canonical requirement JSON and digests, the two Release contract artifacts and their verification |
| `flow_definitions.go` | Strict FDG 2.0 validation, the published definition manifest, graph keys and bundle digest |
