// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { caseLabel } from "@/components/flow/canvas/model/branch-routing";
import { stepName } from "@/lib/shared/format";
import {
  failureHandling,
  failureRoutes,
  mainStory,
  type FlowModel,
  type StepModel,
} from "@/components/flow/canvas/model/flow-model";
import type { StepGroup } from "@/components/flow/canvas/model/step-groups";
import { longestPathRanks, mainLines, readingOrder, type GraphEdge } from "./main-line";
import { placeGraph } from "./elk-placement";
import { groupBands, isStageGroup } from "./group-bands";
import { droppedNote, rpcCardsLayout } from "./rpc-cards";
import { sideLanes } from "./side-lanes";
import { stageBox, stageId } from "./stage-card";
import { stepBox, stepTitle } from "./step-card";
import { boundsOf, type Box, type Link, type Scene, type ViewOpts } from "./scene";

/**
 * The canvas layout: the view graph of the Flow's main story (Steps, or a stage card for each
 * collapsed group), its main lines, ranks and placement, then the bands around open groups. Failure
 * handling is left out and counted on the cards it leaves; `failure-detours.ts` draws it beside a
 * selected Step. Only this module decides what a box is; ELK only places boxes and routes the lines.
 */

/** Groups that hold at least one Step of the Flow, in their declared order. */
function populated(flow: FlowModel, groups: StepGroup[]): StepGroup[] {
  const known = new Set(flow.steps.map((step) => step.id));
  return groups.filter((group) => group.stepIds.some((id) => known.has(id)));
}

/**
 * Whether the Flow has stages: some group is a run or a choice of two or more Steps. Any other group,
 * a one-Step group included, always draws its Steps inside its labelled box.
 */
export function hasStages(flow: FlowModel, groups: StepGroup[]): boolean {
  return populated(flow, groups).some((group) => isStageGroup(flow, group));
}

/** The stage a Step belongs to, by its first group. */
export function groupOfStep(groups: StepGroup[], stepId: string): StepGroup | undefined {
  return groups.find((group) => group.stepIds.includes(stepId));
}

