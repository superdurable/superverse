// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { EvolveGoal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoal";
import { arrayField, asRecord, isRecord, stringField } from "@/lib/shared/json";

/** One goal before and after the change, as the backtest's comparison reports it; null where no run gave a value. */
export interface BacktestGoal {
  id: string;
  metric: string;
  /** Which way the goal is better, as the review read it; null for a guardrail. */
  direction: "up" | "down" | null;
  /** The goal the proposal sets out to move. */
  targeted: boolean;
  /** The value over every run the review read. */
  overAll: number | null;
  /** The value before the change over the runs the after value covers, which the after value compares with. */
  before: number | null;
  after: number | null;
  /** The interval of an estimated after value; equal to it when every run is known. */
  afterLow: number | null;
  afterHigh: number | null;
  /** The compared runs that bear on the goal, by how the backtest knows their after value. */
  known: number;
  estimated: number;
  unknown: number;
  /** The goal's limit as the review read it, which the review checked the change keeps; null for a goal with a direction. */
  guardrail: { max: number | null; min: number | null } | null;
}

/** The backtest's summary: the runs by how it knows them, how well it reproduces history, and each goal. */
export interface Backtest {
  runs: { compared: number; same: number; changedExact: number; changedEstimated: number; unknown: number };
  /** The share of checked runs whose before decisions equal the record; null when none was checked. */
  calibration: { share: number | null; checked: number; matched: number };
  estimation: { interval: number; groups: number; sampleMin: number; sampleMedian: number; sampleMax: number };
  goals: BacktestGoal[];
  /** The gates that fired, in the comparison's words; a refusing gate never reaches an offered proposal. */
  gates: string[];
}

const count = (value: unknown) => (typeof value === "number" && Number.isFinite(value) ? value : 0);
const value = (number: unknown) => (typeof number === "number" && Number.isFinite(number) ? number : null);

/**
 * The proposal's backtest, from the summary the review kept of the JSON the platform's comparison wrote
 * (`goals.BacktestSummary`, with the keys of `goals.Backtest`, snake_case); null when it is not that JSON. Missing
 * counts read as zero and missing values as unknown. A guardrail a backtest breaks never reaches an offered proposal.
 */
export function parseBacktest(json: string): Backtest | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return null;
  }
  if (!isRecord(parsed)) return null;
  const runs = asRecord(parsed.runs);
  const calibration = asRecord(parsed.calibration);
  const estimation = asRecord(parsed.estimation);
  return {
    runs: {
      compared: count(runs.compared),
      same: count(runs.same),
      changedExact: count(runs.changed_exact),
      changedEstimated: count(runs.changed_estimated),
      unknown: count(runs.unknown),
    },
    calibration: {
      share: value(calibration.share),
      checked: count(calibration.checked),
      matched: count(calibration.matched),
    },
    estimation: {
      interval: count(estimation.interval),
      groups: count(estimation.groups),
      sampleMin: count(estimation.sample_min),
      sampleMedian: count(estimation.sample_median),
      sampleMax: count(estimation.sample_max),
    },
    goals: arrayField(parsed.goals)
      .filter(isRecord)
      .map((goal) => ({
        id: stringField(goal.id),
        metric: stringField(goal.metric),
        direction: goal.direction === "up" || goal.direction === "down" ? goal.direction : null,
        targeted: goal.targeted === true,
        overAll: value(goal.before),
        before: value(goal.baseline) ?? value(goal.before),
        after: value(goal.after),
        afterLow: value(goal.after_low),
        afterHigh: value(goal.after_high),
        known: count(goal.known),
        estimated: count(goal.estimated),
        unknown: count(goal.unknown),
        guardrail: isRecord(goal.guardrail) ? { max: value(goal.guardrail.max), min: value(goal.guardrail.min) } : null,
      })),
    gates: arrayField(parsed.gates)
      .filter(isRecord)
      .map((gate) => stringField(gate.message))
      .filter(Boolean),
  };
}

/** The after value is a range: some runs were estimated and the interval is wider than the value. */
export function estimatedRange(goal: BacktestGoal): [number, number] | null {
  const { afterLow: low, afterHigh: high } = goal;
  return goal.estimated > 0 && low !== null && high !== null && low !== high ? [low, high] : null;
}

/** Whether a goal's after value is better or worse than its before value; null when neither can be said. */
export type GoalMovement = "better" | "worse" | null;

/** The direction of a goal of Live's goals file, as the backtest names it. */
const DIRECTIONS = {
  EVOLVE_GOAL_DIRECTION_UNSPECIFIED: null,
  EVOLVE_GOAL_DIRECTION_UP: "up",
  EVOLVE_GOAL_DIRECTION_DOWN: "down",
} as const satisfies Record<EvolveGoal__Output["direction"], BacktestGoal["direction"]>;

/**
 * Whether the after value is better or worse: for a goal with a direction (the review's, else that of `goal` in Live's
 * goals), better or worse when the value moves, an estimated one only when its whole interval does; null for a
 * guardrail, which the review checked the change keeps, no change or an interval that holds the before value.
 */
export function goalMovement(item: BacktestGoal, goal: Pick<EvolveGoal__Output, "direction"> | null): GoalMovement {
  const { before, after } = item;
  const direction = item.direction ?? (goal && DIRECTIONS[goal.direction]);
  if (!direction || before === null || after === null || before === after) return null;
  const [low, high] = estimatedRange(item) ?? [after, after];
  const way = low > before ? "up" : high < before ? "down" : null;
  if (way === null) return null;
  return way === direction ? "better" : "worse";
}

/**
 * The goals whose value the change moves, for a short list beside Approve, each with its movement: the targeted goal
 * first, then every goal that gets worse, which are never left out, then up to `limit` in all with the goals every run
 * knows exactly and then the estimated ones, each group in the comparison's order.
 */
export function movedGoals(
  backtest: Backtest,
  goals: readonly EvolveGoal__Output[],
  limit: number,
): { goal: BacktestGoal; movement: GoalMovement }[] {
  const byId = new Map(goals.map((goal) => [goal.id, goal]));
  const moved = backtest.goals
    .filter((goal) => goal.after !== null && goal.before !== goal.after)
    .map((goal) => ({ goal, movement: goalMovement(goal, byId.get(goal.id) ?? null) }));
  const rank = ({ goal, movement }: (typeof moved)[number]) =>
    goal.targeted ? 0 : movement === "worse" ? 1 : goal.estimated === 0 && goal.unknown === 0 ? 2 : 3;
  return moved.toSorted((a, b) => rank(a) - rank(b)).filter((item, index) => rank(item) <= 1 || index < limit);
}
