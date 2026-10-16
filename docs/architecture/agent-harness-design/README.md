# Agent harness design

The Session Agent is the coding assistant that builds a user's Dex application inside
Superverse. Users are not developers: they describe what they want in business
terms, read the short requirements card the agent builds from (and change it with a
message), and watch the agent work. This directory
is the single home for how that agent works: the model and its context, the tools,
the Sandbox it runs commands in, subagents, notifications, the Studio views, and Studio.

Topic documents describe current behavior; package READMEs link to the full per-Flow contracts.

## The pieces

| Piece | What it is | Topic |
|---|---|---|
| Session | One long-lived conversation per Project (`SessionAgentFlow`). Owns the conversation, turns, questions, the recorded requirements cards, the Stop button, automatic turns, the request side of its Sandbox instance (the accepted lease and the Sandbox deadline) and the decision that the instance is idle. | [session.md](session.md) |
| Model and context | Claude through the official Go SDK; Opus 5.5 by default, Sonnet 5.5 or Haiku 4.5 selectable per Session, and the mock model where the server offers it. An append-only entry log, context segments and simple compaction keep the context small and the prompt cache warm. | [context-management.md](context-management.md) |
| Tools | `bash`, `read`, `edit`, `write`, `grep`, `glob`, `commit_and_push`, platform diagnostics and agent tools. Results are sized when they are recorded; failures go back to the model with an owner and an effect, and platform faults also reach a `PlatformIssueFlow` per signature, which files one GitHub issue. A background command's watch is a `BackgroundCommandFlow` SubFlow. | [tool-orchestration.md](tool-orchestration.md) |
| Sandbox | The per-Project workspace and its Pod instances (owned by `SessionWorkspaceFlow`, which provisions each one through a `SandboxProvisionFlow` and reclaims it at the Session's command), the Preview (`PreviewFlow`), the command ledger and process limits, the Sandbox-local Preview engine, cold start and resources. | [sandbox.md](sandbox.md) |
| Subagents | Background agents (`SubagentFlow`) that investigate or change code in their own git worktrees and report back. | [subagents.md](subagents.md) |
| Connectors | Choosing connectors and building missing ones in the connectors library with connector subagents, one `ConnectorContributionFlow` per contributed connector for its draft pull request and its release candidates, which the application uses in the Preview and Live before the connector's release. | [connectors.md](connectors.md) |
| Notifications and delivery feedback | One notification path into the Session for subagents, background commands, Preview, Release and Deployment outcomes; automatic turns; Preview auto-deploy. | [notifications-and-feedback.md](notifications-and-feedback.md) |
| Commit gate and application tests | `commit_and_push` and its checks, dexcli checks, application API tests. | [commit-gate-and-app-tests.md](commit-gate-and-app-tests.md) |
| Streaming and Studio | What reaches the browser live and what Studio shows. | [streaming-and-studio.md](streaming-and-studio.md) |
| Composition | How the Flows, clients, bootstraps and services are wired in the server, the Kubernetes and Docker runtimes, and the local lanes. | [composition.md](composition.md) |

## How one turn works

```mermaid
flowchart LR
  user[User message or notification] --> await[AwaitUser]
  await --> check[CheckSteered]
  check -->|context too large or prompt changed| compact[CompactContext]
  compact --> check
  check --> call[CallModel]
  call -->|tool_use| route[RouteTool]
  route -->|one call| exec[ExecuteTool]
  route -->|read-only calls| wave[Parallel wave]
  exec --> check
  wave --> check
  call -->|end_turn| await
  check -->|Stop pressed| await
```

- **Between model calls** (`CheckSteered`) the Session applies steering, delivers
  notifications and decides whether to compact.
- **Tool calls** run against the Sandbox on the normal path without any Flow RPC: the
  Step reads the accepted workspace lease from its own Attributes and talks to
  sandboxd.
- **Long work leaves the turn**: commands can run in the background and subagents
  always do. Their completion arrives as a notification, either at the next boundary
  of a running turn or as an automatic turn when the Session is idle.

## Principles

- **One owner per fact.** Each durable fact has exactly one owning Flow. Streams are
  best effort; Snapshot-first reads recover.
- **The model context is append-only.** Nothing sent to the model is edited, removed or
  reordered. Instructions are appended entries; renewing the context means starting a
  new segment with a summary. This keeps Claude's preserved thinking valid and the
  prompt cache warm.
- **Every failure has one handler.** Transient faults are retried by Dex; call and
  application failures go to the model; platform faults go to the model and to a
  GitHub issue. Users never make technical recovery decisions.
- **Users see what the agent is doing, not raw output.** Tool name, target and status
  stream live, plus a reasoning summary; file contents, diffs and command output stay
  out of the browser.
- **Everything is bounded.** Turns, automatic turns, subagents, background commands,
  process lifetime and output, notification queues and retained records all have
  limits that the platform enforces without anyone having to intervene.

## Related documents

- [Superverse V2 architecture](../superverse-v2.md): platform-wide contract; its
  SessionAgentFlow, SubagentFlow, PlatformIssueFlow, BackgroundCommandFlow and Sandbox
  instance Flows sections summarize and link here.
- [StartFlow admission](../start-flow-admission.md) and
  [deployment names](../superverse-v2.md).
- [Sandbox lifecycle](sandbox.md), [Session lifecycle](session.md), [Studio frame](../studio-setup/studio.md), [business API](../../api/resource-grpc.md), [generated Flow graphs](../../flows/README.md) and [runbooks](../../runbooks/README.md).
