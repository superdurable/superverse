# Agent protobuf contract

The authoritative Agent wire contract is `api/superverse/v2/superverse.proto` ([the current contract](resource-grpc.md)). AgentService holds the six Agent commands; SessionService reads and watches a Session (`GetSession`, `WatchSession`) and pages its archived messages (`ListArchivedMessages`). `SessionSnapshot.agent`, activity events, archived messages and question answers use explicit protobuf messages. Agent payloads no longer use `google.protobuf.Struct`. This coordinated V2 server/BFF change replaces the previous unshipped Struct encoding; those two components must be deployed from the same accepted source. Opaque tool arguments remain JSON strings; arbitrary JSON owned by other resources retains its existing Struct representation.

## Generated code and browser projection

`make generate` generates Go with the pinned Buf CLI and the `protoc-gen-go` and `protoc-gen-go-grpc` plugins pinned in the `go.mod` tool block. `npm --prefix web/studio run generate:protobuf` generates TypeScript with the lockfile-pinned `@grpc/proto-loader` generator. `npm --prefix web/studio run generate:check` regenerates into a temporary directory and compares the entire output inventory and bytes. Generated files are never manually edited.

Both generation and runtime decoding select string longs, string enums, default fields and oneof presence. The Agent DTOs keep their closed sets as nested enums with their numbers; each value name is the scope prefix plus the upper-cased domain value, such as `AgentMessage.Role.ROLE_ASSISTANT`, `AgentEvent.Kind.KIND_MODEL_STARTED` and `AgentDescription.Status.STATUS_WAITING_FOR_MESSAGE`, and zero is the `_UNSPECIFIED` value. The conversion section of `internal/dexai/api/agent.go` explicitly converts domain state to protobuf without provider-only metadata, mapping every domain value through `boundary.Enum`; an undeclared value is a programming error that answers Internal. `web/studio/lib/session/agent-view.ts` explicitly projects decoded messages into the retained UI shape:

- Each prefixed enum value becomes the lowercase literal the transport-free Agent UI keeps (`ROLE_ASSISTANT` is `assistant`, `KIND_MODEL_STARTED` is `model_started`), typed from the generated enum; an unspecified value is a server defect and fails the projection. A recovery decision's `resume`/`stop` and `retry`/`continue_with_unknown` map back to `RESOLUTION_*` and `ACTION_*` before the command is sent, so `components/agent` does not change.

- Integers must fit JavaScript's safe integer range; invalid or lossy values fail instead of silently rounding.
- Optional fields preserve legacy null versus omitted semantics, including meaningful zero indices and revisions. Protobuf oneof helper fields are excluded.
- Inner Agent timestamps retain the original RFC3339 string precision. Resource timestamps retain the standard protobuf Timestamp contract.
- Tool argument strings are not recursively transformed. Queued inputs, pending questions (and an old waiting requirements card), approvals, the todo list, the current requirements and archive history preserve their original fields.
- External MCP discovery fields are omitted under the user's explicit removal decision; generic tools, model adapters and the shared UI remain.

Browser presentation types derive from the generated messages and these projection return types in `agent-types.ts`, which defines only the UI's own literals (`AgentStatus`, `EventStream`, `EventKind`) and checks them against the projection. Studio no longer contains the obsolete OpenAPI-generated Agent client, its code generator configuration or its generator dependency. The same-origin BFF remains the browser entry point: `lib/api/agent.ts` and `lib/api/session.ts` post each RPC to `/api/rpc/<Method>` with the Project and Session IDs it names and the caller's cancellation signal, and the server derives authorization and Flow identity independently. They import only the generated types and `lib/shared`, so the enum mapping stays in `agent-view.ts`.

The Session Watch frame is `WatchSessionResponse {oneof event {snapshot, progress}, resume_cursor}`. A `SessionProgress` carries `oneof content {AgentEvent activity; string reasoning_summary; string assistant_text}` with `created_at` and the producing model call as `source`; `SessionProvider` reads `progress.content` to route an activity to the activity list and text to the assistant or reasoning stream. The page creates a new SendMessage `message_id` with `crypto.randomUUID()` for every send. The Agent does not deduplicate by it, so a send repeated after an uncertain failure that was in fact admitted queues a second message.

