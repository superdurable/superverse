# Tenancy clients

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Browser clients of TeamSpaceService, TeamSpaceMemberService and ProjectMemberService over the business RPC route.

Importers: `components/teamspace` and `lib/teamspace` import this directory.

Imports: it imports `lib/api/transport`, `lib/generated/protobuf` and `lib/shared` and calls the `ProjectMemberService`, `TeamSpaceMemberService` and `TeamSpaceService` APIs.

```mermaid
flowchart LR
  self["lib/api/tenancy"]
  c0["components/teamspace"] --> self
  c1["lib/teamspace"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/generated/protobuf"]
  self --> d2["lib/shared"]
  self --> e0(["ProjectMemberService API"])
  self --> e1(["TeamSpaceMemberService API"])
  self --> e2(["TeamSpaceService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
