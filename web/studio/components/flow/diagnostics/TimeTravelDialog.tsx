// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useMemo, useState } from "react";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import type { FlowHistoryEvent, FlowSummary } from "@/lib/api/flow/flow-types";
import { timeTravelFlow, type TimeTravelRequest } from "@/lib/api/flow/flow-diagnostics";
import { useFlowNavigation } from "@/lib/flow/FlowNavigationProvider";
import { stepContext, stepExecutionId, stepMethod } from "@/lib/api/flow/flow-history";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { OPERATION_CONTROL, OperationDialog, OperationField, useDiagnosticOperation } from "./OperationDialog";

/** The Dex TimeTravelType and TimeTravelStepMethod enum values. */
const TIME_TRAVEL_TYPE = {
  BEGINNING: 1,
  HISTORY_EVENT_TIME: 2,
  STEP_TYPE: 3,
  STEP_EXECUTION_ID: 4,
} as const;

const TIME_TRAVEL_STEP_METHOD = {
  WAIT_FOR: 1,
  EXECUTE: 2,
} as const;

const timeTravelTypes = [
  { value: TIME_TRAVEL_TYPE.BEGINNING, label: "Beginning" },
  { value: TIME_TRAVEL_TYPE.HISTORY_EVENT_TIME, label: "History event time" },
  { value: TIME_TRAVEL_TYPE.STEP_TYPE, label: "Step type" },
  { value: TIME_TRAVEL_TYPE.STEP_EXECUTION_ID, label: "Step execution ID" },
];

/** A Step method event preselects its own Step execution and method as the time travel point. */
function eventTimeTravelTarget(event: FlowHistoryEvent | null): { stepExecutionId: string; stepMethod: number } | null {
  if (!event) return null;
  const executionId = stepExecutionId(event);
  const method = stepMethod(event);
  if (!executionId || method === null) return null;
  return {
    stepExecutionId: executionId,
    stepMethod: method === "wait-for" ? TIME_TRAVEL_STEP_METHOD.WAIT_FOR : TIME_TRAVEL_STEP_METHOD.EXECUTE,
  };
}

export function TimeTravelDialog({
  summary,
  events,
  initialEvent,
  onClose,
}: {
  summary: FlowSummary;
  events: FlowHistoryEvent[];
  initialEvent: FlowHistoryEvent | null;
  onClose: () => void;
}) {
  const { navigate } = useFlowNavigation();
  const client = useFlowApi();
  const [initialTarget] = useState(() => eventTimeTravelTarget(initialEvent));
  const [timeTravelType, setTimeTravelType] = useState<number>(
    initialTarget ? TIME_TRAVEL_TYPE.STEP_EXECUTION_ID : TIME_TRAVEL_TYPE.BEGINNING,
  );
  const [target, setTarget] = useState(initialTarget?.stepExecutionId ?? "");
  const [stepMethod, setStepMethod] = useState<number>(initialTarget?.stepMethod ?? 0);
  const [reason, setReason] = useState("");
  const stepTypes = useMemo(() => {
    const values = new Set<string>();
    events.forEach((event) => {
      const stepType = stepContext(event).stepType;
      if (typeof stepType === "string") values.add(stepType);
    });
    return [...values];
  }, [events]);

  const operation = useDiagnosticOperation(
    async (signal) => {
      const request: TimeTravelRequest = {
        flowId: summary.flowId,
        runId: summary.runId,
        timeTravelType,
        reason,
      };
      if (timeTravelType === TIME_TRAVEL_TYPE.STEP_TYPE) request.stepType = target;
      if (timeTravelType === TIME_TRAVEL_TYPE.STEP_EXECUTION_ID) {
        request.stepExecutionId = target;
        request.stepMethod = stepMethod;
      }
      if (timeTravelType === TIME_TRAVEL_TYPE.HISTORY_EVENT_TIME) {
        const date = new Date(target);
        if (!Number.isFinite(date.getTime())) throw new Error("Choose a valid history event time.");
        request.historyEventTime = date.toISOString();
      }
      return timeTravelFlow(client, request, signal);
    },
    (result) => {
      navigate({ surface: "diagnostics", flowId: summary.flowId, runId: result.runId });
      onClose();
    },
    "Time travel failed",
  );

  return (
    <OperationDialog
      titleId="time-travel-title"
      eyebrow="New Flow run"
      title="Time travel"
      closeLabel="Close Time Travel"
      dismissLabel="Cancel"
      busyLabel="Time traveling…"
      canSubmit={
        reason.trim() !== "" &&
        (timeTravelType === TIME_TRAVEL_TYPE.BEGINNING || target !== "") &&
        (timeTravelType !== TIME_TRAVEL_TYPE.STEP_EXECUTION_ID || stepMethod !== 0)
      }
      operation={operation}
      onClose={onClose}
    >
      <p className="mt-1.5 text-ink-faint">
        Time travel creates a new run from a selected Dex point. The current run remains available in history.
      </p>
      <OperationField label="Time travel point">
        <Dropdown
          label="Time travel point"
          size="md"
          value={String(timeTravelType)}
          onChange={(value) => {
            setTimeTravelType(Number(value));
            setTarget("");
            setStepMethod(0);
          }}
          items={timeTravelTypes.map((item) => ({ value: String(item.value), label: item.label }))}
        />
      </OperationField>
      {timeTravelType !== TIME_TRAVEL_TYPE.BEGINNING && (
        <OperationField label="Target">
          {timeTravelType === TIME_TRAVEL_TYPE.STEP_TYPE ? (
            <>
              <input
                className={OPERATION_CONTROL}
                list="time-travel-step-types"
                value={target}
                onChange={(event) => setTarget(event.target.value)}
              />
              <datalist id="time-travel-step-types">
                {stepTypes.map((value) => (
                  <option value={value} key={value} />
                ))}
              </datalist>
            </>
          ) : (
            <input
              className={OPERATION_CONTROL}
              type={timeTravelType === TIME_TRAVEL_TYPE.HISTORY_EVENT_TIME ? "datetime-local" : "text"}
              value={target}
              onChange={(event) => setTarget(event.target.value)}
            />
          )}
        </OperationField>
      )}
      {timeTravelType === TIME_TRAVEL_TYPE.STEP_EXECUTION_ID && (
        <OperationField label="Step method">
          <Dropdown
            label="Step method"
            size="md"
            value={stepMethod === 0 ? null : String(stepMethod)}
            placeholder="Select a method"
            onChange={(value) => setStepMethod(Number(value))}
            items={[
              { value: String(TIME_TRAVEL_STEP_METHOD.WAIT_FOR), label: "WaitFor" },
              { value: String(TIME_TRAVEL_STEP_METHOD.EXECUTE), label: "Execute" },
            ]}
          />
        </OperationField>
      )}
      <OperationField label="Reason">
        <textarea
          className={OPERATION_CONTROL}
          rows={3}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
        />
      </OperationField>
    </OperationDialog>
  );
}
