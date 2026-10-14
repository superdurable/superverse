// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { answeredBy, reachableFromStart, type FlowModel } from "./flow-model";
import { activeStepTypes, type RunOverlay } from "./run-overlay";

type ActionableState = "done" | "current" | "upcoming";

export interface ActionableStep {
  stepId: string;
  stepType: string;
  state: ActionableState;
}

/**
 * The Steps of a Flow that wait for a person, in the order the graph reaches them.
 *
 * Derived, not declared: a Step is human-actionable when something outside the Flow publishes to
 * the Channel it waits on, which `answeredBy` reads straight off the graph. So this needs no new
 * contract — and a Flow that grows a third gate grows a third entry with no UI change.
 */
export function actionableSteps(flow: FlowModel, overlay: RunOverlay | null): ActionableStep[] {
  const open = overlay === null ? [] : activeStepTypes(overlay);
  const ordered = orderedStepIds(flow);
  const waiting = flow.steps
    .filter((step) => answeredBy(flow, step).length > 0)
    .sort((left, right) => ordered.indexOf(left.id) - ordered.indexOf(right.id));

  const currentIndex = waiting.findIndex((step) => open.includes(step.stepType));
  return waiting.map((step, index) => ({
    stepId: step.id,
    stepType: step.stepType,
    state: stateFor(index, currentIndex, overlay, step.stepType),
  }));
}

function stateFor(index: number, currentIndex: number, overlay: RunOverlay | null, stepType: string): ActionableState {
  if (currentIndex === index) return "current";
  // No run yet: the list is the Flow's shape, so nothing has happened to any of it.
  if (overlay === null) return "upcoming";
  const ran = overlay.executions.some((execution) => execution.stepType === stepType);
  if (ran) return "done";
  // Before the open gate in graph order but never executed: a branch skipped it.
  return currentIndex >= 0 && index < currentIndex ? "done" : "upcoming";
}

/**
 * Every Step id in graph order rather than execution order: an upcoming gate has no execution to
 * sort by, and a stepper has to place it before the reader gets there.
 */
function orderedStepIds(flow: FlowModel): string[] {
  const reachable = reachableFromStart(flow);
  const seen = new Set(reachable);
  // Anything unreachable still gets a stable position rather than -1.
  return [...reachable, ...flow.steps.map((step) => step.id).filter((id) => !seen.has(id))];
}

/** Whether a stepper is worth drawing at all. One gate needs no progress strip. */
export function hasMultipleActions(steps: readonly ActionableStep[]): boolean {
  return steps.length > 1;
}
