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
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import type { RecentTriggerEvent__Output } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";
import { formatTimeAgo, formatTimeOfDay } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import { eventKind, fateDelivered, fateWords, type EventEnvironment } from "./trigger-event-fates";
import { useRecentEvents } from "./RecentEventsProvider";

/** The time, read again every 15 seconds so "N min ago" stays true while the list is open. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 15_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

/**
 * The recent events, folded at the foot of the run list: when, what kind, where and what became of
 * each, never the message text, with its raw fields one fold further. The relay keeps them in each
 * server's memory, so the list says so and an empty list never claims nothing was sent.
 */
export function RecentEventsLog() {
  const read = useRecentEvents();
  const nowMs = useNow();
  if (!read?.shown) return null;
  const { environment, events, bufferStartedAt, instanceSwitched, error, loaded } = read;
  const since = bufferStartedAt ? formatTimeOfDay(bufferStartedAt) : "";
  return (
    // Named for what it holds rather than "More details": the narrow list column has no room for both.
    <MoreDetails
      surface="runs.events"
      label="Events received"
      hint={`${events.length}${since ? ` since ${since}` : ""}`}
      className="mt-2 shrink-0 border-t border-line-soft pt-2"
    >
      <section
        aria-label={`Recent events received by ${environment}`}
        className="max-h-72 overflow-y-auto pr-1"
        data-testid="recent-events"
      >
        {error && (
          <Banner role="alert" className="mb-2 text-caption">
            {error}
          </Banner>
        )}
        {loaded && events.length === 0 && !error && (
          <p className="text-caption text-ink-dim" data-testid="recent-events-empty">
            Nothing received recently.
          </p>
        )}
        {events.length > 0 && (
          <ul className="divide-y divide-line-soft" data-testid="recent-events-list">
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
        <p className="mt-2 text-caption text-ink-faint" data-testid="recent-events-instance">
          The platform keeps recent events in memory on each server, so a restart starts this list again.
          {since && ` This server has been recording since ${since}.`}
        </p>
        {instanceSwitched && (
          <p className="mt-1 text-caption text-ink-faint" data-testid="recent-events-switched">
            This list now comes from another server, so its events differ. None were lost.
          </p>
        )}
      </section>
    </MoreDetails>
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
  const receivedAt = timestampToISO(event.receivedAt);
  const raw: [string, string][] = [
    ["Type", event.eventSubtype ? `${event.eventType} · ${event.eventSubtype}` : event.eventType],
    ["Channel", event.channelId],
    ["Team", event.teamId],
    ["Connection", event.connectionName],
    ["HTTP status", event.httpStatus ? String(event.httpStatus) : ""],
    ["Duration", Number(event.durationMs) ? `${String(event.durationMs)} ms` : ""],
    ["Event", event.eventId],
  ];
  return (
    <li className="py-1.5 text-caption" data-testid="recent-event">
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-medium text-ink">
          {eventKind(event.eventType)}
          {event.channelId ? " in a channel" : ""}
        </span>
        <time className="shrink-0 text-ink-faint" dateTime={receivedAt ?? undefined}>
          {formatTimeAgo(receivedAt, nowMs)}
        </time>
      </div>
      <p className={cx(fateDelivered(event.fate) ? "text-ink-dim" : "text-busy")}>
        {fateWords(event.fate, environment)}
      </p>
      <MoreDetails surface="runs.events.raw" className="mt-0.5">
        <dl className="grid grid-cols-[max-content_1fr] gap-x-2 font-mono text-ink-faint">
          {raw
            .filter(([, value]) => value !== "")
            .map(([name, value]) => (
              <div key={name} className="col-span-full grid grid-cols-subgrid">
                <dt>{name}</dt>
                <dd className="wrap-anywhere">{value}</dd>
              </div>
            ))}
        </dl>
      </MoreDetails>
    </li>
  );
}
