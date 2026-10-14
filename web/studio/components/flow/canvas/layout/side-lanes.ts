// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Band, Box, Direction, Link } from "./scene";

/**
 * Side to side: a line that loops around, past both of its cards, or runs long, is redrawn to leave
 * the side of its card, run along a clear lane (a gap between columns, or just beside the drawing)
 * and enter the side of the card it leads to. ELK's route stays unless the side route is shorter and
 * crosses no more lines; each lane track and each run out of a card carries one line. A left-right
 * drawing is transposed, so its lanes run between rows and its lines leave the top or bottom.
 */

type Point = { x: number; y: number };

type Rect = { id: string; x: number; y: number; w: number; h: number };

/** Clearance kept around cards. */
const CLEAR = 14;

/** A route longer than this is worth trying sideways even when it does not loop. */
const LONG = 520;

/** How far a route may pass its cards before it counts as looping around them. */
const SLACK = { along: 30, across: 60 };

/** One crossing costs this much route length when routes are compared. */
const CROSSING_COST = 300;

/** Lane tracks this far apart do not touch. */
const TRACK_GAP = 9;

const flip = (p: Point): Point => ({ x: p.y, y: p.x });

const flipRect = <T extends Rect>(r: T): T => ({ ...r, x: r.y, y: r.x, w: r.h, h: r.w });

function length(points: Point[]): number {
  let total = 0;
  for (let i = 1; i < points.length; i += 1)
    total += Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y);
  return total;
}

function segmentsCross(p: Point, q: Point, r: Point, s: Point): boolean {
  const d = (q.x - p.x) * (s.y - r.y) - (q.y - p.y) * (s.x - r.x);
  if (Math.abs(d) < 1e-9) return false;
  const t = ((r.x - p.x) * (s.y - r.y) - (r.y - p.y) * (s.x - r.x)) / d;
  const u = ((r.x - p.x) * (q.y - p.y) - (r.y - p.y) * (q.x - p.x)) / d;
  return t > 0.001 && t < 0.999 && u > 0.001 && u < 0.999;
}

function crossings(points: Point[], others: Point[][]): number {
  let n = 0;
  for (const other of others)
    for (let i = 1; i < points.length; i += 1)
      for (let j = 1; j < other.length; j += 1)
        if (segmentsCross(points[i - 1], points[i], other[j - 1], other[j])) n += 1;
  return n;
}

/** Whether a route passes beyond both of its cards: over the top, under the bottom or wide to a side. */
function loopsAround(points: Point[], a: Rect, b: Rect): boolean {
  const ys = points.map((p) => p.y);
  const xs = points.map((p) => p.x);
  return (
    Math.min(...ys) < Math.min(a.y, b.y) - SLACK.along ||
    Math.max(...ys) > Math.max(a.y + a.h, b.y + b.h) + SLACK.along ||
    Math.min(...xs) < Math.min(a.x, b.x) - SLACK.across ||
    Math.max(...xs) > Math.max(a.x + a.w, b.x + b.w) + SLACK.across
  );
}

/** Heights on a card's side where a line may leave or enter, middle first. */
function ports(r: Rect): number[] {
  const mid = r.y + r.h / 2;
  return [mid, mid - 12, mid + 12, mid - 24, mid + 24].filter((y) => y > r.y + 8 && y < r.y + r.h - 8);
}

