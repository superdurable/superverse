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
import { OPERATION_CONTROL, OperationDialog, OperationField, useDiagnosticOperation } from "./OperationDialog";

const stopTypes = [
  { value: 1, label: "Cancel", needsReason: false },
  { value: 2, label: "Terminate", needsReason: true },
  { value: 3, label: "Fail", needsReason: true },
];

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
      eyebrow="Destructive operation"
      title="Stop flow"
      closeLabel="Close Stop Flow"
      dismissLabel="Close"
      busyLabel="Stopping…"
      canSubmit={!reasonRequired || reason.trim() !== ""}
      operation={operation}
      onClose={onClose}
    >
      <p className="mt-1.5 text-ink-faint">
        Stop ends the current run. Cancel requests graceful cancellation, terminate forces an immediate stop, and fail
        records a client failure.
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
      <OperationField label={`Reason${reasonRequired ? "" : " (optional)"}`}>
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
