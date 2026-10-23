// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useState, type ReactNode, type RefObject } from "react";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import type { ProposalChoice } from "@/lib/project/goals/proposals";
import { formatMonthDayTime } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import { RequestProblem } from "@/components/publishing/RequestProblem";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { Glyph } from "@/components/ui/Glyph";
import { DeclineDialog } from "./DeclineDialog";
import {
  APPROVE_CONSEQUENCE,
  ASK_AGAIN,
  AWAITING_BUILDER,
  closedWords,
  declined,
  NOT_TOLD,
  UNTOLD,
  waitingWords,
} from "./proposal-words";
import type { ProposalControls } from "./use-proposal-controls";

/**
 * Approve, and Decline with its dialog, for those who build; nothing for those who only operate. `onDecided` hears a
 * decision the server answered.
 */
export function DecisionButtons({
  title,
  controls,
  size = "md",
  onDecided,
}: {
  title: string;
  controls: ProposalControls;
  size?: "sm" | "md";
  onDecided: () => void;
}) {
  const [declining, setDeclining] = useState(false);
  const { decide, deciding, busy } = controls;
  if (!decide) return null;
  const send = async (choice: ProposalChoice, reason?: string) => {
    const answered = await decide(choice, reason);
    if (answered) onDecided();
    return answered;
  };
  return (
    <>
      <Button
        variant="primary"
        size={size}
        disabled={busy}
        title={APPROVE_CONSEQUENCE}
        onClick={() => void send("approve")}
      >
        {deciding === "approve" ? "Approving…" : "Approve"}
      </Button>
      <Button variant="danger" size={size} disabled={busy} onClick={() => setDeclining(true)}>
        Decline
      </Button>
      {declining && (
        <DeclineDialog
          title={title}
          declining={deciding === "decline"}
          failure={controls.failure?.choice === "decline" ? controls.failure.reason : null}
          onDecline={(reason) => send("decline", reason)}
          onClose={() => setDeclining(false)}
        />
      )}
    </>
  );
}

/**
 * The Goals tab's footer of an offered proposal, as the chat's tool approvals look: amber, "Approval required", what
 * approving does, then Approve and Decline.
 */
export function ProposalApproval({
  proposal,
  controls,
  onDecided,
}: {
  proposal: EvolveProposal__Output;
  controls: ProposalControls;
  onDecided: () => void;
}) {
  if (!controls.decide) return null;
  return (
    <section
      className="rounded-lg border border-busy-rule bg-busy-subtle p-3"
      aria-label="Approval required"
      data-testid="proposal-approval"
    >
      <Eyebrow>Approval required</Eyebrow>
      <p className="mt-2 text-body text-ink">{APPROVE_CONSEQUENCE}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        <DecisionButtons title={proposal.title} controls={controls} onDecided={onDecided} />
      </div>
    </section>
  );
}

/**
 * How the last decision on the proposal went, when it did not go through: the proposal changed since the page showed
 * it (the page has read it again), or the request failed; each only while the proposal stays where the answer found
 * it. A failed decline shows in its dialog instead. Every card shows it, and only the card that sent the decision
 * announces it, once: its refusal in a live region that stays in the page, and its failure as an alert.
 */
export function DecisionNote({ controls }: { controls: ProposalControls }) {
  const { stale, failure, announces } = controls;
  const refusal = stale && (
    <p className="text-caption text-busy" data-testid="proposal-stale">
      This proposal changed since you opened it.
    </p>
  );
  return (
    <>
      <div aria-live="polite" className="empty:sr-only">
        {announces && refusal}
      </div>
      {!announces && refusal}
      {!stale && failure?.choice === "approve" && (
        <RequestProblem operation="Approving the proposal" reason={failure.reason} announce={announces} />
      )}
    </>
  );
}

const LINE = "flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-body";

/**
 * Where a decided proposal is, in plain words, with the one thing a builder can do next: tell the agent a decision it
 * did not get, publish the change from Preview, undo it on Live (Tools ▸ Publishing at the release before), or ask the
 * agent again for an expired one. Those who only read get words without instructions they cannot follow, and who
 * decides an offered one. The line stays in the page, hidden while a builder decides; `stateRef` is the line the card
 * of a decision focuses once the proposal leaves offered, which reads the new state out. `undoLive`: an undo proposal
 * of it is Live.
 */
