// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";

/** One drawn link between two Timeline cards, in the Timeline's own coordinates. */
interface StepLinkPath {
  id: string;
  kind: "lineage" | "condition-wait";
  stepExecutionId: string;
  label: string;
  lane: number;
  path: string;
  fromEventId: number;
  toEventId: number;
  duration: string;
  durationX: number;
  durationY: number;
  durationWidth: number;
}

export interface StepLinkLayout {
  width: number;
  height: number;
  paths: StepLinkPath[];
}

/** A lineage link joins a Step to the Step that started it; a condition link joins a WaitFor to its Execute. */
const LINK_STROKE = {
  lineage: cx("stroke-pass-rule"),
  "condition-wait": cx("stroke-accent-rule"),
} satisfies Record<StepLinkPath["kind"], string>;

const ARROW_FILL = {
  lineage: cx("fill-pass-rule"),
  "condition-wait": cx("fill-accent-rule"),
} satisfies Record<StepLinkPath["kind"], string>;

const ARROW_ID = {
  lineage: "timeline-lineage-arrow",
  "condition-wait": "timeline-step-arrow",
} satisfies Record<StepLinkPath["kind"], string>;

/**
 * The links of the selected event drawn over the Timeline's link rail. Each link is a button: hovering
 * or focusing lights it, and clicking or Enter pins it until it is clicked again.
 */
export function TimelineStepLinks({
  layout,
  activeStepLinkID,
  pinnedStepLinkID,
  onInteract,
  onTogglePin,
}: {
  layout: StepLinkLayout;
  activeStepLinkID: string | null;
  pinnedStepLinkID: string | null;
  onInteract: (stepLinkID: string | null) => void;
  onTogglePin: (stepLinkID: string) => void;
}) {
  return (
    <svg
      aria-label="Step execution lineage and condition links"
      className="pointer-events-none absolute top-0 left-0 z-2 overflow-visible"
      height={layout.height}
      role="group"
      viewBox={`0 0 ${layout.width} ${layout.height}`}
      width={layout.width}
    >
      <defs>
        {(["condition-wait", "lineage"] as const).map((kind) => (
          <marker id={ARROW_ID[kind]} key={kind} markerHeight="7" markerWidth="7" orient="auto" refX="5" refY="3.5">
            <path className={ARROW_FILL[kind]} d="M 0 0 L 6 3.5 L 0 7 z" />
          </marker>
        ))}
      </defs>
      {layout.paths.map((link) => {
        const lit = activeStepLinkID === link.id;
        const accessibleLabel = link.duration ? `${link.label}, ${link.duration}` : link.label;
        return (
          <g
            aria-label={accessibleLabel}
            aria-pressed={pinnedStepLinkID === link.id}
            className="cursor-pointer outline-none"
            data-step-execution-id={link.stepExecutionId}
            data-timeline-link-kind={link.kind}
            data-timeline-lane={link.lane}
            key={link.id}
            onBlur={() => onInteract(null)}
            onClick={() => onTogglePin(link.id)}
            onFocus={() => onInteract(link.id)}
            onKeyDown={(event) => {
              if (event.key !== "Enter" && event.key !== " ") return;
              event.preventDefault();
              onTogglePin(link.id);
            }}
            onMouseEnter={() => onInteract(link.id)}
            onMouseLeave={(event) => {
              if (document.activeElement !== event.currentTarget) onInteract(null);
            }}
            role="button"
            tabIndex={0}
          >
            <title>{accessibleLabel}</title>
            <path
              aria-hidden="true"
              d={link.path}
              fill="none"
              pointerEvents="stroke"
              stroke="transparent"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={16}
            />
            <path
              aria-hidden="true"
              className={cx(
                "fill-none transition-[filter,stroke-width] duration-140",
                LINK_STROKE[link.kind],
                lit
                  ? "animate-dash-flow stroke-[2.5] drop-shadow-[0_0_2px_rgba(54,98,90,0.3)] [stroke-dasharray:8_5] motion-reduce:animate-none"
                  : "stroke-2",
              )}
              d={link.path}
              markerEnd={`url(#${ARROW_ID[link.kind]})`}
              pointerEvents="none"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {link.duration && (
              <>
                <rect
                  aria-hidden="true"
                  className={cx(
                    "transition-[fill,opacity,stroke] duration-140",
                    lit ? "fill-pass-subtle stroke-pass-rule opacity-100" : "fill-panel stroke-transparent opacity-0",
                  )}
                  height="20"
                  pointerEvents="painted"
                  rx="7"
                  width={link.durationWidth}
                  x={link.durationX - 5}
                  y={link.durationY - 10}
                />
                <text
                  aria-hidden="true"
                  className={cx(
                    "text-[10px] transition-[fill,font-weight] duration-140",
                    lit ? "fill-pass stroke-pass-subtle font-extrabold" : "fill-accent stroke-panel font-bold",
                  )}
                  dominantBaseline="middle"
                  paintOrder="stroke"
                  pointerEvents="painted"
                  strokeLinejoin="round"
                  strokeWidth={5}
                  x={link.durationX}
                  y={link.durationY}
                >
                  {link.duration}
                </text>
              </>
            )}
          </g>
        );
      })}
    </svg>
  );
}
