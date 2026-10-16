// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionGraph } from "@/lib/api/flow/flow-definition";
import { safeDecode } from "@/components/flow/canvas/model/decode-flow-definition";
import { stepById, type FlowModel, type StepModel } from "@/components/flow/canvas/model/flow-model";
import type { StepMarkText } from "@/components/flow/canvas/FlowGraphCanvas";
import { processName, stepName } from "@/lib/shared/format";
import { designGraph, type DesignedFlow, type FlowDesign } from "./design-graph";

/**
 * A design against the current source, per Flow type, for the marks on the designed Steps and the card's
 * change lines. Steps, Channels and Actions match by name, waits after normalization, and Step-to-Step
 * transitions by their ends and kind: a transition that turns into a failure route moves a Step between the
 * main story and failure handling. Attributes, guards, timeout handlers, SubFlows, views and the Start input
 * are not compared. It never compares a build with its design.
 */

export interface FlowComparison {
  flowType: string;
  status: "new" | "changed" | "unchanged";
  /** By the designed Step's id. */
  marks: ReadonlyMap<string, StepMarkText>;
  addedSteps: string[];
  removedSteps: string[];
  changedSteps: string[];
  /** Action labels. */
  addedActions: string[];
  removedActions: string[];
  addedChannels: string[];
  removedChannels: string[];
}

/** One designed Flow, decoded; null when the decoder cannot read the expansion. */
export function decodeDesignedFlow(flow: DesignedFlow): { graph: FlowDefinitionGraph; model: FlowModel } | null {
  const graph = designGraph(flow);
  const model = safeDecode(graph);
  return model ? { graph, model } : null;
}

const resourceName = (flow: FlowModel, id: string | undefined) =>
  flow.resources.find((resource) => resource.id === id)?.name;

/** "action <rpc>" for a Channel an Action publishes to, "channel <name>" for another, "timer" for a time. */
function waitOf(flow: FlowModel, step: StepModel): string {
  if (step.waitFor === null) return "none";
  return step.waitFor.conditions
    .map((condition) => {
      if (condition.kind === "timer") return "timer";
      if (condition.kind !== "channel") return `${condition.kind} ${condition.label}`;
      const channel = resourceName(flow, condition.resourceId) ?? condition.label;
      const actions = flow.entries
        .filter(
          (entry) =>
            entry.action !== undefined &&
            entry.resources.some((ref) => ref.access === "publish" && resourceName(flow, ref.resourceId) === channel),
        )
        .map((entry) => entry.name)
        .sort();
      return actions.length > 0 ? `action ${actions.join(",")}` : `channel ${channel}`;
    })
    .sort()
    .join(" | ");
}

/** Step-to-Step transitions by "from->to", with the kinds that join them. */
function transitionsOf(flow: FlowModel): Map<string, Set<string>> {
  const pairs = new Map<string, Set<string>>();
  for (const transition of flow.transitions) {
    const from = stepById(flow, transition.fromStepId)?.stepType;
    const to = stepById(flow, transition.toStepId)?.stepType;
    if (from === undefined || to === undefined) continue;
    const key = `${from}->${to}`;
    pairs.set(key, new Set([...(pairs.get(key) ?? []), transition.kind]));
  }
  return pairs;
}

const named = <T>(items: readonly T[], name: (item: T) => string) => new Map(items.map((item) => [name(item), item]));
const missingFrom = <T>(from: ReadonlyMap<string, T>, other: ReadonlyMap<string, unknown>) =>
  [...from.keys()].filter((key) => !other.has(key));
const sameKinds = (a: ReadonlySet<string>, b: ReadonlySet<string>) =>
  a.size === b.size && [...a].every((kind) => b.has(kind));

