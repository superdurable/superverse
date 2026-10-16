# Session Flow design

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Reads which Flow design the Flow tab draws from the Session Snapshot, reads its body once per digest, remembers which card version it showed, and whether its Brief shows.

Importers: `components/build`, `components/project` and `components/session` import this directory.

Imports: it imports `lib/api`, `lib/api/transport`, `lib/browser`, `lib/generated/protobuf`, `lib/session` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/session/design"]
  c0["components/build"] --> self
  c1["components/project"] --> self
  c2["components/session"] --> self
  self --> d0["lib/api"]
  self --> d1["lib/api/transport"]
  self --> d2["lib/browser"]
  self --> d3["lib/generated/protobuf"]
  self --> d4["lib/session"]
  self --> d5["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

| File                     | Responsibility                                                                                                                                                                                                             |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `use-flow-design.ts`     | The design the Flow tab draws: the waiting card's, or the confirmed one until it is built and the Preview's processes are built from the push that built it; its body through `GetFlowDesign`, cached and shared by digest |
| `use-design-shown.ts`    | The card version whose design the Flow tab last showed by itself, in browser storage, so it shows once and the tab's marker goes                                                                                           |
| `use-brief-on-screen.ts` | Whether a Process Brief is mounted, so the chat leaves out its pointer to it                                                                                                                                               |

A stale read (`REVISION_CHANGED`) re-reads the Snapshot; a slot without a design (NOT_FOUND) draws none.
