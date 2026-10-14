# StepOptions and durability

Treat Flow-level durability and method-level StepOptions as one design decision. They determine execution placement, liveness detection, retry exhaustion, state visibility, concurrency, and recovery.

## Choose the default and the exceptions

Durability resolves from the WaitFor or Execute override, then the FlowConfig default, then **SYNC**. WaitFor and Execute overrides are independent.

When most handlers are short and idempotent, set the FlowConfig default to **ASYNC** at Flow start. Let ordinary handlers inherit it. Explicitly override a method to **SYNC** when it is more likely than not to exceed five seconds. LLM calls and other known long-running provider calls usually belong in this category.

Five seconds is a classification heuristic, not a timeout or SLA. Seven seconds is the limit it protects: the current ASYNC local phase permits at most about seven seconds and three attempts, and the heuristic leaves operating margin inside it. Classification does not need to be exact. ShortRunning work is allowed to exceed the estimate and fall back normally. If more than half of real calls fall back, classify the method as long-running because the local optimization is no longer useful.

Fallback starts a regular execution, but the method's durability remains **ASYNC**. Do not describe fallback as switching durability to SYNC.

## Understand the shared attempt budget

The local phase and fallback regular activities are one logical method execution. They share maximum attempts, elapsed retry duration, and 1-based attempt numbers. Fallback starts immediately. Later regular retries continue the existing attempt and backoff sequence.

The local phase currently ignores method timeout and heartbeat timeout. A method timeout still bounds each regular attempt. When an external call needs a strict deadline during both phases, derive a child context from the handler's `dex.Context` with `context.WithTimeout` around that call. Keep external mutations idempotent because an ASYNC result can replay after failure.

## Configure the complete method policy

For every WaitFor and Execute method, decide the applicable:

- durability override or deliberate inheritance
- method timeout
- retry attempts, backoff, and total elapsed budget
- selective AttributeMap and Channel loads
- Attribute or map-instance locks
- exhausted-retry failure route

RPC registration options make the same fixed load and lock decisions for every invocation. When an RPC's exact AttributeMap or ChannelMap instance comes from request data, use the additive RPC invocation options instead (`dex.RPCInvokeOptions` with `Client.InvokeRPCWithOptions`). They cannot add timeout, singleton Attribute locks, whole-map loads, singleton Channel loads, or transactionality. A dynamic AttributeMap read-modify-write must add both the exact instance load and its matching instance lock.

For Execute, also decide the heartbeat timeout. Regular execution defaults to one minute. Keep that default unless a healthy operation can remain silent longer. Then raise it to the longest acceptable healthy silent interval. Use method timeout to cap the whole attempt. Heartbeats and Stream frames show Worker liveness; only heartbeat values provide retry checkpoints.

A non-streaming provider call is silent for its whole duration, so the default fails a healthy 90-second LLM generation. Set heartbeat timeout at least equal to the method timeout, or heartbeat or stream progress during the call. Dex Server holds a streaming call to the Worker for each Execute attempt, so a Worker process that exits and closes that connection fails the attempt without waiting for the heartbeat timeout; a long heartbeat timeout therefore does not delay recovery from such a crash. It still bounds a Worker that stays connected but silent.

Do not infer durability from a large attempt timeout. Attempt timeout is a safety bound. Running classification estimates where most successful calls complete.

## Verify behavior

Test a fast ASYNC method that finishes locally, a slower ASYNC method that falls back and completes, and a known long method that starts as a regular SYNC execution. Verify retry attempts and elapsed budget across fallback. Verify the external child deadline during local execution. Replace the Worker at local fallback, durable waits, and external-effect boundaries.

FlowConfig is persisted when the Flow starts. Changing the application default affects new Flows, not existing executions. Preserve or migrate open-Flow behavior deliberately.
