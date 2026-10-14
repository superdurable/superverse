# Go gotchas

- Define Attributes, Channels, Streams, and maps once at package scope; definitions carry identity.
- Register every Step emitted by `GoTo`/`MovementOf`, and register both parent and child Flows.
- Use `StepDefaultsNoWaitFor[T]` only for a true no-wait Step.
- Flow and Step types default to the Go type name without package or pointer (`*orders.OrderFlow` registers as `OrderFlow`), so Flow types must be unique within the Registry and Step types within their Flow; name Flow types after their domain. Do not implement `GetFlowType` or `GetStepType` for new definitions or as a Studio or FDG workaround; only a generic type or a [rename that keeps an existing durable name](versioning.md#default-flow-and-step-type-names) justifies an override.
- Return every Context state, heartbeat, and Stream error.
- Do not coordinate durable work with process-local mutexes, goroutines, or maps.
- Put durable business time (TTL, reminders, inactivity) in a WaitFor Timer. `time.Sleep` inside Execute occupies the attempt; use it only between rounds of a heartbeating [Polling](patterns.md#polling) Step.
- Preserve omitted versus explicit zero in pointer-valued options.
- `dex.None` is nil-only; pass `nil`, not an invented empty payload.
- Graceful and force terminal decisions have different parallel/cancellation semantics.
- Give Client operations deadlines; long-poll expiry is not Flow failure.
- Validate map instance names: non-empty and no `/`.
- A read of unloaded state is not absence: `AttributeMap.Get` and `PendingMessages` return a not-loaded error that the Step retries until its budget ends, so the run looks stuck; declare the load ([Loading state](primitives.md#loading-state)).
- `AllInstanceKeys` and `MapSize` need the whole map loaded; exact-instance loads cannot enumerate, and the call panics.
- A `Get` after a `Set` of the same instance in one handler still needs the load.
- Locks do not load state, and loads do not lock it.
- `GetConditionResults`, Channel `Size` and every write need no load.
- Keep schema/Flow packages below registry composition to avoid import cycles; inject late Client providers.

Prefer the selected SDK's compile contracts (`dex/contracts_test.go` in the SDK module) and runnable examples over remembered names.
