// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The example proposal of the mock's outreach application, "Follow up once instead of twice": its backtest as the
// review keeps it, its summary and its document. Development only; every number is example data.

/**
 * The summary a review keeps of a proposal's backtest.json, as GetEvolveProposal serves it in backtest_json: the
 * `goals.BacktestSummary` type of internal/dexai/delivery/evolve/goals (`Kept`), aggregates only. A value is null
 * where no run gave it one.
 */
interface MockBacktestSummary {
  export: {
    review_id: string;
    goals_commit: string;
    runs_sha256: string;
    records: number;
    before_commit: string;
    base_commit: string;
  };
  evaluated_at: string;
  runs: Record<
    | "records"
    | "compared"
    | "same"
    | "changed_exact"
    | "changed_estimated"
    | "unknown"
    | "missing_before"
    | "missing_after"
    | "lines_without_record",
    number
  >;
  calibration: { share: number | null; floor: number; checked: number; matched: number };
  estimation: {
    min_runs: number;
    interval: number;
    groups: number;
    sample_min: number;
    sample_median: number;
    sample_max: number;
  };
  goals: MockGoalComparison[];
  gates: { code: string; refuses: boolean; message: string }[];
}

interface MockGoalComparison {
  id: string;
  digest: string;
  metric: string;
  direction?: "up" | "down";
  guardrail?: { max?: number; min?: number };
  targeted: boolean;
  before: number | null;
  baseline: number | null;
  after: number | null;
  after_low: number | null;
  after_high: number | null;
  known: number;
  estimated: number;
  unknown: number;
}

/**
 * The example backtest of review r-3 over its 212 runs, targeting AI spend, as `dexai evolve compare` wrote it for an
 * invented export of the outreach application (`--strata prospect-fit,prospect-angle --target ai-spend`), with the
 * review's binding: goalsCommit is the Live build it reviewed. 113 prospects got no reply after the first message
 * and one follow-up; the 31 at companies of 100 or more people got a second follow-up, which the change drops. Their
 * messages and AI spend are exact; their replies come after the decision, so the landmark estimator estimates them
 * from the 6 groups of recorded closed runs of the same fit and angle that were closed after one follow-up (10 to 17
 * runs each). Calibration is 100%, no run is unknown and no gate fires.
 */
export function exampleBacktest(evaluatedAt: string, goalsCommit: string): MockBacktestSummary {
  return {
    export: {
      review_id: "r-3",
      goals_commit: goalsCommit,
      runs_sha256: "28caa2f72cfee424df555cde9622542e3cbfe750df2de20f85e1823a64651038",
      records: 212,
      before_commit: goalsCommit,
      base_commit: goalsCommit,
    },
    evaluated_at: evaluatedAt,
    runs: {
      records: 212,
      compared: 212,
      same: 181,
      changed_exact: 0,
      changed_estimated: 31,
      unknown: 0,
      missing_before: 0,
      missing_after: 0,
      lines_without_record: 0,
    },
    calibration: { share: 1, floor: 0.98, checked: 212, matched: 212 },
    estimation: { min_runs: 10, interval: 0.9, groups: 6, sample_min: 10, sample_median: 14, sample_max: 17 },
    goals: EXAMPLE_COMPARISONS,
    gates: [],
  };
}

const exact = (value: number) => ({ after: value, after_low: value, after_high: value });

/** Each example goal before and after, as the comparison reports it; the goals are those of goal-measurement.ts. */
const EXAMPLE_COMPARISONS: MockGoalComparison[] = [
  {
    id: "reply-rate",
    digest: "57566d6fd71b25da",
    metric: "rate(replied, of contacted)",
    direction: "up",
    targeted: false,
    before: 0.39622641509433965,
    baseline: 0.39622641509433965,
    after: 0.3872271550129486,
    after_low: 0.37901974303863967,
    after_high: 0.4158275224619914,
    known: 181,
    estimated: 31,
    unknown: 0,
  },
  {
    id: "qualified-rate",
    digest: "652b0afa32a2228d",
    metric: "rate(qualified, of booked)",
    direction: "up",
    targeted: false,
    before: 0.35714285714285715,
    baseline: 0.35714285714285715,
    ...exact(0.35714285714285715),
    known: 14,
    estimated: 0,
    unknown: 0,
  },
  {
    id: "messages",
    digest: "d0b0348b9387e979",
    metric: "max(steps.WriteOutreachMessage)",
    guardrail: { max: 3 },
    targeted: false,
    before: 3,
    baseline: 3,
    ...exact(2),
    known: 212,
    estimated: 0,
    unknown: 0,
  },
  {
    id: "ai-spend",
    digest: "c0fb5da96418a504",
    metric: "mean(ai.usd)",
    direction: "down",
    targeted: true,
    before: 0.04483018867924526,
    baseline: 0.04483018867924526,
    ...exact(0.04132075471698114),
    known: 212,
    estimated: 0,
    unknown: 0,
  },
];

export const SUMMARY =
  "31 of 212 prospects got a second follow-up, and 26 of them never replied. Closing after one follow-up sends 31 " +
  "fewer messages, no prospect gets more than two and AI spend per prospect drops from $0.045 to $0.041, exactly; " +
  "the reply rate moves from 39.6% to an estimated 38.7%, about 2 replies.";

/** The proposal document the agent wrote, aggregates only, as GetEvolveProposal serves it. */
export const DOCUMENT = `# Follow up once instead of twice

## Why

- 113 of 212 prospects in the window had not replied after the first message and one follow-up. The 31 of them at
  companies of 100 or more people got a second follow-up; the other 82 waited for a late reply and were closed.
- 5 of the 31 (16%) replied after the second follow-up. Of the 80 closed after one follow-up, 8 (10%) replied later
  anyway.
- Every follow-up is written by the model, so each one costs AI spend whether or not anyone answers.

## What changes

After the first follow-up, a prospect who has not replied waits for a late reply and is then closed as "no
response", whatever the company's size, instead of getting a second follow-up.

Files: \`internal/prospect/followup.go\` (the follow-up decision, a plain function), \`internal/prospect/prospect_flow.go\`.

## Goals

| Goal | Before | After | Runs |
|---|---|---|---|
| Messages to a prospect (at most 3) | 3 | 2 | exact for all 212 |
| AI spend per prospect (targeted) | $0.0448 | $0.0413 | exact for all 212 |
| Reply rate | 39.6% | 38.7% (37.9% to 41.6%) | 181 known, 31 estimated |
| Qualified, of booked calls | 35.7% | 35.7% | few runs: 14 known |

Before is the value over every run of the review; the change moves only the 31 runs that got a second follow-up.

## Backtest

181 runs same and 31 changed and estimated; none is unknown. Messages and AI spend are exact for all 212; a reply
comes after the decision, so the reply rate of the 31 changed runs is estimated. Calibration 100% (212 of 212
runs). The estimate takes the recorded closed runs of the same fit and angle that reached the same decision and were
closed after one follow-up: 6 groups of 10 to 17 runs (median 14), with 90% Wilson intervals. No booked call came
after a second follow-up, so the qualified share does not move. No gate fired.

## Open runs

33 runs are open: 29 in AwaitReply, 2 in AwaitLateReply and 2 in RecordReply. The change removes no Step and changes
no Attribute; a run that waits after its first follow-up goes on to AwaitLateReply when its wait ends.

## Risks

About 2 of the 5 replies that came after a second follow-up may not come. The next review watches the reply rate.
Nothing the goals file marks off limits (the wording of the first message) changed.

## Undo

Roll Live back to the release before; the next review proposes the code revert.
`;
