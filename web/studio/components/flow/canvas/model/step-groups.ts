// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionGraph } from "@/lib/api/flow/flow-definition";
import type { FlowModel } from "./flow-model";

/** A labelled part of the Flow, authored in the definition and drawn as a band around its Steps. */
export interface StepGroup {
  id: string;
  label: string;
  /** Member Step ids; ids the decoded Flow does not have are dropped. */
  stepIds: string[];
}

export function groupsFromDefinition(graph: FlowDefinitionGraph, flow: FlowModel): StepGroup[] {
  const known = new Set(flow.steps.map((step) => step.id));
  return (graph.groups ?? []).map((group) => ({
    id: group.id,
    label: group.label,
    stepIds: group.stepIds.filter((stepId) => known.has(stepId)),
  }));
}
