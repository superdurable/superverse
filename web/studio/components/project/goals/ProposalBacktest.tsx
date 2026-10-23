// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { EvolveGoal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoal";
import { formatGoalValue, goalStatement, goalUnit } from "@/lib/project/goals/goal-values";
import { goalMovement, type Backtest, type BacktestGoal } from "@/lib/project/goals/proposal-backtest";
import { GoalAfterValue } from "@/components/project/proposal/GoalAfterValue";
import { goalDelta, MOVEMENT_WORDS } from "@/components/project/proposal/proposal-words";
import { Badge } from "@/components/ui/badge/Badge";
import { GoalFacts, goalKindFacts } from "./GoalRows";

const HEADING = "mb-1.5 font-semibold text-ink";
const percent = new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 0 });

/**
 * Each goal before and after the change, as the backtest replayed the review's runs, in the goal rows' style: the
 * statement with its kind and direction or limit, how many runs the after value knows, estimates and leaves unknown,
 * and the values, an estimated one with its interval. Then the runs by how the backtest knows them and its calibration.
 */
export function ProposalBacktest({ backtest, goals }: { backtest: Backtest; goals: readonly EvolveGoal__Output[] }) {
  const byId = new Map(goals.map((goal) => [goal.id, goal]));
  const { runs, calibration, estimation } = backtest;
  const exact = runs.same + runs.changedExact;
  const share = (count: number) => (runs.compared > 0 ? ` (${percent.format(count / runs.compared)})` : "");
  return (
    <>
      <section aria-label="Goals before and after">
        <h4 className={HEADING}>Goals before and after</h4>
        <ol className="divide-y divide-line-faint rounded-lg border border-line-soft bg-panel">
          {backtest.goals.map((item) => (
            <ProposalGoalRow key={item.id} item={item} goal={byId.get(item.id) ?? null} />
          ))}
        </ol>
      </section>
      <section aria-label="Backtest" data-testid="proposal-backtest">
        <h4 className={HEADING}>Backtest</h4>
        <p>
          Of {runs.compared.toLocaleString()} runs replayed, {exact.toLocaleString()} are exact{share(exact)},{" "}
          {runs.changedEstimated.toLocaleString()} estimated{share(runs.changedEstimated)} and{" "}
          {runs.unknown.toLocaleString()} unknown{share(runs.unknown)}.
        </p>
        <p className="mt-1">
          {calibration.share === null
            ? "No recorded run could check the replay."
            : `Calibration ${percent.format(calibration.share)}: the current code reproduced ${calibration.matched.toLocaleString()} of ${calibration.checked.toLocaleString()} recorded runs.`}
          {runs.changedEstimated > 0 &&
            ` Estimates come from ${estimation.groups} groups of ${estimation.sampleMin} to ${estimation.sampleMax} similar runs (median ${estimation.sampleMedian}), with ${percent.format(estimation.interval)} intervals.`}
        </p>
        {backtest.gates.map((gate) => (
          <p key={gate} className="mt-1 text-busy">
            {gate}
          </p>
        ))}
      </section>
    </>
  );
}

function ProposalGoalRow({ item, goal }: { item: BacktestGoal; goal: EvolveGoal__Output | null }) {
  const delta = goalDelta(item);
  const moved = goalMovement(item, goal);
  const overAll =
    item.overAll !== null && item.overAll !== item.before
      ? `${formatGoalValue(item.overAll, goalUnit(item.metric))} over every run`
      : undefined;
  return (
    <li className="flex items-center gap-3 px-3 py-2.5" data-testid="proposal-goal" data-goal={item.id}>
      <div className="min-w-0 flex-1">
        <p className="text-body text-ink" title={goal ? undefined : item.id}>
          {goalStatement(goal)}
        </p>
        <GoalFacts
          facts={[
            ...(goal ? goalKindFacts(goal) : [item.metric, limitWords(item)]),
            item.known > 0 && `${item.known.toLocaleString()} known`,
            item.estimated > 0 && `${item.estimated.toLocaleString()} estimated`,
            item.unknown > 0 && `${item.unknown.toLocaleString()} unknown`,
          ]}
        >
          {item.targeted && <Badge title="The goal the change sets out to move">targeted</Badge>}
        </GoalFacts>
      </div>
      <div className="shrink-0 text-right">
        <p className="text-body whitespace-nowrap tabular-nums">
          <span className="text-ink-dim" title={overAll}>
            {delta.before}
          </span>
          <span className="text-ink-faint"> → </span>
          <GoalAfterValue
            movement={moved}
            className="font-semibold"
            title={MOVEMENT_WORDS[moved ?? "none"].words || undefined}
          >
            {delta.after}
          </GoalAfterValue>
        </p>
        {delta.range && <p className="text-caption text-ink-faint">{delta.range}</p>}
      </div>
    </li>
  );
}

/** The review's limit of a guardrail goal that Live's goals no longer hold, in words; null for a goal with a direction. */
function limitWords(item: BacktestGoal): string | null {
  const unit = goalUnit(item.metric);
  const { max, min } = item.guardrail ?? { max: null, min: null };
  if (max !== null && min !== null) return `Between ${formatGoalValue(min, unit)} and ${formatGoalValue(max, unit)}`;
  if (max !== null) return `At most ${formatGoalValue(max, unit)}`;
  if (min !== null) return `At least ${formatGoalValue(min, unit)}`;
  return null;
}
