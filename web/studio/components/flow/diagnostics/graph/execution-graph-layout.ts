// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import dagre from "dagre";
import type { Edge, Node } from "@xyflow/react";
import type { StepGraphNode } from "./step-graph";

const horizontalGap = 36;
const verticalGap = 64;
const horizontalMargin = 32;
const verticalMargin = 32;
export const minimumGraphZoom = 1 / 3;

/** An execution graph node with its size declared up front, so the layout never measures. */
export type SizedGraphNode = Node<{ model: StepGraphNode }> & { width: number; height: number };

type Rect = { x: number; y: number; width: number; height: number };

/** Longest-path ranks over the edges, with every node a root's rank 0 until an edge says otherwise. */
function logicalRanks(nodes: SizedGraphNode[], edges: Edge[]): Map<string, number> {
  const nodeIDs = new Set(nodes.map((node) => node.id));
  const incomingCounts = new Map(nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(nodes.map((node) => [node.id, [] as string[]]));
  for (const edge of edges) {
    if (!nodeIDs.has(edge.source) || !nodeIDs.has(edge.target)) continue;
    incomingCounts.set(edge.target, (incomingCounts.get(edge.target) ?? 0) + 1);
    outgoing.get(edge.source)?.push(edge.target);
  }
  const ranks = new Map(nodes.map((node) => [node.id, 0]));
  const ready = nodes.filter((node) => incomingCounts.get(node.id) === 0).map((node) => node.id);
  for (let index = 0; index < ready.length; index += 1) {
    const source = ready[index];
    const nextRank = (ranks.get(source) ?? 0) + 1;
    for (const target of outgoing.get(source) ?? []) {
      ranks.set(target, Math.max(ranks.get(target) ?? 0, nextRank));
      const incomingCount = (incomingCounts.get(target) ?? 0) - 1;
      incomingCounts.set(target, incomingCount);
      if (incomingCount === 0) ready.push(target);
    }
  }
  return ranks;
}

export function layoutGraph<N extends SizedGraphNode>(nodes: N[], edges: Edge[]): N[] {
  const topologyNodes = nodes.filter((node) => node.data.model.kind !== "subflow");
  const topologyIDs = new Set(topologyNodes.map((node) => node.id));
  const placed = layoutTopology(
    topologyNodes,
    edges.filter((edge) => topologyIDs.has(edge.source) && topologyIDs.has(edge.target)),
  );
  // SubFlow conditions are not next Steps; keep them off the topology ranks.
  placeSubFlowNodes(
    nodes.filter((node) => node.data.model.kind === "subflow"),
    placed,
  );
  return nodes.map((node) => {
    const at = placed.get(node.id);
    return at === undefined ? node : { ...node, position: { x: at.x, y: at.y } };
  });
}

/** dagre orders each logical rank; the ranks are laid out as centred rows. */
function layoutTopology(nodes: SizedGraphNode[], edges: Edge[]): Map<string, Rect> {
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({ rankdir: "TB", ranksep: verticalGap, nodesep: horizontalGap });
  nodes.forEach((node) => graph.setNode(node.id, { width: node.width, height: node.height }));
  edges.forEach((edge) => graph.setEdge(edge.source, edge.target));
  dagre.layout(graph);

  const nodeRanks = logicalRanks(nodes, edges);
  const rankMembers = new Map<number, { node: SizedGraphNode; order: number }[]>();
  for (const node of nodes) {
    const rank = nodeRanks.get(node.id) ?? 0;
    rankMembers.set(rank, [...(rankMembers.get(rank) ?? []), { node, order: graph.node(node.id).x }]);
  }

  const rows = [...rankMembers.entries()]
    .sort(([left], [right]) => left - right)
    .map(([, members]) => members.sort((left, right) => left.order - right.order).map((member) => member.node));
  const rowWidth = (row: SizedGraphNode[]) =>
    row.reduce((width, node) => width + node.width, 0) + horizontalGap * Math.max(0, row.length - 1);
  const contentWidth = Math.max(...rows.map(rowWidth));
  const placed = new Map<string, Rect>();
  let rowTop = verticalMargin;
  for (const row of rows) {
    const rowHeight = Math.max(...row.map((node) => node.height));
    let nodeLeft = horizontalMargin + (contentWidth - rowWidth(row)) / 2;
    for (const node of row) {
      placed.set(node.id, {
        x: nodeLeft,
        y: rowTop + (rowHeight - node.height) / 2,
        width: node.width,
        height: node.height,
      });
      nodeLeft += node.width + horizontalGap;
    }
    rowTop += rowHeight + verticalGap;
  }
  return placed;
}

/** Stacks each Step's SubFlows to its right, centred on it. */
function placeSubFlowNodes(subFlowNodes: SizedGraphNode[], placed: Map<string, Rect>): void {
  for (const [parentID, parent] of [...placed]) {
    const children = subFlowNodes.filter((node) => node.data.model.parentStepId === parentID);
    if (children.length === 0) continue;
    const stackHeight =
      children.reduce((height, child) => height + child.height, 0) + horizontalGap * (children.length - 1);
    let nodeTop = parent.y + Math.max(0, (parent.height - stackHeight) / 2);
    const nodeLeft = parent.x + parent.width + horizontalGap;
    for (const child of children) {
      placed.set(child.id, { x: nodeLeft, y: nodeTop, width: child.width, height: child.height });
      nodeTop += child.height + horizontalGap;
    }
  }
}

export function defaultGraphZoom(contentWidth: number, viewportWidth: number): number {
  if (contentWidth <= 0 || viewportWidth <= 0) return 1;
  const availableWidth = Math.max(0, viewportWidth - horizontalMargin * 2);
  return Math.max(minimumGraphZoom, Math.min(1, availableWidth / contentWidth));
}

export function graphBounds(nodes: SizedGraphNode[]): { height: number; width: number } {
  return {
    height: Math.max(0, ...nodes.map((node) => node.position.y + node.height)) + verticalMargin,
    width: Math.max(0, ...nodes.map((node) => node.position.x + node.width)) + horizontalMargin,
  };
}
