# Agent transport and Studio projection

The browser reaches `SessionService` and `AgentService` through `/api/rpc/<Method>`. [The resource API](resource-grpc.md) owns the methods and authorization; [the typed Agent wire contract](agent-protobuf-contract.md) owns the DTO mapping. This page describes how Studio consumes those contracts.

## Transport boundary and authorization

Unary requests are JSON POSTs to the Studio BFF; Watches are Server-Sent Events. The BFF sets `CallerContext` from the HttpOnly login cookie and rejects a caller supplied in the body. Go validates the signed login and Project access, then derives the Session FlowID from the ProjectID. Unknown request fields and browser supplied FlowIDs cannot select another execution. Mutations require the same-origin check.

## Operations

- `GetSession` reads the canonical Session snapshot. `WatchSession` sends an initial snapshot, later snapshots and best-effort progress; Studio reads the snapshot first and resumes with a signed cursor after disconnecting.
- `ListArchivedMessages` pages durable history at ten-message boundaries. The browser keeps message sequence and archive cursors from the Session, not from Dex execution history.
- `SendMessage`, `AnswerQuestions`, `DeleteQueuedMessage`, `SteerQueuedMessage` and `ResolveToolApproval` admit commands with typed IDs and inputs. An empty response confirms admission; the Watch supplies the resulting state. A repeated message after an uncertain response uses a new ID and may create a second message.
- `GetFlowDesign` reads the body at the digest named by the current snapshot. The Session owns the design.
- Workspace state comes from its owning Session and Sandbox snapshots. Studio uses resource revisions and epochs to reject stale reads after navigation or reconnect.

## Watch and UI behavior

The shared Agent UI remains transport free. Studio projects generated fields into its view models, preserving meaningful zero and false values. It keeps the Watch active during commands and cancels it when its owner unmounts. A snapshot received during a separate Get takes precedence over an older response. On reconnect, Studio reads the canonical Session again to recover lost best-effort progress.

## Dex Flow Changes

None. [SessionAgentFlow](../architecture/agent-harness-design/session.md) owns the current interaction and messages; the transport does not create another Flow or lifecycle.

## Database Schema Changes

None. The transport adds no history projection, cursor cache or duplicate lifecycle.

## UI/UX

Studio retains history and archive paging, activity and assistant text, queued and steered messages, questions, approvals, recovery decisions and workspace status. It shows uncertain command outcomes separately from read failures and does not assume a missing response means success.

## Tests

Real-dependency integration and browser scenarios exercise the merged BFF, gRPC, Dex and Studio Watch path. Static compilation and generated-code checks verify contract drift but do not establish runtime behavior.

## Documentation

The [resource API](resource-grpc.md), [Agent wire contract](agent-protobuf-contract.md), [Session contract](../architecture/agent-harness-design/session.md) and [Studio streaming contract](../architecture/agent-harness-design/streaming-and-studio.md) own the underlying facts.

## Resource Scalability

The BFF retains no durable Session copy. A browser uses one Watch per Session view; server cursors are signed and bounded. Reconnect reads one Session by identity rather than scanning Projects.
