# Project pages

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The Project pages: the start form, Project creation, Project details, the Project's one Session provider, the surfaces and tools inside the Builder frame, and Setup on either tab.

Importers: `app` and `mock/surface` import this directory.

Imports: it imports `components/build`, `components/flow/scope`, `components/frame`, `components/project/**` (3), `components/publishing/**` (2), `components/session/**` (4), `components/shell/palette`, `components/teamspace`, `components/ui/**` (4), `lib/api/**` (3), `lib/browser`, `lib/flow`, `lib/generated/protobuf`, `lib/project/**` (3), `lib/session/**` (2), `lib/shared/**` (3) and `lib/teamspace`.

```mermaid
flowchart LR
  self["components/project"]
  c0["app"] --> self
  c1["mock/surface"] --> self
  self --> d0["components/build"]
  self --> d1["components/flow/scope"]
  self --> d2["components/frame"]
  self --> d3["components/project/** (3)"]
  self --> d4["components/publishing/** (2)"]
  self --> d5["components/session/** (4)"]
  self --> d6["components/shell/palette"]
  self --> d7["components/teamspace"]
  self --> d8["components/ui/** (4)"]
  self --> d9["lib/api/** (3)"]
  self --> d10["lib/browser"]
  self --> d11["lib/flow"]
  self --> d12["lib/generated/protobuf"]
  self --> d13["lib/project/** (3)"]
  self --> d14["lib/session/** (2)"]
  self --> d15["lib/shared/** (3)"]
  self --> d16["lib/teamspace"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

ProjectStartForm asks what process Dex should run: the description becomes the first message of the
Project's Session, and the examples of `templates/` fill it in without sending. The Project's name follows
from a picked example's title or from the description; Advanced holds the name to change it and the
Session's model picker, preselected on Sonnet or for the administrator on the mock model where the server
offers it; the mock composer takes the first input of a scenario in place of free text when the mock model
is chosen.
ProjectCreationPage admits the managed source with a stable request ID and the Session's
first message with its message ID and model, which ProjectFlow starts the Session with, so the
start needs no open tab; it marks the Project's first conversation in this browser and opens
the Builder. ProjectBuilder follows the Project's stage (`lib/project/stage`): the chat
alone until the agent's first requirements card or the agent's own processes, then the
standard frame, whose tabs, Publish and Preview's status appear as they have something
to show. ProjectPage is the Project's
details: its status, repository, links into the Builder, the Preview's automatic
builds (read from the Project's Session, for those who build) and deletion. Storage errors
and aborted or stale responses retain their existing handling. Deletion always cleans
owned resources and archives the managed repository.

ProjectSurface puts the Project into the Builder frame (`components/frame`): the
Session's chat in the chat column and, for the surface the URL names, the Flow,
Runs, Inbox, Interface or the open tool (ProjectToolContent). Preview Runs and
Inbox read the running Preview; Live ones read the Release Live serves.
