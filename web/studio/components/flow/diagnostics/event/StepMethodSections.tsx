// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { durabilityLabel, pendingStepMethodPhaseLabel } from "@/lib/flow/enum-labels";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { formatDate } from "@/lib/shared/format";
import { asRecord, asRecordArray, hasFields, isPresent, type UnknownRecord } from "@/lib/shared/json";
import { EffectsContent } from "./EffectsSection";
import { FailureDetails } from "./FailureDetails";
import { StepDecisionContent } from "@/components/flow/diagnostics/value/StepDecision";
import { stepContext, stepDecision, stepExecutionId, stepMethod } from "@/lib/api/flow/flow-history";
import { findSourceStepOptions } from "./step-options";
import { StepMethodOptionsView, protobufDuration } from "@/components/flow/diagnostics/value/StepOptionViews";
import { DetailSection, Fields, KeyValues, ValueBlock } from "@/components/flow/diagnostics/value/ValueFields";
import { ConditionResultsContent, WaitingConditionContent } from "@/components/flow/diagnostics/value/WaitingCondition";
import { Subsection } from "@/components/flow/diagnostics/value/DetailRecords";

/** A WaitFor or Execute event: what the method received, what it produced, and how it ran. */

const ASYNC_STEP_INPUT_SNAPSHOT_NOT_RECORDED =
  "This ASYNC Step method exhausted its short retry policy before sync fallback, so its full invocation input snapshot was not recorded. This does not indicate an individual Value blob load failure.";
const STEP_INPUT_SNAPSHOT_UNAVAILABLE =
  "The Server may have set blobStore.asyncStepInputSnapshotsEnabled to false, Blob Store may be disabled, or this snapshot may no longer be retained. The full invocation input snapshot is unavailable; this does not indicate an individual Value blob load failure.";

type StepMethodSectionProps = {
  event: FlowHistoryEvent;
  history: FlowHistoryEvent[];
  parentFlowId: string;
};

export function StepMethodSections(props: StepMethodSectionProps) {
  return (
    <>
      <StepMethodInputSection {...props} />
      <StepMethodOutputSection {...props} />
      <StepMethodContextSection {...props} />
    </>
  );
}

function stepMethodFlags(event: FlowHistoryEvent) {
  const payload = event.payload;
  const input = asRecord(payload.input);
  const context = stepContext(event);
  return {
    payload,
    input,
    output: asRecord(payload.output),
    context,
    lastFailure: asRecord(context.lastFailureInfo),
    isWaitFor: stepMethod(event) === "wait-for",
    isPending: event.type.endsWith("Pending"),
    isAsyncTerminalFailure: event.type.endsWith("Failed") && durabilityLabel(context.durability) === "async",
    hasInput: payload.input !== undefined && input.unavailable !== true,
    executionId: stepExecutionId(event),
  };
}

function StepMethodInputSection({ event, history, parentFlowId }: StepMethodSectionProps) {
  const { input, isWaitFor, isAsyncTerminalFailure, hasInput, executionId } = stepMethodFlags(event);
  return (
    <DetailSection title="Input">
      {input.unavailable === true ? (
        <div className="flex flex-col gap-1.25 rounded-md border border-accent-rule/50 bg-accent-subtle p-2.25 text-[10px]/normal text-ink-dim">
          <strong className="text-ink">Step method input snapshot unavailable</strong>
          <p>{isAsyncTerminalFailure ? ASYNC_STEP_INPUT_SNAPSHOT_NOT_RECORDED : STEP_INPUT_SNAPSHOT_UNAVAILABLE}</p>
          {isAsyncTerminalFailure && (
            <p>
              Step input, attributes, condition results, and step locals are absent for this failure. Follow its source
              arrow in Timeline to inspect the event that scheduled the Step.
            </p>
          )}
        </div>
      ) : hasInput ? (
        <>
          <ValueBlock label="Step input" value={input.stepInput} showEmpty />
          {!isWaitFor && (
            <Subsection title="Condition results">
              <ConditionResultsContent
                value={input.conditionResults}
                subFlowConditions={findSubFlowConditions(history, executionId)}
                parentFlowId={parentFlowId}
                stepExecutionId={executionId}
                showEmpty
              />
            </Subsection>
          )}
          <Subsection title="Attributes">
            <KeyValues values={input.attributes} emptyLabel="No attributes" />
          </Subsection>
          {!isWaitFor && Array.isArray(input.stepExecutionLocals) && input.stepExecutionLocals.length > 0 && (
            <Subsection title="Step locals">
              <KeyValues values={input.stepExecutionLocals} />
            </Subsection>
          )}
        </>
      ) : (
        <p className="text-xs text-ink-faint">No input</p>
      )}
    </DetailSection>
  );
}

function StepMethodOutputSection({ event, parentFlowId }: StepMethodSectionProps) {
  const { payload, output, isPending, executionId } = stepMethodFlags(event);
  const decision = stepDecision(event);
  return (
    <DetailSection title={isPending ? "Status" : "Output"}>
      {isPending && <Fields values={[["Activity phase", pendingStepMethodPhaseLabel(payload.phase)]]} />}
      {event.type === "StepWaitForCompleted" ? (
        <Subsection title="WaitFor condition">
          <WaitingConditionContent
            value={output.waitForCondition}
            parentFlowId={parentFlowId}
            stepExecutionId={executionId}
            showEmpty
          />
        </Subsection>
      ) : decision && hasFields(decision) ? (
        <Subsection title="Step decision">
          <StepDecisionContent value={decision} />
        </Subsection>
      ) : null}
      <EffectsContent value={output} />
      {hasFields(asRecord(output.failure)) && (
        <Subsection title="Failure">
          <FailureDetails value={output.failure} />
        </Subsection>
      )}
    </DetailSection>
  );
}

function StepMethodContextSection({ event, history }: StepMethodSectionProps) {
  const { context, lastFailure, isWaitFor } = stepMethodFlags(event);
  return (
    <DetailSection title="Context">
      <Fields
        compact
        values={[
          ["Execution ID", context.stepExecutionId],
          ["From", context.fromStepExecutionId],
          ["Durability", durabilityLabel(context.durability)],
          ["Final attempt", context.finalAttempt],
          ["Started", isPresent(context.startedTime) ? formatDate(String(context.startedTime)) : undefined],
          ["Duration", protobufDuration(context.duration)],
        ]}
      />
      <StepMethodOptionsView
        value={context.methodOptions}
        stepOptions={findSourceStepOptions(event, history)}
        isWaitFor={isWaitFor}
      />
      {hasFields(lastFailure) && (
        <Subsection title="Last failure">
          <FailureDetails value={lastFailure} />
        </Subsection>
      )}
    </DetailSection>
  );
}

/**
 * The SubFlow conditions of the WaitFor that preceded this Execute. Execute results list SubFlows
 * by position only, so their condition IDs and generated FlowIDs come from that WaitFor.
 */
function findSubFlowConditions(history: FlowHistoryEvent[], executionId: string): UnknownRecord[] {
  if (!executionId) return [];
  const waitFor = history.findLast(
    (candidate) => candidate.type === "StepWaitForCompleted" && stepExecutionId(candidate) === executionId,
  );
  if (!waitFor) return [];
  return asRecordArray(asRecord(asRecord(waitFor.payload.output).waitForCondition).subFlowConditions);
}
