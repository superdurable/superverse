# Testing Go applications

Prefer integration tests against a real Dex Server. Unit tests can verify pure helpers, but mocks cannot prove registration, serialization, durable waits, Worker replacement, retry exhaustion, or terminal behavior. The template has no test target; add tests only when the user asks.

## Harness

Use a real Dex Server, start the application Worker, and create a Client from the same Registry and BlobCache. Give every test a unique Flow ID. Bound setup, calls, polling, and cleanup with `context.WithTimeout`.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/integ/main_test.go)
```go
func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}
```

## Production type renames

For a [type-name override](versioning.md#default-flow-and-step-type-names) that keeps an existing durable name, start a real Flow on the deployed type names, stop at a durable boundary, and replace the Worker with the renamed Go definitions or the upgraded SDK. Verify the registered Flow/Step strings are unchanged and the old execution resumes through its reachable Steps. A new execution alone cannot prove rename safety. New definitions keep the default type names.

## Required scenarios

1. Start, wait, and assert output plus terminal status.
2. Stop a Worker after a durable boundary; replace it with identical definitions and prove resumption. `Worker.Stop` drains in-flight handlers until its context expires, so it cannot simulate a crash during an in-flight Execute; run that Worker as a subprocess and SIGKILL it.
3. Fail Execute until retry exhaustion; assert terminal failure or configured recovery.
4. Publish Channel data before/during the wait; assert consumption, deletion, and RPC idempotency.
5. Restart across a Timer and prove it fires without sleeps in the test.
6. Write/read Stream frames using resume tokens.
7. Test graceful completion, force completion, failure, cancellation, timeout-handler, and uncompleted/dead-end behavior separately.
8. Verify [StartFlow ordering and retry identity](../core/testing.md#startflow-ordering-and-retry-identity), including StartFlow before any reconciliation RPC, same/different/generated Request IDs, IgnoreError enabled/disabled, and a lost accepted-start response.
9. Verify [downstream start recovery](../core/testing.md#downstream-start-recovery) when the owning Flow launches another top-level Flow; crash the Worker before launch and after acceptance but before Step commit, and prove continuation without an API-issued second start or duplicate execution.
10. For each terminal-readable entity, follow the shared [terminal entity read scenario](../core/testing.md#terminal-entity-reads): close the real Flow, invoke its typed `Get*` RPC through the application boundary, assert the final snapshot, and guard against lifecycle/history calls inside that read.

Use `require.Eventually` or Client long polls, never fixed sleeps for convergence. On deadline, report Flow/run IDs, summary, and relevant history. Do not assert scheduler ordering between parallel Steps; assert business invariants. Generate unique entity/map instances when sharing an Attribute Store.
