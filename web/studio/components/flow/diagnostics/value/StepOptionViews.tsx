// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import {
  activeStepSearchModeLabel,
  durabilityLabel,
  executeFailurePolicyLabel,
  flowTimeoutPolicyLabel,
  waitForFailurePolicyLabel,
} from "@/lib/flow/enum-labels";
import { formatElapsedDuration } from "@/lib/shared/format";
import { asRecord, hasFields, isPresent, type UnknownRecord } from "@/lib/shared/json";
import { Subsection } from "./DetailRecords";
import { DetailSection, Fields, displayValue, listText, secondsText, type Field } from "./ValueFields";

/** Step options, Flow configuration and timeout settings as recorded in history. */

function bytesText(value: unknown): string | undefined {
  return isPresent(value) ? `${String(value)} bytes` : undefined;
}

/** A protobuf Duration, either `"12.5s"` or `{seconds, nanos}`, as an elapsed-time label. */
export function protobufDuration(value: unknown): string | undefined {
  if (!isPresent(value)) return undefined;
  if (typeof value === "string") {
    const match = /^(-?\d+(?:\.\d+)?)s$/.exec(value);
    if (match) return formatElapsedDuration(Number(match[1]) * 1000);
  }
  const duration = asRecord(value);
  const seconds = Number(duration.seconds);
  const nanos = Number(duration.nanos);
  if (Number.isFinite(seconds) || Number.isFinite(nanos)) {
    return formatElapsedDuration(
      (Number.isFinite(seconds) ? seconds * 1000 : 0) + (Number.isFinite(nanos) ? nanos / 1_000_000 : 0),
    );
  }
  return displayValue(value);
}

/** The timeout policy, or undefined when it is unspecified. */
export function specifiedTimeoutPolicy(value: unknown): string | undefined {
  const label = flowTimeoutPolicyLabel(value);
  return label === "Unspecified" ? undefined : label;
}

export function flowTimeoutFields(payload: UnknownRecord): Field[] {
  const duration = protobufDuration(payload.flowTimeout);
  if (!duration) return [["Flow timeout", "No timeout"]];
  return [
    ["Flow timeout", duration],
    ["Timeout policy", specifiedTimeoutPolicy(payload.flowTimeoutPolicy)],
  ];
}

/**
 * A retry policy's five settings; Fields hides the ones the policy leaves unset. A zero attempt
 * count or total duration is the Dex spelling of "no limit".
 */
export function retryPolicyFields(policy: UnknownRecord): Field[] {
  return [
    ["Retry initial interval", secondsText(policy.initialIntervalSeconds)],
    ["Retry backoff coefficient", policy.backoffCoefficient],
    ["Retry maximum interval", secondsText(policy.maximumIntervalSeconds)],
    ["Retry maximum attempts", policy.maximumAttempts === 0 ? "Unlimited" : policy.maximumAttempts],
    [
      "Retry total duration",
      policy.totalDurationSeconds === 0 ? "Unlimited" : secondsText(policy.totalDurationSeconds),
    ],
  ];
}

export function StepOptionsView({ value }: { value: unknown }) {
  const options = asRecord(value);
  if (!hasFields(options)) return null;
  return (
    <Subsection title="Options">
      <Fields
        values={[
          ["WaitFor timeout", secondsText(options.waitForTimeoutSeconds)],
          ["Execute timeout", secondsText(options.executeTimeoutSeconds)],
          ["Skip WaitFor", options.skipWaitFor],
          [
            "WaitFor durability",
            isPresent(options.waitForDurabilityOverride)
              ? durabilityLabel(options.waitForDurabilityOverride)
              : undefined,
          ],
          [
            "Execute durability",
            isPresent(options.executeDurabilityOverride)
              ? durabilityLabel(options.executeDurabilityOverride)
              : undefined,
          ],
          [
            "WaitFor failure",
            isPresent(options.waitForFailurePolicy)
              ? waitForFailurePolicyLabel(options.waitForFailurePolicy)
              : undefined,
          ],
          [
            "Execute failure",
            isPresent(options.executeFailurePolicy)
              ? executeFailurePolicyLabel(options.executeFailurePolicy)
              : undefined,
          ],
          ["Failure proceeds to", options.executeFailureProceedStepType],
          ["WaitFor locks", listText(options.waitForLockAttributeKeys)],
          ["Execute locks", listText(options.executeLockAttributeKeys)],
          ["WaitFor AttributeMap loads", listText(options.waitForLoadAttributeMapInstances)],
          ["WaitFor Channel loads", listText(options.waitForLoadChannelNames)],
          ["WaitFor ChannelMap loads", listText(options.waitForLoadChannelMapInstances)],
          ["Execute AttributeMap loads", listText(options.executeLoadAttributeMapInstances)],
          ["Execute Channel loads", listText(options.executeLoadChannelNames)],
          ["Execute ChannelMap loads", listText(options.executeLoadChannelMapInstances)],
        ]}
      />
    </Subsection>
  );
}