export function ProposalState({
  proposal,
  controls,
  undoLive = false,
  stateRef,
}: {
  proposal: EvolveProposal__Output;
  controls: ProposalControls;
  undoLive?: boolean;
  stateRef?: RefObject<HTMLParagraphElement | null>;
}) {
  const { words, tone, action } = stateLine(proposal, controls, undoLive);
  return (
    <p
      ref={stateRef}
      tabIndex={-1}
      className={cx(
        LINE,
        tone,
        "rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
        words === null && "sr-only",
      )}
      data-testid="proposal-state"
      data-status={proposal.status}
    >
      <span>{words}</span>
      {action}
    </p>
  );
}

/** The words, their colour and the next step of a proposal's state line; none for a builder while it is offered. */
function stateLine(
  proposal: EvolveProposal__Output,
  controls: ProposalControls,
  undoLive: boolean,
): { words: ReactNode; tone: string; action?: ReactNode } {
  const { undoHref, tellAgent, publish } = controls;
  const told = !proposal.notifyFailed;
  const tell = tellAgent && (
    <Button size="sm" onClick={tellAgent} data-testid="proposal-tell">
      <Glyph name="spark" className="size-3.5" />
      Tell the agent
    </Button>
  );
  const untold = {
    words: tellAgent ? NOT_TOLD : UNTOLD[declined(proposal) ? "decline" : "approve"],
    tone: "text-busy",
    action: tell,
  };
  switch (proposal.status) {
    case "EVOLVE_PROPOSAL_STATUS_OFFERED":
    case "EVOLVE_PROPOSAL_STATUS_UNSPECIFIED":
      // Those who build decide beside it; those who only read learn who does.
      return { words: controls.decide ? null : AWAITING_BUILDER, tone: "text-ink-dim" };
    case "EVOLVE_PROPOSAL_STATUS_APPROVED":
      return told ? { words: "Approved. The agent applies it on Preview.", tone: "text-ink-dim" } : untold;
    case "EVOLVE_PROPOSAL_STATUS_DECLINED":
      return told ? { words: <DeclinedWords proposal={proposal} />, tone: "text-ink-dim" } : untold;
    case "EVOLVE_PROPOSAL_STATUS_APPLIED":
      return {
        words: waitingWords(Boolean(publish)),
        tone: "text-ink-dim",
        action: publish && (
          <Button size="sm" variant="primary" onClick={publish} data-testid="proposal-publish">
            <Glyph name="upload" className="size-3.5" />
            Publish…
          </Button>
        ),
      };
    case "EVOLVE_PROPOSAL_STATUS_LIVE": {
      const since = timestampToISO(proposal.liveAt);
      return {
        words: `${since ? `Live since ${formatMonthDayTime(since)}.` : "Live."} Measured at the next review.`,
        tone: "text-ink-dim",
        action: undoHref && (
          <Link href={undoHref} className={buttonStyle("secondary", "sm")} data-testid="proposal-undo">
            Undo…
          </Link>
        ),
      };
    }
    case "EVOLVE_PROPOSAL_STATUS_DISCARDED":
      if (declined(proposal)) return { words: <DeclinedWords proposal={proposal} />, tone: "text-ink-dim" };
      return { words: closedWords(proposal), tone: "text-ink-faint" };
    case "EVOLVE_PROPOSAL_STATUS_STALE":
      return {
        words: tellAgent ? `${closedWords(proposal)} ${ASK_AGAIN}` : closedWords(proposal),
        tone: "text-ink-faint",
        action: tellAgent && (
          <Button size="sm" onClick={tellAgent} data-testid="proposal-ask-again">
            <Glyph name="spark" className="size-3.5" />
            Ask the agent
          </Button>
        ),
      };
    case "EVOLVE_PROPOSAL_STATUS_UNDONE":
    case "EVOLVE_PROPOSAL_STATUS_SUPERSEDED":
      return { words: closedWords(proposal, undoLive), tone: "text-ink-faint" };
  }
}

/** Declined, with the reason the person gave. */
function DeclinedWords({ proposal }: { proposal: EvolveProposal__Output }) {
  const reason = proposal.decision?.reason.trim();
  if (!reason) return "Declined.";
  return (
    <>
      Declined: <q className="text-ink">{reason}</q>
    </>
  );
}
