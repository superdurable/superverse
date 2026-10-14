// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { closeDecisionTypeLabel } from "@/lib/flow/enum-labels";
import { asRecord, asRecordArray, hasFields, type UnknownRecord } from "@/lib/shared/json";
import { RecordCard, RecordTitle, Records } from "./DetailRecords";
import { StepOptionsView } from "./StepOptionViews";
import { DetailSection, Fields, ValueBlock, listText, type Field } from "./ValueFields";

/** What a Step decided: the Steps it moves to, the Steps it cancels, and how it closes the Flow. */

export function StepMovements({ values, showFrom = true }: { values: unknown; showFrom?: boolean }) {
  const movements = asRecordArray(values);
  if (movements.length === 0) return null;
  return (
    <Records>
      {movements.map((movement, index) => {
        const fields: Field[] = [["Step type", movement.stepType]];
        if (showFrom) fields.push(["From", movement.fromStepExecutionIdInternalOnly]);
        return (
          <RecordCard key={`${String(movement.stepType)}-${index}`}>
            <Fields compact values={fields} />
            <ValueBlock label="Input" value={movement.stepInput} />
            <StepOptionsView value={movement.stepOptions} />
          </RecordCard>
        );
      })}
    </Records>
  );
}

/** Outputs of completed Steps, as a Flow or SubFlow result carries them. */
export function StepOutputs({ values }: { values: unknown }) {
  const outputs = asRecordArray(values);
  if (outputs.length === 0) return null;
  return (
    <Records>
      {outputs.map((output, index) => (
        <RecordCard key={`${String(output.completedStepExecutionId)}-${index}`}>
          <Fields
            values={[
              ["Step", output.completedStepType],
              ["Execution ID", output.completedStepExecutionId],
            ]}
          />
          <ValueBlock label="Output" value={output.completedStepOutput} />
        </RecordCard>
      ))}
    </Records>
  );
}

export function StepDecisionView({ value }: { value: unknown }) {
  return (
    <DetailSection title="Step decision">
      <StepDecisionContent value={value} />
    </DetailSection>
  );
}

export function StepDecisionContent({ value }: { value: unknown }) {
  const decision = asRecord(value);
  if (!hasFields(decision)) return null;
  const close = asRecord(decision.closeDecision);
  return (
    <Records>
      <StepMovements values={decision.nextSteps} showFrom={false} />
      <CancellationSelectors decision={decision} />
      {hasFields(close) && (
        <RecordCard tone="decision">
          <RecordTitle>{closeDecisionTypeLabel(close.closeDecisionType)}</RecordTitle>
          <Fields values={[["Conditional channels", listText(close.conditionalChannelNames)]]} />
          <ValueBlock label="Close input" value={close.closeInput} />
        </RecordCard>
      )}
    </Records>
  );
}

function CancellationSelectors({ decision }: { decision: UnknownRecord }) {
  const flowStepTypes = listText(decision.cancelStepTypes);
  const siblingStepTypes = listText(decision.cancelSiblingStepTypes);
  if (!flowStepTypes && !siblingStepTypes) return null;
  return (
    <Records>
      {flowStepTypes && (
        <RecordCard tone="decision">
          <RecordTitle>Cancel Step executions</RecordTitle>
          <Fields
            values={[
              ["Scope", "Entire Flow"],
              ["Step types", flowStepTypes],
            ]}
          />
        </RecordCard>
      )}
      {siblingStepTypes && (
        <RecordCard tone="decision">
          <RecordTitle>Cancel sibling Step executions</RecordTitle>
          <Fields
            values={[
              ["Scope", "Same parent execution"],
              ["Step types", siblingStepTypes],
            ]}
          />
        </RecordCard>
      )}
    </Records>
  );
}
