// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionGraph, FlowDefinitionNode, FlowV2Action } from "@/lib/api/flow/flow-definition";
import type { AgentFlowDesign__Output } from "@/lib/generated/protobuf/superverse/v2/AgentFlowDesign";

/**
 * A designed Flow as the subset of Flow Definition Graph 2.0 that the canvas decoder reads, so the design
 * draws with the canvas that draws implemented Flows and splits its main story from its failure handling
 * the same way. Ids follow the analyzer's (`step:`, `rpc:`, `resource:channel:`). `source.language`
 * "design" and `valid: false` keep it out of every catalog and every Runs, Start or Action control.
 */

/** A Flow design as GetFlowDesign returns it. */
export type FlowDesign = AgentFlowDesign__Output;
export type DesignedFlow = FlowDesign["flows"][number];
export type DesignedStep = DesignedFlow["steps"][number];

type WireEdge = FlowDefinitionGraph["edges"][number];

const designStepId = (stepType: string) => `step:${stepType}`;
const channelId = (name: string) => `resource:channel:${name}`;
const rpcId = (rpc: string) => `rpc:${rpc}`;

/** A group's id from its label, as the analyzer derives one from `dex:group`. */
const groupId = (label: string) =>
  label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "") || "group";

/** The wait conditions of a Step: an Action is a wait on the Channel it publishes to, as in the source. */
function waitConditions(step: DesignedStep, flow: DesignedFlow) {
  const { kind, name } = step.waitsFor ?? { kind: "KIND_NONE", name: "" };
  if (kind === "KIND_CHANNEL") return [{ kind: "channel" as const, label: name, resourceId: channelId(name) }];
  if (kind === "KIND_TIMER") return [{ kind: "timer" as const, label: name }];
  if (kind !== "KIND_ACTION") return null;
  const channel = flow.actions.find((action) => action.rpc === name)?.publishes;
  return channel
    ? [{ kind: "channel" as const, label: channel, resourceId: channelId(channel) }]
    : [{ kind: "unknown" as const, label: name }];
}

export function designGraph(flow: DesignedFlow): FlowDefinitionGraph {
  const nodes: FlowDefinitionNode[] = [];
  const edges: WireEdge[] = [];
  const edge = (kind: string, from: string, to: string, label: string) =>
    edges.push({ id: `edge:${String(edges.length + 1).padStart(4, "0")}`, kind, from, to, label });

  for (const channel of flow.channels)
    nodes.push({
      id: channelId(channel.name),
      kind: "channel",
      name: channel.name,
      resource: { valueType: channel.payload || "unknown" },
    });
  for (const attribute of flow.attributes)
    nodes.push({
      id: `resource:attribute:${attribute.name}`,
      kind: "attribute",
      name: attribute.name,
      resource: { valueType: attribute.value || "unknown", map: attribute.isMap },
    });
  for (const action of flow.actions) {
    nodes.push({ id: rpcId(action.rpc), kind: "rpc", name: action.rpc, phase: "rpc" });
    if (action.publishes) edge("resource_publish", rpcId(action.rpc), channelId(action.publishes), "Publish");
  }
  for (const step of flow.steps) {
    const id = designStepId(step.stepType);
    const conditions = waitConditions(step, flow);
    nodes.push({
      id,
      kind: "step",
      name: step.stepType,
      phase: conditions ? "wait_for+execute" : "execute",
      ...(step.stepType === flow.startStep ? { start: true } : {}),
      ...(step.explanation ? { metadata: { explanation: step.explanation } } : {}),
    });
    if (conditions) {
      const waitId = `wait:${id}`;
      nodes.push({
        id: waitId,
        kind: "wait",
        name: "WaitFor",
        parentId: id,
        phase: "wait_for",
        wait: { type: "anyOf", conditions },
      });
      for (const condition of conditions)
        if (condition.kind === "channel") edge("wait_condition", condition.resourceId, waitId, "Wait");
    }
    step.next.forEach((next, index) => {
      // A failure route leaves the Step itself, as an Execute failure route does; one without a Step is the default.
      if (next.isFailureRoute) {
        if (next.toStep) edge("failure_transition", id, designStepId(next.toStep), "Execute failure");
        return;
      }
      const type = next.end === "END_COMPLETE" ? "gracefulComplete" : next.end === "END_FAIL" ? "forceFail" : "goTo";
      const decisionId = `decision:${id}:${index}`;
      nodes.push({
        id: decisionId,
        kind: "decision",
        name: type,
        parentId: id,
        phase: "execute",
        ...(next.when ? { condition: next.when } : {}),
        decision: { type },
      });
      if (type === "goTo") edge("transition", decisionId, designStepId(next.toStep), "GoTo");
    });
  }

  const labels = [...new Set(flow.steps.map((step) => step.group).filter((label) => label !== ""))];
  const actions: FlowV2Action[] = flow.actions.map((action) => ({
    rpcName: action.rpc,
    label: action.label,
    requiredPermission: action.permission,
    condition: { attributeKey: "", operator: "in", values: [] },
    input: { kind: "none" },
  }));
  return {
    schemaVersion: "2.0",
    valid: false,
    source: { language: "design", path: "" },
    flow: { name: flow.flowType, startStepId: designStepId(flow.startStep) },
    nodes,
    edges,
    diagnostics: [],
    groups: labels.map((label) => ({
      id: groupId(label),
      label,
      stepIds: flow.steps.filter((step) => step.group === label).map((step) => designStepId(step.stepType)),
    })),
    v2: {
      indexedAttributes: [],
      summary: { rpcName: "", fields: [] },
      display: { rpcName: "", fields: [] },
      actions,
      start: { stepType: flow.startStep, input: { kind: "null" } },
    },
  };
}
