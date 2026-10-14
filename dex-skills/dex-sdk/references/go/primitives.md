# Go primitives

Read core primitive semantics first. This page supplies Go API shapes; the selected SDK source is authoritative.

## Flow, Step, and decisions

A Flow implements `dex.Flow`; embedding `dex.FlowDefaults` supplies optional behavior. Register the start Step with `dex.DefineStartStep` and every reachable Step with `dex.DefineStep`. Embed `dex.StepDefaultsNoWaitFor[T]` when there is no WaitFor; otherwise implement both methods. Use the default type names (the Go type name without its package); read the [type-name rules](versioning.md#default-flow-and-step-type-names) before adding `GetFlowType` or `GetStepType`.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/flow/workflow.go)
```go
func (ExampleStep) WaitFor(ctx dex.Context, _ int) (*dex.Wait, error) {
	if err := Status.Set(ctx, "running"); err != nil {
		return nil, err
	}
	return dex.SkipWaitImmediately(), nil
}

func (ExampleStep) Execute(_ dex.Context, input int) (*dex.StepDecision, error) {
	return dex.GoTo(Finish{}, input+1), nil
}
```

Use `GoTo`, `GoToMany`, or `DeadEnd` to keep work open. Graceful completion waits for compatible active branches; force completion/failure is an explicit terminal override.

## Wait and Timer

`dex.Until(condition)` waits for one condition; `AllOf` and `AnyOf` combine conditions. Among ready `AnyOf` candidates, Dex uses canonical Timer, Channel, then SubFlow order and preserves argument order within each kind. An earlier unready Condition does not block a later ready one. Only the winning Channel consumes messages. For strict priority, return only the current higher-priority Condition until it resolves. Timers are durable conditions, not sleeps inside Execute.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/timer/workflow.go)
```go
func (timerStep) WaitFor(_ dex.Context, input int) (*dex.Wait, error) {
	return dex.Until(dex.Timer(time.Duration(input) * time.Second)), nil
}

func (timerStep) Execute(_ dex.Context, _ int) (*dex.StepDecision, error) {
	return dex.GracefulComplete("timer-fired"), nil
}
```

## Attribute and AttributeMap

Define typed state at package scope with `DefineAttribute[T]` or `DefineAttributeMap[T]` and register it in `PersistenceSchema`. Reads/writes use `dex.Context` and return errors. AttributeMap instances partition one schema definition; a handler reads only the instances it declares, as [Loading state](#loading-state) shows. Index only query fields. Use an Attribute Store only when state must be queryable outside an invocation.

## Channel and ChannelMap

Channels are durable queues. `ForOne` and `ForN` create conditions; after firing, read condition results and delete/move messages deliberately. ChannelMap separates queues by validated instance name. External callers invoke a typed Flow RPC; its handler publishes through Context.

Pending-message reads inside a Step or RPC are invocation snapshots. Other handlers may consume, delete, or publish concurrently. Transactional execution validates selected deletions and commits writes atomically, but does not lock the whole snapshot. Read and write pending messages directly only when the operation explicitly tolerates that race. When a decision requires the queue to remain unchanged, every cooperating Step and RPC writer must use the same Attribute lock.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/channel/workflow.go)
```go
func (channelWait) WaitFor(_ dex.Context, input int) (*dex.Wait, error) {
	return dex.AnyOf(
		ApprovalMessages.ForOne(),
		dex.Timer(time.Duration(input)*time.Second),
	), nil
}
```

## RPC

Only methods returned from `GetRPCs` are Worker RPCs. Register direct bound Flow methods with `dex.DefineRPC`. Put timeout, locks, transactions, and selective loads in its `dex.RPCOptions`; `Client.InvokeRPC` takes no options argument. A definition without locks or transactional execution can query a retained terminal run when the handler returns no durable effects. Locks, transactions, returned effects, or Server policy require an active execution. Keep handlers short and lock conflicting Attributes. RPC results may request supported movements, publishing, or cancellation; never emulate transactions with process-local locks.

## Loading state

Every Step method and RPC receives ordinary Attributes and Channel sizes. It
receives AttributeMap instances and pending Channel messages only when it
declares them; otherwise the read fails at run time:

| Call | Without a declared load |
| --- | --- |
| `AttributeMap.Get` | returns `*dex.AttributeMapNotLoadedError`, also after a `Set` of the same instance in this handler |
| `AttributeMap.AllInstanceKeys`, `AttributeMap.MapSize` | panic with `*dex.AttributeMapNotLoadedError` unless the whole map is loaded; exact-instance loads do not count |
| `PendingMessages`, `FindPendingMessage` of a Channel or ChannelMap | return `*dex.ChannelMessagesNotLoadedError` |

