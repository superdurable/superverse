// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { storedValueJSONReplacer } from "@/lib/api/flow/flow-blobs";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { flowErrorTypeLabel, flowStatusLabel } from "@/lib/flow/enum-labels";
import { asRecord, asRecordArray, isPresent, type UnknownRecord } from "@/lib/shared/json";
import { EffectsSection } from "./EffectsSection";
import { StepDecisionView, StepMovements, StepOutputs } from "@/components/flow/diagnostics/value/StepDecision";
import { StepMethodSections } from "./StepMethodSections";
import {
  FlowConfigView,
  FlowTimeoutHandlerOptionsView,
  StepOptionsView,
  flowTimeoutFields,
} from "@/components/flow/diagnostics/value/StepOptionViews";
import {
  ChannelMessages,
  DetailSection,
  DetailsRawTabs,
  Fields,
  KeyValues,
  ValueBlock,
  displayValue,
} from "@/components/flow/diagnostics/value/ValueFields";
import { WaitingConditionView } from "@/components/flow/diagnostics/value/WaitingCondition";
import { ALERT, RecordCard, Records, WELL } from "@/components/flow/diagnostics/value/DetailRecords";
import { cx } from "@/components/ui/cx";
import { isContinuedStart, previousRunId, stepMethod } from "@/lib/api/flow/flow-history";

const eventTitles: Record<FlowHistoryEvent["type"], string> = {
  FlowStartedOrContinued: "Flow started",
  FlowClosed: "Flow closed",
  StepWaitForCompleted: "WaitForCondition started",
  StepWaitForFailed: "WaitFor failed",
  StepWaitForPending: "WaitFor pending",
  StepExecuteCompleted: "Execute completed",
  StepExecuteFailed: "Execute failed",
  StepExecutePending: "Execute pending",
  RpcExecutionCompleted: "RPC completed",
  ChannelExternalPublish: "Channel published",
  TimeTravelFork: "Time Travel fork",
};

export function eventTitle(event: FlowHistoryEvent): string {
  if (isSetAttributesEvent(event)) return "Attributes updated";
  if (isContinuedStart(event)) return "Flow continued";
  return eventTitles[event.type];
}

export function eventTypeLabel(event: FlowHistoryEvent): string {
  return isSetAttributesEvent(event) ? "SetAttributes" : event.type;
}

function isSetAttributesEvent(event: FlowHistoryEvent): boolean {
  return event.type === "RpcExecutionCompleted" && event.payload.isSetAttributeApi === true;
}

function InitialStartDetails({ payload, showHeading = true }: { payload: UnknownRecord; showHeading?: boolean }) {
  const start = asRecord(payload.initialStart);
  return (
    <>
      <DetailSection title={showHeading ? "Initial start" : undefined}>
        <Fields values={[...flowTimeoutFields(payload), ["Start step", start.startStepType]]} />
        <ValueBlock label="Step input" value={start.stepInput} />
        <StepOptionsView value={start.stepOptions} />
      </DetailSection>
      {Array.isArray(start.initialAttributes) && start.initialAttributes.length > 0 && (
        <DetailSection title="Initial attributes">
          <KeyValues values={start.initialAttributes} />
        </DetailSection>
      )}
      <FlowConfigView value={payload.flowConfig} />
      <FlowTimeoutHandlerOptionsView value={payload.timeoutHandlerOptions} />
    </>
  );
}

function ContinuedStartDetails({
  payload,
  parentFlowId,
  showHeading = true,
}: {
  payload: UnknownRecord;
  parentFlowId: string;
  showHeading?: boolean;
}) {
  const continued = asRecord(payload.continuedStart);
  const resumes = asRecordArray(continued.stepsToResume);
  const pendingChannels = asRecord(continued.pendingChannelMessages);
  return (
    <>
      <DetailSection title={showHeading ? "Continued run" : undefined}>
        <Fields values={[["Previous run ID", continued.previousRunId], ...flowTimeoutFields(payload)]} />
      </DetailSection>
      {Array.isArray(continued.stepsToStart) && continued.stepsToStart.length > 0 && (
        <DetailSection title="Steps to start">
          <StepMovements values={continued.stepsToStart} />
        </DetailSection>
      )}
      {resumes.length > 0 && (
        <DetailSection title="Steps to resume">
          <Records>
            {resumes.map((resume, index) => (
              <RecordCard key={`${String(resume.stepExecutionId)}-${index}`}>
                <Fields values={[["Execution ID", resume.stepExecutionId]]} />
                <StepMovements values={[resume.step]} />
                <WaitingConditionView
                  value={resume.waitingCondition}
                  parentFlowId={parentFlowId}
                  stepExecutionId={String(resume.stepExecutionId ?? "")}
                />
                <KeyValues values={resume.stepExeLocals} />
              </RecordCard>
            ))}
          </Records>
        </DetailSection>
      )}
      {Object.keys(pendingChannels).length > 0 && (
        <DetailSection title="Pending channels">
          <ChannelMessages
            values={Object.entries(pendingChannels).flatMap(([channelName, entry]) =>
              asRecordArray(asRecord(entry).values).map((value) => ({ channelName, value })),
            )}
          />
        </DetailSection>
      )}
      {Array.isArray(continued.attributes) && continued.attributes.length > 0 && (
        <DetailSection title="Attributes">
          <KeyValues values={continued.attributes} />
        </DetailSection>
      )}
      {Array.isArray(continued.completedSteps) && continued.completedSteps.length > 0 && (
        <DetailSection title="Completed steps">
          <StepOutputs values={continued.completedSteps} />
        </DetailSection>
      )}
      <FlowConfigView value={payload.flowConfig} />
      <FlowTimeoutHandlerOptionsView value={payload.timeoutHandlerOptions} />
    </>
  );
}

