# BFF server

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Provides BFF gRPC transport, account cookies, exact host and origin selection, event streaming, and bounded request decoding.

Importers: ``, `app`, `lib/server/connector-oauth`, `lib/server/flow`, `lib/server/login-host`, `lib/server/rpc`, `mock`, `mock/backend`, `mock/store`, `mock/store/attachments`, `mock/store/delivery`, `mock/store/setup`, `mock/store/tenancy` and `mock/store/validation` import this directory.

Imports: it imports `lib/generated/protobuf` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/server"]
  c0[""] --> self
  c1["app"] --> self
  c2["lib/server/connector-oauth"] --> self
  c3["lib/server/flow"] --> self
  c4["lib/server/login-host"] --> self
  c5["lib/server/rpc"] --> self
  c6["mock"] --> self
  c7["mock/backend"] --> self
  c8["mock/store"] --> self
  c9["mock/store/attachments"] --> self
  c10["mock/store/delivery"] --> self
  c11["mock/store/setup"] --> self
  c12["mock/store/tenancy"] --> self
  c13["mock/store/validation"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

`grpc-client.ts` loads the business services once and holds the
`RpcBackend` seam, `grpcBackend` (`callRpc` and its Watch counterpart) and the
gRPC-to-HTTP error mapping. `event-stream.ts` is the one Server-Sent Events
relay; `login-host/auth-handler.ts` serves `/api/auth`. The root never imports `rpc/` or
`flow/`, and nothing a `'use client'` module reaches may import this
directory (`npm run check:modules`).

For the effect-free GetRelease read only, upstream DEADLINE_EXCEEDED and
UNAVAILABLE become HTTP 503 with `Retry-After: 1`. This also covers the BFF's
own 35-second deadline; a slow read never claims a build failed. Other RPCs
retain their status mapping, including command deadlines and caller cancellation.
