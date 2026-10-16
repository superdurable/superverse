# Project resources

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Project resources: the Project Watch provider and list, resource Watches, release versions, Process definitions, waiting runs, configuration reads, requirement gap names, the pending creation and the template's Design.

Importers: `app`, `components/build`, `components/flow/scope`, `components/frame/bar`, `components/project`, `components/publishing`, `components/publishing/publish`, `components/session`, `components/shell`, `components/shell/palette`, `components/teamspace/connections`, `lib/project/publishing`, `lib/project/setup` and `lib/project/stage` import this directory.

Imports: it imports `lib/api`, `lib/api/flow`, `lib/api/transport`, `lib/browser`, `lib/generated/protobuf`, `lib/generated/template-design` and `lib/shared` and calls the `DeploymentService`, `ProjectService` and `ReleaseService` APIs.

```mermaid
flowchart LR
  self["lib/project"]
  c0["app"] --> self
  c1["components/build"] --> self
  c2["components/flow/scope"] --> self
  c3["components/frame/bar"] --> self
  c4["components/project"] --> self
  c5["components/publishing"] --> self
  c6["components/publishing/publish"] --> self
  c7["components/session"] --> self
  c8["components/shell"] --> self
  c9["components/shell/palette"] --> self
  c10["components/teamspace/connections"] --> self
  c11["lib/project/publishing"] --> self
  c12["lib/project/setup"] --> self
  c13["lib/project/stage"] --> self
  self --> d0["lib/api"]
  self --> d1["lib/api/flow"]
  self --> d2["lib/api/transport"]
  self --> d3["lib/browser"]
  self --> d4["lib/generated/protobuf"]
  self --> d5["lib/generated/template-design"]
  self --> d6["lib/shared"]
  self --> e0(["DeploymentService API"])
  self --> e1(["ProjectService API"])
  self --> e2(["ReleaseService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
