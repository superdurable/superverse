// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useId } from "react";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { useEvolve } from "@/lib/project/goals/EvolveProvider";
import { stillMarked, type ProposalChoice } from "@/lib/project/goals/proposals";
import type { useFrameLinks } from "@/components/frame/use-frame-links";
import { openPublishPopover } from "@/components/publishing/publish/publish-opener";
import { tellAgentText } from "./proposal-words";

/**
 * What a proposal card can do and how its last decision went. The Builder's orchestration builds it for each card,
 * which only renders it, so the Goals tab and the chat act on the same proposal at the revision they show.
 */
export interface ProposalControls {
  /** Approve or Decline at the revision shown, resolving to whether the server answered; absent for operators. */
  decide?: (choice: ProposalChoice, reason?: string) => Promise<boolean>;
  /** The decision in flight on this proposal. */
  deciding: ProposalChoice | null;
  /** A decision is in flight on some proposal of the page, which holds the others back. */
  busy: boolean;
  /** The server refused the last decision on it, as the proposal changed since the page showed it; until it moves on. */
  stale: boolean;
  /** The last decision on it that failed, and why, until the proposal moves on. */
  failure: { choice: ProposalChoice; reason: unknown } | null;
  /** This card sent the page's last decision, so it alone announces a refusal or failure, which both cards show. */
  announces: boolean;
  /**
   * For those who build: puts the decision the agent did not get into the composer and follows it until done, or, for
   * an expired proposal, a request to redo it.
   */
  tellAgent?: () => void;
  /** Opens the bar's Publish popover on the applied commit, else Tools ▸ Publishing (no Publish, or one runs). */
  publish?: () => void;
  /** Tools ▸ Publishing at the rollback to the release Live ran before a live proposal, or at Publishing without it. */
  undoHref?: string;
}

/**
 * The controls of one card's `proposal` for this page's person: decisions through the Builder's shared
 * `DecideEvolveProposal`, which reads `GetEvolve` again for every card; Tell the agent through the composer
 * (`askAgent`), after which the Builder reads `GetEvolve` until the agent carries the decision out; Publish through the
 * bar's popover, else Tools ▸ Publishing; Undo at Publishing's release Live ran before the proposal went live, as the
 * measurement recorded it (`previousLiveReleaseId`), else at Publishing. Operators get none of the actions.
 */
export function useProposalControls(
  proposal: EvolveProposal__Output,
  links: Pick<ReturnType<typeof useFrameLinks>, "toolHref" | "releaseHref">,
): ProposalControls {
  const { decision, toldAgent } = useEvolve();
  const { askAgent, canIntervene } = useFlowHost();
  const router = useRouter();
  const card = useId();
  const publishingHref = links.toolHref("publishing");
  const before = proposal.previousLiveReleaseId;
  const undoHref = before ? links.releaseHref(before) : publishingHref;
  return {
    decide: canIntervene ? (choice, reason = "") => decision.decide(proposal, choice, reason, card) : undefined,
    deciding: decision.target?.reviewId === proposal.reviewId ? decision.deciding : null,
    busy: decision.deciding !== null,
    stale: stillMarked(decision.stale, proposal),
    failure: stillMarked(decision.target, proposal) ? decision.failure : null,
    announces: decision.target?.source === card,
    tellAgent: askAgent
      ? () => {
          askAgent("Goals", tellAgentText(proposal));
          if (proposal.status !== "EVOLVE_PROPOSAL_STATUS_STALE") toldAgent(proposal.reviewId);
        }
      : undefined,
    publish: canIntervene
      ? () => {
          if (!openPublishPopover({ mode: "version", commit: proposal.appliedCommit })) router.push(publishingHref);
        }
      : undefined,
    undoHref: canIntervene && proposal.status === "EVOLVE_PROPOSAL_STATUS_LIVE" ? undoHref : undefined,
  };
}