export async function layoutFlow(whole: FlowModel, opts: ViewOpts): Promise<Scene> {
  if (whole.steps.length === 0) {
    // An entity Flow runs only through RPCs, so they are the whole drawing.
    if (whole.entries.some((entry) => entry.kind === "rpc")) return rpcCardsLayout(whole, opts);
    return { boxes: [], links: [], bands: [], width: 640, height: 200, notes: ["No steps."] };
  }
  const flow = mainStory(whole);
  const failuresOf = (stepId: string): number => failureRoutes(whole, stepId).length;
  const groups = populated(flow, opts.groups ?? []);
  const open = opts.openStages ?? new Set<string>();
  const staged = hasStages(flow, groups);
  const collapsedGroup = (stepId: string): StepGroup | undefined => {
    if (!staged) return undefined;
    const group = groupOfStep(groups, stepId);
    return group && !open.has(group.id) && isStageGroup(flow, group) ? group : undefined;
  };
  const nodeOf = (stepId: string): string => {
    const group = collapsedGroup(stepId);
    return group ? stageId(group.id) : stepId;
  };

  // An RPC that starts a Step is docked to it as an entry chip; only Step-to-Step transitions are lines.
  const stepIds = new Set(flow.steps.map((step) => step.id));
  const transitions = flow.transitions.filter((t) => stepIds.has(t.fromStepId) && stepIds.has(t.toStepId));
  const stepEdges: GraphEdge[] = transitions.map((t) => ({
    id: t.id,
    from: t.fromStepId,
    to: t.toStepId,
    failure: t.kind === "failure_transition",
  }));
  const stepOrder = readingOrder(
    flow.steps.map((step) => step.id),
    stepEdges,
    flow.startStepId,
  );
  const order = [...new Set(stepOrder.map(nodeOf))];
  // Main lines are chosen once, between Steps, and every view draws the ones that cross between its
  // nodes: a line drawn while a stage is closed is still drawn when it opens, and a chip stays a chip.
  const stepMain = mainLines(
    stepOrder,
    stepEdges,
    longestPathRanks(stepOrder, stepEdges),
    (id) => groupOfStep(groups, id)?.id,
  );

  // Transitions between view nodes; one inside a collapsed stage marks that stage as repeating.
  const edges: GraphEdge[] = [];
  const drawn = new Set<string>();
  const conditions = new Map<string, number>();
  const repeating = new Set<string>();
  const readAt = new Map(stepOrder.map((id, i) => [id, i]));
  for (const t of transitions) {
    const from = nodeOf(t.fromStepId);
    const to = nodeOf(t.toStepId);
    if (from === to) {
      // Only a transition back to an earlier Step of the stage, or to itself, repeats it.
      const back = (readAt.get(t.toStepId) ?? 0) <= (readAt.get(t.fromStepId) ?? 0);
      if (from !== t.fromStepId && back) repeating.add(from);
      continue;
    }
    const failure = t.kind === "failure_transition";
    const id = from === t.fromStepId && to === t.toStepId ? t.id : `${from}->${to}:${failure ? "failure" : "control"}`;
    if (!edges.some((e) => e.id === id)) edges.push({ id, from, to, failure });
    if (stepMain.has(t.id)) drawn.add(id);
    conditions.set(id, (conditions.get(id) ?? 0) + Math.max(1, t.mergedGuards.length));
  }
  const jumps = edges.filter((e) => !drawn.has(e.id));
  const arriving = new Map<string, number>();
  for (const e of edges) arriving.set(e.to, (arriving.get(e.to) ?? 0) + 1);
  // Cards are ranked by the lines drawn between them, so every main line points along the reading
  // direction; a chip needs no rank, and ranking by chips set a stage that everything jumps into, such
  // as Prospect's Pipeline, above the stages whose lines lead to it.
  const ranks = longestPathRanks(
    order,
    edges.filter((e) => drawn.has(e.id)),
  );

  const stepById = new Map(flow.steps.map((step) => [step.id, step]));
  const titleOf = (id: string): string => {
    const step = stepById.get(id);
    if (step) return stepTitle(step);
    return groups.find((group) => stageId(group.id) === id)?.label ?? id;
  };
  const boxes: Box[] = order.map((id) => {
    const own = jumps.filter((jump) => jump.from === id);
    const step = stepById.get(id);
    if (step) {
      const docked = own.map((jump) => ({ ...jump, label: titleOf(jump.to) }));
      return stepBox(flow, step, opts, { jumps: docked, arriving: arriving.get(id) ?? 0, failures: failuresOf(id) });
    }
    const group = groups.find((g) => stageId(g.id) === id) as StepGroup;
    const members = stepOrder.flatMap((stepId) => (nodeOf(stepId) === id ? [stepById.get(stepId) as StepModel] : []));
    return stageBox(flow, group, members, opts, {
      jumps: own.map((jump) => ({ to: jump.to, failure: jump.failure, label: titleOf(jump.to) })),
      loops: repeating.has(id),
    });
  });

  // Members of one group sit together inside a rank; reading order breaks ties.
  const position = new Map(order.map((id, i) => [id, i]));
  const groupKey = (id: string): number => {
    const group = stepById.has(id) ? groupOfStep(groups, id) : undefined;
    if (!group) return position.get(id) ?? 0;
    return Math.min(...group.stepIds.map((member) => position.get(nodeOf(member)) ?? Number.MAX_SAFE_INTEGER));
  };
  // A Step that routes on one name labels each line with the case it is taken on, as n8n labels an IF.
  const cases = new Map<string, string>();
  for (const e of edges) {
    const step = stepById.get(e.from);
    const label = step && stepById.has(e.to) && !e.failure ? caseLabel(step, e.to) : null;
    if (label) cases.set(e.id, label);
  }
  for (const [id, label] of readableCases(edges, cases))
    cases.set(id, label.length > 22 ? `${label.slice(0, 21)}…` : label);
  // Every group drawn as Steps is one container, so its members sit together and its band is one region.
  const drawnOpen = (group: StepGroup) => !isStageGroup(flow, group) || open.has(group.id);
  const containers = groups.filter(drawnOpen).map((group) => ({
    id: `group:${group.id}`,
    members: group.stepIds.filter((id) => stepById.has(id) && nodeOf(id) === id),
  }));
  const placement = await placeGraph(
    boxes.map((box) => ({
      id: box.id,
      w: box.w,
      h: box.h,
      rank: ranks.get(box.id) ?? 0,
      order: groupKey(box.id) * 1000 + (position.get(box.id) ?? 0),
    })),
    edges.filter((e) => drawn.has(e.id)).map((e) => ({ ...e, label: cases.get(e.id) })),
    opts.direction,
    containers.filter((c) => c.members.length > 0),
  );
  for (const box of boxes) {
    const at = placement.positions.get(box.id);
    if (at) Object.assign(box, at);
  }
  const routed: Link[] = edges
    .filter((e) => drawn.has(e.id))
    .map((e) => {
      const count = conditions.get(e.id) ?? 1;
      const text = cases.get(e.id);
      const label = text
        ? { text, at: placement.labels.get(e.id), always: true }
        : count > 1
          ? { text: `${count} conditions`, always: false }
          : undefined;
      return {
        id: e.id,
        from: e.from,
        to: e.to,
        family: e.failure ? ("failure" as const) : ("control" as const),
        points: placement.routes.get(e.id) ?? [],
        ...(label ? { label } : {}),
      };
    });
  // A container ELK placed is the band itself; a group it could not contain falls back to hugging bands.
  const contained = groups.flatMap((group, index) => {
    const box = placement.boxes.get(`group:${group.id}`);
    return box
      ? [
          {
            id: group.id,
            groupId: group.id,
            ...box,
            label: group.label,
            hue: index % 2,
            collapsible: isStageGroup(flow, group),
          },
        ]
      : [];
  });
  const bands = [
    ...contained,
    ...groupBands(
      groups,
      boxes,
      (group) => drawnOpen(group) && !placement.boxes.has(`group:${group.id}`),
      (group) => isStageGroup(flow, group),
    ),
  ];
  // A line that loops around its cards or runs long goes side to side instead, where that is clearer.
  const links = sideLanes(boxes, bands, routed, opts.direction);
  const notes = coverageNotes(flow, staged, groups.length, jumps.length, failureHandling(whole).size);
  if (placement.degraded !== undefined) {
    notes.unshift(
      `The layout engine could not place this flow, so its lines are drawn straight and may cross cards: ${placement.degraded}`,
    );
  }
  return { boxes, links, bands, ...boundsOf(boxes, bands, links), notes };
}

