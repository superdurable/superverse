# Server bootstrap

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Assembles and runs the single Superverse server process for `cmd/superverse-server`: configuration, providers, Flows, Flow clients, Services, the Dex Worker, the business gRPC server and health endpoints.

Callers: `cmd/superverse-server` and `cmd/tools/check-flow-registry` import this package.

Dependencies: it imports `cmd/superverse-server/config`, `internal/agents/**` (23), `internal/api/**` (4), `internal/comm`, `internal/delivery/**` (8), `internal/platform/**` (12), `internal/projects/**` (11), `internal/studio/**` (3) and `internal/tenancy/**` (8) and calls the AWS S3, AWS STS and Dex Flow APIs.

```mermaid
flowchart LR
  self["internal/bootstrap"]
  c0["cmd/superverse-server"] --> self
  c1["cmd/tools/check-flow-registry"] --> self
  self --> d0["cmd/superverse-server/config"]
  self --> d1["internal/agents/** (23)"]
  self --> d2["internal/api/** (4)"]
  self --> d3["internal/comm"]
  self --> d4["internal/delivery/** (8)"]
  self --> d5["internal/platform/** (12)"]
  self --> d6["internal/projects/** (11)"]
  self --> d7["internal/studio/** (3)"]
  self --> d8["internal/tenancy/** (8)"]
  self --> e0(["AWS S3 API"])
  self --> e1(["AWS STS API"])
  self --> e2(["Dex Flow API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

`Run` assembles and runs the single Superverse server process from the validated
[server configuration](../../cmd/superverse-server/config/README.md) and the
secrets of the environment. It reads no other environment variable. Only the
binaries under `cmd/` import this package.

| File | Responsibility |
| --- | --- |
| `run.go` | `Run`, the process entry point, and the resources it releases |
| `config.go`, `secrets.go` | Builds the providers that need nothing but settings from the validated configuration, among them the repository host and the prefix of its repository names, which `repositoryPrefix` decides from `runtime`, `localKind` and `environment`; `secrets.go` is the one place the server reads credentials from its environment |
| `kubernetes.go` | The one file that touches Kubernetes (`runtime: kubernetes`): it builds the Kubernetes API client, the project infrastructure (with the Local Kind or the project cloud backend), the Live serving and the Sandbox instance provider, and hands the composition the interfaces they implement |
| `docker.go` | The Docker runtime of Local Native (`runtime: docker`): the project infrastructure, the Live serving and the Sandbox instance provider on the local Docker Engine, and the routes the server serves the gateway |
| `composition.go` | Builds the Flows, Flow clients and Services on them, and the business gRPC server; with `agent.mockModel` on it first starts the [mock model](../agents/session/modelprovider/mockmodel/README.md), whose client the model routers of the Sessions and their subagents use for mock Sessions, and tells the API which models it offers. The subagents' bootstrap shares the Session's pooled Sandbox resolver |
| `serve.go` | Runs the Dex Worker, the business gRPC server and the health endpoints, which a Local Native server labels with its run ID, and the listeners of the local Git host and of the Native gateway's routes when the configuration has them |
| `flow_registry.go` | The server's Flows in one list for the Dex registry, and `CheckFlowRegistry`, which builds that registry from them without dependencies for `make check-flow-registry` |

Remote agent connections are composed once from the shared PostgreSQL pool,
Session Service and typed executor read. The same capability is injected into
business RPC registration and its independent HTTP Endpoint; bootstrap starts
and drains the configured listener with the existing process. Native publishes
its configured per-instance loopback origin, Kind its direct 28089 forward and
hosted environments the Studio origin, with no path prefix. The Bridge joins
shared protocol paths itself. The
credential DAO also enters existing Session cleanup; no parallel bootstrap or
background cleaner exists. See [connection operation](../../docs/runbooks/remote-agent.md).