/** Reroutes, in place, the lines of a top-down drawing (or a transposed left-right one). */
function routeTopDown(boxes: Rect[], bands: Rect[], links: Link[]): void {
  const byId = new Map(boxes.map((b) => [b.id, b]));
  const minX = Math.min(...boxes.map((b) => b.x));
  const maxX = Math.max(...boxes.map((b) => b.x + b.w));
  const tracks: { x: number; y0: number; y1: number }[] = [];
  const runs: { y: number; x0: number; x1: number }[] = [];
  const hitsCard = (x0: number, x1: number, y0: number, y1: number, skip: string) =>
    boxes.some(
      (b) => b.id !== skip && x1 > b.x - CLEAR && x0 < b.x + b.w + CLEAR && y1 > b.y - CLEAR && y0 < b.y + b.h + CLEAR,
    );
  const trackFree = (x: number, y0: number, y1: number) =>
    !tracks.some((t) => Math.abs(t.x - x) < TRACK_GAP && t.y1 > y0 - 6 && t.y0 < y1 + 6);
  const runFree = (y: number, x0: number, x1: number) =>
    !runs.some((r) => Math.abs(r.y - y) < 7 && r.x1 > x0 && r.x0 < x1);
  // A lane through another stage's box would read as part of that stage.
  const throughBand = (x: number, y0: number, y1: number, a: Rect, b: Rect) =>
    bands.some((g) => x > g.x && x < g.x + g.w && y1 > g.y && y0 < g.y + g.h && !inside(a, g) && !inside(b, g));
  const lanes: number[] = [];
  for (let k = 0; k < 24; k += 1) lanes.push(minX - 26 - k * 12, maxX + 26 + k * 12);
  for (let x = minX - 10; x < maxX + 10; x += 10) lanes.push(x);

  const candidates = links
    .filter((l) => l.points.length > 1 && byId.has(l.from) && byId.has(l.to))
    .filter((l) => {
      const a = byId.get(l.from) as Rect;
      const b = byId.get(l.to) as Rect;
      return loopsAround(l.points, a, b) || length(l.points) > LONG;
    })
    .sort((p, q) => length(p.points) - length(q.points));

  for (const link of candidates) {
    const a = byId.get(link.from) as Rect;
    const b = byId.get(link.to) as Rect;
    const others = links.filter((l) => l !== link && l.points.length > 1).map((l) => l.points);
    const before = crossings(link.points, others);
    let best: { points: Point[]; cost: number; x: number; sy: number; ty: number; sx: number; tx: number } | null =
      null;
    for (const x of lanes) {
      if (x < 8) continue;
      // The side of each card that faces the lane; a lane through a card's own width has no side.
      const sx = x > a.x + a.w / 2 ? a.x + a.w : a.x;
      const tx = x > b.x + b.w / 2 ? b.x + b.w : b.x;
      if (Math.abs(x - sx) < 10 || Math.abs(x - tx) < 10) continue;
      if ((x > a.x && x < a.x + a.w) || (x > b.x && x < b.x + b.w)) continue;
      for (const sy of ports(a))
        for (const ty of ports(b)) {
          const y0 = Math.min(sy, ty);
          const y1 = Math.max(sy, ty);
          if (hitsCard(Math.min(sx, x), Math.max(sx, x), sy, sy, a.id)) continue;
          if (hitsCard(Math.min(tx, x), Math.max(tx, x), ty, ty, b.id)) continue;
          if (hitsCard(x, x, y0, y1, "")) continue;
          if (!trackFree(x, y0, y1) || !runFree(sy, Math.min(sx, x), Math.max(sx, x))) continue;
          if (!runFree(ty, Math.min(tx, x), Math.max(tx, x))) continue;
          const points = [
            { x: sx, y: sy },
            { x, y: sy },
            { x, y: ty },
            { x: tx, y: ty },
          ];
          const n = crossings(points, others);
          if (n > before) continue;
          const cost = length(points) + n * CROSSING_COST + (throughBand(x, y0, y1, a, b) ? 200 : 0);
          if (best === null || cost < best.cost) best = { points, cost, x, sy, ty, sx, tx };
        }
    }
    if (best === null || best.cost >= length(link.points) + before * CROSSING_COST) continue;
    link.points = best.points;
    tracks.push({ x: best.x, y0: Math.min(best.sy, best.ty), y1: Math.max(best.sy, best.ty) });
    runs.push(
      { y: best.sy, x0: Math.min(best.sx, best.x), x1: Math.max(best.sx, best.x) },
      { y: best.ty, x0: Math.min(best.tx, best.x), x1: Math.max(best.tx, best.x) },
    );
    if (link.label?.at) link.label = { ...link.label, at: { x: best.x, y: (best.sy + best.ty) / 2 } };
  }
}

function inside(r: Rect, g: Rect): boolean {
  return r.x >= g.x && r.y >= g.y && r.x + r.w <= g.x + g.w && r.y + r.h <= g.y + g.h;
}

/** Side routes for the lines that loop around or run long; returns new links, leaving the input untouched. */
export function sideLanes(boxes: Box[], bands: Band[], links: Link[], direction: Direction): Link[] {
  const lr = direction === "lr";
  const t = <T extends Rect>(r: T): Rect => (lr ? flipRect({ id: r.id, x: r.x, y: r.y, w: r.w, h: r.h }) : r);
  const work = links.map((l) => ({
    ...l,
    points: lr ? l.points.map(flip) : l.points,
    ...(l.label?.at ? { label: { ...l.label, at: lr ? flip(l.label.at) : l.label.at } } : {}),
  }));
  routeTopDown(boxes.map(t), bands.map(t), work);
  if (!lr) return work;
  return work.map((l) => ({
    ...l,
    points: l.points.map(flip),
    ...(l.label?.at ? { label: { ...l.label, at: flip(l.label.at) } } : {}),
  }));
}
