// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveProposal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposal";
import { focusAnchorWithin, useAnchorRequests } from "@/lib/browser/hash-focus";
import { parseBacktest } from "@/lib/project/goals/proposal-backtest";
import { proposalAnchor, proposalSections, undoIsLive } from "@/lib/project/goals/proposals";
import { useProposalDocument } from "@/lib/project/goals/use-proposal-document";
import { timestampToISO } from "@/lib/shared/protobuf";
import { MarkdownContent } from "@/components/agent";
import { useFrameLinks } from "@/components/frame/use-frame-links";
import { DecisionNote, ProposalApproval, ProposalState } from "@/components/project/proposal/ProposalDecision";
import { KIND_WORDS, proposalBadge } from "@/components/project/proposal/proposal-words";
import { useDecisionFocus } from "@/components/project/proposal/use-decision-focus";
import { useProposalControls } from "@/components/project/proposal/use-proposal-controls";
import { HistoryList, HistoryRow } from "@/components/publishing/HistoryRows";
import { RequestProblem } from "@/components/publishing/RequestProblem";
import { Badge } from "@/components/ui/badge/Badge";
import { Button } from "@/components/ui/Button";
import { ProposalBacktest } from "./ProposalBacktest";

/**
 * The agent's proposals, newest first, with Publishing's history rows: the title, where it is (a badge that a narrow
 * row never cuts) and when its review proposed it. A row opens to the why in one line, each goal before and after in
 * the backtest, the backtest's shares, and why, what changes, open runs, risks and undo from the agent's document; then
 * Approve and Decline while it is offered, or where it went, which a decision announces and focuses. A link to a row's
 * anchor (`proposalAnchor`) opens it, scrolls to it and focuses it, from the URL's hash when the list first shows or
 * from a jump (`focusAnchor`). Nothing shows before the first proposal.
 */
export function GoalProposals({ projectId, evolve }: { projectId: string; evolve: Evolve__Output }) {
  // Ages are read when the list first renders.
  const [now] = useState(() => Date.now());
  // The rows open now, and those ever opened, whose documents are read and kept.
  const [shown, setShown] = useState<ReadonlySet<string>>(() => new Set());
  const [opened, setOpened] = useState<ReadonlySet<string>>(() => new Set());
  const toggle = (id: string, open: boolean) => {
    setShown((current) => (current.has(id) === open ? current : toggled(current, id)));
    if (open) setOpened((current) => (current.has(id) ? current : new Set(current).add(id)));
  };
  const list = useRef<HTMLOListElement>(null);
  const [requested, setRequested] = useState<string | null>(null);
  const anchored = (anchor: string) =>
    evolve.proposals.find((proposal) => proposalAnchor(proposal.reviewId) === anchor);
  useAnchorRequests(
    (anchor) => anchored(anchor) !== undefined,
    (anchor) => {
      const proposal = anchored(anchor);
      if (!proposal) return;
      toggle(proposal.reviewId, true);
      setRequested(anchor);
    },
  );
  useEffect(() => {
    const container = list.current;
    if (!requested || !container) return;
    const frame = requestAnimationFrame(() => {
      focusAnchorWithin(container, requested);
      setRequested(null);
    });
    return () => cancelAnimationFrame(frame);
  }, [requested]);
  if (evolve.proposals.length === 0) return null;
  return (
    <HistoryList label="Proposals" heading="Proposals" testId="goal-proposals">
      <ol ref={list} className="divide-y divide-line-faint">
        {evolve.proposals.map((proposal) => {
          const badge = proposalBadge(proposal);
          const kind = KIND_WORDS[proposal.kind];
          const id = proposal.reviewId;
          return (
            <li key={id} data-testid="goal-proposal" data-status={proposal.status}>
              <HistoryRow
                at={timestampToISO(proposal.offeredAt)}
                now={now}
                open={shown.has(id)}
                onToggle={(open) => toggle(id, open)}
                anchor={proposalAnchor(id)}
                wide
                trailing={<Badge tone={badge.tone}>{badge.words}</Badge>}
                detail={
                  <ProposalDetail projectId={projectId} evolve={evolve} proposal={proposal} opened={opened.has(id)} />
                }
              >
                {kind && <span className="text-ink-faint">{kind}: </span>}
                <span className="text-ink">{proposal.title}</span>
              </HistoryRow>
            </li>
          );
        })}
      </ol>
    </HistoryList>
  );
}

/** `set` with `id` added when it lacks it, else removed. */
function toggled(set: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(set);
  if (!next.delete(id)) next.add(id);
  return next;
}

/** An open proposal row: what it is for, its numbers, the agent's document and its decision or state. */
function ProposalDetail({
  projectId,
  evolve,
  proposal,
  opened,
}: {
  projectId: string;
  evolve: Evolve__Output;
  proposal: EvolveProposal__Output;
  /** The row was opened, so its document is read. */
  opened: boolean;
}) {
  const controls = useProposalControls(proposal, useFrameLinks(projectId, "goals"));
  const backtest = useMemo(() => parseBacktest(proposal.backtestJson), [proposal.backtestJson]);
  const { document, retry } = useProposalDocument(projectId, opened ? proposal.reviewId : null);
  const { stateRef, decided } = useDecisionFocus(proposal.status);
  return (
    <div className="grid gap-4" data-testid="proposal-detail">
      {proposal.summary && <p className="text-body text-ink">{proposal.summary}</p>}
      {backtest ? (
        <ProposalBacktest backtest={backtest} goals={evolve.goals?.goals ?? []} />
      ) : (
        <p className="text-ink-faint">The backtest&apos;s numbers could not be read.</p>
      )}
      {document.state === "ready" && <MarkdownContent value={proposalSections(document.markdown)} />}
      {document.state === "reading" && opened && <p className="text-ink-faint">Reading the agent&apos;s proposal…</p>}
      {document.state === "absent" && (
        <p className="text-ink-faint">The agent&apos;s write-up of it is no longer kept.</p>
      )}
      {document.state === "failed" && (
        <div className="grid justify-items-start gap-2">
          <RequestProblem operation="Reading the proposal" reason={document.error} />
          <Button size="sm" onClick={retry}>
            Read it again
          </Button>
        </div>
      )}
      {proposal.status === "EVOLVE_PROPOSAL_STATUS_OFFERED" && (
        <ProposalApproval proposal={proposal} controls={controls} onDecided={decided} />
      )}
      <ProposalState
        proposal={proposal}
        controls={controls}
        undoLive={undoIsLive(evolve, proposal.reviewId)}
        stateRef={stateRef}
      />
      <DecisionNote controls={controls} />
    </div>
  );
}
