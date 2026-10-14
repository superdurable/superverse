// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { BaseEdge, type Edge, type EdgeProps } from "@xyflow/react";
import type { CSSProperties, JSX } from "react";
import { polylineLength } from "@/components/flow/canvas/camera/camera";
import type { LinkFamily } from "@/components/flow/canvas/layout/scene";

/** An edge's family and state as data attributes; false flags are omitted. */
export function edgeAttributes(family: LinkFamily, flags: Record<`data-${string}`, boolean>): Edge["domAttributes"] {
  const attributes: Record<string, string> = { "data-canvas-edge": family };
  for (const [name, on] of Object.entries(flags)) if (on) attributes[name] = "true";
  return attributes;
}

type Point = { x: number; y: number };

/**
 * A line the layout routed: React Flow draws it as given instead of inventing a path between handles.
 * `labelAt` is where the layout reserved room for its label.
 */
export type RoutedEdgeData = { points: Point[]; labelAt?: Point; travel?: number };

/** Orthogonal bends rounded off at up to `radius`, never more than half of either segment. */
function roundedPath(points: Point[], radius: number): string {
  const first = points[0];
  let d = `M${first.x},${first.y}`;
  for (let i = 1; i < points.length - 1; i += 1) {
    const a = points[i - 1];
    const p = points[i];
    const n = points[i + 1];
    const into = Math.hypot(p.x - a.x, p.y - a.y);
    const out = Math.hypot(n.x - p.x, n.y - p.y);
    const r = Math.min(radius, into / 2, out / 2);
    if (r < 0.5) {
      d += ` L${p.x},${p.y}`;
      continue;
    }
    const before = { x: p.x - ((p.x - a.x) / into) * r, y: p.y - ((p.y - a.y) / into) * r };
    const after = { x: p.x + ((n.x - p.x) / out) * r, y: p.y + ((n.y - p.y) / out) * r };
    d += ` L${before.x.toFixed(1)},${before.y.toFixed(1)} Q${p.x},${p.y} ${after.x.toFixed(1)},${after.y.toFixed(1)}`;
  }
  const last = points[points.length - 1];
  return `${d} L${last.x},${last.y}`;
}

/** The middle of the longest segment: where a label sits clear of the bends. */
function labelPoint(points: Point[]): Point {
  let best = { x: points[0].x, y: points[0].y, length: -1 };
  for (let i = 1; i < points.length; i += 1) {
    const a = points[i - 1];
    const b = points[i];
    const length = Math.hypot(b.x - a.x, b.y - a.y);
    if (length > best.length) best = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, length };
  }
  return best;
}

export function RoutedEdge({
  id,
  data,
  style,
  markerEnd,
  label,
  labelStyle,
  labelBgStyle,
}: EdgeProps<Edge<RoutedEdgeData>>): JSX.Element | null {
  const points = data?.points ?? [];
  if (points.length < 2) return null;
  const at = data?.labelAt ?? labelPoint(points);
  const path = roundedPath(points, 10);
  // Its length lets a new line draw in from the card it leaves.
  const length = { "--len": `${polylineLength(points)}px` } as CSSProperties;
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        style={{ ...style, ...length }}
        data-testid="canvas-line"
        markerEnd={markerEnd}
        label={label}
        labelX={at.x}
        labelY={at.y}
        labelStyle={labelStyle}
        labelBgStyle={labelBgStyle}
        labelBgPadding={[4, 2]}
        labelBgBorderRadius={4}
        interactionWidth={14}
      />
      {data?.travel ? (
        // The run moving on along this line: a head with a fading trail, like the running Dex mark.
        <g key={data.travel} data-canvas-travel="true" aria-hidden="true">
          <path d={path} data-travel-part="trail" style={length} />
          <path d={path} data-travel-part="head" style={length} />
        </g>
      ) : null}
    </>
  );
}

/**
 * Arrowheads paint with `context-stroke`, so a head takes its line's colour from CSS, and use
 * `userSpaceOnUse`, so its size does not scale with the stroke width. A failure line differs from a
 * transition by its dash and hue, not by its head.
 */
export const EDGE_MARKER: Record<LinkFamily, string> = {
  control: "p-arrow-solid",
  failure: "p-arrow-solid",
};

/** Mounted once per canvas, because SVG marker ids are document-scoped. */
export function ArrowDefs(): JSX.Element {
  return (
    <svg aria-hidden="true" focusable="false" width="0" height="0" style={{ position: "absolute" }}>
      <defs>
        <marker
          id="p-arrow-solid"
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          markerWidth="10"
          markerHeight="10"
          markerUnits="userSpaceOnUse"
          orient="auto-start-reverse"
        >
          <path d="M 1 1.5 L 9 5 L 1 8.5 z" fill="context-stroke" />
        </marker>
      </defs>
    </svg>
  );
}
