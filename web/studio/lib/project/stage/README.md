# Project stage

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: How far a Project has come, from its first conversation to built and Live, and the Builder tabs it has shown in this browser.

Importers: `components/build`, `components/project` and `components/session` import this directory.

Imports: it imports `lib/browser`, `lib/generated/protobuf`, `lib/project` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/project/stage"]
  c0["components/build"] --> self
  c1["components/project"] --> self
  c2["components/session"] --> self
  self --> d0["lib/browser"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/project"]
  self --> d3["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

`ProjectStageProvider` takes the Builder's Session and reads its Project; `useProjectStage()` returns
the stage (`conversation`, `shaping`, `built` or `live`) with the facts behind it: whether processes are
built from the agent's own push (`hasOwnProcesses`), whether a published version runs (`hasPublished`),
whether the requirements card waits or was confirmed, and whether the Preview has served a page of the agent's
push. Only a Project created in this browser has a first conversation: the creation page calls
`beginFirstRun`, and the Builder switches once to the standard view when the requirements card arrives or
processes are built, or when a link names another surface (`leaveFirstRun`). The switch and "built" are
kept per Project in browser storage and never go back.

`useRevealedSurfaces` keeps the tabs a Project has shown, so a tab never disappears once it appeared, and
marks those that appeared since the Project was first shown here until they are opened.
