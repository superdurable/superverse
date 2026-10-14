# API tests

A generated application tests each Flow through its HTTP API against a real
Dex and Temporal that exist only for the run, with connector mocks. The tests
verify source; they never prove provider, deployment or acceptance behavior.

## Harness

```go
//go:build apptest

func TestOrderApproval(t *testing.T) {
	env := apptest.Start(t, worker.Connections{Payments: paymentsmock.New(t, "payments")})
	accepted, err := env.API.CreateOrder(env.Context(t), &generated.CreateOrderRequest{RequestId: uuid.New()})
	// ...
	env.Eventually(t, 20*time.Second, func() bool { /* read through env.API */ })
}
```

- `apptest.Start` registers `worker.Application(connections)` on a Worker of
  the test engine and serves the real handler; `env.API` is the generated
  client. `env.Dex` is only for what Studio does: an Inbox Action, Start Flow
  or a Display read.
- `make test-app` generates the contracts (`make generate`), starts the engine
  and runs `go test -tags apptest` on `internal/apptest`; `make test-app RUN='TestA|TestB'` selects tests. Never
  start Dex or Temporal yourself, never sleep, never use `t.Parallel`.
- Mocks are imported only by `internal/apptest`. A production import of a mock
  package fails the commit gate (`connector_mock_in_production`).

## Mocks

The connector catalog names each connector's `mockPackage`. A mock fails the
test on an unscripted call and defaults to adversarial data (several pages,
unordered, duplicated), so script what the scenario needs: the branch a call
returns, a retry, an uncertain outcome, the pages of a paged read. `Calls()`
lists the calls a mock received; assert that an uncertain mutation was not
sent again. The mocks record every call they answer, and the gate counts a wired
branch as covered only when a test made a mock return it; a test reports
nothing itself. Until a connector has a mock package, the gate lists it in
`tests.uncovered` and you test what does not need the provider.

## Scenarios per Flow

- the happy path, and every wired connector branch with its visible outcome;
- paged reads: all pages, window edges, unordered input, duplicates, empty sets;
- a repeated `requestId`; Inbox Actions approved, revised, repeated and stale;
- an uncertain mutation that is reported and not resent;
- the first read after each write; input the API rejects with 4xx.

## The gate's test stage

`commit_and_push` runs `make test-app` after the static and definition stages,
then checks the run:

| Check | Fails as |
| --- | --- |
| A test failed, or the run timed out | `application_tests_failed` |
| A Step ended with 3 or more failed attempts, or an SDK usage error (state not loaded, panic) | `application_tests_failed` |
| No test ran; a rendered Flow never started; a wired branch of a mocked connector never returned | `application_tests_incomplete` |
| A confirmed requirement has no passing test and no `untested_reason` | `requirements_untested` |
| The engine did not start, or other runs held it past the stage limit | `test_environment_unavailable` |

Reading a failure: the report lists up to six failing tests, the engine
excerpts (`<FlowID> (<FlowType>) <status>; Step <Step> attempt <n>: <failure>`),
the coverage and requirement lines, the run directory under
`/tmp/superverse-apptest/runs/`, which keeps `output.log`, `summary.json` and
the engine logs, and the `make test-app RUN=...` to rerun. Read the directory
with `bash`, fix the source or the test, rerun the named tests, then call
`commit_and_push` again. Trace each confirmed requirement with the tests that
prove it, or an `untested_reason` when none can.
