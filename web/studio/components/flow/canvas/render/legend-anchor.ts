// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Band, Box } from "@/components/flow/canvas/layout/scene";

/** Reserved legend box; the card sits at its bottom so it can collapse without moving that edge. */
export const LEGEND_W = 292;
export const LEGEND_BOX_H = 320;

/** How far above the lowest card still counts as level with the bottom. */
const BOTTOM_BAND = 140;

/** Gap between the end of the drawing and the legend. */
const GAP = 40;

/**
 * Where the legend sits: bottom-aligned with the lowest card and right of whatever is level with
 * the bottom, so it reads as part of the drawing. When its reserved box reaches back into the
 * content, it moves right until clear; the bottom never moves.
 */
export function legendAnchor(boxes: Box[], bands: Band[]): { x: number; y: number } | undefined {
  if (boxes.length === 0) return undefined;
  const maxY = Math.max(...boxes.map((b) => b.y + b.h));
  const all = [...boxes, ...bands];
  const nearBottom = all.filter((b) => b.y + b.h > maxY - BOTTOM_BAND);
  const source = nearBottom.length > 0 ? nearBottom : boxes;
  const y = maxY - LEGEND_BOX_H;
  let x = Math.max(...source.map((b) => b.x + b.w)) + GAP;

  const hits = (): (Box | Band)[] =>
    all.filter((b) => x < b.x + b.w && b.x < x + LEGEND_W && y < b.y + b.h && b.y < y + LEGEND_BOX_H);
  // Each pass clears at least one obstruction, so the item count bounds the loop.
  for (let pass = 0; pass < all.length; pass++) {
    const blocking = hits();
    if (blocking.length === 0) break;
    x = Math.max(...blocking.map((b) => b.x + b.w)) + GAP;
  }
  return { x, y };
}
