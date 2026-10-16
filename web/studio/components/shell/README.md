# App shell

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The app frame: the sign-in and registration form, the signed-in sidebar with its rail, Projects, Team, theme and account, the account pages' bar, and the brand marks.

Importers: `app` imports this directory.

Imports: it imports `components/shell/palette`, `components/teamspace`, `components/ui`, `components/ui/pane`, `lib/account`, `lib/api`, `lib/api/transport`, `lib/browser`, `lib/generated/protobuf`, `lib/project`, `lib/shared` and `lib/teamspace`.

```mermaid
flowchart LR
  self["components/shell"]
  c0["app"] --> self
  self --> d0["components/shell/palette"]
  self --> d1["components/teamspace"]
  self --> d2["components/ui"]
  self --> d3["components/ui/pane"]
  self --> d4["lib/account"]
  self --> d5["lib/api"]
  self --> d6["lib/api/transport"]
  self --> d7["lib/browser"]
  self --> d8["lib/generated/protobuf"]
  self --> d9["lib/project"]
  self --> d10["lib/shared"]
  self --> d11["lib/teamspace"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The sidebar works in the current Teamspace (`lib/teamspace`): its switcher row opens the person's Teamspaces,
SidebarProjects lists every Project of the Teamspace with the person's role in it and refreshes them, and the Team
section below them (Connections, then People) stays in view while the Projects scroll. A Project in
which the person has no role carries only its name, status, setup status and Live presence and is not a link, because
its page cannot load. A Project with a role links to its Builder.
