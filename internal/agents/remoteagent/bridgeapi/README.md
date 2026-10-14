# Bridge protocol

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Declares the remote agent endpoint's paths, body limits, JSON requests, responses, reports and error codes, shared by the endpoint and the Bridge.

Callers: `cmd/superverse-bridge`, `cmd/superverse-bridge/mcpserver`, `cmd/superverse-bridge/platform`, `cmd/superverse-bridge/relay`, `cmd/superverse-bridge/state` and `internal/agents/remoteagent` import this package.

Dependencies: it imports no other package of this module and calls no remote API.

```mermaid
flowchart LR
  self["internal/agents/remoteagent/bridgeapi"]
  c0["cmd/superverse-bridge"] --> self
  c1["cmd/superverse-bridge/mcpserver"] --> self
  c2["cmd/superverse-bridge/platform"] --> self
  c3["cmd/superverse-bridge/relay"] --> self
  c4["cmd/superverse-bridge/state"] --> self
  c5["internal/agents/remoteagent"] --> self
```
<!-- END GENERATED PACKAGE SUMMARY -->

The [remote agent endpoint](../../../../docs/api/remote-agent.md)
and `cmd/superverse-bridge/platform` both build on these types, so the two sides cannot drift. The
package imports nothing from `internal/`.

- Every request but the downloads manifest is a `POST` with a JSON body; both sides refuse unknown
  fields. `pair` carries no bearer token; the others carry `Authorization: Bearer <token>`.
- A `Report` carries exactly one of `events` (at most 32), `toolCall` or `turnCompleted`, and is
  numbered by `sequence` from 1 per connection. `Report.Validate` checks the shape and the bounds.
- A refusal is an `ErrorBody` with a declared `ErrorCode`; `ErrorCode.Status` is its HTTP status.
  `sequence_gap` names the expected sequence; `rate_limited`, `reports_pending` (429: the Session
  holds 8 unconsumed reports or 2 MiB of them) and `unavailable` are transient, and the Bridge
  resends the same request, a report with its same sequence, after a backoff.
- `RemoteCallID` spells the call ID `remote-<epoch>-<sequence>` that the Session gives a remote tool
  call and that its `tool_result` entry answers.
- `sync` returns a projection: only user-role input entries this connection did not report and the
  `tool_result` entries of its remote calls, never assistant entries, thinking, system markers or
  platform tool arguments. `Entry.Text` is the entry's text blocks joined by a blank line: the exact
  string the model reads (an input's canonical sender envelope, which carries the actor), which the
  Bridge sends to Codex unchanged. `Entry.Delivered` decides which entries the Bridge sends.
- A sync answer aims for `SyncSoftBudgetBytes` (1 MiB) of entries but always includes the first
  entry after the cursor, so a Bridge reads up to `MaxSyncResponseBytes` (16 MiB). The next
  request's `after` is always the answer's `examinedThrough`. `dropped` names a range retention
  removed while the Bridge was behind. `examinedThrough` never passes an entry left out for the
  budget, so the next answer starts with it. When `stateDigest` differs from the request's, the
  pending question, requirements and todo list replace the cached values and an absent one clears;
  when it is unchanged, absent means keep. State values stay typed JSON whose text may end with
  "[truncated: open Studio for the full <field>]".
- `context: true` asks for `agentContext`, everything the platform tells the remote agent (at most
  64 KiB, outside the state digest); such a request is answered at once, never held as a long
  poll. The Bridge relays it into its brief and its MCP `instructions` and adds none of its own.
