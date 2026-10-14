# Go design patterns

Each item states its invariant and links a runnable example in the Dex repository. Preserve the invariant when adapting code.

## Parallel Steps

- **Static:** emit a fixed heterogeneous set with `GoToMany`; branches are independent. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel/static.go).
- **Dynamic:** build `[]dex.StepMovement` from input; each branch carries retry-safe input. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel/dynamic.go).
- **Await all:** workers publish once and `DeadEnd`; a coordinator waits on `Channel.ForN(count)`. Count exactly once per successful branch. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel/await_parallel.go).
- **First win:** winner completes with `CancelSiblingSteps`; losers tolerate cooperative cancellation and cannot leave uncompensated commits. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel/first_win.go).

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel/static.go)
```go
func (staticInit) Execute(_ dex.Context, input string) (*dex.StepDecision, error) {
	return dex.GoToMany(dex.MovementOf(workA{}, input), dex.MovementOf(workB{}, input)), nil
}
```

## Parallel SubFlows

Apply the Core SubFlow evolution gate before using these APIs. The runnable
examples document confirmed parent-child shapes; they do not make SubFlows the
default for new designs or for ordinary parallel work.

- **Basic:** turn each request into `dex.SubFlow` and wait with `AllOf`; use only when one parent can hold the batch. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/basic_parent_flow.go).
- **Long-lived parent:** RPC checks queue size and publishes; a loop drains into children. Complete only after stop plus drain invariants. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/advanced_long_live_parent_flow.go).
- **Short-lived parent:** process a bounded batch and atomically complete only when Channels are empty. A later run uses explicit ID reuse. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/advanced_short_live_parent_flow.go).
- **Wait for half:** race children against an all-done signal and wait for `(total+1)/2`; success means quorum, not universal completion. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/wait_for_half_parent_flow.go).
- **Partitioning:** hash a stable affinity key to a fixed parent-ID set. Version the set when changing count would break affinity. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/submit_request_flow.go).
- **Back pressure:** submit through a durable Flow; rejection becomes a Step error, so retry/backoff survives Worker loss. Bound the parent queue before accepting. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/submit_request_flow.go).

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/patterns/parallel-subflows/basic_parent_flow.go)
```go
func (step subFlows) WaitFor(_ dex.Context, requests []string) (*dex.Wait, error) {
	conditions := make([]dex.Condition, 0, len(requests))
	for _, request := range requests {
		conditions = append(conditions, dex.SubFlow(step.exampleFlow, request))
	}
	return dex.AllOf(conditions...), nil
}
```

## Polling

