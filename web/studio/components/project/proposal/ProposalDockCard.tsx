// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useState } from "react";
import { focusAnchor } from "@/lib/browser/hash-focus";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { BacktestGoal, GoalMovement } from "@/lib/project/goals/proposal-backtest";
import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { Glyph } from "@/components/ui/Glyph";
import { GoalAfterValue } from "./GoalAfterValue";
import { DecisionButtons, DecisionNote, ProposalState } from "./ProposalDecision";
import { goalDelta, KIND_WORDS, MOVEMENT_WORDS } from "./proposal-words";
import type { ProposalControls } from "./use-proposal-controls";
import { useDecisionFocus } from "./use-decision-focus";

/**
 * A goal the change moves, with the statement it shows and whether it gets better or worse; a goal Live's goals no
 * longer have is named by its ID in the statement.
 */
interface Delta {
  goal: BacktestGoal;
  movement: GoalMovement;
  statement: string;
}

/**
 * The agent's proposal above the chat's composer, which stays usable: the title, why (in full while it waits for a
 * decision, so its risks show beside Approve, then in one line), the goals it moves, a worse one in red, Approve and
 * Decline while it is offered, then where it is until it ships, and Details on the Goals tab. It renders what the
 * Builder hands it and fetches nothing; it folds to one line, and the Builder keys it by proposal. After a decision
 * the focus moves to the state line, or, when the card leaves, to `onLeave`; `onFold` hears the person fold it.
 */
export function ProposalDockCard({
  proposal,
  deltas,
  controls,
  details,
  onFold,
  onLeave,
}: {
  proposal: EvolveProposal__Output;
  deltas: readonly Delta[];
  controls: ProposalControls;
  /** The proposal's row on the Goals tab: the link and the row's anchor, which the link jumps to. */
  details: { href: string; anchor: string };
  onFold?: () => void;
  onLeave?: () => void;
}) {
  const [open, setOpen] = useState(true);
  const offered = proposal.status === "EVOLVE_PROPOSAL_STATUS_OFFERED";
  const kind = KIND_WORDS[proposal.kind];
  const { stateRef, cardRef, decided } = useDecisionFocus(proposal.status, onLeave);
  return (
    <div className="border-t border-line bg-panel px-4 py-2.5" data-testid="proposal-dock">
      <section
        ref={cardRef}
        aria-label="Proposal"
        className={cx(
          "rounded-lg border px-3 py-2",
          offered ? "border-busy-rule bg-busy-subtle" : "border-line-soft bg-panel",
        )}
        data-status={proposal.status}
      >
        <div className="flex items-center gap-2">
          <Eyebrow className="shrink-0">Proposal</Eyebrow>
          <p className="min-w-0 flex-1 truncate text-body font-medium text-ink" title={proposal.title}>
            {kind && <span className="text-ink-faint">{kind}: </span>}
            {proposal.title}
          </p>
          <button
            type="button"
            aria-expanded={open}
            aria-label={open ? "Fold the proposal" : "Show the proposal"}
            className="-mr-1 shrink-0 rounded-md p-1 text-ink-faint hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
            onClick={() => {
              if (open) onFold?.();
              setOpen(!open);
            }}
          >
            <Glyph name="chevronDown" className={cx("size-4 transition-transform", !open && "rotate-180")} />
          </button>
        </div>
        {open && (
          <div className="mt-1 grid grid-cols-1 gap-2">
            {proposal.summary && (
              <p
                className={cx("text-caption text-ink-dim", !offered && "truncate")}
                title={offered ? undefined : proposal.summary}
                data-testid="proposal-summary"
              >
                {proposal.summary}
              </p>
            )}
            {deltas.length > 0 && (
              <ul className="grid grid-cols-1 gap-0.5 text-caption" aria-label="What it changes">
                {deltas.map((delta) => (
                  <DeltaRow key={delta.goal.id} {...delta} />
                ))}
              </ul>
            )}
            <div className="flex flex-wrap items-center gap-2">
              {offered && <DecisionButtons title={proposal.title} controls={controls} size="sm" onDecided={decided} />}
              <ProposalState proposal={proposal} controls={controls} stateRef={stateRef} />
              <Link
                href={details.href}
                onClick={() => focusAnchor(details.anchor)}
                className="ml-auto inline-flex items-center gap-0.5 text-caption text-accent hover:underline"
              >
                Details
                <Glyph name="chevronRight" className="size-3.5" />
              </Link>
            </div>
            <DecisionNote controls={controls} />
          </div>
        )}
      </section>
    </div>
  );
}

/** One goal before and after: the after value better or worse, as the Goals tab shows it. */
function DeltaRow({ goal, movement, statement }: Delta) {
  const delta = goalDelta(goal);
  return (
    <li className="flex items-baseline gap-2" data-testid="proposal-delta" data-movement={movement ?? "none"}>
      <span className="min-w-0 flex-1 truncate text-ink-dim" title={statement}>
        {statement}
      </span>
      <span className="shrink-0 tabular-nums">
        <span className="text-ink-dim">{delta.before} → </span>
        <GoalAfterValue
          movement={movement}
          className="font-medium"
          title={[MOVEMENT_WORDS[movement ?? "none"].words, delta.range].filter(Boolean).join(", ") || undefined}
        >
          {delta.range ? `about ${delta.after}` : delta.after}
        </GoalAfterValue>
      </span>
    </li>
  );
}
