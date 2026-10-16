// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { TIME_TRAVEL_STEP_METHOD, TIME_TRAVEL_TYPE, timeTravelFlow } from "@/lib/api/flow/flow-diagnostics";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import type { RunFailure } from "@/components/flow/canvas/run-failure";
import { OperationDialog, useDiagnosticOperation } from "@/components/flow/diagnostics/OperationDialog";

/**
 * Retry this step: a Time travel to the failed Step's own execution and method, confirmed in one
 * sentence. The run keeps its Flow ID, so the host reads it again rather than leaving for Diagnostics.
 */
export function RetryStepDialog({
  summary,
  failure,
  onClose,
  onRetried,
}: {
  summary: FlowSummary;
  failure: RunFailure;
  onClose: () => void;
  onRetried: () => void;
}) {
  const client = useFlowApi();
  const operation = useDiagnosticOperation(
    (signal) =>
      timeTravelFlow(
        client,
        {
          flowId: summary.flowId,
          runId: summary.runId,
          timeTravelType: TIME_TRAVEL_TYPE.STEP_EXECUTION_ID,
          stepExecutionId: failure.stepExecutionId,
          stepMethod: failure.method === "waitFor" ? TIME_TRAVEL_STEP_METHOD.WAIT_FOR : TIME_TRAVEL_STEP_METHOD.EXECUTE,
          reason: `Retry ${failure.stepType} after it failed`,
        },
        signal,
      ),
    () => {
      onRetried();
      onClose();
    },
    "Retry failed",
  );
  return (
    <OperationDialog
      titleId="retry-step-title"
      title="Retry this step"
      closeLabel="Close"
      dismissLabel="Cancel"
      busyLabel="Retrying…"
      canSubmit
      submitVariant="primary"
      operation={operation}
      onClose={onClose}
    >
      <p className="mt-1.5 text-body text-ink-dim">
        This runs {failure.title} again, and the request carries on from there.
      </p>
    </OperationDialog>
  );
}
