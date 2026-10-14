# Observability for Go applications

## Correlation and signals

Log Flow ID, run ID, Step type/execution ID, attempt, and operation. Keep high-cardinality IDs out of metric labels. Persist external request IDs when needed across retries.

Heartbeat is liveness/checkpoint data for one long attempt, not business state. Resume only when a previous value exists, and return heartbeat errors.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/heartbeat/workflow.go)
```go
for batch := completedBatches; batch < batches; batch++ {
	select {
	case <-ctx.Done():
		return dex.DeadEnd(), nil
	default:
	}
	time.Sleep(2 * time.Second)
	if err := ctx.RecordHeartbeat(batch + 1); err != nil {
		return nil, err
	}
}
return dex.GracefulComplete("processed"), nil
```

Use summary/status for execution state, history for causality, a typed snapshot RPC for business state, Streams for progress, and metrics for saturation/failure rates. Track attempts, exhaustion, timeout handlers, RPC latency/conflicts, Channel backlog, Stream lag, Worker connectivity, and BlobCache errors.

For a stall: confirm IDs/status; inspect latest history and active attempt; verify registered types, since an open Flow keeps the [type name](versioning.md#default-flow-and-step-type-names) it started with; verify Dex can reach the advertised Worker target; then inspect the application error chain. `examples/order/client.go` in the selected SDK module (directory: `go list -m -f '{{.Dir}}' github.com/superdurable/dex/sdk-go`) calls every Client API.
