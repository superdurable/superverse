// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useMemo, useState } from "react";
import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import { useEvolve } from "@/lib/project/goals/EvolveProvider";
import { goalStatement } from "@/lib/project/goals/goal-values";
import { movedGoals, parseBacktest } from "@/lib/project/goals/proposal-backtest";
import { chatProposal, proposalAnchor, stillMarked } from "@/lib/project/goals/proposals";
import { useComposerDraft } from "@/lib/session/ComposerDraftProvider";
import { CHAT_HEADING_ID } from "@/components/frame/ChatColumn";
import { useFrameLinks } from "@/components/frame/use-frame-links";
import { proposalBadge } from "./proposal-words";
import { ProposalDockCard } from "./ProposalDockCard";
import { useProposalControls } from "./use-proposal-controls";

/** The most goals the chat's card names, besides every one that gets worse. */
const DELTAS = 3;

/**
 * The Builder's orchestration of the chat's proposal card: from the shared goal measurement it picks the proposal
 * that waits for a decision, the approved one until it ships, or one whose decision the agent did not get, and hands
 * the card its goals, controls and links. A decision the server refused as stale keeps the proposal it named in view
 * instead, with where it went and why nothing happened, until it moves on, the next decision or a fold. No card shows
 * while there is none. When the card leaves after a decision, or with the focus in it, a live region that stays in the
 * chat says where the proposal went, and the composer takes the focus, or the chat's heading when the composer cannot.
 */
export function ProposalChat({
  projectId,
  persistentQuery,
}: {
  projectId: string;
  persistentQuery?: readonly string[];
}) {
  const { measurement, decision } = useEvolve();
  const requestFocus = useComposerDraft()?.requestFocus;
  // The proposal whose card left the chat while the person was on it.
  const [left, setLeft] = useState<string | null>(null);
  const leave = useCallback(
    (reviewId: string) => {
      setLeft(reviewId);
      requestFocus?.();
      // A composer that cannot take the focus now (it waits for a model key, or a request runs) leaves it to the
      // chat's heading, where it shows.
      requestAnimationFrame(() => {
        if (document.activeElement === null || document.activeElement === document.body)
          document.getElementById(CHAT_HEADING_ID)?.focus();
      });
    },
    [requestFocus],
  );
  const evolve = measurement.state === "ready" ? measurement.evolve : null;
  const refused = evolve?.proposals.find((proposal) => stillMarked(decision.stale, proposal));
  const proposal = refused ?? chatProposal(evolve);
  const gone = left !== proposal?.reviewId ? evolve?.proposals.find((item) => item.reviewId === left) : undefined;
  return (
    <>
      <p role="status" className="sr-only" data-testid="proposal-chat-status">
        {gone && `${proposalBadge(gone).words}: ${gone.title}`}
      </p>
      {evolve && proposal && (
        <BoundCard
          projectId={projectId}
          evolve={evolve}
          proposal={proposal}
          persistentQuery={persistentQuery}
          onFold={refused ? decision.dismissStale : undefined}
          onLeave={leave}
        />
      )}
    </>
  );
}

function BoundCard({
  projectId,
  evolve,
  proposal,
  persistentQuery,
  onFold,
  onLeave,
}: {
  projectId: string;
  evolve: Evolve__Output;
  proposal: EvolveProposal__Output;
  persistentQuery?: readonly string[];
  onFold?: () => void;
  onLeave: (reviewId: string) => void;
}) {
  const links = useFrameLinks(projectId, "goals", persistentQuery);
  const controls = useProposalControls(proposal, links);
  const deltas = useMemo(() => {
    const backtest = parseBacktest(proposal.backtestJson);
    const goals = evolve.goals?.goals ?? [];
    return (backtest ? movedGoals(backtest, goals, DELTAS) : []).map(({ goal, movement }) => {
      const current = goals.find((item) => item.id === goal.id);
      // Several goals Live no longer has would read alike, so each leads with its ID.
      const statement = current ? goalStatement(current) : `${goal.id}: ${goalStatement(undefined).toLowerCase()}`;
      return { goal, movement, statement };
    });
  }, [evolve.goals, proposal.backtestJson]);
  // Proposals show on Live's Goals tab, whose list opens, scrolls to and focuses the row its anchor names.
  const anchor = proposalAnchor(proposal.reviewId);
  return (
    <ProposalDockCard
      key={proposal.reviewId}
      proposal={proposal}
      deltas={deltas}
      controls={controls}
      details={{ href: `${links.environmentHref("live")}#${anchor}`, anchor }}
      onFold={onFold}
      onLeave={() => onLeave(proposal.reviewId)}
    />
  );
}