Apply the Core [Polling pattern](../core/patterns.md#polling) to every wait on an external system: one long-running Step's `Execute` loops until a terminal status, and its StepOptions alone bound the wait. Follow the [design pattern](https://docs.superdurable.io/design-patterns/polling) and its Go `PollingFlow` example.

- **Round:** call the provider through `context.WithTimeout(ctx, 10*time.Second)`. Return a transient error wrapped with `dex.ErrorWithStack`; “not ready” stays in the loop. Return the next decision on a terminal status, including a business-failure decision when the provider reports a definite failure. Never compute a deadline in the loop.
- **Progress:** write a Stream frame when the status changes, otherwise call `ctx.RecordHeartbeat(checkpoint)`; then `time.Sleep(interval)`. Do not `select` on `ctx.Done()`. A new attempt resumes with `ctx.GetLastHeartbeatValue(&checkpoint)`; the checkpoint holds resume state such as the last reported status, never a deadline.
- **StepOptions:** `ExecuteMethodTimeout` is the maximum wait; `ExecuteRetry` allows a few attempts with `TotalDuration` equal to the maximum wait (all attempts, measured from the first); keep `HeartbeatTimeout` at one minute with `interval + call timeout <= HeartbeatTimeout - 10s`; `ExecuteDurability: dex.StepDurabilitySync` under an ASYNC Flow default.
- **Expiry:** set `ExecuteFailure: dex.ProceedToOnExecuteFailure(recordFailureStep, nil)`. The recovery Step reads `ctx.RecoveryError()` (`Detail`, `ErrorType`) and records the business failure; without `ExecuteFailure` the Flow fails.
- **Forbidden:** a WaitFor Timer plus `GoTo` loop, `RetryAfter` or retry policy as the loop, and a hand-written deadline that duplicates `ExecuteMethodTimeout` or `TotalDuration`. Paging a released connector Query is a bounded Step loop, not polling; see [Page a connector Query](#page-a-connector-query).
- **Beside other work:** when the poll is long and the Flow has other ongoing work, follow the Core [long poll beside other work](../core/patterns.md#a-long-poll-beside-other-work): register a child Flow whose starting Step is the polling Step, return `dex.Until(dex.SubFlow(child, input))` from the parent Step's `WaitFor`, and read the child's output in `Execute` with `dex.SubFlowResult(ctx)` and `DecodeSingleOutput`. To stop the child when the parent shuts down, race it in `dex.AnyOf` against a stop Channel or the child's instance of a stop ChannelMap (`stops.ForOne(childKey)`), publish the stop at shutdown instead of cancelling the waiting Step, and when `GetConditionResults` holds the stop, pass `dex.SubFlowID(ctx)` to `Client.StopFlow` (from a dependency the Step holds), treating `*dex.FlowNotActiveOrNotFoundError` as stopped. Pass `dex.SubFlow` the same type the Worker registered, a pointer when the registry holds pointers.

[SDK README example](https://github.com/superdurable/dex/blob/main/sdk-go/README.md)
```go
func (ParentStep) WaitFor(_ dex.Context, input ChargeInput) (*dex.Wait, error) {
	return dex.Until(dex.SubFlow(ChargeFlow{}, input)), nil
}

func (ParentStep) Execute(ctx dex.Context, _ ChargeInput) (*dex.StepDecision, error) {
	result, err := dex.SubFlowResult(ctx)
	if err != nil {
		return nil, err
	}
	var receipt Receipt
	if err := result.DecodeSingleOutput(&receipt); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(receipt), nil
}
```

In a parent with other ongoing work, this Step runs as one parallel branch, and its `Execute` returns the parent's next decision for the poll's result instead of completing the Flow.

## Page a connector Query

A released Query returns one page and its next-page output, such as github `listReleases` with `ReleasePage.NextPage` (zero on the last page). Read every page:

1. The first Step freezes the business window once from `ctx.FirstAttemptAt()` ([Capture business time once](../core/patterns.md#capture-business-time-once)) and stores the page request in an Attribute, because a Query result does not carry its input.
2. The Query Step maps that request to the operation input.
3. A store Step filters the page by the stored window, not by provider order, and writes each item to an AttributeMap keyed by the provider's stable ID, so a repeated page or a retry overwrites instead of duplicating. While the next-page output is non-zero, it stores the next request and moves back to the Query Step, up to a page bound.
4. Reaching the bound with pages left is a visible outcome, such as an incomplete status, never a silent success.

```go
const maximumReleasePages = 20

type ReleasePageRequest struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Page       int    `json:"page"`
}

dex.DefineStep(github.NewListReleasesStep(github.ListReleasesStepConfig[ReleasePageRequest]{
	StepType: "ListReleases", Connection: flow.GitHub, ConnectionName: GitHubConnectionName,
	Annotations: sdkgo.StepAnnotations{GroupID: "collection", GroupLabel: "Releases", Explanation: "Read one page of releases."},
	MapToOperationInput: func(request ReleasePageRequest) github.ListReleasesInput {
		return github.ListReleasesInput{Owner: request.Owner, Repository: request.Repository, Page: request.Page, PageSize: 100}
	},
	Listed: sdkgo.GoTo(StoreReleasePage{}),
	// Every failure branch routes to a Step that records it.
	NotFound: sdkgo.GoTo(RecordReleaseFailure{}), ProviderRejected: sdkgo.GoTo(RecordReleaseFailure{}), // ...
})),

func (StoreReleasePage) Execute(ctx dex.Context, result github.ListReleasesResult) (*dex.StepDecision, error) {
	window, err := ReleaseWindow.Get(ctx)
	if err != nil {
		return nil, err
	}
	request, err := CurrentReleasePage.Get(ctx)
	if err != nil {
		return nil, err
	}
	for _, release := range result.Value.Releases {
		// Provider order is not publication order: filter every item and never stop early.
		if release.PublishedAt == nil || release.PublishedAt.Before(window.Start) || !release.PublishedAt.Before(window.End) {
			continue
		}
		if err := CollectedReleases.Set(ctx, strconv.FormatInt(release.ID, 10), release); err != nil {
			return nil, err
		}
	}
	if result.Value.NextPage == 0 {
		return dex.GoTo(DraftNewsletter{}, nil), nil
	}
	if request.Page >= maximumReleasePages {
		return dex.GoTo(RecordIncompleteReleases{}, nil), nil
	}
	request.Page = result.Value.NextPage
	if err := CurrentReleasePage.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ReleasePageRequest]("ListReleases"), request), nil
}
```

Writing a map instance needs no load; the Step that enumerates `CollectedReleases` loads the whole map ([Loading state](primitives.md#loading-state)). Each page is one Query execution with its own retry and failure routing, so this loop is not polling.

## Durable Timer

- **Cron:** race Timer with trigger/skip Channels, then schedule the run and next wait in parallel. Validate interval/count. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/cron/workflow.go).
- **Reminder:** race Timer with opt-out, send at most once each iteration, and carry schedule in durable input. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/reminders/workflow.go).
- **Inactivity:** race Timer with activity; activity restarts wait, Timer moves to processing. Drain/coalesce bursts. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/inactiveness-tracker-timer/workflow.go).

## Failure handling

- **Execute recovery:** proceed to an idempotent compensating Step with enough durable input to identify the original commit. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/recovery/workflow.go).
- **WaitFor recovery:** proceed only when the recovery Step can distinguish failure and rebuild wait-side state. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/primitives/proceed-on-wait-failure/workflow.go).
- **Manual recovery:** after exhaustion, wait on durable operator decision Channels exposed through action-verb RPCs. Add durable command-ID deduplication when repeats must be idempotent. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/intervention/workflow.go).
- **Graceful timeout:** start with Handler policy and return an explicit terminal decision from `HandleTimeout`; request map/channel loads it needs. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/timeout/workflow.go).

## Draining, interruption, responsive updates, and entity state

- **Internal Channel drain:** coordinate main/side Steps, then finalize only after accepted messages drain. Keep publish/consume/delete accounting aligned. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/drain-channels/internal-drain/workflow.go).
- **External publishing drain:** accept via RPC while active and use `ForceCompleteIfChannelsEmpty` for atomic close. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/drain-channels/external-publishing/workflow.go).
- **Interruptible execution:** RPC records a signal; work uses bounded slices and checks between them. Latency is the slice duration. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/interruptible/workflow.go).
- **Responsive Step:** Client waits for one named Step boundary, not Flow completion. The server derives a stable namespaced Request ID from the Step execution when options omit one. See `dex/client_integration_test.go` in the selected SDK module.
- **Responsive Attribute:** wait for a revision match, keep the returned watermark, then reload durable state. The server derives a namespaced Request ID from the Attribute predicate. See the same SDK test file.
- **Responsive Stream:** long-poll and resume with the returned token; make consumers duplicate-tolerant. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/primitives/stream/controller.go).
- **Entity store:** use AttributeMap instances for entities, opt into Attribute Store, and lock exactly the instance an RPC mutates. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/entity-store/workflow.go).