/**
 * Case values as words: constants one Step routes on usually share a prefix (`UpdateReply`,
 * `UpdateClose`), which says nothing on a line, so a shared leading word is dropped and the rest is
 * written as `stepName` writes a Step ("Reply", "Close"). Quoted values are already words.
 */
function readableCases(edges: GraphEdge[], cases: Map<string, string>): Map<string, string> {
  const out = new Map<string, string>();
  const bySource = new Map<string, string[]>();
  for (const e of edges) if (cases.has(e.id)) bySource.set(e.from, [...(bySource.get(e.from) ?? []), e.id]);
  for (const ids of bySource.values()) {
    const words = ids.map((id) => stepName(cases.get(id) as string).split(" "));
    const shared = words.length > 1 && words.every((w) => w.length > 1 && w[0] === words[0][0]) ? 1 : 0;
    ids.forEach((id, i) => {
      const rest = words[i].slice(shared).join(" ");
      out.set(id, rest.charAt(0).toUpperCase() + rest.slice(1));
    });
  }
  return out;
}

/** The legend's notes about what the drawing leaves out or writes as text, so nothing reads as missing. */
function coverageNotes(flow: FlowModel, staged: boolean, stages: number, jumps: number, hidden: number): string[] {
  const notes: string[] = [];
  if (hidden > 0)
    notes.push(
      `${hidden} failure-handling step${hidden === 1 ? " is" : "s are"} left out: steps only a failure leads to. ⚠ on a step counts the failures that leave it; select the step to see where they go.`,
    );
  if (staged)
    notes.push(
      `The definition groups its steps into ${stages} stages. ⊕ opens a stage in place and ⊖ closes it; Expanded opens them all and Collapsed closes them all.`,
    );
  if (jumps > 0) {
    notes.push(
      `${jumps} transition${jumps === 1 ? " is" : "s are"} written as ↪ chips on the card they leave. Each step is drawn with one line in, from the latest step that leads to it.`,
    );
  }
  const gates = flow.steps.filter((step) => step.actor === "external").length;
  const answered = flow.entries.some((entry) => entry.opensGates.length > 0);
  if (gates > 0 && !answered && flow.entries.some((entry) => entry.action)) {
    notes.push(
      `The definition does not say which Actions answer the ${gates} waiting step${gates === 1 ? "" : "s"}: their calls publish through helper functions the analyser does not follow.`,
    );
  }
  const unresolved = flow.steps.filter((step) => step.actor === "unknown").length;
  if (unresolved > 0) {
    notes.push(
      `${unresolved} step${unresolved === 1 ? " waits" : "s wait"} on something the analyser could not name; any of them may need a person.`,
    );
  }
  const dropped = droppedNote(flow);
  if (dropped !== null) notes.push(dropped);
  return notes;
}
