// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionCatalog } from "@/lib/api/flow/flow-types";
import { safeDecode } from "@/components/flow/canvas/model/decode-flow-definition";
import {
  reachableFromStart,
  whoActs,
  type FlowModel,
  type StepModel,
  type WhoActs,
} from "@/components/flow/canvas/model/flow-model";
import { groupsFromDefinition, type StepGroup } from "@/components/flow/canvas/model/step-groups";
import { hasStages } from "@/components/flow/canvas/layout/layout-flow";
import { connectorName } from "@/components/flow/canvas/layout/step-card";

interface ProcessStep {
  id: string;
  label: string;
  who: WhoActs;
  /** The service a connector Step reaches, when one does. */
  app?: string;
}

/** One card of the project map: a valid Flow Type with its Steps in reading order. */
export interface ProcessCard {
  flowType: string;
  steps: ProcessStep[];
  /** The labels of the Flow's stages in reading order, when it has them; empty draws its Steps instead. */
  stages: string[];
  /** Services its connector Steps reach, in reading order. */
  apps: string[];
  /** Flow Types of this Project that this one starts as a child Flow. */
  starts: string[];
}

/** The Steps in the order a reader meets them: reachable from the start first, the rest after. */
function orderedSteps(flow: FlowModel): StepModel[] {
  const reached = reachableFromStart(flow);
  const rest = flow.steps.filter((step) => !reached.includes(step.id)).map((step) => step.id);
  return [...reached, ...rest].flatMap((id) => flow.steps.filter((step) => step.id === id));
}

function stagesOf(steps: ProcessStep[], groups: StepGroup[]): string[] {
  const stages = new Map<string, string>();
  for (const step of steps) {
    const group = groups.find((g) => g.stepIds.includes(step.id));
    if (group) stages.set(group.id, group.label);
  }
  return [...stages.values()];
}

/**
 * The project map's cards, one per valid Flow Definition of the catalog, ordered so that a Flow Type
 * comes before the ones it starts; a link is drawn only where a subflow node names a Flow Type of
 * this Project.
 */
export function processCards(definitions: FlowDefinitionCatalog | null): ProcessCard[] {
  const flows = (definitions?.definitions ?? [])
    .filter((definition) => definition.valid && definition.schemaVersion === "2.0")
    .flatMap((definition) => {
      const flow = safeDecode(definition.graph);
      return flow
        ? [{ flowType: definition.flowName, flow, groups: groupsFromDefinition(definition.graph, flow) }]
        : [];
    })
    .sort((a, b) => a.flowType.localeCompare(b.flowType));
  const known = new Set(flows.map((item) => item.flowType));
  const cards: ProcessCard[] = flows.map(({ flowType, flow, groups }) => {
    const steps = orderedSteps(flow).map((step) => ({
      id: step.id,
      label: step.label,
      who: whoActs(flow, step),
      ...(step.connector ? { app: connectorName(step.connector.connectorId) } : {}),
    }));
    return {
      flowType,
      steps,
      stages: hasStages(flow, groups) ? stagesOf(steps, groups) : [],
      apps: [...new Set(steps.flatMap((step) => (step.app ? [step.app] : [])))],
      starts: [...new Set(flow.subflows.map((subflow) => subflow.flowType))].filter(
        (type) => known.has(type) && type !== flowType,
      ),
    };
  });
  // Parents before the children they start, otherwise alphabetical.
  const started = new Set(cards.flatMap((card) => card.starts));
  const order: ProcessCard[] = [];
  const visit = (card: ProcessCard) => {
    if (order.includes(card)) return;
    order.push(card);
    for (const child of card.starts) {
      const next = cards.find((item) => item.flowType === child);
      if (next) visit(next);
    }
  };
  cards.filter((card) => !started.has(card.flowType)).forEach(visit);
  cards.forEach(visit);
  return order;
}
