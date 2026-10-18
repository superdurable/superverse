# Browser API clients

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Browser clients of the BFF, one module per browser service with functions named after its RPCs, and the account client that reaches AccountService through `/api/auth`.

Importers: `components/build`, `components/project`, `components/project/activity`, `components/project/creation`, `components/publishing`, `components/publishing/progress`, `components/publishing/publish`, `components/session`, `components/session/executor`, `components/session/remoteagent`, `components/shell`, `lib/project`, `lib/project/publishing`, `lib/project/setup`, `lib/session`, `lib/session/attachments` and `lib/session/design` import this directory.

Imports: it imports `lib/api/transport`, `lib/generated/protobuf` and `lib/shared` and calls the `AgentService`, `AttachmentService`, `DeploymentService`, `PreviewService`, `ProjectService`, `ReleaseService`, `RemoteAgentService` and `SessionService` APIs.

```mermaid
flowchart LR
  self["lib/api"]
  c0["components/build"] --> self
  c1["components/project"] --> self
  c2["components/project/activity"] --> self
  c3["components/project/creation"] --> self
  c4["components/publishing"] --> self
  c5["components/publishing/progress"] --> self
  c6["components/publishing/publish"] --> self
  c7["components/session"] --> self
  c8["components/session/executor"] --> self
  c9["components/session/remoteagent"] --> self
  c10["components/shell"] --> self
  c11["lib/project"] --> self
  c12["lib/project/publishing"] --> self
  c13["lib/project/setup"] --> self
  c14["lib/session"] --> self
  c15["lib/session/attachments"] --> self
  c16["lib/session/design"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/shared"]
  self --> e0(["AgentService API"])
  self --> e1(["AttachmentService API"])
  self --> e2(["DeploymentService API"])
  self --> e3(["PreviewService API"])
  self --> e4(["ProjectService API"])
  self --> e5(["ReleaseService API"])
  self --> e6(["RemoteAgentService API"])
  self --> e7(["SessionService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->

Every service function takes `(input, signal)`, where the input is the RPC's
request less only what the module derives.
Each module keeps its service's own rules: Project creation retries a 503,
validation checks its request ID, and the retry rule of `transport/api-error.ts`
decides what a failed command keeps. Get/List reads share the bounded transient
policy of [the transport](transport/README.md). The modules import only `lib/shared`,
`lib/generated` and `lib/api`, so the Agent enum mapping stays in
`lib/session/agent-view.ts`.
