// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { asRecord, asRecordArray, hasFields, isPresent, stringField, type UnknownRecord } from "@/lib/shared/json";
import {
  FLOW_TIMEOUT_HANDLER_STEP_TYPE,
  START_NODE_ID,
  isRpcSourceEvent,
  nextSteps,
  priorStartEvents,
  rpcSourceName,
  stepContext,
  stepExecutionId,
} from "@/lib/api/flow/flow-history";

/**
 * The Step options an event's Step execution was scheduled with, found where Dex recorded them: the
 * timeout handler options, a continued run's carried Steps, the initial start, or the decision of
 * the Step or RPC it came from. `visitedEventIDs` stops a cycle through failure recovery.
 */
export function findSourceStepOptions(
  event: FlowHistoryEvent,
  history: FlowHistoryEvent[],
  visitedEventIDs = new Set<number>(),
): UnknownRecord {
  if (visitedEventIDs.has(event.eventId)) return {};
  visitedEventIDs.add(event.eventId);

  const context = stepContext(event);
  const stepExecutionID = stringField(context.stepExecutionId);
  const fromStepExecutionID = stringField(context.fromStepExecutionId);
  const stepType = stringField(context.stepType);
  const stepInput = asRecord(event.payload.input).stepInput;
  if (!stepExecutionID || !stepType) return {};

  if (stepType === FLOW_TIMEOUT_HANDLER_STEP_TYPE) {
    return findTimeoutHandlerStepOptions(event, history);
  }

  const carriedOptions = findContinuedStepOptions(
    event,
    history,
    stepExecutionID,
    fromStepExecutionID,
    stepType,
    stepInput,
  );
  if (hasFields(carriedOptions)) return carriedOptions;

  if (fromStepExecutionID === START_NODE_ID) {
    const initialOptions = findInitialStepOptions(event, history, stepType);
    if (hasFields(initialOptions)) return initialOptions;
  }

  const sourceEvents = history
    .filter((candidate) => candidate.eventId < event.eventId)
    .filter((candidate) => isSourceEvent(candidate, fromStepExecutionID))
    .sort((left, right) => right.eventId - left.eventId);
  for (const sourceEvent of sourceEvents) {
    const movement = selectStepMovement(nextSteps(sourceEvent), stepType, fromStepExecutionID, stepInput);
    const movementOptions = asRecord(movement.stepOptions);
    if (hasFields(movementOptions)) return movementOptions;

    if (sourceEvent.type === "StepExecuteFailed") {
      const failedSourceOptions = findSourceStepOptions(sourceEvent, history, visitedEventIDs);
      if (failedSourceOptions.executeFailureProceedStepType === stepType) {
        const proceedOptions = asRecord(failedSourceOptions.executeFailureProceedStepOptions);
        if (hasFields(proceedOptions)) return proceedOptions;
      }
    }
  }
  return {};
}

function findTimeoutHandlerStepOptions(event: FlowHistoryEvent, history: FlowHistoryEvent[]): UnknownRecord {
  const startEvent = priorStartEvents(event, history).find((candidate) =>
    hasFields(asRecord(candidate.payload.timeoutHandlerOptions)),
  );
  const options = asRecord(startEvent?.payload.timeoutHandlerOptions);
  if (!hasFields(options)) return {};
  return {
    executeFailurePolicy: options.failurePolicy,
    executeFailureProceedStepType: options.failureProceedStepType,
    executeFailureProceedStepOptions: options.failureProceedStepOptions,
    executeLockAttributeKeys: options.lockAttributeKeys,
    executeLoadAttributeMapInstances: options.loadAttributeMapInstances,
    executeLoadChannelNames: options.loadChannelNames,
    executeLoadChannelMapInstances: options.loadChannelMapInstances,
  };
}

function findContinuedStepOptions(
  event: FlowHistoryEvent,
  history: FlowHistoryEvent[],
  stepExecutionID: string,
  fromStepExecutionID: string,
  stepType: string,
  stepInput: unknown,
): UnknownRecord {
  for (const startEvent of priorStartEvents(event, history)) {
    const continued = asRecord(startEvent.payload.continuedStart);
    if (!hasFields(continued)) continue;
    const resume = asRecordArray(continued.stepsToResume).find(
      (candidate) => candidate.stepExecutionId === stepExecutionID,
    );
    const resumeOptions = asRecord(asRecord(resume?.step).stepOptions);
    if (hasFields(resumeOptions)) return resumeOptions;

    const movement = selectStepMovement(
      asRecordArray(continued.stepsToStart),
      stepType,
      fromStepExecutionID,
      stepInput,
    );
    const movementOptions = asRecord(movement.stepOptions);
    if (hasFields(movementOptions)) return movementOptions;
  }
  return {};
}

function findInitialStepOptions(event: FlowHistoryEvent, history: FlowHistoryEvent[], stepType: string): UnknownRecord {
  const startEvent = priorStartEvents(event, history).find(
    (candidate) => asRecord(candidate.payload.initialStart).startStepType === stepType,
  );
  return asRecord(asRecord(startEvent?.payload.initialStart).stepOptions);
}

function isSourceEvent(event: FlowHistoryEvent, fromStepExecutionID: string): boolean {
  return rpcSourceName(fromStepExecutionID) !== ""
    ? isRpcSourceEvent(event, fromStepExecutionID)
    : stepExecutionId(event) === fromStepExecutionID;
}

function selectStepMovement(
  movements: UnknownRecord[],
  stepType: string,
  fromStepExecutionID: string,
  stepInput: unknown,
): UnknownRecord {
  const typeMatches = movements.filter((movement) => movement.stepType === stepType);
  const hasStampedSources = typeMatches.some((movement) => isPresent(movement.fromStepExecutionIdInternalOnly));
  const candidates = hasStampedSources
    ? typeMatches.filter((movement) => movement.fromStepExecutionIdInternalOnly === fromStepExecutionID)
    : typeMatches;
  if (candidates.length === 1) return candidates[0];

  const inputMatches = candidates.filter((movement) => valuesEqual(movement.stepInput, stepInput));
  if (inputMatches.length === 1) return inputMatches[0];
  const remaining = inputMatches.length > 0 ? inputMatches : candidates;
  if (remaining.length === 0) return {};
  const firstOptions = remaining[0].stepOptions;
  return remaining.every((movement) => valuesEqual(movement.stepOptions, firstOptions)) ? remaining[0] : {};
}

function valuesEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}
