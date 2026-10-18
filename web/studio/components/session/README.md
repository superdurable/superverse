# Session UI

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Binds the page's Session to the Agent UI: the chat and its rail, composer or mock composer, model picker, pending action dock, status, workspace renewal and startup phases.

Importers: `components/project` imports this directory.

Imports: it imports `components/agent`, `components/session/composer`, `components/session/executor`, `components/session/mock`, `components/session/start`, `components/ui`, `components/ui/problem`, `components/ui/reveal`, `components/ui/timings`, `lib/api`, `lib/browser`, `lib/generated/protobuf`, `lib/project`, `lib/project/stage`, `lib/session`, `lib/shared` and `lib/shared/problem`.

```mermaid
flowchart LR
  self["components/session"]
  c0["components/project"] --> self
  self --> d0["components/agent"]
  self --> d1["components/session/composer"]
  self --> d2["components/session/executor"]
  self --> d3["components/session/mock"]
  self --> d4["components/session/start"]
  self --> d5["components/ui"]
  self --> d6["components/ui/problem"]
  self --> d7["components/ui/reveal"]
  self --> d8["components/ui/timings"]
  self --> d9["lib/api"]
  self --> d10["lib/browser"]
  self --> d11["lib/generated/protobuf"]
  self --> d12["lib/project"]
  self --> d13["lib/project/stage"]
  self --> d14["lib/session"]
  self --> d15["lib/shared"]
  self --> d16["lib/shared/problem"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The `remoteagent` child owns the Connect dialog, acknowledgement and one-time
code. `SessionChat` places the Agent picker beside either composer, passes the
Project's Builder capability and keeps the dialog scoped to that Project.
The chat's header holds only a dot while live updates reconnect (`SessionConnectionIndicator`); the
workspace's time, Keep running for 7 days and its startup phases are in Project details (`WorkspaceDetails`).
`HistoryJumps` floats Jump to beginning and Jump to latest at the bottom of the history once the reader
scrolls away from the latest message; scrolling up loads earlier messages on its own.
Requests use the thin typed Studio API and remain outside shared Agent UI.