Writes need no load: `Set`, `Delete`, `Publish` and Channel `Delete` by message
ID. Channel `Size`, ChannelMap keys and sizes and `GetConditionResults` need none
either. A missing instance of a loaded map is `*dex.AttributeNotFoundError`, a
different case. An unloaded read fails every retry of a Step, so the run looks
stuck until the retry budget ends, four hours by default.

Declare the loads with the handler that reads:

- **Step:** `GetStepOptions` on the Step type sets `ExecuteLoadAttributeMaps`
  (whole maps), `ExecuteLoadAttributeMapInstances` (exact instances),
  `ExecuteLoadChannels`, `ExecuteLoadChannelMaps` or
  `ExecuteLoadChannelMapInstances`. WaitFor has its own `WaitForLoad*` fields;
  WaitFor and Execute load separately.
- **Key known only at run time:** the moving Step adds
  `dex.WithStepOptions(&dex.StepOptions{ExecuteLoadAttributeMapInstances: ...})`
  to its `GoTo`. Each field it sets replaces the same field of `GetStepOptions`.
- **RPC:** `LoadAttributeMaps`, `LoadAttributeMapInstances`, `LoadChannels`,
  `LoadChannelMaps` and `LoadChannelMapInstances` in its `dex.RPCOptions`.
- **RPC instance chosen by the caller:** `Client.InvokeRPCWithOptions` with
  `RPCInvokeOptions.LoadAttributeMapInstances`. A read-modify-write also locks
  the same instance with `LockAttributeMapInstances`. When that call is in
  another package than the Flow, declare
  `// dex:invocation-load attribute-map:<name>` (or `channel-map:<name>`) on
  the RPC method, with the map's Dex name, so the analyzer sees the load.

Locks do not load, and a load does not lock. Load the whole map only to
enumerate it; otherwise load the exact instances. Never add a no-op read to
satisfy a check. `dexcli visualize` reports a read that no load of the Flow's
package selects as a blocking diagnostic, also through same-package helpers;
loads it cannot evaluate, such as options returned by another package, leave
that read unchecked.

```go
type DraftNewsletter struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

// Execute enumerates the collected releases and reads the queued revision notes.
func (DraftNewsletter) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteLoadAttributeMaps: []dex.AttributeDef{CollectedReleases},
		ExecuteLoadChannels:      []dex.ChannelDef{RevisionNotes},
	}
}

// A runtime key: load exactly the record the next Step reads.
return dex.GoTo(SendToSubscriber{}, email, dex.WithStepOptions(&dex.StepOptions{
	ExecuteLoadAttributeMapInstances: []dex.AttributeMapLoad{Deliveries.Load(email)},
})), nil
```

## Read another Flow from a Step

