# Application Instructions

This is a Dex `go-react-v1` application. `TextSummaryFlow` is a complete
reference: a browser form calls the API, the API starts a Dex Flow, a Step
calls a real connector, and the API and Studio read the result. Studio also
provides Configuration, Runs, Start Flow, Display, Actions and Inbox.

## Files to change for a feature

1. `internal/process/<name>_flow.go`: one Flow per file with its Attributes,
   Channels, Steps and RPCs, structured like `text_summary_flow.go` with its
   start and read helpers and per-branch result handling. Records several
   Flows share live in one domain Flow that the others read through its RPCs.
2. `internal/worker/connections.go`: one `Connections` field per connection,
   opened with the connector's generated `NewProjectConnection`; set every
   Step's `ConnectionName` to the same name. `internal/worker/application.go`:
   the Flow list. Never write credential codecs or providers, and leave
   `internal/connectorconfiguration` unchanged.
3. `dex-app.yaml`: one `flowDefinitions` entry per Flow file and one
   `connectors` entry per connection. Find connectors with `connector_catalog`,
   `go get <module>@<version>`, then read `connector.yaml` in the directory
   `go list -m -f '{{.Dir}}' <module>` prints.
4. `openapi/openapi.yaml`, then `make generate`, then `internal/api/handler.go`.
5. `web/src/App.tsx` and `web/src/styles.css` for the UI.
6. `internal/apptest/<name>_test.go`: the API tests (below).

If the application does not summarize text, delete `text_summary_flow.go`, its
connection, its `dex-app.yaml` entries, endpoints, form and tests.

## Read before using a feature

Under `/opt/superverse/dex-skills/`:

| Feature | Section |
| --- | --- |
| Loads, locks, shared records, cross-Flow reads | `dex-sdk/references/go/primitives.md` "Loading state", "Read another Flow from a Step" |
| Paged Query | `dex-sdk/references/go/patterns.md` "Page a connector Query" |
| Inbox Action | `dex-app-builder/references/fdg-authoring.md` "Answer a waiting Step" |
| RPC rejections | `dex-sdk/references/go/error-handling.md` "RPC outcomes" |
| Business time, polling | `dex-sdk/references/core/patterns.md` |
| Connector Steps, uncertain mutations | `dex-app-builder/references/connector-architecture.md` |
| API tests, mocks, a failed test stage | `dex-app-builder/references/api-tests.md` |

## Rules

- A clear request authorizes its edits, generation and checks. Decide names,
  storage and Flow design yourself; ask only for business facts the request
  leaves open. Confirm a new application or a change of the confirmed
  requirements once with `confirm_requirements` before code: a revised card
  keeps every confirmed feature's number and never reuses a removed one.
  Make a small change (copy, styling, a bug fix) directly. Check a user's
  technical claim against module source before acting on it.
- Keep records in Dex Flows: an Attribute for one value, an AttributeMap for a
  keyed collection. No files or databases.
- `openapi/openapi.yaml` is the only HTTP contract. Never edit or commit
  `internal/api/generated` or `web/src/api/generated`.
- Credentials live only in Studio Configuration, never in source, state or
  logs; their absence does not block a complete source commit. The
  text-summary endpoint is public and each call is a paid model request.
- Keep provider effects in Step `Execute`. Give paid or non-idempotent calls one
  attempt and an explicit unknown-outcome Step. Register every primitive in
  `GetPersistenceSchema`. Start Flows with a stable FlowID and `RequestID`.
- A Step or RPC receives Attributes and Channel sizes only. AttributeMap `Get`,
  `AllInstanceKeys`, `MapSize` and Channel `PendingMessages` need a declared
  load (`ExecuteLoad*` in `GetStepOptions`, `Load*` in `dex.RPCOptions`) or fail
  at run time. Writes, `Publish` and locks need none. Never add no-op code to
  silence a check.
- A Step that receives several connector branches switches on `result.Branch`;
  failure branches never look like success.
- Every Step `Execute` and RPC has a named `err` result and defers
  `logFailure(ctx, <Flow>{}, "<Step or RPC>", &err)`. Never log payloads.
- Every Flow is strict FDG 2.0 with `GetDexSummary` and `GetDexDisplay`; every
  Step has one group and one explanation. A Flow or Step type is its Go type
  name and durable identity: name it after its domain (`OrderFlow`) and do not
  rename a type with open runs.
- Versions are not pinned: move to a newer stable SDK, connector or Dex CLI when
  useful, after reading its "Breaking Changes"; keep `go.mod`, `go.sum` and
  `dex-app.yaml` consistent; never patch modules.

## Tests

- Test each Flow through the API: `env := apptest.Start(t, worker.Connections{...})`,
  then `env.API`; use `env.Dex` only for what Studio does (Actions, Start
  Flow). Wait with `env.Eventually`, never sleep; no `t.Parallel`.
- Pass each connection the mock from the connector's mock package (the
  catalog's `mockPackage`) and script every wired branch (see
  `text_summary_test.go`); the mocks record every call they answer, so a test
  reports nothing. A connector without mocks is listed as untested; test the
  rest. Mocks stay in `internal/apptest`.
- Cover every confirmed requirement: the happy path, each branch's visible
  outcome, empty sets, every page, a repeated `requestId`, repeated and stale
  Actions, an uncertain mutation that is not resent, and 4xx input.
- Never start your own Dex or Temporal; `make test-app` provides the engine.
  Mocked tests verify source only: name the real execution still unverified.

## Commands

- `make bootstrap` restores Go and npm dependencies (once per workspace).
- `make generate` regenerates the OpenAPI server and client.
- `make check-fdg-v2` validates every Flow, including its state loads.
- `make test-app RUN=<regexp>` generates the contracts and runs the selected
  API tests.
- `commit_and_push` runs the whole gate: `make check-static`, the platform's
  Flow-definition check, then `make test-app` with coverage and the
  requirement trace (each confirmed ID with its passing tests).