function FlowClosedDetails({ payload }: { payload: UnknownRecord }) {
  const errorType = isPresent(payload.errorType) ? flowErrorTypeLabel(payload.errorType) : undefined;
  return (
    <>
      <DetailSection title="Outcome">
        <Fields
          values={[
            ["Status", flowStatusLabel(payload.flowStatus)],
            ["Continued to run", payload.continuedToRunId],
          ]}
        />
      </DetailSection>
      {(isPresent(payload.errorMessage) || (errorType && errorType !== "Unspecified")) && (
        <DetailSection title="Failure">
          <div className={ALERT}>
            <strong>{displayValue(payload.errorMessage)}</strong>
            <Fields values={[["Type", errorType]]} />
          </div>
        </DetailSection>
      )}
      {Array.isArray(payload.results) && payload.results.length > 0 && (
        <DetailSection title="Flow results">
          <StepOutputs values={payload.results} />
        </DetailSection>
      )}
    </>
  );
}

function TimeTravelForkDetails({ event, parentFlowId }: { event: FlowHistoryEvent; parentFlowId: string }) {
  const previousRun = previousRunId(event);
  return (
    <DetailSection title="Fork origin">
      <p className="grid gap-1.5">
        <span className="text-[10px] font-bold text-ink-faint uppercase">Previous run</span>
        {previousRun ? (
          <FlowLink
            className="text-inherit underline decoration-pass/35 underline-offset-3 hover:text-pass hover:decoration-current"
            title={previousRun}
            to={{ surface: "diagnostics", flowId: parentFlowId, runId: previousRun }}
          >
            <code className="wrap-anywhere">{previousRun}</code>
          </FlowLink>
        ) : (
          <span>—</span>
        )}
      </p>
    </DetailSection>
  );
}

function RPCDetails({ payload }: { payload: UnknownRecord }) {
  return (
    <>
      <DetailSection title="RPC call">
        <Fields values={[["RPC name", payload.rpcName]]} />
        <ValueBlock label="Input" value={payload.input} />
        <ValueBlock label="Output" value={payload.output} />
      </DetailSection>
      <StepDecisionView value={payload.stepDecision} />
      <EffectsSection value={payload} />
    </>
  );
}

function SetAttributesDetails({ payload }: { payload: UnknownRecord }) {
  return (
    <DetailSection title="Updated attributes">
      <KeyValues values={payload.upsertAttributes} emptyLabel="No attributes updated" />
    </DetailSection>
  );
}

export function SemanticEventDetails({
  event,
  history = [event],
  parentFlowId = "",
  showStartHeading = true,
}: {
  event: FlowHistoryEvent;
  history?: FlowHistoryEvent[];
  parentFlowId?: string;
  showStartHeading?: boolean;
}) {
  if (stepMethod(event) !== null) {
    return <StepMethodSections event={event} history={history} parentFlowId={parentFlowId} />;
  }
  if (event.type === "FlowStartedOrContinued") {
    return isContinuedStart(event) ? (
      <ContinuedStartDetails payload={event.payload} parentFlowId={parentFlowId} showHeading={showStartHeading} />
    ) : (
      <InitialStartDetails payload={event.payload} showHeading={showStartHeading} />
    );
  }
  if (event.type === "FlowClosed") return <FlowClosedDetails payload={event.payload} />;
  if (event.type === "TimeTravelFork") {
    return <TimeTravelForkDetails event={event} parentFlowId={parentFlowId} />;
  }
  if (isSetAttributesEvent(event)) return <SetAttributesDetails payload={event.payload} />;
  if (event.type === "RpcExecutionCompleted") return <RPCDetails payload={event.payload} />;
  return (
    <DetailSection title="Published messages">
      <ChannelMessages values={event.payload.messages} />
    </DetailSection>
  );
}

export function EventDetails({
  event,
  history,
  parentFlowId,
}: {
  event: FlowHistoryEvent;
  history: FlowHistoryEvent[];
  parentFlowId: string;
}) {
  const [view, setView] = useState<"details" | "raw">("details");
  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      <DetailsRawTabs label="Event payload view" view={view} onView={setView} />
      {view === "details" ? (
        <SemanticEventDetails event={event} history={history} parentFlowId={parentFlowId} />
      ) : (
        <pre className={cx(WELL, "max-h-[520px]")}>{JSON.stringify(event.payload, storedValueJSONReplacer, 2)}</pre>
      )}
    </div>
  );
}
