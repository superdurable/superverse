# Mock backend

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Provides explicit development business-service fixtures through generated protobuf codecs and store Watches, without production fallback or acceptance evidence.

Importers: `app` imports this directory.

Imports: it imports `lib/generated/protobuf`, `lib/server`, `lib/shared`, `mock/fixture`, `mock/store`, `mock/store/delivery`, `mock/store/scenarios`, `mock/store/setup`, `mock/store/tasks` and `mock/store/tenancy`.

```mermaid
flowchart LR
  self["mock/backend"]
  c0["app"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/server"]
  self --> d2["lib/shared"]
  self --> d3["mock/fixture"]
  self --> d4["mock/store"]
  self --> d5["mock/store/delivery"]
  self --> d6["mock/store/scenarios"]
  self --> d7["mock/store/setup"]
  self --> d8["mock/store/tasks"]
  self --> d9["mock/store/tenancy"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The typed `RemoteAgentService` fixture lives with Agent services to preserve the
backend's ten-file limit. Builders may create ten-minute codes, list and remove
up to six dialog records per Project; operator access is refused. A weak store
key releases this state with the development store. Its endpoint is the reserved
`http://remote-agent.invalid` origin: development mode implements no remote bearer
listener, redemption, active executor or provider connection. Codes are generated
only for dialog interaction and are not retained or logged. Real Bridge pairing
and executor acceptance require the ordinary real-dependency local lanes.
