# Studio routes

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The Next.js routes: signed-in and sign-in pages, the BFF route handlers and the `/dex` Flow API routes, and the only files that choose mock or production code.

Importers: no production directory imports this directory.

Imports: it imports `components/project`, `components/session/remoteagent`, `components/shell`, `components/teamspace`, `components/teamspace/connections`, `lib/account`, `lib/browser`, `lib/generated/protobuf`, `lib/project`, `lib/server`, `lib/server/connector-oauth`, `lib/server/flow`, `lib/server/rpc`, `lib/shared`, `lib/teamspace`, `mock`, `mock/backend`, `mock/store`, `mock/store/attachments` and `mock/surface` and calls the `AccountService` API.

```mermaid
flowchart LR
  self["app"]
  self --> d0["components/project"]
  self --> d1["components/session/remoteagent"]
  self --> d2["components/shell"]
  self --> d3["components/teamspace"]
  self --> d4["components/teamspace/connections"]
  self --> d5["lib/account"]
  self --> d6["lib/browser"]
  self --> d7["lib/generated/protobuf"]
  self --> d8["lib/project"]
  self --> d9["lib/server"]
  self --> d10["lib/server/connector-oauth"]
  self --> d11["lib/server/flow"]
  self --> d12["lib/server/rpc"]
  self --> d13["lib/shared"]
  self --> d14["lib/teamspace"]
  self --> d15["mock"]
  self --> d16["mock/backend"]
  self --> d17["mock/store"]
  self --> d18["mock/store/attachments"]
  self --> d19["mock/surface"]
  self --> e0(["AccountService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->

Route segments follow Next.js conventions and have no README of their own;
this one covers all of `app/`. A route file stays thin: it reads its route
parameters, chooses between the production code and `mock/` with
`isStudioMockEnabled()`, and hands the request to its owner in `lib/server/` or
renders a component. `globals.css` loads the stylesheets in cascade order and
`layout.tsx` applies the stored theme before first paint.