export function compareFlow(designed: FlowModel, current: FlowModel | null): FlowComparison {
  const designedSteps = named(designed.steps, (step) => step.stepType);
  const designedActions = named(
    designed.entries.filter((entry) => entry.action !== undefined),
    (entry) => entry.name,
  );
  const designedChannels = named(
    designed.resources.filter((resource) => resource.kind === "channel"),
    (resource) => resource.name,
  );
  const label = (actions: ReadonlyMap<string, { action?: { label: string } }>, rpc: string) =>
    actions.get(rpc)?.action?.label || rpc;
  if (current === null)
    return {
      flowType: designed.flowType,
      status: "new",
      marks: new Map(designed.steps.map((step) => [step.id, "new"])),
      addedSteps: [...designedSteps.keys()],
      removedSteps: [],
      changedSteps: [],
      addedActions: [...designedActions.keys()].map((rpc) => label(designedActions, rpc)),
      removedActions: [],
      addedChannels: [...designedChannels.keys()],
      removedChannels: [],
    };
  const currentSteps = named(current.steps, (step) => step.stepType);
  const currentActions = named(
    current.entries.filter((entry) => entry.action !== undefined),
    (entry) => entry.name,
  );
  const currentChannels = named(
    current.resources.filter((resource) => resource.kind === "channel"),
    (resource) => resource.name,
  );
  const designedPairs = transitionsOf(designed);
  const currentPairs = transitionsOf(current);
  // A Step changes with its wait or with a transition that leaves it.
  const changed = new Set<string>();
  for (const [stepType, step] of designedSteps) {
    const before = currentSteps.get(stepType);
    if (before !== undefined && waitOf(designed, step) !== waitOf(current, before)) changed.add(stepType);
  }
  for (const pair of new Set([...designedPairs.keys(), ...currentPairs.keys()])) {
    const [after, before] = [designedPairs.get(pair), currentPairs.get(pair)];
    if (after && before && sameKinds(after, before)) continue;
    const from = pair.slice(0, pair.indexOf("->"));
    if (designedSteps.has(from) && currentSteps.has(from)) changed.add(from);
  }
  const marks = new Map<string, StepMarkText>();
  for (const [stepType, step] of designedSteps) {
    if (!currentSteps.has(stepType)) marks.set(step.id, "new");
    else if (changed.has(stepType)) marks.set(step.id, "changed");
  }
  const comparison: FlowComparison = {
    flowType: designed.flowType,
    status: "changed",
    marks,
    addedSteps: missingFrom(designedSteps, currentSteps),
    removedSteps: missingFrom(currentSteps, designedSteps),
    changedSteps: [...changed],
    addedActions: missingFrom(designedActions, currentActions).map((rpc) => label(designedActions, rpc)),
    removedActions: missingFrom(currentActions, designedActions).map((rpc) => label(currentActions, rpc)),
    addedChannels: missingFrom(designedChannels, currentChannels),
    removedChannels: missingFrom(currentChannels, designedChannels),
  };
  const differs = [
    comparison.addedSteps,
    comparison.removedSteps,
    comparison.changedSteps,
    comparison.addedActions,
    comparison.removedActions,
    comparison.addedChannels,
    comparison.removedChannels,
  ].some((list) => list.length > 0);
  return differs ? comparison : { ...comparison, status: "unchanged" };
}

/** The current source's graphs by Flow type, or null when it is not the Session's head and nothing compares. */
export type CurrentGraphs = ReadonlyMap<string, FlowDefinitionGraph> | null;

/** Every designed Flow against the current source; null when nothing compares. */
function compareDesign(design: FlowDesign, current: CurrentGraphs): FlowComparison[] | null {
  if (current === null) return null;
  return design.flows.flatMap((flow) => {
    const designed = decodeDesignedFlow(flow)?.model;
    if (!designed) return [];
    const graph = current.get(flow.flowType);
    return [compareFlow(designed, graph ? safeDecode(graph) : null)];
  });
}

/**
 * What a comparison says in one line, in the names people read on the canvas, such as "Text summary · changed:
 * adds Await summary approval, Approve summary; changes Generate summary".
 */
function changeLine(comparison: FlowComparison): string {
  const { status } = comparison;
  const flow = processName(comparison.flowType);
  if (status === "unchanged") return `${flow} · unchanged`;
  if (status === "new") {
    const steps = comparison.addedSteps.length;
    const actions = comparison.addedActions.length > 0 ? `, ${comparison.addedActions.join(", ")}` : "";
    return `${flow} · new: ${steps} step${steps === 1 ? "" : "s"}${actions}`;
  }
  const added = [...comparison.addedSteps.map(stepName), ...comparison.addedActions];
  const removed = [...comparison.removedSteps.map(stepName), ...comparison.removedActions];
  const parts = [
    added.length > 0 ? `adds ${added.join(", ")}` : "",
    comparison.changedSteps.length > 0 ? `changes ${comparison.changedSteps.map(stepName).join(", ")}` : "",
    removed.length > 0 ? `removes ${removed.join(", ")}` : "",
  ].filter((part) => part !== "");
  if (parts.length === 0) {
    const channels = [...comparison.addedChannels, ...comparison.removedChannels].map(stepName);
    parts.push(`changes the messages it waits for: ${channels.join(", ")}`);
  }
  return `${flow} · changed: ${parts.join("; ")}`;
}

/** The card's lines of what the design changes; one note instead when the current source is not the head's. */
export function designChangeLines(design: FlowDesign, current: CurrentGraphs): string[] {
  const comparisons = compareDesign(design, current);
  if (comparisons === null) return ["Not compared: build the processes of the current version to see what changes."];
  const removed = design.removedFlows.length > 0 ? [`Removes ${design.removedFlows.map(processName).join(", ")}`] : [];
  return [...comparisons.map(changeLine), ...removed];
}