`agent-types.ts` is a source filename, not a separate wire schema. The term **contract** in this document describes the
actual server/BFF/browser protocol, whose producer, consumers and compatibility
rules are defined here.

The BFF's descriptor-driven decoder rejects unknown keys at any depth, a body `caller`, a zero or undeclared enum value and a mistyped field with 400 before protobuf encoding could discard them. Trusted caller data is supplied separately from the authenticated cookie as `caller = 100`. Browser `flowId` and `caller` properties never establish authority. Go also validates required fields, the presence of the optional `is_approved` and `expected_revision`, answer limits, identity and business admission, and rejects a zero or undeclared `Resolution` or `Action` as InvalidArgument before lower-casing its domain value.

## Dex Flow Changes

None. This transport migration does not change durable Flow identity, Steps, Attributes, AttributeMaps, Channels, ChannelMaps, Streams or retention. The existing Session/Agent/Sandbox contracts remain authoritative. Snapshot-first Watch, signed source cursors and authoritative state reconciliation remain unchanged.

## Database Schema Changes

None. No persistence or projection is added.

## UI/UX

The shared Agent UI is retained. Studio preserves conversation history, archive paging, queued and steered input, the progress line, the requirements card with its current version, approvals, questions, activity/text streams and workspace state. The typed wire migration does not introduce a second launcher, a generic operation resource or a fixture fallback.

## Tests

Historical pre-removal Go golden/protobuf and Node/mock browser checks passed on
2026-09-29. Their suites and golden files were then removed under the explicit
real-dependency-only policy; they are not current acceptance evidence. See the
transport parity audit for the historical scope.

Current static validation includes generated-code drift, Go build/vet/compile,
the single Studio build/typecheck/lint, including its local Agent presentation source. Retained real
AccountFlow/authenticated-gRPC integration passed against the owned Kind Dex and
Temporal. Real Agent approval/recovery/archive/stream coverage remains
incomplete until exercised through the merged server and browser. All three
complete Kind scenarios remain mandatory before hosted publication.

## Documentation

[Agent transport parity](agent-transport-parity.md) inventories the retained operations and intentional resource-API differences. Changes to Agent messages must update the canonical proto, generated Go/TypeScript, explicit safe projection and the relevant real integration/E2E verification together.

The unused `GetAgentCapabilities` RPC and its exclusive protobuf/provider catalog and
browser projection were removed by explicit user approval. Model configuration
remains server-owned; the current snapshot retains model/tool display fields.

### Session identity and command admission

AgentSnapshot no longer has field 1 (`run_id`); before launch removed fields are
not reserved. No business response contains an engine RunID. GetSession and every Watch snapshot use one internal
combined Session snapshot plus the separately owned Workspace/Preview view.
SendMessage, AnswerQuestions, UpdateSessionModel, DeleteQueuedMessage,
SteerQueuedMessage, ResolveToolApproval (formerly ApproveTool) and InterruptAgent each return
their own empty `<Rpc>Response`, which confirms admission only; Watch owns durable state. AnswerQuestions answers the pending
question batch named by `call_id` with one answer per question. A requirements card does not wait:
the Session records it as `confirmed_requirements`, the current version, and a person changes it
with SendMessage. A `KIND_REQUIREMENTS` pending input exists only on a Session that waited at a card
before cards stopped waiting, and SendMessage answers it. ResolveToolApproval, which approves or denies, takes an
explicit optional `is_approved`. A failure carries `AGENT_COMMAND_NOT_DISPATCHED`,
`AGENT_COMMAND_NOT_ADMITTED` or, for a Session whose engine execution stopped,
`SESSION_EXECUTION_FAILED`, `_CANCELED` or `_TERMINATED` only when the command
definitely did not run.

ListArchivedMessages pages back from an `int64` `before_sequence` ten-message
archive boundary with a `page_size` of 10 to 200 in steps of ten, 10 by default,
and returns `AgentHistoryPage history`, whose optional `next_before_sequence`
continues the paging.

GetAgentSnapshot, WaitForWaitingInputRound and ListRecentEvents public RPCs are
removed with their exclusive generated types and browser/client wrappers.