/** The method's retry settings, with the policy and locks its Step options set for that method. */
export function StepMethodOptionsView({
  value,
  stepOptions,
  isWaitFor,
}: {
  value: unknown;
  stepOptions: unknown;
  isWaitFor: boolean;
}) {
  const options = asRecord(value);
  const movementOptions = asRecord(stepOptions);
  if (!hasFields(options) && !hasFields(movementOptions)) return null;
  const failurePolicy = isWaitFor ? movementOptions.waitForFailurePolicy : movementOptions.executeFailurePolicy;
  const lockingAttributes = isWaitFor
    ? movementOptions.waitForLockAttributeKeys
    : movementOptions.executeLockAttributeKeys;
  return (
    <Subsection title="Step options">
      <Fields
        compact
        values={[
          ["Timeout", secondsText(options.timeoutSeconds)],
          ...retryPolicyFields(asRecord(options.retryPolicy)),
          [
            "Failure policy",
            isPresent(failurePolicy)
              ? isWaitFor
                ? waitForFailurePolicyLabel(failurePolicy)
                : executeFailurePolicyLabel(failurePolicy)
              : undefined,
          ],
          ["Failure proceeds to", isWaitFor ? undefined : movementOptions.executeFailureProceedStepType],
          ["Locking attributes", listText(lockingAttributes), true],
        ]}
      />
    </Subsection>
  );
}

export function FlowConfigView({ value }: { value: unknown }) {
  const config = asRecord(value);
  if (!hasFields(config)) return null;
  return (
    <DetailSection title="Flow configuration">
      <Fields
        values={[
          ["Step durability", isPresent(config.stepDurability) ? durabilityLabel(config.stepDurability) : undefined],
          [
            "Active step search",
            isPresent(config.activeStepSearchMode) ? activeStepSearchModeLabel(config.activeStepSearchMode) : undefined,
          ],
          ["Continue-as-new threshold", config.continueAsNewThreshold],
          ["Continue-as-new page size", bytesText(config.continueAsNewPageSizeInBytes)],
        ]}
      />
    </DetailSection>
  );
}

export function FlowTimeoutHandlerOptionsView({ value }: { value: unknown }) {
  const options = asRecord(value);
  if (!hasFields(options)) return null;
  return (
    <DetailSection title="Timeout handler options">
      <Fields
        values={[
          ["Method timeout", secondsText(options.methodTimeoutSeconds)],
          ["Heartbeat timeout", secondsText(options.heartbeatTimeoutSeconds)],
          ...retryPolicyFields(asRecord(options.retryPolicy)),
          [
            "Failure policy",
            isPresent(options.failurePolicy) ? executeFailurePolicyLabel(options.failurePolicy) : undefined,
          ],
          ["Failure proceeds to", options.failureProceedStepType],
          [
            "Durability",
            isPresent(options.durabilityOverride) ? durabilityLabel(options.durabilityOverride) : undefined,
          ],
          ["Locking attributes", listText(options.lockAttributeKeys)],
          ["AttributeMap loads", listText(options.loadAttributeMapInstances)],
          ["Channel loads", listText(options.loadChannelNames)],
          ["ChannelMap loads", listText(options.loadChannelMapInstances)],
        ]}
      />
      <StepOptionsView value={options.failureProceedStepOptions} />
    </DetailSection>
  );
}
