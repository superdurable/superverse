// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { failureRoutes, stepById, stepByType, type FlowModel } from "@/components/flow/canvas/model/flow-model";
import { cardStatus, type RunOverlay, type StepExecution } from "@/components/flow/canvas/model/run-overlay";
import { stepTitle } from "@/components/flow/canvas/layout/step-card";
import { stepName } from "@/lib/shared/format";

/**
 * A Step's failure in a run, read only from the run and its Flow Definition: the execution that
 * failed, the engine's words for it, and the Step the run went on to from it, if any.
 */
export interface RunFailure {
  stepType: string;
  /** The Step as the canvas titles it. */
  title: string;
  /** The failed execution and method: the point a retry runs again from. */
  stepExecutionId: string;
  method: "waitFor" | "execute";
  attempts: number;
  error?: string;
  errorType?: string;
  /**
   * The Step an execution started from the failed one names, or null when none did. `viaFailureRoute`
   * says whether the definition routes this Step's failures there.
   */
  next: { title: string; viaFailureRoute: boolean } | null;
  /** The selected Step is the one that failed; otherwise this is the run's latest failure. */
  atSelectedStep: boolean;
}

/** The selected Step's failure when its latest execution failed, else the run's latest failure; null when nothing failed. */
export function runFailure(flow: FlowModel, run: RunOverlay, selectedStepType: string): RunFailure | null {
  const startedFrom = (e: StepExecution) => run.executions.find((n) => n.fromStepExecutionId === e.stepExecutionId);
  const selected = run.executions.filter((e) => e.stepType === selectedStepType).at(-1);
  const atSelectedStep = selected !== undefined && cardStatus(selected) === "failed";
  const failed = run.executions.filter((e) => cardStatus(e) === "failed");
  // A failed run ended at a failure nothing picked up, which is not always the last one to fail.
  const execution = atSelectedStep ? selected : (failed.filter((e) => !startedFrom(e)).at(-1) ?? failed.at(-1));
  if (execution === undefined) return null;
  const step = stepByType(flow, execution.stepType);
  const next = startedFrom(execution);
  const nextStep = next && stepByType(flow, next.stepType);
  return {
    stepType: execution.stepType,
    title: step ? stepTitle(step) : stepName(execution.stepType),
    stepExecutionId: execution.stepExecutionId,
    method: execution.waitFor?.status === "failed" ? "waitFor" : "execute",
    attempts: execution.attempts,
    error: execution.lastFailure,
    errorType: execution.lastFailureType,
    next: next
      ? {
          title: nextStep ? stepTitle(nextStep) : stepName(next.stepType),
          viaFailureRoute:
            step !== undefined &&
            failureRoutes(flow, step.id).some((route) => stepById(flow, route.toStepId)?.stepType === next.stepType),
        }
      : null,
    atSelectedStep,
  };
}
