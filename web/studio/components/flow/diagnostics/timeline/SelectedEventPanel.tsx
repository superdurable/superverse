// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef } from "react";
import { displayEventNumber } from "./timeline-links";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { EventDetails, eventTitle } from "@/components/flow/diagnostics/event/EventDetails";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { cx } from "@/components/ui/cx";

function canScroll(element: HTMLElement, deltaY: number): boolean {
  if (deltaY < 0) return element.scrollTop > 0;
  if (deltaY > 0) return element.scrollTop + element.clientHeight < element.scrollHeight;
  return false;
}

function containSidebarWheel(event: WheelEvent) {
  const sidebar = event.currentTarget;
  if (!(sidebar instanceof HTMLElement) || sidebar.scrollHeight <= sidebar.clientHeight) return;
  // Every pre in the details is a bounded well that scrolls on its own.
  const nested = event.target instanceof Element ? event.target.closest<HTMLElement>("pre") : null;
  if (nested && nested !== sidebar && canScroll(nested, event.deltaY)) return;
  if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
  event.preventDefault();
  event.stopPropagation();
  const multiplier =
    event.deltaMode === WheelEvent.DOM_DELTA_LINE
      ? 16
      : event.deltaMode === WheelEvent.DOM_DELTA_PAGE
        ? sidebar.clientHeight
        : 1;
  sidebar.scrollTop += event.deltaY * multiplier;
}

export function SelectedEventPanel({
  selectedEvent,
  history,
  parentFlowId,
}: {
  selectedEvent: FlowHistoryEvent | null;
  history: FlowHistoryEvent[];
  parentFlowId: string;
}) {
  const sidebar = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = sidebar.current;
    if (!element) return;
    element.addEventListener("wheel", containSidebarWheel, { passive: false });
    return () => element.removeEventListener("wheel", containSidebarWheel);
  }, []);

  return (
    <div
      className="sticky top-4.5 max-h-[calc(100cqh-36px)] [scrollbar-gutter:stable] overflow-x-hidden overflow-y-auto overscroll-y-contain rounded-xl border border-line-faint bg-raised text-ink-dim shadow-(--shadow-lift) max-lg:static max-lg:max-h-none max-lg:[scrollbar-gutter:auto] max-lg:overflow-visible max-lg:overscroll-auto"
      ref={sidebar}
    >
      <SelectedEventSummary event={selectedEvent} history={history} parentFlowId={parentFlowId} variant="sidebar" />
    </div>
  );
}

/** How the summary sits in the sidebar beside the graph and Timeline, or in an Overview card. */
const SUMMARY_LAYOUT = {
  sidebar: {
    Heading: "h3",
    headerClass: cx(
      "sticky top-0 z-5 border-b border-line-faint bg-raised px-3.75 pt-3.75 pb-2.5 shadow-[0_8px_18px_rgba(45,79,42,0.06)] backdrop-blur-md",
    ),
    headingClass: cx("mb-2.5 text-xs font-semibold text-ink-dim"),
    Body: "section",
    bodyClass: cx("p-3.75"),
  },
  overview: {
    Heading: "h2",
    headerClass: undefined,
    headingClass: cx("mb-2.5 text-sm font-semibold tracking-normal text-ink"),
    Body: "div",
    bodyClass: cx("mt-3.5"),
  },
} as const;

/**
 * The selected event's title, position in the history and details, or a prompt to pick one. Only
 * a selected event's header carries `data-selected-event-target`, which the Timeline connector
 * draws to.
 */
export function SelectedEventSummary({
  event,
  history,
  parentFlowId,
  variant,
}: {
  event: FlowHistoryEvent | null;
  history: FlowHistoryEvent[];
  parentFlowId: string;
  variant: keyof typeof SUMMARY_LAYOUT;
}) {
  const { Heading, headerClass, headingClass, Body, bodyClass } = SUMMARY_LAYOUT[variant];
  return (
    <>
      <header className={headerClass} data-selected-event-target={event ? true : undefined}>
        <Eyebrow className="mb-1.5">Selected event</Eyebrow>
        <Heading className={headingClass}>{event ? eventTitle(event) : "None"}</Heading>
        {event && (
          <div className="flex flex-col gap-0.75 text-[11px] text-ink-faint">
            <span>Event {displayEventNumber(history, event)}</span>
            <span>{event.eventTime || "No timestamp"}</span>
          </div>
        )}
      </header>
      <Body className={bodyClass}>
        {event ? (
          <EventDetails event={event} history={history} parentFlowId={parentFlowId} />
        ) : (
          <p className="text-xs text-ink-faint">Select an event from Timeline or Execution graph.</p>
        )}
      </Body>
    </>
  );
}
