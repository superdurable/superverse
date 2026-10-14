// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useLayoutEffect, useState, type RefObject } from "react";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";

interface ConnectorPath {
  height: number;
  path: string;
  width: number;
}

/**
 * Below Tailwind's lg breakpoint the sidebar stacks under the timeline, so there is nothing to join.
 * The same media query as the layout's `max-lg:`, because a rem breakpoint follows the reader's
 * default font size and a pixel width would not.
 */
const STACKED_LAYOUT = "(width < 64rem)";

/**
 * The dashed elbow from the selected timeline card to the selected-event sidebar. Both ends are
 * measured in the DOM and re-measured whenever either end, the content or the viewport moves.
 */
export function SelectedEventConnector({
  contentRef,
  selectedEvent,
  enabled,
}: {
  /** The positioned container both ends sit in; the path is drawn in its coordinates. */
  contentRef: RefObject<HTMLElement | null>;
  selectedEvent: FlowHistoryEvent | null;
  enabled: boolean;
}) {
  const [connector, setConnector] = useState<ConnectorPath | null>(null);

  useLayoutEffect(() => {
    const content = contentRef.current;
    const source =
      enabled && selectedEvent
        ? content?.querySelector<HTMLElement>(`[data-event-id="${selectedEvent.eventId}"] [data-event-card]`)
        : null;
    const target = content?.querySelector<HTMLElement>("[data-selected-event-target]");
    if (!content || !selectedEvent || !source || !target) {
      setConnector(null);
      return;
    }
    const stacked = window.matchMedia(STACKED_LAYOUT);
    let animationFrame = 0;
    const update = () => {
      animationFrame = 0;
      if (stacked.matches) {
        setConnector(null);
        return;
      }
      const contentRect = content.getBoundingClientRect();
      const sourceRect = source.getBoundingClientRect();
      const targetRect = target.getBoundingClientRect();
      const startX = sourceRect.right - contentRect.left + 4;
      const startY = sourceRect.top + sourceRect.height / 2 - contentRect.top;
      const endX = targetRect.left - contentRect.left - 4;
      const endY = targetRect.top + Math.min(48, targetRect.height / 2) - contentRect.top;
      const bendX = startX + (endX - startX) / 2;
      setConnector({
        height: content.scrollHeight,
        path: `M ${startX} ${startY} H ${bendX} V ${endY} H ${endX}`,
        width: content.scrollWidth,
      });
    };
    const scheduleUpdate = () => {
      if (!animationFrame) animationFrame = window.requestAnimationFrame(update);
    };
    const observer = new ResizeObserver(scheduleUpdate);
    observer.observe(content);
    observer.observe(source);
    observer.observe(target);
    stacked.addEventListener("change", scheduleUpdate);
    window.addEventListener("resize", scheduleUpdate);
    window.addEventListener("scroll", scheduleUpdate, true);
    scheduleUpdate();
    return () => {
      if (animationFrame) window.cancelAnimationFrame(animationFrame);
      observer.disconnect();
      stacked.removeEventListener("change", scheduleUpdate);
      window.removeEventListener("resize", scheduleUpdate);
      window.removeEventListener("scroll", scheduleUpdate, true);
    };
  }, [contentRef, enabled, selectedEvent]);

  if (!connector) return null;
  return (
    <svg
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-4 overflow-visible"
      height={connector.height}
      viewBox={`0 0 ${connector.width} ${connector.height}`}
      width={connector.width}
    >
      <path
        className="stroke-accent"
        d={connector.path}
        fill="none"
        strokeDasharray="5 5"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
      />
    </svg>
  );
}
