// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The graph decisions every view shares, over plain ids so the Step view and the stage view use the
 * same rules: the order a reader meets the nodes, the rank each sits at, and the main line, the one
 * line each node is drawn with. Every other transition is docked to its source as a jump chip.
 */

export interface GraphEdge {
  id: string;
  from: string;
  to: string;
  failure: boolean;
}

/**
 * Breadth-first from the start over ordinary transitions, then over failures, then the nodes nothing
 * reaches, each in its given order. Ties follow the given order, so the result is stable.
 */
export function readingOrder(ids: string[], edges: GraphEdge[], startId: string | undefined): string[] {
  const seed = startId !== undefined && ids.includes(startId) ? startId : ids[0];
  if (seed === undefined) return [];
  const order = [seed];
  const seen = new Set(order);
  const walk = (useFailures: boolean) => {
    for (let i = 0; i < order.length; i += 1) {
      for (const e of edges) {
        if (e.from !== order[i] || e.from === e.to || seen.has(e.to) || (!useFailures && e.failure)) continue;
        seen.add(e.to);
        order.push(e.to);
      }
    }
  };
  walk(false);
  walk(true);
  for (const id of ids) if (!seen.has(id)) order.push(id);
  return order;
}

/**
 * Longest-path ranks over every edge, drawn or docked, with back edges (those closing a cycle in a
 * depth-first walk from the start) removed, so a shared finish or error node sits after everything
 * that leads to it. A node nothing reaches, such as a Step an RPC starts, sits one rank before the
 * first node it leads to.
 */
export function longestPathRanks(order: string[], edges: GraphEdge[]): Map<string, number> {
  const out = new Map<string, string[]>();
  for (const e of edges) if (e.from !== e.to) out.set(e.from, [...(out.get(e.from) ?? []), e.to]);
  const onStack = new Set<string>();
  const seen = new Set<string>();
  const back = new Set<string>();
  const key = (a: string, b: string) => `${a}\u0000${b}`;
  const visit = (v: string): void => {
    seen.add(v);
    onStack.add(v);
    for (const w of out.get(v) ?? []) {
      if (onStack.has(w)) back.add(key(v, w));
      else if (!seen.has(w)) visit(w);
    }
    onStack.delete(v);
  };
  for (const id of order) if (!seen.has(id)) visit(id);

  const forward = edges.filter((e) => e.from !== e.to && !back.has(key(e.from, e.to)));
  const inbound = new Set(forward.map((e) => e.to));
  const rank = new Map(order.map((id) => [id, 0]));
  // Relaxation converges in at most `order.length` rounds on a DAG.
  for (let round = 0; round < order.length; round += 1) {
    let moved = false;
    for (const e of forward) {
      const want = (rank.get(e.from) ?? 0) + 1;
      if (want > (rank.get(e.to) ?? 0)) {
        rank.set(e.to, want);
        moved = true;
      }
    }
    if (!moved) break;
  }
  const start = order[0];
  for (const id of order) {
    if (id === start || inbound.has(id)) continue;
    const next = forward.filter((e) => e.from === id).map((e) => rank.get(e.to) ?? 0);
    if (next.length > 0) rank.set(id, Math.max(0, Math.min(...next) - 1));
  }
  return rank;
}

/**
 * The main line into each node: the transition from the latest node that leads to it, the one ranked
 * closest before it, and from a node in its own stage when one leads to it, so a chain reads straight,
 * a stage's Steps are joined inside its box, and a shortcut, an early exit or a handler called from
 * several stages is a chip rather than the line a node is drawn with. Ordinary transitions come before
 * failures; between sources of the same rank the earlier in reading order wins, and from one source
 * the first in source order. A node with no way in from a lower rank keeps the line from the earliest
 * node in reading order that leads to it. Every node with a way in, other than the start, has exactly
 * one, and each main line climbs in rank, so the main lines form a tree from the start. Self-loops are
 * never main lines: the card marks them. Only ranks, stages and reading order pick between sources, so
 * the choice does not depend on how the edges are listed.
 */
export function mainLines(
  order: string[],
  edges: GraphEdge[],
  ranks: Map<string, number>,
  stageOf: (id: string) => string | undefined,
): Set<string> {
  const position = new Map(order.map((id, i) => [id, i]));
  const at = (id: string) => position.get(id) ?? order.length;
  const rank = (id: string) => ranks.get(id) ?? 0;
  const real = edges.filter((e) => e.from !== e.to && e.to !== order[0]);
  // Stable sorts keep each source's edges in source order.
  const latestFirst = [...real].sort((a, b) => rank(b.from) - rank(a.from) || at(a.from) - at(b.from));
  const readingFirst = [...real].sort((a, b) => at(a.from) - at(b.from));
  const main = new Map<string, string>();
  const take = (list: GraphEdge[], pass: (e: GraphEdge) => boolean) => {
    for (const e of list) if (!main.has(e.to) && pass(e)) main.set(e.to, e.id);
  };
  const sameStage = (e: GraphEdge) => stageOf(e.to) !== undefined && stageOf(e.from) === stageOf(e.to);
  take(latestFirst, (e) => !e.failure && rank(e.from) < rank(e.to) && sameStage(e));
  take(latestFirst, (e) => !e.failure && rank(e.from) < rank(e.to));
  take(latestFirst, (e) => rank(e.from) < rank(e.to));
  take(readingFirst, () => true);
  return new Set(main.values());
}
