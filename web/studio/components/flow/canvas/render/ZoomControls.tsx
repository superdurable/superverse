// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Panel, useReactFlow } from "@xyflow/react";
import type { ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { BUTTON_MAX_ZOOM, BUTTON_MIN_ZOOM, ZOOM_STEP } from "./zoom";
import { cameraMotion } from "@/components/flow/canvas/camera/camera";

const BUTTON = cx(
  "grid h-7 min-w-7.5 place-items-center px-2 text-ink-dim hover:bg-hover hover:text-ink focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none",
);

/**
 * The canvas's zoom, as the prototype draws it: − and + move 15 points between 40% and 140%, and Fit
 * draws the whole Flow again, marked while the view is fitted. It sits bottom right, left of the Step
 * panel when one is open (`insetRightPx`), after the canvas's own `controls`, such as its View button.
 */
export function ZoomControls({
  fitted,
  onFit,
  onZoomed,
  insetRightPx,
  controls,
}: {
  fitted: boolean;
  onFit: () => void;
  /** Called after − or +, which leave the fitted view. */
  onZoomed: () => void;
  insetRightPx?: number | null;
  controls?: ReactNode;
}) {
  const rf = useReactFlow();
  const zoomBy = (step: number) => {
    const next = Math.min(BUTTON_MAX_ZOOM, Math.max(BUTTON_MIN_ZOOM, rf.getZoom() + step));
    void rf.zoomTo(next, cameraMotion(200));
    onZoomed();
  };
  return (
    <Panel
      position="bottom-right"
      className="flex items-center gap-1.5"
      style={insetRightPx ? { right: insetRightPx } : undefined}
    >
      {controls}
      <div
        role="group"
        aria-label="Zoom"
        className="flex overflow-hidden rounded-lg border border-line-soft bg-panel text-xs [&>button+button]:border-l [&>button+button]:border-line-soft"
      >
        <button
          type="button"
          className={BUTTON}
          title="Zoom out"
          aria-label="Zoom out"
          onClick={() => zoomBy(-ZOOM_STEP)}
        >
          −
        </button>
        <button
          type="button"
          className={cx(BUTTON, fitted && "font-semibold text-accent")}
          title="Fit the whole flow"
          aria-pressed={fitted}
          onClick={onFit}
        >
          Fit
        </button>
        <button type="button" className={BUTTON} title="Zoom in" aria-label="Zoom in" onClick={() => zoomBy(ZOOM_STEP)}>
          +
        </button>
      </div>
    </Panel>
  );
}
