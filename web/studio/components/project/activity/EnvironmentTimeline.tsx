// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { listEnvironmentEvents } from "@/lib/api/project";
import { PREVIEW_PHASE_LABELS } from "@/lib/project/setup/environment-status";
import { usePagedList } from "@/lib/browser/use-paged-list";
import { formatTimeAgo } from "@/lib/shared/format";
import { errorProblem, failureProblem } from "@/lib/shared/problem/problem";
import { timestampToISO } from "@/lib/shared/protobuf";
import { Button } from "@/components/ui/Button";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { PhaseTimings } from "@/components/ui/timings/PhaseTimings";
import { DEPLOYMENT_PHASE_LABELS, RELEASE_PHASE_LABELS } from "@/components/publishing/timings/phase-labels";
import { eventKey, eventOutcome, eventSentence, eventVersion, OUTCOME_WORDS } from "./event-text";

/** Each environment's timeline: its scope, heading and the names of the phases its events record. */
const SCOPES = {
  preview: {
    rpc: "PROJECT_SCOPE_PREVIEW",
    heading: "Preview timeline",
    operation: "Reading the Preview timeline",
    phaseLabels: PREVIEW_PHASE_LABELS,
  },
  // A Live event's phases are its build's or its deployment's.
  live: {
    rpc: "PROJECT_SCOPE_LIVE",
    heading: "Live timeline",
    operation: "Reading the Live timeline",
    phaseLabels: { ...RELEASE_PHASE_LABELS, ...DEPLOYMENT_PHASE_LABELS },
  },
} as const;

/** The environment a timeline lists. */
export type TimelineScope = keyof typeof SCOPES;

/**
 * One environment's history, newest first: the Preview's recent starts and applies, or Live's
 * publishes and deployments of an existing version. The server derives it from existing state, so
 * the Preview keeps only its newest 50 starts. A failure shows as the one problem format and a
 * version as "V12 · subject", with the commit only inside the problem's folded details.
 */
export function EnvironmentTimeline({ projectId, scope }: { projectId: string; scope: TimelineScope }) {
  const { rpc, heading, operation, phaseLabels } = SCOPES[scope];
  // Ages are read when the list loads.
  const [now] = useState(() => Date.now());
  const events = usePagedList({
    key: `${projectId}:${scope}`,
    fetchPage: async (cursor, signal) => {
      const page = await listEnvironmentEvents({ projectId, scope: rpc, pageSize: 25, pageToken: cursor }, signal);
      return { items: page.events, nextPageToken: page.nextPageToken };
    },
    idOf: eventKey,
  });
  return (
    <section aria-label={heading} data-testid={`${scope}-timeline`}>
      <div className="flex items-center gap-3">
        <h3 className="text-[11px] font-normal tracking-[0.07em] text-ink-faint uppercase">{heading} · newest first</h3>
        <Button className="ml-auto" size="sm" onClick={() => void events.refresh()}>
          Refresh
        </Button>
      </div>
      {events.error && <ProblemPanel className="mt-2" problem={errorProblem(events.error, { operation })} />}
      {!events.loaded && !events.error && <p className="mt-2 text-sm text-ink-faint">Reading the timeline…</p>}
      {events.loaded && events.items.length === 0 && (
        <p className="mt-2 text-sm text-ink-faint">Nothing has happened here yet.</p>
      )}
      <ol className="mt-1 divide-y divide-line-soft">
        {events.items.map((event) => {
          const at = timestampToISO(event.occurredAt);
          const outcome = eventOutcome(event);
          return (
            <li key={eventKey(event)} className="py-2 text-[13px]">
              <p className="flex flex-wrap items-baseline gap-x-2">
                <span className="text-ink">
                  {eventSentence(event, scope)} <strong className="font-medium">{eventVersion(event)}</strong>
                </span>
                {outcome && <span className={OUTCOME_WORDS[outcome].tone}>{OUTCOME_WORDS[outcome].words}</span>}
                <time className="ml-auto text-xs text-ink-faint" dateTime={at ?? undefined}>
                  {formatTimeAgo(at, now)}
                </time>
              </p>
              {event.failure && (
                <ProblemPanel
                  className="mt-1.5"
                  problem={failureProblem(event.failure, {
                    operation: scope === "preview" ? "The Preview start" : "The publish",
                    version: event.version,
                    commit: event.commit,
                    at: at ?? "",
                  })}
                />
              )}
              {event.phaseTimings.length > 0 && (
                <details className="mt-1">
                  <summary className="cursor-pointer text-xs text-ink-dim">
                    Phase timings of {eventVersion(event)}
                  </summary>
                  <PhaseTimings title="Phases" timings={event.phaseTimings} labels={phaseLabels} />
                </details>
              )}
            </li>
          );
        })}
      </ol>
      {events.nextPageToken && (
        <Button className="mt-2" size="sm" disabled={events.loading} onClick={() => void events.loadMore()}>
          {events.loading ? "Reading…" : "Older events"}
        </Button>
      )}
    </section>
  );
}
