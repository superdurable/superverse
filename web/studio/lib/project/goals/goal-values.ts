// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { EvolveGoal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoal";
import type { EvolveGoalValue__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoalValue";
import type { EvolveReview__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReview";

/** The goals file of an application, as its repository holds it. */
export const GOALS_FILE = ".superverse/goals.md";

/** What a goal of a proposal's backtest is called: its statement in Live's goals, or words for one they no longer have. */
export function goalStatement(goal: EvolveGoal__Output | null | undefined): string {
  return goal?.statement || "A goal no longer in the goals file";
}

/** The most reviews a goal's trend draws. */
const TREND_REVIEWS = 8;

/**
 * What a metric's value reads as, from the metric language of the goals file: `rate(…)` is a share of runs, a metric
 * over `ai.usd` is in dollars, one over `duration_h` or `wait_h.<Step>` in hours, and every other one a number.
 */
type GoalUnit = "share" | "dollars" | "hours" | "number";

export function goalUnit(metric: string): GoalUnit {
  if (/\bai\.usd\b/.test(metric)) return "dollars";
  if (/\b(?:duration_h|wait_h\.)/.test(metric)) return "hours";
  // A ratio of two rates is a plain number.
  if (/^\s*rate\(/.test(metric) && !/\)\s*\//.test(metric)) return "share";
  return "number";
}

/** A goal's value, target or limit in its metric's unit; a dash when no run gave it a value. */
export function formatGoalValue(value: number | undefined, unit: GoalUnit): string {
  if (value === undefined || !Number.isFinite(value)) return "—";
  switch (unit) {
    case "share":
      return new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 1 }).format(value);
    case "dollars":
      return new Intl.NumberFormat(undefined, {
        style: "currency",
        currency: "USD",
        minimumFractionDigits: 2,
        maximumFractionDigits: Math.abs(value) < 0.1 && value !== 0 ? 3 : 2,
      }).format(value);
    case "hours":
      return `${plainNumber(value)} h`;
    case "number":
      return plainNumber(value);
  }
}

function plainNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: Math.abs(value) >= 100 ? 0 : 2 }).format(value);
}

/** A guardrail's limit in words, such as "At most 3"; null for a goal with a direction. */
export function guardrailWords(goal: EvolveGoal__Output): string | null {
  const unit = goalUnit(goal.metric);
  const { guardrailMin: min, guardrailMax: max } = goal;
  if (min !== undefined && max !== undefined)
    return `Between ${formatGoalValue(min, unit)} and ${formatGoalValue(max, unit)}`;
  if (max !== undefined) return `At most ${formatGoalValue(max, unit)}`;
  if (min !== undefined) return `At least ${formatGoalValue(min, unit)}`;
  return null;
}

/** A review measured the goals when it has their values: measurement alone, or the measurement an agent review read. */
const measured = (review: EvolveReview__Output) => review.values.length > 0;

/**
 * Each goal's current value, from the newest review that measured, and its trend: the values the measured reviews
 * gave it, oldest first, when at least two reviews measured it. Reviews come newest first.
 */
export function goalReadings(reviews: readonly EvolveReview__Output[]) {
  const done = reviews.filter(measured);
  const latest = done[0] ?? null;
  return {
    value: (goalId: string): EvolveGoalValue__Output | null =>
      latest?.values.find((item) => item.goalId === goalId) ?? null,
    trend: (goalId: string): number[] =>
      done
        .slice(0, TREND_REVIEWS)
        .reverse()
        .map((review) => review.values.find((item) => item.goalId === goalId)?.value)
        .filter((value): value is number => value !== undefined && Number.isFinite(value)),
  };
}

/** One problem of an invalid goals file: the parser writes "line <n>: <message>". */
export function goalsProblem(error: string): { line: number | null; message: string } {
  const match = /^line (\d+): ([\s\S]*)$/.exec(error);
  return match ? { line: Number(match[1]), message: match[2] } : { line: null, message: error };
}
