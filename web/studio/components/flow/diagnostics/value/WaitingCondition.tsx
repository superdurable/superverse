// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import {
  conditionStatusLabel,
  flowErrorTypeLabel,
  flowStatusLabel,
  subFlowReusePolicyLabel,
  waitingConditionTypeLabel,
} from "@/lib/flow/enum-labels";
import { formatUnixSeconds } from "@/lib/shared/format";
import { asRecord, asRecordArray, hasFields, isPresent, type UnknownRecord } from "@/lib/shared/json";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { generatedSubFlowID } from "@/lib/api/flow/flow-history";
import { buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { ALERT, ConditionIcon, RecordCard, RecordTitle, Records, Subsection } from "./DetailRecords";
import { StepOutputs } from "./StepDecision";
import { retryPolicyFields, specifiedTimeoutPolicy } from "./StepOptionViews";
import { DetailSection, Fields, KeyValues, ValueBlock, displayValue, listText, secondsText } from "./ValueFields";

/** What a WaitFor waits on (Channels, Timers, SubFlows) and how each condition resolved. */

export function WaitingConditionView({
  value,
  parentFlowId,
  stepExecutionId,
}: {
  value: unknown;
  parentFlowId: string;
  stepExecutionId: string;
}) {
  return (
    <DetailSection title="WaitFor condition">
      <WaitingConditionContent value={value} parentFlowId={parentFlowId} stepExecutionId={stepExecutionId} />
    </DetailSection>
  );
}

function waitingConditionCompletionRule(condition: UnknownRecord, conditionCount: number): string {
  if (conditionCount === 0) return "Empty condition — skips WaitFor immediately";
  if (conditionCount === 1) return "Single condition";
  return waitingConditionTypeLabel(condition.waitingConditionType);
}

export function WaitingConditionContent({
  value,
  parentFlowId = "",
  stepExecutionId = "",
  showEmpty = false,
}: {
  value: unknown;
  parentFlowId?: string;
  stepExecutionId?: string;
  showEmpty?: boolean;
}) {
  const condition = asRecord(value);
  if (!hasFields(condition) && !showEmpty) return null;
  const channels = asRecordArray(condition.channelConditions);
  const timers = asRecordArray(condition.timerConditions);
  const subFlows = asRecordArray(condition.subFlowConditions);
  const combinations = asRecordArray(condition.conditionCombinations);
  return (
    <>
      <Fields
        values={[
          [
            "Completion rule",
            waitingConditionCompletionRule(condition, channels.length + timers.length + subFlows.length),
          ],
        ]}
      />
      {channels.length > 0 && (
        <Records>
          {channels.map((channel, index) => (
            <RecordCard key={`${String(channel.channelName)}-${index}`}>
              <RecordTitle channel>{displayValue(channel.channelName)}</RecordTitle>
              <Fields
                values={[
                  ["Condition ID", channel.conditionId],
                  ["At least", channel.atLeast],
                  ["At most", channel.atMost],
                ]}
              />
            </RecordCard>
          ))}
        </Records>
      )}
      {timers.length > 0 && (
        <Records>
          {timers.map((timer, index) => (
            <RecordCard key={`${String(timer.conditionId)}-${index}`}>
              <RecordTitle>Timer {index + 1}</RecordTitle>
              <Fields
                values={[
                  ["Condition ID", timer.conditionId],
                  ["Delay", secondsText(timer.durationSeconds)],
                  ["Fires at", formatUnixSeconds(timer.firingUnixTimestampSeconds)],
                ]}
              />
            </RecordCard>
          ))}
        </Records>
      )}
      {subFlows.length > 0 && (
        <Records>
          {subFlows.map((subFlow, index) => (
            <SubFlowRecord
              value={subFlow}
              index={index}
              parentFlowId={parentFlowId}
              stepExecutionId={stepExecutionId}
              key={`${parentFlowId}-${stepExecutionId}-${index}`}
            />
          ))}
        </Records>
      )}
      {combinations.length > 0 && (
        <Subsection title="Condition combinations">
          {combinations.map((combination, index) => (
            <p className="font-mono text-[9px] text-ink-faint" key={index}>
              {listText(combination.conditionIds) || "—"}
            </p>
          ))}
        </Subsection>
      )}
    </>
  );
}

/** Which conditions completed when Execute ran, with Channel values and SubFlow results. */
export function ConditionResultsContent({
  value,
  subFlowConditions = [],
  parentFlowId = "",
  stepExecutionId = "",
  showEmpty = false,
}: {
  value: unknown;
  subFlowConditions?: UnknownRecord[];
  parentFlowId?: string;
  stepExecutionId?: string;
  showEmpty?: boolean;
}) {
  const results = asRecord(value);
  if (!hasFields(results)) {
    return showEmpty ? <p className="text-xs text-ink-faint">No condition results</p> : null;
  }
  const channels = asRecordArray(results.channelResults);
  const timers = asRecordArray(results.timerResults);
  const subFlows = asRecordArray(results.subFlowResults);
  return (
    <>
      {results.waitForFailed === true && <p className={ALERT}>WaitFor failed</p>}
      <Records>
        {channels.map((channel, index) => (
          <RecordCard key={`${String(channel.channelName)}-${index}`}>
            <RecordTitle channel>{displayValue(channel.channelName)}</RecordTitle>
            <Fields
              values={[
                ["Condition ID", channel.conditionId],
                ["Status", conditionStatusLabel(channel.conditionStatus)],
              ]}
            />
            {Array.isArray(channel.values) &&
              channel.values.map((entry, valueIndex) => (
                <ValueBlock label={`Value ${valueIndex + 1}`} value={entry} key={valueIndex} />
              ))}
          </RecordCard>
        ))}
        {timers.map((timer, index) => (
          <RecordCard key={`${String(timer.conditionId)}-${index}`}>
            <RecordTitle>Timer {index + 1}</RecordTitle>
            <Fields
              values={[
                ["Condition ID", timer.conditionId],
                ["Status", conditionStatusLabel(timer.conditionStatus)],
              ]}
            />
          </RecordCard>
        ))}
        {subFlows.map((subFlow, index) => (
          <SubFlowResultRecord
            value={subFlow}
            condition={subFlowConditions[index]}
            parentFlowId={parentFlowId}
            stepExecutionId={stepExecutionId}
            index={index}
            key={index}
          />
        ))}
      </Records>
    </>
  );
}

function SubFlowRecord({
  value,
  index,
  parentFlowId,
  stepExecutionId,
}: {
  value: UnknownRecord;
  index: number;
  parentFlowId: string;
  stepExecutionId: string;
}) {
  const options = asRecord(value.options);
  const flowId = generatedSubFlowID(parentFlowId, stepExecutionId, index);
  return (
    <SubFlowNavigation flowId={flowId} label={`Open SubFlow ${flowId || index + 1}`}>
      <SubFlowTitle flowId={flowId} />
      <Fields
        values={[
          ["Condition ID", value.conditionId],
          ["Reuse policy", subFlowReusePolicyLabel(options.reusePolicy)],
          ["Timeout", secondsText(options.flowTimeoutSeconds)],
          [
            "Timeout policy",
            Number(options.flowTimeoutSeconds) > 0 ? specifiedTimeoutPolicy(options.flowTimeoutPolicy) : undefined,
          ],
          ["Start delay", secondsText(options.flowStartDelaySeconds)],
          ...retryPolicyFields(asRecord(options.retryPolicy)),
        ]}
      />
      <ValueBlock label="Input" value={value.stepInput} />
      <ValueBlock label="Starting Step options" value={value.stepOptions} />
      <ValueBlock label="Flow configuration override" value={options.flowConfigOverride} />
      {asRecordArray(options.attributes).length > 0 && (
        <Subsection title="Initial Attributes">
          <KeyValues values={options.attributes} />
        </Subsection>
      )}
    </SubFlowNavigation>
  );
}

function SubFlowResultRecord({
  value,
  condition = {},
  parentFlowId,
  stepExecutionId,
  index,
}: {
  value: UnknownRecord;
  condition?: UnknownRecord;
  parentFlowId: string;
  stepExecutionId: string;
  index: number;
}) {
  const flowId = generatedSubFlowID(parentFlowId, stepExecutionId, index);
  return (
    <SubFlowNavigation flowId={flowId} label={`Open SubFlow result ${flowId || index + 1}`}>
      <SubFlowTitle flowId={flowId} />
      <Fields
        values={[
          ["Condition ID", condition.conditionId],
          ["Status", flowStatusLabel(value.flowStatus)],
          ["Failure type", isPresent(value.errorType) ? flowErrorTypeLabel(value.errorType) : undefined],
          ["Failure", value.errorMessage],
        ]}
      />
      <StepOutputs values={value.results} />
    </SubFlowNavigation>
  );
}

function SubFlowTitle({ flowId }: { flowId: string }) {
  return (
    <strong className="flex min-w-0 items-center gap-1.5 text-[10px]">
      <ConditionIcon kind="subflow" className="size-3.75 shrink-0" />
      <code className="wrap-anywhere">{flowId}</code>
    </strong>
  );
}

/** A SubFlow record with a link to the SubFlow's own Diagnostics. */
function SubFlowNavigation({ flowId, label, children }: { flowId: string; label: string; children: ReactNode }) {
  return (
    <RecordCard>
      {children}
      {flowId && (
        <FlowLink className={cx(buttonStyle("secondary", "sm"), "self-start")} to={{ surface: "diagnostics", flowId }}>
          {label}
        </FlowLink>
      )}
    </RecordCard>
  );
}
