// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { Banner } from "@/components/ui/Banner";
import { cx } from "@/components/ui/cx";
import type { RecentTriggerEvent__Output } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";
import { formatTimeAgo, formatTimeOfDay } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import { fateDelivered, fateWords, type EventEnvironment } from "./trigger-event-fates";
import { useRecentTriggerEvents } from "./use-recent-trigger-events";

/** The time, read again every 15 seconds so "N min ago" stays true while the panel is open. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 15_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

const SCOPES = {
  Preview: "PROJECT_SCOPE_PREVIEW",
  Live: "PROJECT_SCOPE_LIVE",
} as const satisfies Record<EventEnvironment, string>;

/**
 * The Slack events recently received for one environment, newest first: when, what kind, which
 * channel and what became of it. Never the message text. The relay keeps them in memory, so the
 * panel says since when it has been recording, and an empty list never claims Slack sent nothing.
 */
export function RecentEvents({
  projectId,
  environment,
  open = true,
}: {
  projectId: string;
  environment: EventEnvironment;
  open?: boolean;
}) {
  const nowMs = useNow();
  const { events, bufferStartedAt, instanceSwitched, error, loaded } = useRecentTriggerEvents(
    projectId,
    SCOPES[environment],
    open,
  );
  const since = bufferStartedAt ? formatTimeOfDay(bufferStartedAt) : "";
  return (
    <section aria-label={`Recent events received by ${environment}`} data-testid="recent-events">
      <h3 className="text-[13px] font-semibold text-ink">Recent events received</h3>
      {error && (
        <Banner role="alert" className="mt-2">
          {error}
        </Banner>
      )}
      {since && (
        <p className="mt-2 text-[12.5px] text-ink-dim" data-testid="recent-events-instance">
          Received by this server instance since {since}.
        </p>
      )}
      {instanceSwitched && (
        <p className="mt-2 text-[12.5px] text-busy" data-testid="recent-events-switched">
          This view switched to another server instance, so its events differ; none were lost.
        </p>
      )}
      {loaded && events.length === 0 && !error && (
        <p className="mt-2 text-[12.5px] leading-snug text-ink-dim" data-testid="recent-events-empty">
          No events received{since ? ` since the platform began recording ${since}` : ""}. The list is kept in memory
          and starts empty after a restart, so this does not mean Slack sent nothing.
        </p>
      )}
      {events.length > 0 && (
        <ul className="mt-2 divide-y divide-line-soft" data-testid="recent-events-list">
          {events.map((event, index) => (
            <EventRow
              key={`${event.eventId}/${event.connectionName}/${index}`}
              event={event}
              environment={environment}
              nowMs={nowMs}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function EventRow({
  event,
  environment,
  nowMs,
}: {
  event: RecentTriggerEvent__Output;
  environment: EventEnvironment;
  nowMs: number;
}) {
  const kind = event.eventSubtype ? `${event.eventType} · ${event.eventSubtype}` : event.eventType;
  return (
    <li className="py-1.5 text-[12.5px]" data-testid="recent-event">
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-medium text-ink">{kind}</span>
        <time className="shrink-0 text-ink-faint" dateTime={timestampToISO(event.receivedAt) ?? undefined}>
          {formatTimeAgo(timestampToISO(event.receivedAt), nowMs)}
        </time>
      </div>
      <p className="text-ink-dim">
        {event.channelId ? `Channel ${event.channelId}` : "No channel"}
        {event.connectionName ? ` · ${event.connectionName}` : ""}
      </p>
      <p className={cx(fateDelivered(event.fate) ? "text-ink-dim" : "text-busy")}>
        {fateWords(event.fate, environment, event.httpStatus)}
      </p>
    </li>
  );
}
