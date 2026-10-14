// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { formatElapsedDuration } from "@/lib/shared/format";
import { buildSelectedTimelineLinks, displayEventNumbers, newestTimelineEvents } from "./timeline-links";
import { TimelineEventRow } from "./TimelineEventRow";
import { TimelineStepLinks, type StepLinkLayout } from "./TimelineStepLinks";

export function Timeline({
  flowId,
  events,
  selectedEvent,
  onSelectEvent,
}: {
  flowId: string;
  events: FlowHistoryEvent[];
  selectedEvent: FlowHistoryEvent | null;
  onSelectEvent: (event: FlowHistoryEvent) => void;
}) {
  const timelineRef = useRef<HTMLDivElement>(null);
  const eventCards = useRef(new Map<number, HTMLDivElement>());
  const orderedEvents = useMemo(() => newestTimelineEvents(events), [events]);
  const eventNumbers = useMemo(() => displayEventNumbers(events), [events]);
  const stepLinks = useMemo(
    () => buildSelectedTimelineLinks(events, selectedEvent?.eventId),
    [events, selectedEvent?.eventId],
  );
  const linkLaneCount = Math.max(1, ...stepLinks.map((link) => link.lane + 1));
  const timelineStyle = {
    "--timeline-link-rail-width": `${88 + (linkLaneCount - 1) * 48}px`,
  } as CSSProperties;
  const [stepLinkLayout, setStepLinkLayout] = useState<StepLinkLayout>({ width: 0, height: 0, paths: [] });
  const [interactionStepLinkID, setInteractionStepLinkID] = useState<string | null>(null);
  const [pinnedStepLinkID, setPinnedStepLinkID] = useState<string | null>(null);
  const updateStepLinks = useCallback(() => {
    const timeline = timelineRef.current;
    if (!timeline) return;
    const timelineRect = timeline.getBoundingClientRect();
    const paths = stepLinks.flatMap((link) => {
      const fromCard = eventCards.current.get(link.fromEventId);
      const toCard = eventCards.current.get(link.toEventId);
      if (!fromCard || !toCard) return [];
      const fromRect = fromCard.getBoundingClientRect();
      const toRect = toCard.getBoundingClientRect();
      const flowsUpward = fromRect.top > toRect.top;
      const fromX = fromRect.left - timelineRect.left - 1;
      const toX = toRect.left - timelineRect.left + 1;
      const fromY = fromRect.top + fromRect.height * (flowsUpward ? 0.34 : 0.66) - timelineRect.top;
      const toY = toRect.top + toRect.height * (flowsUpward ? 0.66 : 0.34) - timelineRect.top;
      const linkX = Math.min(fromX, toX) - 22 - link.lane * 16;
      const duration = formatElapsedDuration(link.elapsedDurationMs);
      const durationWidth = Math.max(28, duration.length * 7 + 10);
      return [
        {
          id: `${link.kind}-${link.stepExecutionId}-${link.fromEventId}-${link.toEventId}`,
          kind: link.kind,
          stepExecutionId: link.stepExecutionId,
          label: link.label,
          lane: link.lane,
          path: `M ${fromX} ${fromY} H ${linkX} V ${toY} H ${toX}`,
          fromEventId: link.fromEventId,
          toEventId: link.toEventId,
          duration,
          durationX: linkX - durationWidth,
          durationY: (fromY + toY) / 2,
          durationWidth,
        },
      ];
    });
    setStepLinkLayout({ width: timelineRect.width, height: timelineRect.height, paths });
  }, [stepLinks]);

  useLayoutEffect(() => {
    const timeline = timelineRef.current;
    if (!timeline) return;
    const frame = window.requestAnimationFrame(updateStepLinks);
    const observer = new ResizeObserver(updateStepLinks);
    observer.observe(timeline);
    eventCards.current.forEach((card) => observer.observe(card));
    window.addEventListener("resize", updateStepLinks);
    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", updateStepLinks);
    };
  }, [orderedEvents, updateStepLinks]);

  if (!events.length)
    return (
      <div className="grid min-h-60 place-items-center rounded-lg border border-line-faint bg-panel p-10 text-center">
        <h3 className="m-0 text-[13px] font-semibold text-ink-dim">No semantic events loaded</h3>
      </div>
    );
  const startMs = events.reduce((earliest, event) => {
    const eventMs = event.eventTime ? Date.parse(event.eventTime) : 0;
    if (!eventMs) return earliest;
    return earliest ? Math.min(earliest, eventMs) : eventMs;
  }, 0);
  const selectedStepLink = stepLinkLayout.paths.find(
    (link) => link.fromEventId === selectedEvent?.eventId || link.toEventId === selectedEvent?.eventId,
  );
  const pinnedStepLink = stepLinkLayout.paths.find((link) => link.id === pinnedStepLinkID);
  const activeStepLinkID = interactionStepLinkID ?? pinnedStepLink?.id ?? selectedStepLink?.id ?? null;
  const activeStepLink = stepLinkLayout.paths.find((link) => link.id === activeStepLinkID);
  return (
    <div className="rounded-lg border border-line-faint bg-panel p-4.5">
      <h2 className="mb-3.25 text-sm font-semibold tracking-normal text-ink">{events.length} events</h2>
      <div className="relative" ref={timelineRef} style={timelineStyle}>
        {stepLinkLayout.paths.length > 0 && (
          <TimelineStepLinks
            layout={stepLinkLayout}
            activeStepLinkID={activeStepLinkID}
            pinnedStepLinkID={pinnedStepLinkID}
            onInteract={setInteractionStepLinkID}
            onTogglePin={(stepLinkID) => setPinnedStepLinkID((current) => (current === stepLinkID ? null : stepLinkID))}
          />
        )}
        {orderedEvents.map((event) => {
          const eventMs = event.eventTime ? Date.parse(event.eventTime) : 0;
          const selected = selectedEvent?.eventId === event.eventId;
          const linked = activeStepLink?.fromEventId === event.eventId || activeStepLink?.toEventId === event.eventId;
          const counterpart = activeStepLink?.id === selectedStepLink?.id && linked && !selected;
          return (
            <TimelineEventRow
              key={event.eventId}
              event={event}
              flowId={flowId}
              eventNumber={eventNumbers.get(event.eventId)}
              relativeSeconds={startMs && eventMs ? Math.max(0, Math.round((eventMs - startMs) / 1000)) : null}
              state={selected ? "selected" : counterpart ? "counterpart" : linked ? "linked" : "plain"}
              cardRef={(node) => {
                if (node) eventCards.current.set(event.eventId, node);
                else eventCards.current.delete(event.eventId);
              }}
              onSelect={() => {
                setInteractionStepLinkID(null);
                setPinnedStepLinkID(null);
                onSelectEvent(event);
              }}
            />
          );
        })}
      </div>
    </div>
  );
}
