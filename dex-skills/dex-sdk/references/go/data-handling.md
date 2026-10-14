# Go data handling

For application indexes, keep business Attribute names and supply explicit generic typed keys such as `keyword1`, `keywordList1`, `text1`, or `int1`; reuse the same typed slots across Flow types and constrain application run searches by FlowType. Parenthesize additional caller filters when composing the query. See [core primitives](../core/primitives.md#attribute). Runnable examples may use other index keys; they do not define the naming policy.

Use `dex.LockAttribute(CurrentMessagesLock)` in `ExecuteLockAttributes` (or the applicable method lock option) and registration-time `RPCOptions.LockAttributes`. Define the non-indexed singleton `currentMessagesLock` with `dex.DefineAttribute` and type `bool`, then register it alongside the map. Follow [whole-map coordination](../core/data-handling.md#whole-map-coordination): every protected writer uses the same singleton lock, its bool value is not an acquisition flag, and map loads remain explicit.

Step input/output describes transitions; Attributes hold current state; AttributeMaps partition it; Channels hold queued intent; Streams hold incremental output; BlobCache carries large serialized values. Choose one authority for each fact.

## Schema and commit boundary

Definitions are stable package-level values registered in `PersistenceSchema`. Concrete generic types determine codecs. Treat exported fields/types as contracts for open Flows.

Writes through `dex.Context` are buffered in an invocation and commit with its successful result. Do not report success externally before return. On error, expect retry from the prior durable boundary.

## Maps and selective loading

Map instance names must be non-empty and contain no `/`. Request exact instances for entity handlers and whole maps only for scans. An unrequested instance raises a typed not-loaded error; that differs from an absent key.

For append-only history, keep a bounded `current` instance and archive each full chunk under its zero-padded first sequence. Page by loading one chunk with `RPCInvokeOptions`; do not enumerate the whole map. Archived chunks stay immutable, while appenders lock and rewrite only `current`.

For keyed records, derive a stable partition name from the lookup key and keep a map keyed by the full canonical value inside the bucket. The official email pattern trims ASCII whitespace, lowercases ASCII letters, rejects empty or non-ASCII values, computes wrapping 32-bit FNV-1a, and selects `hash % 1000`. Keep those rules and the partition count stable across deployments. See the [sequential pattern](https://github.com/superdurable/dex/blob/main/examples/go/patterns/sequentially-chunked-attribute-map/workflow.go) and [hash pattern](https://github.com/superdurable/dex/blob/main/examples/go/patterns/hash-partitioned-attribute-map/workflow.go).

## Streams and large values

Use Stream for replayable progress and Attribute for latest authoritative state. Persist resume tokens. Buffered text reduces write amplification but adds a flush boundary; flush final chunks.

Client and Worker share BlobCache for values beyond inline limits. Size it for the working set and use suitable local storage. Do not put large opaque payloads in indexed Attributes. The template's `internal/worker/worker.go` creates the shared BlobCache; "Running a Worker" in the selected SDK module's `README.md` describes it.

The Server keeps payloads through 100 bytes inline by default. Treat internal blob references as opaque: hydration and cache lookup use the owning Flow ID with the reference, and Dex rewrites blob-backed values that cross into another Flow. String and Object references share a compact Base36-date shape such as `p1|c/ab3de7kp2x`; their Value arms distinguish them. Object Blobs store the complete EncodedObject with `json`, `raw`, or a custom encoding, so references have no encoding suffix.

Superverse's Dex Server stores successful ASYNC local Step method inputs for semantic history; this does not affect execution, retry, or recovery.

Go nil values use the Value null arm and decode through the requested Go type. In an Attribute write null deletes the Attribute, and as a Flow completion output it is discarded. Return an explicit result struct when terminal null and no output must differ.

## Evolution

Add payload fields compatibly, keep old variants readable, and never repurpose persisted fields. For breaking changes, add a new Flow/Step type and retain old definitions until open runs drain.