Keep records that several Flows use, such as subscribers, in one domain Flow: an
AttributeMap keyed by the record identity plus typed RPCs. Such an entity Flow
returns no Steps, so it stays open and serves its RPCs. Every RPC declares the
state it reads ([Loading state](#loading-state)):

```go
var Subscribers = dex.DefineAttributeMap[Subscriber]("subscribers") // keyed by canonical email

const SubscriberRegistryFlowID = "subscriber-registry"

func (flow SubscriberRegistryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		// ... GetDexSummary and GetDexDisplay
		// Enumeration needs the whole map in the RPC snapshot.
		dex.DefineRPC(flow.ListSubscribers, &dex.RPCOptions{LoadAttributeMaps: []dex.AttributeDef{Subscribers}}),
		// The caller locks and loads the one instance it changes.
		dex.DefineRPC(flow.AddSubscriber, nil),
	}
}

func (SubscriberRegistryFlow) ListSubscribers(ctx dex.Context, _ dex.None) (*dex.RPCResult[[]string], error) {
	return &dex.RPCResult[[]string]{Output: Subscribers.AllInstanceKeys(ctx)}, nil
}

// AddSubscriber reads the instance its caller loads and locks.
//
// dex:invocation-load attribute-map:subscribers
func (SubscriberRegistryFlow) AddSubscriber(ctx dex.Context, email string) (*dex.RPCResult[AddSubscriberResult], error) {
	_, err := Subscribers.Get(ctx, email)
	var missing *dex.AttributeNotFoundError
	switch {
	case err == nil:
		return &dex.RPCResult[AddSubscriberResult]{Output: AddSubscriberResult{Outcome: "already_subscribed"}}, nil
	case !errors.As(err, &missing):
		return nil, err
	}
	if err := Subscribers.Set(ctx, email, Subscriber{Email: email}); err != nil {
		return nil, err
	}
	return &dex.RPCResult[AddSubscriberResult]{Output: AddSubscriberResult{Outcome: "subscribed"}}, nil
}
```

The API starts the registry with a stable FlowID and `RequestID`, ignoring an
already started run, then adds the subscriber with a lock and a load of that
instance, so a duplicate request sees the first one:

```go
func AddSubscriber(ctx context.Context, client *dex.Client, email string) (AddSubscriberResult, error) {
	requestID := SubscriberRegistryFlowID
	_, err := client.StartFlow(ctx, SubscriberRegistryFlow{}, SubscriberRegistryFlowID, nil, dex.StartFlowOptions{
		IDReusePolicy:  dex.IDReuseDisallow,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		RequestID:      &requestID,
	})
	if err != nil {
		return AddSubscriberResult{}, err
	}
	var result AddSubscriberResult
	err = client.InvokeRPCWithOptions(ctx, SubscriberRegistryFlowID, SubscriberRegistryFlow{}.AddSubscriber, email, &result, dex.RPCInvokeOptions{
		LockAttributeMapInstances: []dex.AttributeLock{dex.LockAttributeMap(Subscribers, email)},
		LoadAttributeMapInstances: []dex.AttributeMapLoad{Subscribers.Load(email)},
	})
	return result, err
}
```

Another Flow reads the registry in a Step's `Execute`, never in `WaitFor`,
through the application's Dex Client. Inject a shared holder when the Flow is
registered and set it after `dex.NewClient`, before the Worker starts. Until
the first subscriber starts the registry it does not exist: treat
`*dex.FlowNotActiveOrNotFoundError` as no subscribers, or the Step retries the
missing Flow until its retry budget ends:

```go
type Clients struct{ Dex *dex.Client }

type NewsletterFlow struct {
	dex.FlowDefaults
	Clients *Clients
}

func (flow NewsletterFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		// ...
		dex.DefineStep(SnapshotSubscribers{Clients: flow.Clients}),
	}
}

type SnapshotSubscribers struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	Clients *Clients
}

func (step SnapshotSubscribers) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	var subscribers []string
	// The read changes nothing, so the Step's retry policy may repeat it.
	err := step.Clients.Dex.InvokeRPC(ctx, SubscriberRegistryFlowID, SubscriberRegistryFlow{}.ListSubscribers, nil, &subscribers)
	var missing *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &missing) {
		// Nobody has subscribed yet, so the registry was never started.
		subscribers = nil
	} else if err != nil {
		return nil, err
	}
	if err := SubscriberSnapshot.Set(ctx, subscribers); err != nil {
		return nil, err
	}
	return dex.GoTo(SendToSubscribers{Clients: step.Clients}, nil), nil
}

// worker.go
clients := &process.Clients{}
registry, err := dex.NewRegistry([]dex.Flow{process.SubscriberRegistryFlow{}, process.NewsletterFlow{Clients: clients}})
// ... client, err := dex.NewClient(registry, cache, options)
clients.Dex = client
```

Give each per-recipient side effect, such as one email, its own `Execute` Step
with a stable identity per recipient, so that a retry never repeats a delivery.

## Stream

Define a Stream with a byte limit and register it. A Step writes ordered progress; consumers resume from the Client token. A Stream is a feed, not authoritative state. Only a Step writes: a write or a `NewBufferedTextStream` in an RPC or a timeout handler, also through a helper, is the blocking `step_progress_outside_step` diagnostic. An RPC that needs progress publishes a Channel message or returns `NextSteps` to a Step that writes.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/stream/workflow.go)
```go
var Progress = dex.DefineStream[string]("Progress", 10<<20)

type StreamFlow struct {
	dex.FlowDefaults
}
```

Use `Client.ReadStream` for forward, one-at-a-time, optionally long-polling consumption. Use `Client.ListStreamMessages` for non-blocking newest-first pages. Pass the typed Stream directly, and pass `NextPageToken` unchanged to the next call until it is empty.

[Runnable example](https://github.com/superdurable/dex/blob/main/examples/go/primitives/stream/controller.go)
```go
var page sdk.StreamMessagesPage[string]
err = controller.client.ListStreamMessages(
	request.Request.Context(),
	flowID,
	Progress,
	int32(pageSize),
	request.Query("beforePageToken"),
	&page,
)
```

The before-page token is exclusive and scope-bound. The first page uses an empty token. Listing is a best-effort retained snapshot: concurrent newer writes stay outside the older-page chain, while trimming may remove messages. A trimmed anchor returns an empty page. The server requires a positive page size and caps it at 1000 by default.

## SubFlow and Client

`dex.SubFlow(child, input)` is a parent-owned Wait condition. Register parent and child, and explicitly model what parent completion means for unfinished children. Client owns Flow lifecycle, typed RPC invocation, Attribute-match and Step-completion waits, Stream operations, search, config, timers, and time travel. Read and write Flow-owned Attribute and Channel state through typed RPCs, not removed direct Client methods. Always pass a bounded context and match typed errors with `errors.As`.

## Selection rule

Use Attribute for current state, Channel for queued intent, RPC for synchronous mutation/snapshot, Stream for incremental output, Timer for durable time, and SubFlow for an independently identified durable child.
