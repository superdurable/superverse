// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's goal measurement of a Project's Live runs, as EvolveService answers it: example goals of an invented
// outreach application, three kept reviews, the newest with the agent's proposal (evolve-proposals.ts), and Review now
// reviewed on the mock clock. Development only; no real Project, account or run is behind these numbers.
import type { Evolve } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveGoal } from "@/lib/generated/protobuf/superverse/v2/EvolveGoal";
import type { EvolveGoalsStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoalsStatus";
import type { EvolveGoalValue } from "@/lib/generated/protobuf/superverse/v2/EvolveGoalValue";
import type { EvolveReview } from "@/lib/generated/protobuf/superverse/v2/EvolveReview";
import type { EvolveReviewRefusal } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewRefusal";
import { httpError } from "@/lib/server/request-body";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { nowIso } from "@/mock/store/clock";
import { mockProposals, PROPOSAL_OFFERED_AGO, PROPOSAL_REVIEW } from "@/mock/store/delivery/evolve-proposals";
import type { MockStore } from "@/mock/store/store";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
/** The runs a review reads: those that closed within the Live retention of closed runs, and the open ones. */
const WINDOW = 30 * DAY;
/** How long a mock Review now exports, measures and is reviewed before the agent finds nothing new to propose. */
const REVIEW_MILLISECONDS = 8_000;

/** Example goals: an outreach application that writes to prospects and records their replies. */
const EXAMPLE_GOALS: (EvolveGoal & { id: string })[] = [
  {
    id: "reply-rate",
    kind: "EVOLVE_GOAL_KIND_OUTCOME",
    statement: "Most people we write to answer",
    metric: "rate(replied, of contacted)",
    direction: "EVOLVE_GOAL_DIRECTION_UP",
    target: 0.4,
  },
  {
    id: "qualified-rate",
    kind: "EVOLVE_GOAL_KIND_OUTCOME",
    statement: "Half of the discovery calls we book qualify",
    metric: "rate(qualified, of booked)",
    direction: "EVOLVE_GOAL_DIRECTION_UP",
    target: 0.5,
  },
  {
    id: "messages",
    kind: "EVOLVE_GOAL_KIND_EXPERIENCE",
    statement: "No prospect gets more than three messages",
    metric: "max(steps.WriteOutreachMessage)",
    guardrailMax: 3,
  },
  {
    id: "ai-spend",
    kind: "EVOLVE_GOAL_KIND_COST",
    statement: "AI spend under 4 cents per prospect",
    metric: "mean(ai.usd)",
    direction: "EVOLVE_GOAL_DIRECTION_DOWN",
    target: 0.04,
  },
];

/** Example parser problems, for the invalid goals fixture. */
const EXAMPLE_PROBLEMS = [
  "line 14: review must be weekly, biweekly or off",
  'line 31: goal reply-rate: unknown outcome "answered"',
];

/** Each example goal's value as [value, runs] in the two measured reviews, older first; fewer than 20 runs is few. */
const EXAMPLE_VALUES: Record<string, [older: [number, number], newer: [number, number]]> = {
  "reply-rate": [
    [0.31, 188],
    [0.396, 212],
  ],
  "qualified-rate": [
    [0.5, 12],
    [0.357, 14],
  ],
  messages: [
    [3, 188],
    [3, 212],
  ],
  "ai-spend": [
    [0.048, 188],
    [0.0448, 212],
  ],
};

type GoalsFixture = "valid" | "invalid" | "none";

const GOALS_STATUS = {
  valid: "EVOLVE_GOALS_STATUS_VALID",
  invalid: "EVOLVE_GOALS_STATUS_INVALID",
  none: "EVOLVE_GOALS_STATUS_NONE",
} as const satisfies Record<GoalsFixture, EvolveGoalsStatus__Output>;

interface MockGoalMeasurement {
  goals: GoalsFixture;
  /** Newest first. */
  reviews: EvolveReview[];
  reviewSeq: number;
  active: string;
  activeStartedMs: number;
  lastManualMs: number | null;
  /** Review now's request IDs and the review each started. */
  requests: Map<string, string>;
}

// The page's fixture query and the BFF's mock backend are separate module instances; they share the state here.
const shared = globalThis as { superverseMockGoalMeasurements?: Map<string, MockGoalMeasurement> };
const measurements = () => (shared.superverseMockGoalMeasurements ??= new Map());

const nowMs = () => Date.parse(nowIso());
const at = (ms: number) => isoToTimestamp(new Date(ms).toISOString());

function goalValues(index: 0 | 1): EvolveGoalValue[] {
  return EXAMPLE_GOALS.map((goal) => {
    const [value, runs] = EXAMPLE_VALUES[goal.id][index];
    return {
      goalId: goal.id,
      value,
      runs,
      fewRuns: runs < 20,
      ...(goal.guardrailMax !== undefined ? { holds: value <= Number(goal.guardrailMax) } : {}),
    };
  });
}

function review(
  seq: number,
  requestedMs: number,
  fields: Pick<EvolveReview, "trigger" | "status"> & Partial<EvolveReview>,
): EvolveReview {
  return {
    reviewId: `r-${seq}`,
    requestedAt: at(requestedMs),
    startedAt: at(requestedMs + 2_000),
    windowFrom: at(requestedMs - WINDOW),
    windowTo: at(requestedMs),
    export: { parts: 0, runs: 0, bytes: 0, truncated: false, deleted: false },
    ...fields,
  };
}

/**
 * The example history: a scheduled review the first Live version skipped, then Review now, measured before agent
 * reviews were on, and a scheduled one whose agent review proposed a change.
 */
function exampleMeasurement(now: number, commit: string): MockGoalMeasurement {
  const measured = (index: 0 | 1, runs: number) => ({
    status: "EVOLVE_REVIEW_STATUS_MEASURED" as const,
    goalsCommit: commit,
    runs,
    values: goalValues(index),
    export: { parts: 1, runs, bytes: runs * 1_900, truncated: false, deleted: false },
  });
  const manualMs = now - 6 * DAY - 3 * HOUR;
  return {
    goals: "valid",
    reviewSeq: 3,
    active: "",
    activeStartedMs: 0,
    lastManualMs: manualMs,
    requests: new Map(),
    reviews: [
      review(Number(PROPOSAL_REVIEW.slice(2)), now - PROPOSAL_OFFERED_AGO - 50 * 60_000, {
        trigger: "EVOLVE_REVIEW_TRIGGER_SCHEDULE",
        finishedAt: at(now - PROPOSAL_OFFERED_AGO),
        ...measured(1, 212),
        status: "EVOLVE_REVIEW_STATUS_PROPOSAL",
      }),
      review(2, manualMs, {
        trigger: "EVOLVE_REVIEW_TRIGGER_MANUAL",
        actorAccountId: "mock-account",
        finishedAt: at(manualMs + 3 * 60_000),
        ...measured(0, 188),
      }),
      review(1, now - 8 * DAY - 5 * HOUR, {
        trigger: "EVOLVE_REVIEW_TRIGGER_SCHEDULE",
        status: "EVOLVE_REVIEW_STATUS_SKIPPED",
        reason: "EVOLVE_REVIEW_REASON_NO_GOALS",
        finishedAt: at(now - 8 * DAY - 5 * HOUR + 2_000),
      }),
    ],
  };
}

function liveCommit(store: MockStore, projectId: string): string {
  const live = store.liveDeployments.get(projectId);
  if (!live) throw httpError(404, "the Project has had no Live deployment, so nothing measures its goals yet");
  return live.releaseId;
}

function measurementOf(store: MockStore, projectId: string): MockGoalMeasurement {
  const commit = liveCommit(store, projectId);
  const all = measurements();
  let current = all.get(projectId);
  if (!current) all.set(projectId, (current = exampleMeasurement(nowMs(), commit)));
  advance(current, commit);
  return current;
}

/** Ends the running review measured once its time on the mock clock passed, with the newer values moved a little. */
function advance(state: MockGoalMeasurement, commit: string) {
  const running = state.reviews.find((item) => item.reviewId === state.active);
  if (!running || nowMs() - state.activeStartedMs < REVIEW_MILLISECONDS) return;
  const runs = 221;
  const values = goalValues(1).map((item) => {
    const moved =
      { "reply-rate": 1.03, "qualified-rate": 1.05, messages: 1, "ai-spend": 0.96 }[String(item.goalId)] ?? 1;
    // Few prospects answer, so the rate over those who answered stays on few runs.
    const goalRuns = Number(item.runs) + (item.goalId === "qualified-rate" ? 2 : 9);
    return { ...item, value: Number(item.value) * moved, runs: goalRuns, fewRuns: goalRuns < 20 };
  });
  Object.assign(running, {
    status: "EVOLVE_REVIEW_STATUS_NO_CHANGE",
    finishedAt: at(nowMs()),
    goalsCommit: commit,
    runs,
    values,
    export: { parts: 1, runs, bytes: runs * 1_900, truncated: false, deleted: false },
  } satisfies Partial<EvolveReview>);
  state.active = "";
}

/** GetEvolve: NOT_FOUND before the Project's first Live deployment, as the server answers. */
export function mockEvolve(store: MockStore, projectId: string): Evolve {
  const state = measurementOf(store, projectId);
  const live = store.liveDeployments.get(projectId)!;
  const now = nowMs();
  const valid = state.goals === "valid";
  const again = state.lastManualMs !== null && now - state.lastManualMs < DAY ? state.lastManualMs + DAY : null;
  return {
    projectId,
    live: {
      // Live's revision is the Live pointer's, as the Project's snapshot shows it.
      revision: live.generation,
      releaseId: live.releaseId,
      buildCommit: live.releaseId,
      committedAt: at(now - DAY * 9),
    },
    goals: {
      commit: live.releaseId,
      status: GOALS_STATUS[state.goals],
      goals: valid ? EXAMPLE_GOALS : [],
      errors: state.goals === "invalid" ? EXAMPLE_PROBLEMS : [],
      readAt: at(now - DAY * 9),
      flowTypes: valid ? ["ProspectFlow"] : [],
    },
    schedule: {
      period: "EVOLVE_REVIEW_PERIOD_WEEKLY",
      active: valid,
      nextAt: valid ? at(now + 6 * DAY - 5 * HOUR) : null,
    },
    activeReviewId: state.active,
    reviews: state.reviews,
    agentReviews: true,
    proposals: mockProposals(store, projectId),
    nextManualReviewAt: again === null ? null : at(again),
  };
}

/** StartEvolveReview: a known request answers its review; otherwise refused, or a new review that exports. */
export function startMockReview(
  store: MockStore,
  projectId: string,
  requestId: string,
  actorAccountId: string,
): { reviewId: string; refusal: EvolveReviewRefusal } {
  if (!requestId) throw httpError(400, "Request identity is required");
  const state = measurementOf(store, projectId);
  const known = state.requests.get(requestId);
  if (known) return { reviewId: known, refusal: "EVOLVE_REVIEW_REFUSAL_UNSPECIFIED" };
  const refuse = (refusal: EvolveReviewRefusal) => ({ reviewId: "", refusal });
  if (state.goals === "none") return refuse("EVOLVE_REVIEW_REFUSAL_NO_GOALS");
  if (state.goals === "invalid") return refuse("EVOLVE_REVIEW_REFUSAL_GOALS_INVALID");
  if (state.active) return refuse("EVOLVE_REVIEW_REFUSAL_REVIEW_RUNNING");
  const now = nowMs();
  if (state.lastManualMs !== null && now - state.lastManualMs < DAY)
    return refuse("EVOLVE_REVIEW_REFUSAL_RATE_LIMITED");
  state.reviewSeq += 1;
  const started = review(state.reviewSeq, now, {
    trigger: "EVOLVE_REVIEW_TRIGGER_MANUAL",
    status: "EVOLVE_REVIEW_STATUS_EXPORTING",
    actorAccountId,
    startedAt: at(now),
  });
  state.reviews.unshift(started);
  state.active = String(started.reviewId);
  state.activeStartedMs = now;
  state.lastManualMs = now;
  state.requests.set(requestId, state.active);
  return { reviewId: state.active, refusal: "EVOLVE_REVIEW_REFUSAL_UNSPECIFIED" };
}

/** `?goalsFixture=invalid` or `none` shows that goals file on a Live Project's Goals tab; `valid` the example again. */
export function seedGoalsFixture(store: MockStore, projectId: string, name: string | undefined) {
  if (name === undefined || !store.liveDeployments.has(projectId)) return;
  const state = measurementOf(store, projectId);
  state.goals = name === "invalid" || name === "none" ? name : "valid";
}
