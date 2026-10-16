# Runs target cache and per-connection Watch authorization

Two per-replica hot paths of the business API: the Studio Runs target, which an
open run canvas polls about every 5 seconds, and the resource Watch, which
streams Dex progress. This document is the contract of what they cache, what
they always read afresh, and how fast a lost access ends a Watch. The
[`studio/runs`](../../internal/dexai/studio/runs/README.md) and
[`api/watch`](../../internal/dexai/api/watch/README.md) packages implement it.

## Problem

- **Studio Runs.** Each `Runs.Invoke` (and `Runs.InvokeLive`, the Agent's
  platform tools) resolved the whole target per request: authorization
  (PostgreSQL), the read model's Live target (PostgreSQL) or the Preview's
  serving state (Session Flow reads), the definitions bundle (S3 `GetObject`,
  KMS-decrypted in AWS, then parsed), the identity checks of the dial (Live:
  storage boundary, Dex Service and Dex Deployment, four Kubernetes API calls;
  Preview: namespace, Sandbox and Service GETs plus a Dex health check) and a
  fresh gRPC connection that it closed at the end of the request.
- **Session Watch.** Every streamed event reauthenticated the caller
  (PostgreSQL) and ran `AuthorizeSession`: Project authorization (PostgreSQL)
  plus a Dex read of the Session summary. A token burst of the assistant Stream
  cost two database queries and one Dex read per token.

## Runs: cache what is derived from a fresh identity, never the currency decision

The reads that decide whether a target is current stay per request:

1. `authorizeReady`: `ProjectFlowClient.Authorize` from the read model, and for
   a Live write `AuthorizeCurrent`, which confirms the Live pointer with
   ProjectFlow (decision 27).
2. Live: `ProjectDeployFlowClient.ReadLiveTarget` (one PostgreSQL row) and every
   comparison of `resolveLive` against the request. Preview:
   `SessionFlowService.ReadPreviewServing` and every comparison of
   `resolvePreview`.

So a read on stale rows resolves the deployment those rows select, a Live write
is refused once ProjectFlow's pointer moved, a read is refused once Dex writes
the new rows, and a request naming a replaced Preview instance is refused before
any cache is consulted (`TestLiveReadModelDexIntegration`,
`TestRunsPreviewTargetFollowsReplacedInstanceDexIntegration`).

Only values that are a pure function of identities returned by those fresh
reads are cached, in two bounded structures that each `Runs` owns per replica.

### Definitions cache

Parsed `*fdg.Bundle` values, keyed by the immutable content they were parsed
from:

| Target | Key |
| --- | --- |
| Live | the deployment's recorded `Inputs.Definitions` object: Key, Version and content Digest |
| Preview | the Preview's `Definitions` object (Key, Version and content Digest) with the ProjectID, SourceCommit and DefinitionRevision that the read checks it against |

An object reference whose content Digest is empty is not cached. The store
verifies the Digest on read and an immutable Put never rewrites a version, so
the bytes under a key never change: a cached entry can never be wrong, and its
10-minute TTL only releases memory held for targets nobody polls. A redeploy, a
new Preview revision or a replaced instance names other definitions, or
reaches the cache only after the fresh read admitted it. Errors (S3 failure,
digest or revision mismatch, invalid bundle) are never cached.

Bounds: at most 256 entries and 64 MiB of graph JSON per replica, LRU. A
bundle may hold up to 128 graphs of 8 MiB (`fdg`), so the cache is bounded by
bytes, not by 256 times the largest bundle; a bundle larger than the byte
budget is served without being cached. The parsed form holds the graphs and
their decoded contracts, a small multiple of the JSON bytes.

### Dex connection pool

Verified gRPC connections to a Live Dex or a Preview Sandbox Dex, keyed by the
recorded identity that the dial verified, which also determines the address:

| Target | Key |
| --- | --- |
| Live | ProjectID, storage boundary name and UID, Dex Service boundary, name and UID, Temporal namespace |
| Preview | ProjectID, WorkspaceRequestID, instance Generation, namespace, Sandbox and Service names and UIDs |

- A connection serves requests for 30 seconds after its dial verified the
  identity (`dexVerification`, a package constant). The next request after that
  retires it and dials again through the unchanged `infra.Connections`
  `DialProjectDex` / `DialSandboxDex`, which repeat every identity check.
- A replaced namespace, Sandbox, Service or Dex Service has new UIDs, so its key
  misses and verification runs; the stale entry ages out by LRU or TTL. A
  deleted-but-not-replaced object is refused earlier by the fresh currency read.
  A request that ends with the connection in `TRANSIENT_FAILURE` or `SHUTDOWN`
  retires it, so the next request verifies again.
- The pool owns every pooled connection. It dials on its own goroutine under a
  pool-owned context, detached from the request and bounded to 30 seconds, so
  the `context.AfterFunc` of the dial functions closes the connection exactly
  when the pool cancels it; `infra.Connections` does not change. Requests for a
  key share one dial; each waits with its own context.
- Leases are reference-counted: a retired entry closes when its last request
  releases it, so retirement never cuts a running request. Expiry is checked on
  access and the LRU tail is retired on insert; there is no janitor goroutine.
  `Runs.Close`, which `internal/dexai/bootstrap` calls at shutdown, retires every
  entry.
