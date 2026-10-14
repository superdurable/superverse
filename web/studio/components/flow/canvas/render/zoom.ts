// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { READABLE_ZOOM } from "@/components/flow/canvas/layout/elk-placement";

/**
 * Zoom is purely geometric; level of detail is a separate control. These adjustments keep a
 * zoomed-out graph readable without reflowing it.
 */

export const MIN_ZOOM = 0.14;
export const MAX_ZOOM = 2.2;

/**
 * The canvas opens on the whole Flow when it fits at a readable size (titles of 10px or more, up to
 * 1:1). A Flow too large for that opens at the readable floor on its start rather than shrinking
 * past legibility or centring the start off screen; the reader pans, or uses Fit.
 */
export const OPEN_FLOOR = READABLE_ZOOM;
const OPEN_CEILING = 1;

/** The Fit button shows the whole Flow at whatever size that takes. */
const FIT_FLOOR = 0.2;

/** The zoom buttons move this much, within these bounds; the wheel keeps the wider range above. */
export const ZOOM_STEP = 0.15;
export const BUTTON_MIN_ZOOM = 0.2;
export const BUTTON_MAX_ZOOM = 1.4;

/**
 * Edge opacity multiplier in [1, 1.9] that rises as the graph zooms out, gamma-corrected because
 * lightness is perceptual.
 */
export function edgeContrastBoost(zoom: number): number {
  if (!Number.isFinite(zoom)) return 1;
  const t = Math.min(1, Math.max(0, (1 - zoom) / (1 - 0.2)));
  const eased = t ** (1 / 2.2);
  return 1 + eased * 0.9;
}

/** Stroke and label counter-scaling below 1:1, so edges stay lines rather than hairlines. */
export function chromeCompensation(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom >= 1) return 1;
  return Math.min(2.6, 1 / Math.max(zoom, 0.2));
}

const PADDING = { top: "6%", bottom: "4%", left: "4%", right: "4%" } as const;

/**
 * Fit options. The Step panel overlays the pane, so fitting to the whole pane would run the drawing
 * under it: the right padding reserves the panel's share of the pane width. `whole` is the Fit
 * button's view; otherwise this is the view the canvas opens on.
 */
export function fitOptions(insetRightPx: number | null | undefined, paneWidth: number, whole: boolean) {
  const right: `${number}%` =
    insetRightPx == null || insetRightPx <= 0 || paneWidth <= 0
      ? PADDING.right
      : `${Math.min(58, Math.max(18, (insetRightPx / paneWidth) * 100 + 4))}%`;
  return {
    padding: { ...PADDING, right },
    minZoom: whole ? FIT_FLOOR : OPEN_FLOOR,
    maxZoom: OPEN_CEILING,
  } as const;
}