## Partitioned AttributeMap state

- **Sequentially chunked:** `ChunkedSubscriberFlow` keeps at most 100 ordered records in `current`, archives full chunks under zero-padded starting sequences, and reads one page through `RPCInvokeOptions.LoadAttributeMapInstances`. Use this for append-only message, event, or subscriber history. Concurrent appenders share the `current` lock and retry a rejected call after `RPCLockConflictError`. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/sequentially-chunked-attribute-map/workflow.go).
- **Hash-partitioned:** `CustomerDirectoryFlow` canonicalizes ASCII email, computes wrapping FNV-1a 32-bit, and stores the complete canonical key in one of 1000 buckets. Upserts pass the same bucket through both `LockAttributeMapInstances` and `LoadAttributeMapInstances`; reads load only the bucket. [Runnable](https://github.com/superdurable/dex/blob/main/examples/go/patterns/hash-partitioned-attribute-map/workflow.go).

Keep the hash algorithm and partition count versioned with persisted state. Hash collisions share a bucket but remain distinct map entries. Changing 1000 to another count requires rehashing every record or introducing a new Flow version.

### Revision Attribute match

Initialize the integer revision Attribute to zero. Every state-changing Step or RPC must use the same Attribute lock and increment the revision inside the locked invocation. The [Job Posting Flow](https://github.com/superdurable/dex/blob/main/examples/go/products/job-post/workflow.go) demonstrates that mutation boundary.

Use `AttributeMatchEqual`, `AttributeMatchNotEqual`, `AttributeMatchGreaterThan`, `AttributeMatchGreaterThanOrEqual`, `AttributeMatchLessThan`, or `AttributeMatchLessThanOrEqual`. Pass a non-nil output pointer; the Client decodes the actual matched current value into it, including for non-equal operators. Singleton Attributes use `WaitForAttributeMatch`; AttributeMap entries use `WaitForAttributeMapInstanceMatch` with the instance name. `RequestID` is an optional override; the server otherwise derives it from the exact Attribute predicate. `RequestTimeout` bounds the complete call across transparent reattachments; zero waits indefinitely, and the earlier of it or the caller context wins. A positive expiry returns `*dex.RequestTimeoutError` without terminating the accepted durable wait. Leave `InternalHandlerTimeout` at zero unless abandoned waits could approach the per-Flow limit on concurrently accepted waits. A positive value transparently rolls an active call to another handler generation, releasing the old slot but counting toward the per-Flow total of accepted waits. Prefer it comfortably longer than normal request timeouts and reconnect gaps. See the core responsive-update guidance for the full tradeoff. After the wait, call the application's read RPC. The revision is a coalescing watermark, so one wait may skip intermediate values.

Durable Attribute wait from `dex/client_integration_test.go` in the selected SDK module (directory: `go list -m -f '{{.Dir}}' github.com/superdurable/dex/sdk-go`); the test is inside package `dex`, so application code qualifies these names with `dex.`:

```go
var matched string
attributeOptions := WaitForAttributeOptions{}
require.NoError(t, client.WaitForAttributeMatch(
	ctx,
	"reattach-attribute",
	clientTestStatus,
	AttributeMatchEqual("ready"),
	&matched,
	attributeOptions,
))
require.Equal(t, "ready", matched)
```