- The size is the server setting `studio.runsDexConnections` (default 128,
  1 to 4096; [configuration reference](../../cmd/superverse-server/config/README.md#studio)).
- `checkWorker` (Start only) is not cached: it proves the Worker serves at the
  moment of a Start.

`Runs.Invoke` and `Runs.InvokeLive` use both structures; `Definitions` uses the
definitions cache only.

## Watch: authorize per connection, on its own ticker

Every resource Watch (`watch.Resource`) declares `Authorize`, the cheap access
check of its resource. The Watch authorizes once when it starts (the caller's
`Reauthenticate`, then `Authorize`) and then on its own ticker every
`reconcileInterval` (5 seconds), independently of the snapshot reads, the
Stream reads and the sends. A failed authorization cancels the Watch's context
at once and the Watch ends with that error, so a blocked Stream read, a slow
snapshot read or a blocked send cannot delay it.

- Each `Authorize` is the authorization its snapshot read already performs,
  one read model query: Session, Release and Deployment `Authorize` with Build
  (a Session whose engine execution stopped stays readable, as in its
  snapshot); Project `ReadAuthorized` with View, which keeps a deleted Project
  readable for its terminal snapshot.
- The per-event path no longer authenticates. A Watch with `ResolveSources`
  (the Session) calls it without authorization; the Session's returns the
  sources of its latest snapshot, so an event costs no database or Dex read.
  Watches without `ResolveSources` (Project, Release, Deployment) keep their
  per-event snapshot read, which is also their generation check.
- The 5-second snapshot reread still authenticates and authorizes through
  `ReadSnapshot`.
- Revocation bound: a membership removal, a lost role, account deactivation or
  logout ends the stream with `PermissionDenied` or `Unauthenticated` within
  `reconcileInterval` plus the duration of one authorization (milliseconds;
  at most `readTimeout`, 30 seconds) after the change is visible to that
  authorization. Project roles are read from the SQL read model that Dex
  writes, so its sync lag precedes that bound.

## Dex Flow Changes

None. No Flow, Step, Attribute, Channel, Stream or RPC changes; the caches hold
only values derived from existing Flow and read-model reads, and every currency
decision keeps its owner (ProjectFlow, ProjectDeployFlow's read model rows,
SessionAgentFlow and SessionWorkspaceFlow).

## Database Schema Changes

None. No table, projection or mirrored state.

## UI/UX

None visible. Run canvas polls answer faster; a Watch whose caller loses access
closes within the stated bound.

## Tests

Real dependencies only (Dex, PostgreSQL, MinIO, Docker in Local Native or Kind);
no cache unit tests.

- `TestLiveReadModelDexIntegration` runs its unchanged assertions with the
  caches on the same `Runs`, then reads the Live deployment twice through
  `Invoke` and requires one dial (`DexDialsForTest`).
- `TestRunsPreviewTargetFollowsReplacedInstanceDexIntegration` reads the engine
  through `Invoke` on the first instance twice with one dial, and after the
  replacement on the new instance with a new dial: the changed identity misses.
- `TestWatchSessionRevocationDexIntegration` opens `WatchSession` through the
  lane's API as a Project builder of an idle Session, whose Stream reads block,
  removes the builder, and requires the stream to end with `PermissionDenied`
  within `reconcileInterval` plus a margin after the read model reflects the
  removal.

## Resource Scalability

No cloud, Kubernetes, Temporal, Dex or database resource is created. Per server
replica, in memory:

| Resource | Unit | Bound | Deleted |
| --- | --- | --- | --- |
| Parsed definitions bundles | definitions object in use on this replica | 256 entries and 64 MiB of graph JSON, 10-minute TTL | LRU or TTL on access |
| Pooled Dex gRPC connections (HTTP/2, one TCP socket each) | verified Live Dex or Preview Sandbox Dex in use on this replica | `studio.runsDexConnections` pooled (default 128), 30-second verification; a retired connection stays open only while a request still holds it | LRU, TTL, failed transport, `Runs.Close` |
| Watch authorization ticker | open Watch connection | one per connection | with the connection |

- Fleet worst case is replicas times `studio.runsDexConnections` sockets, and
  each target Dex holds at most one pooled connection per replica. The caches
  are per replica and never shared; correctness never depends on another
  replica's cache.
- Hot path: a Runs poll does the authorization and the currency read and, within
  30 seconds of the last verification, no Kubernetes or Docker call, S3 read or
  dial: the Kubernetes API sees at most four GETs per target per replica per 30
  seconds instead of four per poll per open canvas. A Session Watch event does
  no database or Dex read; each open Watch adds one authentication and one read
  model query per 5 seconds besides its snapshot reread (about 2,000 queries per
  second each for 10,000 open Watches), and no Dex read.
- The Kubernetes API server rate (API Priority and Fairness, cluster defaults)
  is the only provider limit on this path, and this change lowers its use.
- At 10,000 Projects with one open canvas each spread over the replicas, a
  replica holding more distinct targets than the pool size re-verifies the LRU
  tail, which degrades to the former per-request cost for those targets and
  never fails.
- Shared sharded Live Dex (B3): when Live Projects share Dex shards, a pooled
  connection is keyed by the shard's verified identity (its address and
  recorded UIDs) and the tenant travels with each request, so a replica holds
  one connection per shard rather than per Project and the pool size stops
  being a limit. The keying rule is unchanged, the recorded identity that the
  dial verified, which for a shard no longer includes the Project. The
  definitions keys are content digests and stay as they are.
