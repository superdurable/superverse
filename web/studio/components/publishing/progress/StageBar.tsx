// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { StageState, StageView } from "@/lib/project/publishing/publish-stages";
import { formatElapsedDuration } from "@/lib/shared/format";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";

/** Each state's segment: its colours and the words its accessible name ends with. */
const SEGMENT = {
  done: { tone: "bg-pass-subtle text-pass", words: "done" },
  running: { tone: "bg-accent text-panel", words: "running" },
  waiting: { tone: "text-ink-faint", words: "waiting" },
  failed: { tone: "bg-fail-subtle text-fail", words: "failed" },
  stopped: { tone: "text-ink-dim", words: "cancelled here" },
} as const satisfies Record<StageState, { tone: string; words: string }>;

function StateMark({ state }: { state: StageState }) {
  if (state === "running") return <Spinner className="size-3.5" />;
  if (state === "done") return <Glyph name="check" className="size-3.5" />;
  if (state === "failed") return <Glyph name="close" className="size-3.5" />;
  return <Glyph name="clock" className="size-3.5" />;
}

/**
 * The stage bar: Check → Lock settings → Build → Deploy → Go live (Deploy and Go live for a rollback),
 * each with its state and, once done, how long it took. On a narrow panel only the stage that runs, failed
 * or was cancelled keeps its name; every stage keeps it in its hover hint and the bar's value text.
 */
export function StageBar({ stages, label }: { stages: readonly StageView[]; label: string }) {
  const reached = stages.filter((view) => view.state === "done").length;
  const current = stages.find((view) => view.state !== "done" && view.state !== "waiting");
  return (
    <div
      className="@container/stages mx-4 mb-3.5 flex overflow-hidden rounded-full bg-well"
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={stages.length}
      aria-valuenow={reached}
      aria-valuetext={
        current
          ? `${current.stage.label} ${SEGMENT[current.state].words}, ${reached} of ${stages.length} stages done`
          : `${reached} of ${stages.length} stages done`
      }
      data-testid="publish-stages"
    >
      {stages.map((view) => {
        const duration = view.durationMs === null ? "" : formatElapsedDuration(view.durationMs);
        const name = `${view.stage.label}: ${SEGMENT[view.state].words}${duration ? `, ${duration}` : ""}`;
        return (
          <span
            key={view.stage.id}
            title={name}
            data-stage={view.stage.id}
            data-state={view.state}
            className={cx(
              "flex h-8.5 min-w-0 flex-1 items-center justify-center gap-1.5 rounded-full px-2 text-[13px] whitespace-nowrap",
              SEGMENT[view.state].tone,
            )}
          >
            <span
              className={cx(
                "truncate",
                (view.state === "done" || view.state === "waiting") && "hidden @[34rem]/stages:inline",
              )}
            >
              {view.stage.label}
            </span>
            <StateMark state={view.state} />
            {duration && <span className="font-mono text-[11.5px] opacity-85">{duration}</span>}
          </span>
        );
      })}
    </div>
  );
}
