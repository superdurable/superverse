# Flow scope

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Studio's only entry into the Flow UI: binds it to a Project-scoped Flow API, supplies navigation and the catalog notice, and loads its views on demand.

Importers: `components/build`, `components/project`, `components/project/setup`, `components/publishing/publish`, `components/session`, `components/teamspace/connections` and `mock/surface` import this directory.

Imports: it imports `components/flow/canvas`, `components/flow/connections`, `components/flow/connections/model`, `components/flow/connector/team-space`, `components/flow/design`, `components/flow/diagnostics`, `components/flow/frame`, `components/flow/map`, `components/flow/run`, `components/ui`, `lib/api/flow`, `lib/api/transport`, `lib/browser`, `lib/flow`, `lib/project`, `lib/session` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/scope"]
  c0["components/build"] --> self
  c1["components/project"] --> self
  c2["components/project/setup"] --> self
  c3["components/publishing/publish"] --> self
  c4["components/session"] --> self
  c5["components/teamspace/connections"] --> self
  c6["mock/surface"] --> self
  self --> d0["components/flow/canvas"]
  self --> d1["components/flow/connections"]
  self --> d2["components/flow/connections/model"]
  self --> d3["components/flow/connector/team-space"]
  self --> d4["components/flow/design"]
  self --> d5["components/flow/diagnostics"]
  self --> d6["components/flow/frame"]
  self --> d7["components/flow/map"]
  self --> d8["components/flow/run"]
  self --> d9["components/ui"]
  self --> d10["lib/api/flow"]
  self --> d11["lib/api/transport"]
  self --> d12["lib/browser"]
  self --> d13["lib/flow"]
  self --> d14["lib/project"]
  self --> d15["lib/session"]
  self --> d16["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

LiveFlowScope checks the selected Release against Project's current Live
pointer and binds every Flow API request to that pointer's Deployment ID.
The FlowCatalogProvider reads definitions from the deployed READY build through
that API, not GetRelease's prepared source. A later FAILED rebuild therefore
does not hide the running deployment's catalog. A deployment change remounts
the scope, cancelling the previous requests and closing its dialogs; stale
targets still fail server-side admission.
