// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { predecessors, stepById, successors, type Actor, type FlowModel, type StepModel } from "./flow-model";
import type { StepGroup } from "./step-groups";

interface StepFact {
  label: string;
  value: string;
}

export interface StepContextView {
  stepType: string;
  /** The Step's declared purpose, or null when the Flow never said. Never invented. */
  explanation: string | null;
  facts: StepFact[];
  connector?: StepModel["connector"];
}

/** Who moves this Step along. The graph knows this; a reader should not have to infer it. */
const ACTOR_PHRASE: Record<Actor, string> = {
  external: "a person or system outside the flow",
  child: "a SubFlow",
  clock: "a timer",
  machine: "the flow itself",
  unknown: "not stated",
};

/**
 * Where a Step sits in its flow, in the flow's own terms.
 *
 * Purpose first, then placement: the phase it belongs to, who acts, what reaches it, what it
 * waits for, and where it can go. Every line is read from the graph — nothing is inferred, and a
 * fact the Flow did not declare is omitted rather than guessed at.
 */
export function stepContext(flow: FlowModel, step: StepModel, groups: readonly StepGroup[]): StepContextView {
  const facts: StepFact[] = [];

  const phase = groups.find((group) => group.stepIds.includes(step.id));
  if (phase !== undefined) facts.push({ label: "In phase", value: phase.label });

  facts.push({ label: "Who acts", value: ACTOR_PHRASE[step.actor] });

  const reachedFrom = step.isStart
    ? "the flow starts here"
    : stepTypesOf(flow, predecessors(flow, step.id, "transition"));
  if (reachedFrom !== "") facts.push({ label: "Reached from", value: reachedFrom });

  // The wait phase already carries one prose line, paraphrased with identifiers verbatim.
  if (step.waitFor !== null && step.waitFor.sentence !== "") {
    facts.push({ label: "Waits for", value: step.waitFor.sentence });
  }

  const goesTo = stepTypesOf(flow, successors(flow, step.id, "transition"));
  if (goesTo !== "") facts.push({ label: "Then goes to", value: goesTo });

  const onFailure = stepTypesOf(flow, successors(flow, step.id, "failure_transition"));
  if (onFailure !== "") facts.push({ label: "If it fails", value: onFailure });

  return {
    stepType: step.stepType,
    explanation: step.explanation ?? null,
    facts,
    connector: step.connector,
  };
}

/** The distinct StepTypes of these Step ids, comma-separated; ids that name no Step are skipped. */
function stepTypesOf(flow: FlowModel, stepIds: string[]): string {
  const stepTypes = stepIds.flatMap((id) => stepById(flow, id)?.stepType ?? []);
  return [...new Set(stepTypes)].join(", ");
}
