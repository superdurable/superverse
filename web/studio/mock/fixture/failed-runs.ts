// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Two invented failed runs of the recorded TextSummaryFlow, made from the recorded completed run so that Runs can
 * show a failure in mock mode: in one, Generate summary failed into its failure route, Record summary uncertainty,
 * and the run finished; in the other, Save summary result failed with no failure route and the run ended as failed.
 * Their Step failures have the StepMethodFailure shape Dex records. Nothing here was recorded.
 */

import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { asRecord } from "@/lib/shared/json";
import { shiftTimes, type RecordedFlowRun, type RecordedFlowRuns } from "./flow-runs";

const HANDLED = {
  flowId: "flow-0c6f3f1e-5b7d-4f0e-9a51-2f4a8e7c1d01",
  runId: "0c6f3f1e-0001-7a0e-9a51-2f4a8e7c1d01",
  /** How long before the completed run it started. */
  minutesEarlier: 20,
  error: "llm generateText: provider response lost after the request was sent",
  notice:
    "The model request did not return a result. The provider may have processed and billed it. Start a new summary only after checking the run.",
};

const STUCK = {
  flowId: "flow-5e2b9d47-8c1a-4d6f-b3e0-7a9c2f6e4b02",
  runId: "5e2b9d47-0002-7c1a-b3e0-7a9c2f6e4b02",
  minutesEarlier: 8,
  error: "save summary result: write summary attributes: context deadline exceeded",
  attempts: 3,
};

/** The recorded runs with the two failed runs after them, made from the completed one; unchanged without it. */
export function withFailedRuns(recorded: RecordedFlowRuns): RecordedFlowRuns {
  const completed = recorded.runs.find((run) => run.summary.flowStatusCode === 2 && run.history.events.length >= 8);
  if (!completed) return recorded;
  return { ...recorded, runs: [...recorded.runs, handledRun(completed), stuckRun(completed)] };
}

/** Generate summary failed once, its failure route recorded the lost response, and the run completed. */
function handledRun(template: RecordedFlowRun): RecordedFlowRun {
  const run = copyOf(template, HANDLED.flowId, HANDLED.runId, HANDLED.minutesEarlier);
  const [started, prepared, generated] = run.history.events;
  const failed = failedExecute(generated, HANDLED.error, 1);
  const recorded: FlowHistoryEvent = {
    eventId: failed.eventId + 6,
    eventTime: later(failed.eventTime, 40),
    type: "StepExecuteCompleted",
    payload: {
      context: {
        durability: 1,
        duration: "0.014s",
        finalAttempt: 1,
        fromStepExecutionId: "GenerateSummary-1",
        startedTime: later(failed.eventTime, 26),
        stepExecutionId: "RecordSummaryUncertainty-1",
        stepType: "RecordSummaryUncertainty",
      },
      input: generated.payload.input,
      output: {
        stepDecision: { closeDecision: { closeDecisionType: 2, closeInput: { nullValue: null } } },
        upsertAttributes: [summaryStatus("unknown"), { key: "summary-notice", value: { stringValue: HANDLED.notice } }],
      },
    },
  };
  const closed = closedEvent(recorded, 2);
  return finish(run, [started, prepared, failed, recorded, closed], {
    status: "unknown",
    notice: HANDLED.notice,
  });
}

/** Save summary result failed on every try after the summary was approved; nothing routes its failure. */
function stuckRun(template: RecordedFlowRun): RecordedFlowRun {
  const run = copyOf(template, STUCK.flowId, STUCK.runId, STUCK.minutesEarlier);
  const events = run.history.events;
  const saved = events.find((event) => stepExecutionOf(event) === "SaveSummaryResult-1");
  if (!saved) return run;
  const failed = failedExecute(saved, STUCK.error, STUCK.attempts);
  const before = events.slice(0, events.indexOf(saved));
  return finish(run, [...before, failed, closedEvent(failed, 3)], { status: "awaiting-approval", notice: "" });
}

/** The template copied under new identities, every time moved back by `minutes`. */
function copyOf(template: RecordedFlowRun, flowId: string, runId: string, minutes: number): RecordedFlowRun {
  const text = JSON.stringify(shiftTimes(template, -minutes * 60))
    .replaceAll(template.summary.flowId, flowId)
    .replaceAll(template.summary.runId, runId);
  return JSON.parse(text) as RecordedFlowRun;
}

/** A completed Execute turned into the failure of its last try, with the worker's error as Dex records it. */
function failedExecute(completed: FlowHistoryEvent, error: string, attempts: number): FlowHistoryEvent {
  const context = asRecord(completed.payload.context);
  const options = asRecord(context.methodOptions);
  return {
    ...completed,
    type: "StepExecuteFailed",
    payload: {
      context: {
        ...context,
        finalAttempt: attempts,
        methodOptions: { ...options, retryPolicy: { ...asRecord(options.retryPolicy), maximumAttempts: attempts } },
      },
      input: completed.payload.input,
      output: {
        failure: {
          backendError: "FLOW_ERROR_TYPE_WORKER_API_FAIL",
          details: {
            detail: error,
            subStatus: 4,
            originalWorkerErrorDetail: error,
            originalWorkerErrorType: "*fmt.wrapError",
            originalWorkerErrorStatus: 2,
          },
          attempt: attempts,
        },
      },
    },
  };
}

function closedEvent(last: FlowHistoryEvent, flowStatus: number): FlowHistoryEvent {
  return {
    eventId: last.eventId + 5,
    eventTime: later(last.eventTime, 20),
    type: "FlowClosed",
    payload: { flowStatus },
  };
}

/** The run's documents made to agree with its new history: its status, close time, row and Display. */
function finish(
  run: RecordedFlowRun,
  events: FlowHistoryEvent[],
  { status, notice }: { status: string; notice: string },
): RecordedFlowRun {
  const closed = events[events.length - 1];
  const flowStatusCode = Number(closed.payload.flowStatus);
  const flowStatus = flowStatusCode === 2 ? "Completed" : "Failed";
  const empty = { "summary-model": "", "summary-text": "", "total-tokens": "0" };
  return {
    summary: { ...run.summary, flowStatus, flowStatusCode, closeTime: closed.eventTime },
    row: {
      ...run.row,
      flowStatus,
      flowStatusCode,
      closeTime: closed.eventTime,
      indexedAttributes: { "summary-status": status },
      summary: empty,
    },
    display: {
      ...run.display,
      flowStatus,
      flowStatusCode,
      isActive: false,
      display: {
        ...run.display.display,
        ...empty,
        "input-tokens": "0",
        "output-tokens": "0",
        "summary-status": status,
        "summary-notice": notice,
      },
      attributeSnapshot: { "summary-status": status },
      eligibleActions: [],
    },
    state: null,
    history: { events, nextInternalEventId: closed.eventId + 1 },
  };
}

function summaryStatus(value: string) {
  return {
    indexConfig: { enable: true, indexKey: "keyword1", type: 1 },
    key: "summary-status",
    value: { stringValue: value },
  };
}

function stepExecutionOf(event: FlowHistoryEvent): unknown {
  return asRecord(event.payload.context).stepExecutionId;
}

function later(time: string | null, ms: number): string {
  return new Date(Date.parse(time ?? "") + ms).toISOString();
}
