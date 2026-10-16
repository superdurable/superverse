// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { stopFlow } from "@/lib/api/flow/flow-diagnostics";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { OPERATION_CONTROL, OperationDialog, OperationField, useDiagnosticOperation } from "./OperationDialog";

/** Dex stop types; stopping a request is Cancel, the others wait behind More details. */
const stopTypes = [
  { value: 1, label: "Cancel", needsReason: false },
  { value: 2, label: "Terminate", needsReason: true },
  { value: 3, label: "Fail", needsReason: true },
];

/**
 * Stops a run, the same in Runs and Diagnostics: one Stop with an optional reason, which cancels it.
 * Terminate and Fail, which need a reason, wait behind the dialog's More details.
 */
export function StopFlowDialog({
  summary,
  onClose,
  onStopped,
}: {
  summary: FlowSummary;
  onClose: () => void;
  onStopped: () => void;
}) {
  const client = useFlowApi();
  const [stopType, setStopType] = useState(1);
  const [reason, setReason] = useState("");
  const operation = useDiagnosticOperation(
    (signal) => stopFlow(client, { flowId: summary.flowId, runId: summary.runId, stopType, reason }, signal),
    () => {
      onStopped();
      onClose();
    },
    "Stop failed",
  );
  const reasonRequired = (stopTypes.find((item) => item.value === stopType) ?? stopTypes[0]).needsReason;

  return (
    <OperationDialog
      titleId="stop-flow-title"
      title="Stop this request"
      closeLabel="Close"
      dismissLabel="Keep it running"
      busyLabel="Stopping…"
      canSubmit={!reasonRequired || reason.trim() !== ""}
      operation={operation}
      onClose={onClose}
    >
      <p className="mt-1.5 text-body text-ink-dim">
        It stops where it is and goes no further. Steps it already finished stay done.
      </p>
      <OperationField label={`Reason${reasonRequired ? "" : " (optional)"}`}>
        <textarea
          className={OPERATION_CONTROL}
          rows={3}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
        />
      </OperationField>
      <MoreDetails surface="runs.stop-type" className="mt-3.5">
        <p className="text-caption text-ink-faint">
          Cancel requests graceful cancellation, terminate forces an immediate stop, and fail records a client failure.
          Terminate and fail need a reason.
        </p>
        <OperationField label="Stop type">
          <Dropdown
            label="Stop type"
            size="md"
            value={String(stopType)}
            onChange={(value) => setStopType(Number(value))}
            items={stopTypes.map((item) => ({ value: String(item.value), label: item.label }))}
          />
        </OperationField>
      </MoreDetails>
    </OperationDialog>
  );
}
