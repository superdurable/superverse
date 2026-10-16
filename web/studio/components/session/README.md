# Session UI

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Binds the page's Session to the Agent UI: the chat and its rail, composer or mock composer, model picker, pending action dock, status, workspace renewal and startup phases.

Importers: `components/project` imports this directory.

Imports: it imports `components/agent`, `components/flow/scope`, `components/session/composer`, `components/session/executor`, `components/session/mock`, `components/session/remoteagent`, `components/session/start`, `components/ui`, `components/ui/menu`, `components/ui/problem`, `components/ui/reveal`, `components/ui/timings`, `lib/api`, `lib/browser`, `lib/project`, `lib/project/stage`, `lib/session`, `lib/session/design`, `lib/shared` and `lib/shared/problem`.

```mermaid
flowchart LR
  self["components/session"]
  c0["components/project"] --> self
  self --> d0["components/agent"]
  self --> d1["components/flow/scope"]
  self --> d2["components/session/composer"]
  self --> d3["components/session/executor"]
  self --> d4["components/session/mock"]
  self --> d5["components/session/remoteagent"]
  self --> d6["components/session/start"]
  self --> d7["components/ui"]
  self --> d8["components/ui/menu"]
  self --> d9["components/ui/problem"]
  self --> d10["components/ui/reveal"]
  self --> d11["components/ui/timings"]
  self --> d12["lib/api"]
  self --> d13["lib/browser"]
  self --> d14["lib/project"]
  self --> d15["lib/project/stage"]
  self --> d16["lib/session"]
  self --> d17["lib/session/design"]
  self --> d18["lib/shared"]
  self --> d19["lib/shared/problem"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The `remoteagent` child owns the Connect dialog, acknowledgement and one-time
code; `SessionChatStatus` supplies its Project identity and Builder capability.
Requests use the thin typed Studio API and remain outside shared Agent UI.
Executor mode/menu belongs to RAEXEC.
