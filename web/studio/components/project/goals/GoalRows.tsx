// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import type { EvolveGoal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoal";
import type { EvolveGoalValue__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoalValue";
import type { EvolveReview__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReview";
import { formatGoalValue, goalReadings, goalUnit, guardrailWords } from "@/lib/project/goals/goal-values";
import { Badge } from "@/components/ui/badge/Badge";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { KIND_WORDS } from "./review-words";

/**
 * One row per goal: its statement, its kind, which way it should move or the limit it must hold, its target, and
 * the value of the newest measured review with the runs it rests on and a trend over the reviews before.
 */
export function GoalRows({
  goals,
  reviews,
}: {
  goals: readonly EvolveGoal__Output[];
  reviews: readonly EvolveReview__Output[];
}) {
  const readings = goalReadings(reviews);
  return (
    <ol className="divide-y divide-line-faint">
      {goals.map((goal) => (
        <GoalRow key={goal.id} goal={goal} value={readings.value(goal.id)} trend={readings.trend(goal.id)} />
      ))}
    </ol>
  );
}

function GoalRow({
  goal,
  value,
  trend,
}: {
  goal: EvolveGoal__Output;
  value: EvolveGoalValue__Output | null;
  trend: number[];
}) {
  const unit = goalUnit(goal.metric);
  const format = (number: number | undefined) => formatGoalValue(number, unit);
  const limit = guardrailWords(goal);
  const holds = value?.holds;
  const runs = value?.runs ?? 0;
  const down = goal.direction === "EVOLVE_GOAL_DIRECTION_DOWN";
  // Each fact keeps its separator, so a wrapped line never starts with one.
  const facts: ReactNode[] = [
    KIND_WORDS[goal.kind],
    limit ?? (
      <span className="inline-flex items-center gap-0.5">
        <Glyph name={down ? "arrowDown" : "arrowUp"} className="size-3" />
        {down ? "Down" : "Up"}
      </span>
    ),
    goal.target !== undefined && `Target ${format(goal.target)}`,
    value && `${runs.toLocaleString()} run${runs === 1 ? "" : "s"}`,
  ].filter(Boolean);
  return (
    <li className="flex items-center gap-3 px-3 py-2.5" data-testid="goal-row" data-goal={goal.id}>
      <div className="min-w-0 flex-1">
        <p className="text-[13px] text-ink">{goal.statement}</p>
        <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-caption text-ink-faint">
          {facts.map((fact, index) => (
            <span key={index} className="inline-flex items-center gap-1.5 whitespace-nowrap">
              {fact}
              {index < facts.length - 1 && <span aria-hidden="true">·</span>}
            </span>
          ))}
          {value?.fewRuns && <Badge title="Fewer than 20 runs: read it as a hint">few runs</Badge>}
        </p>
      </div>
      {trend.length >= 2 && <Trend values={trend} format={format} />}
      <div className="flex w-22 shrink-0 items-center justify-end gap-1.5">
        {holds !== undefined && (
          <span role="img" aria-label={holds ? "Holds" : "Broken"} title={holds ? "Holds" : "Broken"}>
            <Glyph name={holds ? "check" : "alert"} className={cx("size-4", holds ? "text-pass" : "text-fail")} />
          </span>
        )}
        <span
          className={cx("text-title font-semibold tabular-nums", holds === false ? "text-fail" : "text-ink")}
          title={value?.value === undefined ? "No run gave this goal a value" : undefined}
          data-testid="goal-value"
        >
          {format(value?.value)}
        </span>
      </div>
    </li>
  );
}

const WIDTH = 56;
const HEIGHT = 18;
const PAD = 2;

/** A small line of a goal's values over the measured reviews, oldest first, ending in a dot at the newest. */
function Trend({ values, format }: { values: number[]; format: (value: number) => string }) {
  const low = Math.min(...values);
  const high = Math.max(...values);
  const points = values.map((value, index) => [
    PAD + (index * (WIDTH - 2 * PAD)) / (values.length - 1),
    high === low ? HEIGHT / 2 : HEIGHT - PAD - ((value - low) / (high - low)) * (HEIGHT - 2 * PAD),
  ]);
  const [lastX, lastY] = points[points.length - 1];
  const label = `Trend over ${values.length} reviews: ${values.map(format).join(", ")}`;
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-4.5 w-14 shrink-0 text-mark max-sm:hidden"
      fill="none"
      data-testid="goal-trend"
    >
      <title>{label}</title>
      <polyline
        points={points.map((point) => point.join(",")).join(" ")}
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx={lastX} cy={lastY} r="2" className="fill-ink-dim" />
    </svg>
  );
}
