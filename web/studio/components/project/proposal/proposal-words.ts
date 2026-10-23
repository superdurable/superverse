// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { BadgeTone } from "@/components/ui/badge/Badge";
import type { GlyphName } from "@/components/ui/Glyph";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { EvolveProposalKind__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalKind";
import type { EvolveProposalStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalStatus";
import type { BacktestGoal, GoalMovement } from "@/lib/project/goals/proposal-backtest";
import { estimatedRange } from "@/lib/project/goals/proposal-backtest";
import { formatGoalValue, goalUnit } from "@/lib/project/goals/goal-values";
import type { ProposalChoice } from "@/lib/project/goals/proposals";

/** A proposal's status as its badge: amber while it waits for a person, blue while it moves, green once Live. */
const STATUS_BADGES = {
  EVOLVE_PROPOSAL_STATUS_UNSPECIFIED: { words: "Proposal", tone: "neutral" },
  EVOLVE_PROPOSAL_STATUS_OFFERED: { words: "Needs a decision", tone: "busy" },
  EVOLVE_PROPOSAL_STATUS_APPROVED: { words: "Approved", tone: "accent" },
  EVOLVE_PROPOSAL_STATUS_APPLIED: { words: "On Preview", tone: "accent" },
  EVOLVE_PROPOSAL_STATUS_LIVE: { words: "Live", tone: "pass" },
  EVOLVE_PROPOSAL_STATUS_DECLINED: { words: "Declined", tone: "neutral" },
  EVOLVE_PROPOSAL_STATUS_DISCARDED: { words: "Closed", tone: "neutral" },
  EVOLVE_PROPOSAL_STATUS_UNDONE: { words: "Undone", tone: "neutral" },
  EVOLVE_PROPOSAL_STATUS_SUPERSEDED: { words: "Replaced", tone: "neutral" },
  EVOLVE_PROPOSAL_STATUS_STALE: { words: "Expired", tone: "neutral" },
} as const satisfies Record<EvolveProposalStatus__Output, { words: string; tone: BadgeTone }>;

/** The badge of a proposal; a discarded one that a person declined still reads Declined. */
export function proposalBadge(proposal: EvolveProposal__Output): { words: string; tone: BadgeTone } {
  if (proposal.status === "EVOLVE_PROPOSAL_STATUS_DISCARDED" && declined(proposal))
    return STATUS_BADGES.EVOLVE_PROPOSAL_STATUS_DECLINED;
  return STATUS_BADGES[proposal.status];
}

/** A proposal that is not a plain change says what it is before its title. */
export const KIND_WORDS = {
  EVOLVE_PROPOSAL_KIND_UNSPECIFIED: "",
  EVOLVE_PROPOSAL_KIND_CHANGE: "",
  EVOLVE_PROPOSAL_KIND_TRIAL: "Trial",
  EVOLVE_PROPOSAL_KIND_UNDO: "Undo",
} as const satisfies Record<EvolveProposalKind__Output, string>;

export function declined(proposal: EvolveProposal__Output): boolean {
  return proposal.decision?.decision === "EVOLVE_PROPOSAL_DECISION_DECLINE";
}

/** What Approve does, in the words both cards show it with. */
export const APPROVE_CONSEQUENCE = "Approving applies the change on Preview; nothing reaches Live until you publish.";

/** An offered proposal as those who only read see it: who decides. */
export const AWAITING_BUILDER = "Waiting for one of the app's builders to approve or decline it.";

/** What a decision the agent did not get asks of a person who builds, and what Tell the agent puts in the composer. */
export const NOT_TOLD = "The agent didn't get this. Tell it in the chat.";

/** A decision the agent did not get, as those who only read see it. */
export const UNTOLD = {
  approve: "Approved; the agent hasn't been told yet.",
  decline: "Declined; the agent hasn't been told yet.",
} as const satisfies Record<ProposalChoice, string>;

/** What an expired proposal leaves a person who builds: the agent can propose it again. */
export const ASK_AGAIN = "Ask the agent in the chat if you still want it.";

/** An applied proposal waiting on Preview: for a person who can publish, what to do; for others, where it is. */
export function waitingWords(canPublish: boolean): string {
  return canPublish ? "On Preview. Publish to ship it." : "On Preview, waiting to be published.";
}

/**
 * What Tell the agent puts in the composer: a decision the agent did not get, or for an expired proposal, a request to
 * redo it.
 */
export function tellAgentText(proposal: EvolveProposal__Output): string {
  const named = `the proposal "${proposal.title}" of review ${proposal.reviewId}`;
  if (proposal.status === "EVOLVE_PROPOSAL_STATUS_STALE")
    return `I still want ${named}, which expired before it shipped. Redo it on Preview.`;
  const reason = proposal.decision?.reason.trim();
  return declined(proposal)
    ? `I declined ${named}${reason ? `: ${reason}` : ""}. Discard it.`
    : `I approved ${named}. Apply it on Preview.`;
}

/**
 * What a closed proposal became, in plain words: why it will not ship, or how it was undone: by a rollback of Live, or
 * by an undo proposal that went Live (`undoLive`).
 */
export function closedWords(proposal: EvolveProposal__Output, undoLive = false): string {
  switch (proposal.status) {
    case "EVOLVE_PROPOSAL_STATUS_SUPERSEDED":
      return "A newer proposal replaced it before anyone decided.";
    case "EVOLVE_PROPOSAL_STATUS_STALE":
      return proposal.appliedCommit
        ? "Expired: it was on Preview for 30 days without being published."
        : "Expired: the agent didn't apply it within a week.";
    case "EVOLVE_PROPOSAL_STATUS_DISCARDED":
      return "Closed without shipping; the agent removed its work.";
    case "EVOLVE_PROPOSAL_STATUS_UNDONE":
      return undoLive
        ? "Undone: an undo proposal took it off Live and Preview."
        : "Undone: Live was rolled back. The change is still on Preview, so the next review proposes removing it.";
    case "EVOLVE_PROPOSAL_STATUS_UNSPECIFIED":
    case "EVOLVE_PROPOSAL_STATUS_OFFERED":
    case "EVOLVE_PROPOSAL_STATUS_APPROVED":
    case "EVOLVE_PROPOSAL_STATUS_DECLINED":
    case "EVOLVE_PROPOSAL_STATUS_APPLIED":
    case "EVOLVE_PROPOSAL_STATUS_LIVE":
      // Still under way; its card says where it is.
      return "";
  }
}

/**
 * How a goal's after value reads beside its before value: green with a check and "Better", red with an alert and
 * "Worse", or plain; the glyph says it to those who cannot tell the colours apart.
 */
export const MOVEMENT_WORDS = {
  better: { words: "Better", tone: "text-pass", glyph: "check" },
  worse: { words: "Worse", tone: "text-fail", glyph: "alert" },
  none: { words: "", tone: "text-ink", glyph: null },
} as const satisfies Record<
  NonNullable<GoalMovement> | "none",
  { words: string; tone: string; glyph: GlyphName | null }
>;

/** A goal's value before and after in its unit: "35% → 31.8%", the after value with "about" when estimated. */
export function goalDelta(goal: BacktestGoal): { before: string; after: string; range: string | null } {
  const unit = goalUnit(goal.metric);
  const format = (value: number | null) => formatGoalValue(value ?? undefined, unit);
  const range = estimatedRange(goal);
  return {
    before: format(goal.before),
    after: format(goal.after),
    range: range && `${format(range[0])} to ${format(range[1])}`,
  };
}
