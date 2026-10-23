// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { EvolveProposalStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalStatus";

/** The agent's two answers to an offered proposal that a person gives. */
export type ProposalChoice = "approve" | "decline";

/** The statuses whose next move is the agent's: it applies an approved proposal and discards a declined one. */
const AGENT_MOVES: readonly EvolveProposalStatus__Output[] = [
  "EVOLVE_PROPOSAL_STATUS_APPROVED",
  "EVOLVE_PROPOSAL_STATUS_DECLINED",
];

/** The statuses the chat keeps a proposal in view for: waiting for a decision, applying, or waiting to be published. */
const IN_CHAT: readonly EvolveProposalStatus__Output[] = [
  "EVOLVE_PROPOSAL_STATUS_OFFERED",
  "EVOLVE_PROPOSAL_STATUS_APPROVED",
  "EVOLVE_PROPOSAL_STATUS_APPLIED",
];

/** Whether the chat keeps `proposal` in view: in IN_CHAT, or decided while the agent did not get the decision. */
function inChat(proposal: EvolveProposal__Output): boolean {
  return IN_CHAT.includes(proposal.status) || (AGENT_MOVES.includes(proposal.status) && proposal.notifyFailed);
}

/**
 * Whether the measurement is about to change without a person: a review runs, or the agent was told of a decision
 * it has yet to carry out. One the agent was not told of waits for the person to tell it, unless they did in the
 * chat (`told`, by review).
 */
export function agentAtWork(evolve: Evolve__Output, told: ReadonlySet<string>): boolean {
  return (
    Boolean(evolve.activeReviewId) ||
    evolve.proposals.some(
      (proposal) => AGENT_MOVES.includes(proposal.status) && (!proposal.notifyFailed || told.has(proposal.reviewId)),
    )
  );
}

/**
 * Whether the measurement has yet to follow Live at revision `liveKey`: the Project moves Live first, and the
 * measurement then reads the new build's goals, stores them with the build's commit and moves applied and live
 * proposals. Never while there is no Live (`liveKey` empty).
 */
export function behindLive(evolve: Evolve__Output, liveKey: string): boolean {
  if (!liveKey) return false;
  return evolve.live?.revision !== liveKey || evolve.goals?.commit !== evolve.live.buildCommit;
}

/** The newest proposal that waits for a person's Approve or Decline; null when none does. Proposals come newest first. */
export function offeredProposal(evolve: Evolve__Output | null): EvolveProposal__Output | null {
  return evolve?.proposals.find((proposal) => proposal.status === "EVOLVE_PROPOSAL_STATUS_OFFERED") ?? null;
}

/**
 * The proposal the chat keeps in view: the offered one, else the newest one that waits to ship (approved or applied)
 * or whose decision the agent did not get, approved or declined, which a person has to tell it.
 */
export function chatProposal(evolve: Evolve__Output | null): EvolveProposal__Output | null {
  return offeredProposal(evolve) ?? evolve?.proposals.find(inChat) ?? null;
}

/** A proposal a decision named, at a revision: the one a decision was sent at, or the one a stale refusal found. */
export interface ProposalMark {
  reviewId: string;
  revision: string;
}

/**
 * Whether `proposal` is the one `mark` names and has not moved on since: what a failed or refused decision said about
 * it still holds. Revisions grow with every change, so a later read of a newer revision ends it.
 */
export function stillMarked(mark: ProposalMark | null, proposal: EvolveProposal__Output): boolean {
  return mark !== null && mark.reviewId === proposal.reviewId && BigInt(proposal.revision) <= BigInt(mark.revision);
}

/** Whether an undo proposal of the proposal of `reviewId` is Live, which took its change off Live and Preview. */
export function undoIsLive(evolve: Evolve__Output, reviewId: string): boolean {
  return evolve.proposals.some(
    (proposal) =>
      proposal.kind === "EVOLVE_PROPOSAL_KIND_UNDO" &&
      proposal.undoes === reviewId &&
      proposal.status === "EVOLVE_PROPOSAL_STATUS_LIVE",
  );
}

/** The anchor of a proposal's row on the Goals tab. */
export function proposalAnchor(reviewId: string): string {
  return `proposal-${reviewId}`;
}

/** The sections of the proposal document the Goals tab shows under the backtest, in this order. */
const SHOWN_SECTIONS = ["Why", "What changes", "Open runs", "Risks", "Undo"];

/**
 * The parts of the agent's proposal document that the backtest's numbers do not already show: Why (the findings in
 * full), What changes (in plain words, then the files), Open runs, Risks and Undo, each under its heading. A document
 * that has none of them shows whole, without its title, which the row already shows. Its headings sit at the level of
 * the row's own (h4), under the list's.
 */
export function proposalSections(markdown: string): string {
  const sections = new Map<string, string>();
  for (const part of markdown.split(/^(?=## )/m)) {
    const heading = /^## +(.+?)\s*$/m.exec(part);
    if (heading && part.startsWith("## ")) sections.set(heading[1].toLowerCase(), part.trim());
  }
  const shown = SHOWN_SECTIONS.map((name) => sections.get(name.toLowerCase())).filter(Boolean);
  return underRow(shown.length > 0 ? shown.join("\n\n") : markdown.replace(/^\s*# .*$/m, "").trim());
}

/** `markdown` with every heading above h4 made an h4, outside fenced code. */
function underRow(markdown: string): string {
  let fenced = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (/^ {0,3}(```|~~~)/.test(line)) fenced = !fenced;
      return fenced ? line : line.replace(/^ {0,3}#{1,3}(?=\s)/, "####");
    })
    .join("\n");
}
