# Session state

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The page-owned Session: `SessionProvider`, its owner hook with the single Watch and the Agent commands, the reducer, the Agent protobuf projection, message merging and the mock scenarios.

Importers: `components/agent/attachments`, `components/agent/conversation`, `components/agent/interaction`, `components/build`, `components/flow/scope`, `components/project`, `components/publishing`, `components/session`, `components/session/composer`, `components/session/executor`, `components/session/mock`, `components/session/start` and `lib/session/design` import this directory.

Imports: it imports `lib/api`, `lib/api/transport`, `lib/generated/protobuf`, `lib/session/attachments`, `lib/session/credentials`, `lib/session/draft`, `lib/session/model`, `lib/shared`, `lib/shared/problem` and `lib/shared/version` and calls the `SessionService` API.

```mermaid
flowchart LR
  self["lib/session"]
  c0["components/agent/attachments"] --> self
  c1["components/agent/conversation"] --> self
  c2["components/agent/interaction"] --> self
  c3["components/build"] --> self
  c4["components/flow/scope"] --> self
  c5["components/project"] --> self
  c6["components/publishing"] --> self
  c7["components/session"] --> self
  c8["components/session/composer"] --> self
  c9["components/session/executor"] --> self
  c10["components/session/mock"] --> self
  c11["components/session/start"] --> self
  c12["lib/session/design"] --> self
  self --> d0["lib/api"]
  self --> d1["lib/api/transport"]
  self --> d2["lib/generated/protobuf"]
  self --> d3["lib/session/attachments"]
  self --> d4["lib/session/credentials"]
  self --> d5["lib/session/draft"]
  self --> d6["lib/session/model"]
  self --> d7["lib/shared"]
  self --> d8["lib/shared/problem"]
  self --> d9["lib/shared/version"]
  self --> e0(["SessionService API"])
```
<!-- END GENERATED DIRECTORY SUMMARY -->
