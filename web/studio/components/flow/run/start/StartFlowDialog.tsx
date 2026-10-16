// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import type { FlowV2StartDefinition } from "@/lib/api/flow/flow-definition";
import type { StartFlowCapability } from "@/lib/api/flow/flow-types";
import { processName } from "@/lib/shared/format";
import { StartForm } from "./StartForm";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";

/** The start form of a process in a dialog: "Start a test run of Text summary". */
export function StartFlowDialog({
  definition,
  definitionRevision,
  flowType,
  target,
  live = false,
  onClose,
  onDefinitionChanged,
  onStarted,
}: {
  definition: FlowV2StartDefinition;
  definitionRevision: string;
  flowType: string;
  target: StartFlowCapability;
  /** A run on Live: real data and real people, so the dialog says so before it starts. */
  live?: boolean;
  onClose: () => void;
  onDefinitionChanged: (error: unknown) => boolean;
  /** The started run, and the words that say so: "Started a test run of Text summary." */
  onStarted: (flowID: string, words: string) => void;
}) {
  const [submitting, setSubmitting] = useState(false);
  const title = `Start a ${live ? "live" : "test"} run of ${processName(flowType)}`;
  return (
    <Dialog
      aria-labelledby="start-flow-title"
      width="lg"
      onDismiss={() => {
        if (!submitting) onClose();
      }}
    >
      <StartForm
        definition={definition}
        definitionRevision={definitionRevision}
        flowType={flowType}
        target={target}
        live={live}
        layout="dialog"
        autoFocus
        onBusyChange={setSubmitting}
        onDefinitionChanged={(error) => {
          const changed = onDefinitionChanged(error);
          if (changed) onClose();
          return changed;
        }}
        onStarted={onStarted}
        header={
          <header className="flex items-center gap-2.5 border-b border-line-faint px-4.5 py-3.5">
            <h2 className="m-0 text-title font-semibold" id="start-flow-title">
              {title}
            </h2>
            <Button
              aria-label="Close"
              className="ml-auto"
              disabled={submitting}
              variant="quiet"
              size="icon"
              onClick={onClose}
            >
              ×
            </Button>
          </header>
        }
        cancel={
          <Button disabled={submitting} variant="quiet" onClick={onClose}>
            Cancel
          </Button>
        }
      />
    </Dialog>
  );
}
