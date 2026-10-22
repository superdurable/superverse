# Composition

Every package this design adds or changes, what it owns, its level in
`scripts/check-package-layers.py`, and how the server is wired. The repository's package and Studio source rules hold without exception;
several affected packages are already at ten files, so new code goes into new packages where a
responsibility seam allows, and into existing files only where the responsibility is the same.

## Server packages

| Package | New or changed | Owns | Level | Files |
|---|---|---|---|---|
| `internal/dexai/agents/session/conversation` | changed | `Actor`, `SenderPreamble`, `EntryMeta.Actor` and `Via`, the `run` notification source, the `remote_activity` event kind | 2 (unchanged) | 10, unchanged count: the types join `entry.go`, `messages.go`, `notification.go`, `events.go` |
| `internal/dexai/agents/remoteexec` | **new**, a sibling of `session` because `session` already has 10 sub-packages | `Executor`, `RemoteExecutor`, `Mode`; `RemoteReport` and its admission and consumption rules (epoch, sequence, capacity, stopped and ended turns); the remote call ID; the projection `ReadRemote` returns. Imports only `conversation`. | 5 | about 4: `executor.go`, `report.go`, `delivery.go`, `calls.go` |
| `internal/dexai/agents/session/agentloop` | changed | `RemoteTools()` and its digest (the catalog lives here); the `watch_preview_run` tool definition; the remote branch of `planModelCall`'s continuation, `SegmentChange` reason `executor` and the handoff entry with its budget and pair-preserving cut, the remote-mode retention rule, actors on `UserMessage`, `AgentInterrupt`, `SessionModel`, `ConfirmedRequirements` | 6 (unchanged) | 10, unchanged count |
| `internal/dexai/agents/session` | changed | `SessionAgentFlow`: `Executor`, `RemoteInbox`, `AwaitRemoteAgent`, `SetExecutor`, `ReportRemote`, `ReadRemote`; the client's three methods | 7 (unchanged) | 10, unchanged count: `session_model.go` becomes `session_executor.go` and holds the remote branch's Step and RPC bodies beside the platform model's; `session_agent_flow.go` keeps only declarations, registration and one-line bodies, at or below 1,520 lines ([executor](executor.md#dex-flow-changes)) |
| `internal/dexai/agents` | changed | `SessionFlowService`: `AuthorizeSession` returns `SessionCaller`; `ActivateRemoteAgent`, `SetSessionExecutor`, `ObserveRun`; the snapshot joins the connection's liveness; `CompleteProjectSession` deletes the connections | 8 (unchanged) | 10, unchanged count |
| `internal/dexai/agents/remoteagentdao` | **new** | SQL of `remote_agent_connections`: pairing, redemption, activation, revocation, authentication join, liveness, cleanup | 1 | 2 and a README, plus its integration test |
| `internal/dexai/agents/remoteagent` | **new** | Pairing and token issuance, request authentication, the remote agent endpoint (`pair`, `activate`, `sync`, `report`, `disconnect`, `downloads`) and its limits | 9 | about 6 |
| `internal/dexai/agents/remoteagent/bridgeapi` | **new** | The Bridge protocol's JSON types and paths, shared by the endpoint and the Bridge; imports nothing from `internal` | shared root | 1 |
| `internal/dexai/agents/sandbox/preview` | changed | `PreviewFlow`: `ObservedRuns`, `RunWatchGeneration`, `ObserveRun`, `WatchRuns`, `SettleRunObservations`; the narrow port `runHistoryWaiter` | 6 (unchanged) | 10, unchanged count |
| `internal/dexai/agents/sandbox/preview/runwatch` | **new** | `Observation`, its bounds and one watch round over the observed Runs | 5 | about 3 |
| `internal/dexai/studio/runs` | changed | implements `runHistoryWaiter` with its Sandbox Dex connection pool | 9 (unchanged) | 6, unchanged count |
| `internal/dexai/api` | changed | the actor and executor fields of the snapshot; `StartFlow` on a Preview registers the observation and answers `agent_watch`; `FlowRunService.ObserveFlowRun` (the service's ninth RPC) | 11 (unchanged) | 10, unchanged count (full since main `552968fc1`) |
| `internal/dexai/api/remoteagent` | **new** | the `RemoteAgentService` handlers (`CreateRemoteAgentPairing`, `ListRemoteAgentConnections`, `RevokeRemoteAgentConnection`, `SetSessionExecutor`), beside `internal/dexai/api/tenancy` | 10 | about 2 |
| `internal/dexai/bootstrap` | changed | wires the DAO, `remoteagent`, the listener and the port | 12 (unchanged) | 9, unchanged count |
| `internal/comm/platform/postgres` | changed | migration `0007_remote_agent_connections.sql` (`0006` is `studio_setup` since main `552968fc1`) | shared root | — |
| `cmd/superverse-server/config` | changed | `remoteAgent.listen` (default `:8088`), `remoteAgent.maxLongPolls` (1,000; 1 to 10,000), `remoteAgent.maxConcurrentReads` (4; 1 to 64, message-loading `ReadRemote` passes per replica), `remoteAgent.maxConcurrentReports` (16; 1 to 256, `report` requests in flight per replica, taken before the body is read and answered 429 at once when full; each holds its body, at most 512 KiB, and `ReportRemote`'s load of the pending `RemoteInbox`, at most 2 MiB, each with about three decode copies, so about 8 MiB per report and about 128 MiB per replica: estimates that RAEXEC confirms by measurement), `remoteAgent.minimumBridgeVersion` (default `0.1.0`, a semantic version), `remoteAgent.acceptDevBridge` (default false; true only in the local profiles, which `Validate` refuses elsewhere), `remoteAgent.downloadsDir` (default `/opt/superverse/bridge`); `sandbox.runObservationWindow` (30 minutes; 1 minute to 2 hours) and `sandbox.maxRunWatches` (200; 1 to 2,000); validation that the port differs from every other listener | — | 2, unchanged count |

`internal/dexai/agents` gains two sub-packages (5 → 7) and `internal/dexai/agents/session` its tenth.
`internal/` keeps its ten entries.

**Dependencies.** `remoteagent` imports `agents` (the Service), `remoteagentdao`, `bridgeapi` and
the account and Project authorization; `api` imports `remoteagent` for the pairing RPC; only
`bootstrap` constructs them. `PreviewFlow` reaches the Sandbox Dex only through `runHistoryWaiter`,
which `internal/dexai/studio/runs` implements and `bootstrap` injects, because `studio/runs` sits above
`preview`.

## The dexai client

All client implementation packages under `cmd/dexai-cli` are private.

| Path | Owns |
|---|---|
| `cmd/dexai-cli` | Foreground connect, legacy serve, MCP, status and targeted disconnect |
| `cmd/dexai-cli/codexdaemon` | Direct Codex daemon requests, per-thread MCP, TUI and public history |
| `cmd/dexai-cli/claudeheadless` | Claude stream-json child, per-process MCP and public transcript |
| `cmd/dexai-cli/mcpserver` | Credential-free stdio MCP and the private local socket |
| `cmd/dexai-cli/platform` | Remote endpoint requests, strict shared types and bounded backoff |
| `cmd/dexai-cli/relay` | One-write delivery, report sequencing, event resume and echo suppression |
| `cmd/dexai-cli/state` | Private per-connection state, locks and bounded report store |
| `cmd/dexai-cli/trace` | Bounded, content-free local timing diagnostics |

The public server-side `bridgeapi` package remains the single wire owner.
Client packages keep the same package/file bounds as the internal source tree.

## Studio

| Directory | Change |
|---|---|
| `components/session` (9 files) | new `RemoteAgentDialog.tsx` (connect, code, takeover warning); executor state in `SessionChatStatus.tsx` |
| `components/agent/conversation` (9 files) | author lines and the "via Codex" and "imported" labels in `ConversationMessage.tsx`; remote activity in `WorkLog.tsx` |
| `lib/api/agent.ts` | `createRemoteAgentPairing`, `setSessionExecutor` |
| `lib/session` (10 files, full) | actor and executor fields in the existing reducer and view files; no new file |
| `mock/backend` (10 files, full) | the two RPCs and a second fixture account in the existing agent mock file |

## Infrastructure

- **Chart**: server container port 8088 `remote-agent` (a typed constant with a chart helper, as for
  the Slack events port), the Service port, the NetworkPolicy rule for that port only, and, for
  hosted environments, the `/remote-agent/` path on the existing load balancer. Local Kind has no
  Ingress controller: the Kind port forward exposes the port on loopback 28089. These are Kind
  paths, so the pull request that adds them runs on Local Kind.
- **EKS (Terraform)**: the same Ingress path on the existing ALB; no new load balancer, listener or
  certificate.
- **Native**: a loopback port in `native.yaml` from the [Native instance port layout](../../runbooks/local-native.md).
- **Distribution**: the endpoint serves only fixed, manifest-listed artifacts provisioned in
  `remoteAgent.downloadsDir`. The separate local `dexai` release script cross-compiles
  macOS arm64 and amd64 archives for a GitHub release after public mirror publication.
  Neither path adds a GitHub Actions build.

## Dex Flow Changes

None beyond those in [executor](executor.md#dex-flow-changes),
[collaboration](collaboration.md#dex-flow-changes) and [run feedback](run-feedback.md#dex-flow-changes).
`flow_registry.go` gains no Flow type.

## Database Schema Changes

`0007_remote_agent_connections.sql` ([connection](connection.md#database-schema-changes)).

## UI/UX

See [executor](executor.md#uiux), [connection](connection.md#uiux),
[collaboration](collaboration.md#uiux) and [bridge](bridge.md#uiux).

## Tests

Each pull request runs `make ci`, `make required-lanes` and the tests its topic lists. The lanes are
Native, plus Local Kind for the pull requests that change the chart or the Ingress.

## Documentation

Package READMEs of every new package (generated summary blocks), `docs/api/remote-agent.md`,
`docs/runbooks/remote-agent.md`, the config reference and the rule files for each changed subject.

## Resource Scalability

See [connection](connection.md#resource-scalability) and [run feedback](run-feedback.md#resource-scalability).
