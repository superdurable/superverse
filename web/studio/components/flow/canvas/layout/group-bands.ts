// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { mainStory, type FlowModel } from "@/components/flow/canvas/model/flow-model";
import type { StepGroup } from "@/components/flow/canvas/model/step-groups";
import type { Band, Box } from "./scene";

/**
 * The fallback for a group ELK could not place as one container: bands that describe where its
 * members landed. Members that can share one rectangle without enclosing another card do; the rest
 * get their own, so a band is never a false claim about a stranger.
 */

const PAD = 12;
/** Room above the members for the label strip. */
const LABEL_H = 28;

type Rect = { x: number; y: number; w: number; h: number };

function around(members: Box[]): Rect {
  const x = Math.min(...members.map((b) => b.x)) - PAD;
  const y = Math.min(...members.map((b) => b.y)) - LABEL_H;
  return {
    x,
    y,
    w: Math.max(...members.map((b) => b.x + b.w)) + PAD - x,
    h: Math.max(...members.map((b) => b.y + b.h)) + PAD - y,
  };
}

function overlaps(r: Rect, b: Box): boolean {
  return b.x < r.x + r.w && b.x + b.w > r.x && b.y < r.y + r.h && b.y + b.h > r.y;
}

/**
 * One band per cluster of a group's drawn members. Clusters merge greedily, nearest first, while the
 * union encloses no other box.
 */
export function groupBands(
  groups: StepGroup[],
  boxes: Box[],
  drawn: (group: StepGroup) => boolean,
  collapsible: (group: StepGroup) => boolean,
): Band[] {
  const bands: Band[] = [];
  groups.forEach((group, index) => {
    if (!drawn(group)) return;
    const owned = new Set(group.stepIds);
    const members = boxes.filter((b) => b.kind === "step" && owned.has(b.id));
    if (members.length === 0) return;
    const strangers = boxes.filter((b) => !owned.has(b.id));
    const clean = (r: Rect) => !strangers.some((b) => overlaps(r, b));
    const clusters = members.map((b) => [b]);
    const centre = (c: Box[]) => {
      const r = around(c);
      return { x: r.x + r.w / 2, y: r.y + r.h / 2 };
    };
    for (let merged = true; merged;) {
      merged = false;
      const pairs: [number, number, number][] = [];
      for (let a = 0; a < clusters.length; a += 1) {
        for (let b = a + 1; b < clusters.length; b += 1) {
          const p = centre(clusters[a]);
          const q = centre(clusters[b]);
          pairs.push([a, b, Math.hypot(p.x - q.x, p.y - q.y)]);
        }
      }
      for (const [a, b] of pairs.sort((x, y) => x[2] - y[2])) {
        const union = [...clusters[a], ...clusters[b]];
        if (!clean(around(union))) continue;
        clusters.splice(b, 1);
        clusters[a] = union;
        merged = true;
        break;
      }
    }
    for (const cluster of clusters) {
      const r = around(cluster);
      bands.push({
        id: `${group.id}:${Math.round(r.x)}:${Math.round(r.y)}`,
        groupId: group.id,
        ...r,
        label: group.label,
        hue: index % 2,
        collapsible: collapsible(group),
      });
    }
  });
  return bands;
}

/**
 * The group's main-story Steps split into the parts that ordinary transitions inside the group join,
 * each part in the group's own order. Failure handling is drawn apart from groups, so it joins nothing.
 */
function groupParts(story: FlowModel, group: StepGroup): string[][] {
  const known = new Set(story.steps.map((step) => step.id));
  const members = group.stepIds.filter((id) => known.has(id));
  const root = new Map(members.map((id) => [id, id]));
  const find = (id: string): string => {
    let at = id;
    while (root.get(at) !== at) at = root.get(at) as string;
    return at;
  };
  for (const t of story.transitions) {
    if (t.isSelfLoop || !root.has(t.fromStepId) || !root.has(t.toStepId)) continue;
    root.set(find(t.fromStepId), find(t.toStepId));
  }
  const parts = new Map<string, string[]>();
  for (const id of members) parts.set(find(id), [...(parts.get(find(id)) ?? []), id]);
  return [...parts.values()];
}

/**
 * Whether a group is a stage: two or more main-story Steps that form one run (they lead to each other)
 * or one choice (a single Step leads into every part, like an agent's tools). Opening any other group
 * would scatter unrelated pieces, so its Steps always show; Dex's analyzer warns about it
 * (`v2_step_group_shape`) with the same test.
 */
export function isStageGroup(flow: FlowModel, group: StepGroup): boolean {
  const story = mainStory(flow);
  const parts = groupParts(story, group);
  if (parts.flat().length < 2) return false;
  if (parts.length === 1) return true;
  const members = new Set(parts.flat());
  const sourcesOf = parts.map((part) => {
    const inPart = new Set(part);
    return new Set(
      story.transitions.filter((t) => inPart.has(t.toStepId) && !members.has(t.fromStepId)).map((t) => t.fromStepId),
    );
  });
  const [first, ...rest] = sourcesOf;
  return [...(first ?? [])].some((source) => rest.every((sources) => sources.has(source)));
}
