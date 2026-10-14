# Production operations

Before reporting a mutation as effective or complete, identify its [write/read consistency and completion boundary](read-after-write.md). CLI `updated: true` and `skipped: true` acknowledge request acceptance. A subsequent Flow state read may block until the Flow processes the accepted request; search and projections can still lag.

Use this guide for production inspection, authorized recovery, and the project Dex engine boundary. Read [testing.md](testing.md) for verification scenarios and [troubleshooting.md](troubleshooting.md) for diagnostic routing.

## Diagnostic order

1. Capture the exact Flow ID, run ID, Flow type, SDK version, and Dex Server address.
2. Check the application logs for handler and connectivity errors. The Preview logs are in `/var/lib/superverse/preview/logs` in the Sandbox ([local environment](getting-started.md#local-environment)); the Superverse Agent reads the end of the Live application's log with `read_live_app_runs` and `operation: logs`. The application template logs every Step or RPC that returns an error and every 5xx response.
3. Inspect the run in Studio Runs and Diagnostics; without Studio access, ask the user for what it shows when that evidence is needed.
4. Run a bounded read-only inspection. In the Sandbox, dexcli reads the Preview engine at `127.0.0.1:8801` (it uses `--server host:port`, else `DEX_FLOW_SERVICE_ADDRESS`, else that default). dexcli does not reach Live or the platform: the Superverse Agent reads Live runs with `read_live_app_runs` (search, summary, state or history, without blobs), and the Setup, Preview, Release and Deployment of its Project with `read_delivery_status` and `read_platform_flow`. Their output is redacted and names no run ID:

```bash
dexcli flow summary <flow-id> --timeout 30s --no-hydrate
dexcli flow state <flow-id> --timeout 30s --no-hydrate
dexcli flow history <flow-id> --page-size 80 --timeout 30s --no-hydrate
```

5. Compare the active Step, Attributes, Channel waits, Timers, and recent semantic events with the intended graph.
6. Reproduce with the smallest matching integration test.
7. Fix application code or configuration, then decide whether existing executions need recovery.

## Inspect pending Channel messages

Invoke the application's typed snapshot RPC to obtain a Channel's current FIFO values and server-assigned IDs. An ID disappears once its message is consumed or deleted. If delete or transactional move returns Channel-message-not-found, treat the local view as stale, invoke the snapshot RPC again, and let the user choose again.

Only pending Channel state is mutable through these operations. Editing a message means deleting it successfully and publishing a replacement with a new ID; it does not rewrite Flow history or application conversation Attributes.

Use a transactional RPC when deletion must commit atomically with a replacement publication or other Flow-state writes. Without transactional execution, a missing deletion may be a no-op while other effects commit; reconcile from a fresh list.

For an application queue UI, prefer one application snapshot RPC that returns durable conversation state, description, and loaded pending queues together. Refresh that snapshot after mutations and live events, on focus or reconnect, and periodically at low frequency. Keep optimistic items only as a short bridge; the snapshot is canonical.

Use **dexcli flow search**, **summary**, **state**, and **history** for narrower JSON output. Use **--no-hydrate** when payload contents are unnecessary or sensitive.

## Inspect raw Temporal payloads

Dex stores its internal Temporal payloads as binary protobuf.
`dexcli codec-server` starts a loopback Codec Server (`http://127.0.0.1:8804`;
`--address` selects another loopback address) that a browser on the same
machine uses to show known Dex payloads as ProtoJSON in Temporal Cloud or a
local Temporal Web UI; other payloads pass through unchanged. The Superverse
sandbox has no browser or Temporal Web UI, so inspect Flows there with
`dexcli flow state`, `history`, or `inspect`, which return decoded Dex values.

## Indexed Attribute capacity

The Sandbox's Preview engine and the Local Kind Live engines run Temporal's development server with SQLite visibility; hosted Live engines use Temporal Cloud. SQLite allocates a fixed number of custom Search Attribute slots per namespace and type. In a fresh SQLite namespace, Dex uses three of those slots for system indexes, leaving this initial capacity for application Indexed Attributes:

| Dex index type | SQLite slots | Dex system indexes | Application slots initially left |
| --- | ---: | --- | ---: |
| **KEYWORD** | 10 | **FlowType**, **DexParentFlowID** | 8 |
| **KEYWORD_ARRAY** | 3 | **ActiveStepTypes** | 2 |
| **FULL_TEXT** | 3 | None | 3 |
| **BOOL**, **DATETIME**, **DOUBLE**, **INT** | 3 each | None | 3 each |

Index keys are shared across Flow types. Reusing one key with the same type does not consume another slot. Keys registered earlier in the same namespace remain and reduce the current remainder. Worker startup fails while synchronizing indexes if a new key exceeds the remaining capacity.

Plan one namespace-wide pool before implementation. All application index keys must be explicit generic typed slots, reusable by every Flow type: **keyword1**, **keyword2**, **keywordList1**, **keywordList2**, **text1**, **text2**, **int1**, **int2**, **bool1**, **double1**, **datetime1**, and later numbers as needed within the available capacity. **keywordListN** uses **KEYWORD_ARRAY** and **textN** uses **FULL_TEXT**. Keep one type per physical key throughout the namespace; allocate different slots for different indexed fields within one Flow. Keep business meaning in Attribute names and document each Flow type's slot mapping. Application run searches must constrain **FlowType** and parenthesize additional caller filters before combining them with it. Dex system indexes keep their reserved names. Do not index sensitive data or PII.

Changing an index key, type, or enabled state does not remove the old Search Attribute registration or backfill existing Flow executions. Allocate a new slot rather than retyping a used one.

The SQLite pool limits the Preview and Local Kind engines, not the application schema; do not remove production query fields merely to fit it. Temporal Cloud has its own per-namespace limits; see [Temporal's current Search Attribute limits](https://docs.temporal.io/search-attribute#custom-search-attribute-limits).

## FDG 2.0 management metadata

Superverse Studio reads the FDG 2.0 management metadata that the Go analyzer
(`dexcli visualize --schema-version 2.0`) generates from each Flow file listed
in `dex-app.yaml`: Indexed Attribute presentation, Summary and Display RPC
fields, Action forms and permissions, groups and explanations, and Action input
capture hints. Design, Start Flow, Runs, Display, Actions, and Inbox depend on
it. `make check-fdg-v2` validates it strictly; see
[FDG authoring](../../../dex-app-builder/references/fdg-authoring.md).

## Connector credentials

Superverse Studio Configuration holds connector connections and credentials; Preview uses the Session's scope and Live the project's. The application declares each connection in `dex-app.yaml`, uses operation-specific factories with a static connection name and static Trigger binding names, and loads connections through the template's `internal/connectorconfiguration`; see [Connector architecture](../../../dex-app-builder/references/connector-architecture.md#hosted-configuration-and-credential-boundary). Never put credential material, access tokens, API keys, or secret-bearing generated values into Flow state, logs, browser messages, or application responses.

Connection credentials and Trigger binding matchers are separate. One connection may serve several Flows without sharing their filters. Trigger delivery is at least once: use deterministic Flow IDs for starts. Application RPCs own their redelivery policy, bounded deduplication state when needed, and business locks.

## Native hosted project configuration

In Superverse, project configuration and credentials are owned by Superverse:
Studio Configuration edits them, and the Superverse configuration host stores
them with the Connector SDK's `sdkgo/projectconfig` storage contract. Dex Server
runs only the engine (`api,interpreter`) and has no project configuration UI or
endpoints.

Ordinary snapshots contain settings and logical connection IDs; credentials
remain separately versioned private objects. The application loads its accepted
snapshot at startup through the template's loader, so a settings edit needs a
new Preview start or Live deployment, while a credential replacement or refresh
applies on the next provider call. The Connector SDK refreshes a known-expired
credential at actual use after durable admission; an uncertain response or
restart is reconciled rather than blindly dispatched again. Never persist
provider secrets in application Flow state or return them to the browser.

## Deploy Dex Server components

Superverse provisions, configures, and upgrades every Dex engine; an application never deploys Dex Server. A project's Live engine runs the `api` and `interpreter` services without Dex Web and serves FlowService gRPC on port 8801. The Preview engine is the Dex Server that the Sandbox's `dexcli` embeds, serving 127.0.0.1:8801; its Dex Web is never exposed. Superverse sets `DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, and `DEX_WORKER_TARGET` for the application's Worker and Client in Preview and Live, and in Live also `DEX_WORKER_HEADLESS=true`: the Live target is one endpoint per project that the Live Dex resolves itself and that each new Live deployment takes over.

A Live deployment therefore moves every open Flow to the new version's code, as a rolling deploy does: a Flow that the previous version started runs its next Step on the new version. The new version is responsible for compatibility with those Flows. Keep a Flow type, Step type, Attribute or Channel registered, with a compatible payload, while an open Flow may still use it, or finish or migrate those Flows first. A Step that the new version no longer registers fails each attempt with a definition error, which the Flow's history shows; once its retry policy's total duration passes, the Step takes its failure route or fails the Flow.

Worker startup calls `GetServerInfo`, selects the highest protocol common to the SDK and Server intervals, and stops before binding when they do not overlap. `dexcli version check` compares the CLI's interval with the Server's in the same way.

The engine offloads payloads larger than 100 bytes to its Blob Store. Application code must treat blob references as opaque; `--no-hydrate` shows the references instead of the values.

## Safe recovery

Diagnosis is read-only by default. Stop, time travel, publish, invoke, skip a Timer, or mutate Attributes only when the user asks to change the Flow. In the Sandbox these dexcli subcommands need `--allow-mutation`, which is reserved for that explicit request.

Before a mutation:

- resolve the current exact run
- explain the expected state change
- use an application Flow RPC or an explicit dexcli operation
- satisfy any explicit confirmation flag
- re-inspect the Flow afterward

Time travel is appropriate after deploying a code fix when replaying from a safe Step boundary will not duplicate an unprotected side effect. Preserve the original business resource, Flow ID, and operation identity; capture the exact diagnostic run and Step method before mutation. Reconcile provider objects and previously accepted writes first. Do not replace the account or resource merely to avoid its failed history. If a safe boundary cannot be established, design an explicit recovery or compensation Step instead.

A time-travel reset cannot invent a missing method boundary or rewrite the original Start options. A failure before the first Execute may have no Execute reset point. Do not silently replace it with a fresh Flow. If a corrected start is needed, obtain authorization for that recovery, prove the old execution is closed, reconcile effects, and preserve the intended Flow ID and logical request identity under the exact reuse policy. Closed-execution reuse is not cross-run request deduplication; application admission must protect external effects. Where a recorded WaitFor boundary is appropriate, select that method explicitly and decide whether replaying later writes is safe before choosing `--skip-writes-reapply`.

When a durable-history bug has already failed a Flow, validate the fix against that
same history when safe:

1. Deploy or restart the Worker with the corrected code.
2. Time travel the failed Flow to the last safe execution before the failure.
3. Let Dex replay and continue with the corrected Worker.
4. Re-inspect the new run and verify the formerly failing path and durable state.

This is stronger than testing only a new Flow because it exercises recovery from the
actual recorded history. Do not cross an external side effect unless it is idempotent,
compensated, or explicitly safe to repeat.

Sources:

- Dex CLI: https://docs.superdurable.io/references/cli
- Application operations: https://docs.superdurable.io/production/application-operations
- Versioning: https://docs.superdurable.io/production/application-operations#versioning-flow-code
