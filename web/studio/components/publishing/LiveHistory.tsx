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
import { usePagedList } from "@/lib/browser/use-paged-list";
import type { EnvironmentEvent__Output } from "@/lib/generated/protobuf/superverse/v2/EnvironmentEvent";
import { PUBLISH_STAGES, timingSpan } from "@/lib/project/publishing/publish-stages";
import { formatElapsedDuration } from "@/lib/shared/format";
import { failureProblem } from "@/lib/shared/problem/problem";
import { timestampToISO } from "@/lib/shared/protobuf";
import {
  eventKey,
  eventOutcome,
  eventSentence,
  eventVersion,
  OUTCOME_WORDS,
} from "@/components/project/activity/event-text";
import { Button } from "@/components/ui/Button";
import { HistoryList, HistoryRow } from "./HistoryRows";
import { RequestProblem } from "./RequestProblem";

/** The phases of the stages a build runs, apart from those a deployment runs. */
const BUILD_PHASES = new Set<string>(
  PUBLISH_STAGES.filter((stage) => stage.of === "build").flatMap((stage) => stage.phases),
);

/** The phase that ends each stage after Check; the first stage whose phase is missing is where a cancel stopped. */
const STAGE_ENDS = [
  ["Lock settings", "CONFIGURATION_FREEZE"],
  ["Build", "ARTIFACT_PUBLICATION"],
  ["Deploy", "ApplyApplication"],
  ["Go live", "CommitCurrentLive"],
] as const;

/** The line under an event: its durations, the stage a cancel stopped in, or why it failed. */
function eventDetail(event: EnvironmentEvent__Output): string {
  if (event.kind === "KIND_PUBLISH_CANCELLED") {
    const phases = new Set(event.phaseTimings.map((timing) => timing.phase));
    const rollback = !event.buildRevisionId;
    const stopped = STAGE_ENDS.find(
      ([label, phase]) => !phases.has(phase) && (!rollback || label === "Deploy" || label === "Go live"),
    );
    return `Cancelled during ${stopped?.[0] ?? "Go live"}; Live kept running what it ran`;
  }
  if (event.failure) return failureProblem(event.failure, { operation: "The publish" }).why;
  if (event.outcome === "START_OUTCOME_RUNNING") return "";
  const build = timingSpan(event.phaseTimings.filter((timing) => BUILD_PHASES.has(timing.phase)));
  const deploy = timingSpan(event.phaseTimings.filter((timing) => !BUILD_PHASES.has(timing.phase)));
  return [
    build !== null && `Build ${formatElapsedDuration(build)}`,
    deploy !== null && `deploy and health check ${formatElapsedDuration(deploy)}`,
    event.previewSkipReason && `published without a Preview run: ${event.previewSkipReason}`,
  ]
    .filter(Boolean)
    .join(" · ");
}

/** Live's history, newest first, from `ListEnvironmentEvents` for Live, read again when `reloadKey` changes. */
export function useLiveHistory(projectId: string, reloadKey: string) {
  return usePagedList({
    key: `${projectId}:live-history`,
    fetchPage: async (cursor, signal) => {
      const page = await listEnvironmentEvents(
        { projectId, scope: "PROJECT_SCOPE_LIVE", pageSize: 25, pageToken: cursor },
        signal,
      );
      return { items: page.events, nextPageToken: page.nextPageToken };
    },
    idOf: eventKey,
    reloadKey,
  });
}

/**
 * Live's history: publishes, rollbacks and cancelled publishes, one line each with who and when, opening
 * underneath to the durations, the stage a cancel stopped in or why it failed. A failure's full problem is
 * under the stage bar while it is the latest publish.
 */
export function LiveHistory({ events }: { events: ReturnType<typeof useLiveHistory> }) {
  // Ages are read when the list first renders.
  const [now] = useState(() => Date.now());
  return (
    <HistoryList
      label="Live history"
      heading="Live history"
      testId="live-history"
      footer={
        events.nextPageToken && (
          <Button className="mt-2" size="sm" disabled={events.loading} onClick={() => void events.loadMore()}>
            {events.loading ? "Reading…" : "Older events"}
          </Button>
        )
      }
    >
      {events.error && <RequestProblem operation="Reading Live's history" reason={events.error} />}
      {!events.loaded && !events.error && (
        <p className="px-3 py-2.5 text-[13px] text-ink-faint">Reading the history…</p>
      )}
      {events.loaded && events.items.length === 0 && (
        <p className="px-3 py-2.5 text-[13px] text-ink-faint">Nothing has been published yet.</p>
      )}
      <ol className="divide-y divide-line-faint">
        {events.items.map((event) => {
          const outcome = eventOutcome(event);
          const detail = eventDetail(event);
          return (
            <li key={eventKey(event)} data-kind={event.kind}>
              <HistoryRow at={timestampToISO(event.occurredAt)} now={now} detail={detail || undefined}>
                <span className="text-ink">
                  {eventSentence(event, "live")} <b className="font-semibold">{eventVersion(event)}</b>
                </span>
                {outcome && outcome !== "READY" && (
                  <span className={OUTCOME_WORDS[outcome].tone}> · {OUTCOME_WORDS[outcome].words}</span>
                )}
              </HistoryRow>
            </li>
          );
        })}
      </ol>
    </HistoryList>
  );
}
