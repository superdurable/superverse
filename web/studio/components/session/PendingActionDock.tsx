// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { PendingQuestionBatch, RequirementsCard } from "@/components/agent";
import { cx } from "@/components/ui/cx";
import type { AgentDescription, RequirementsDecision, UserInputAnswer } from "@/lib/session/agent-types";
import { useBriefOnScreen } from "@/lib/session/design/use-brief-on-screen";
import { ToolApprovalCard } from "./ToolApprovalCard";
import { useCardDesign } from "./use-card-design";

/**
 * Keeps blocking Agent actions above the composer so Approve, Reject and Submit stay on screen; it
 * is furniture like the header stripe, so it takes the panel and scrolls on its own. In a short
 * panel it shrinks with its cards, whose middle rows scroll, so the composer below stays visible.
 */
const PENDING_ACTION_DOCK = cx(
  "flex max-h-[min(68dvh,44rem)] min-h-28 flex-col overflow-x-hidden overflow-y-auto overscroll-contain border-t border-line bg-panel px-4 py-3 max-md:max-h-[min(72dvh,36rem)] [&>*+*]:mt-3",
);

type PendingApproval = NonNullable<AgentDescription["pendingApproval"]>;
type PendingInput = NonNullable<AgentDescription["pendingUserInput"]>;

/**
 * What the Agent waits for: a tool approval, requirements card or question batch.
 * With answeredBy, input cards show who answers them instead of their controls. A requirements card
 * is left out while the Flow tab shows its Process Brief.
 */
export function PendingActionDock({
  approval,
  input,
  confirmedRevision,
  disabled,
  approving,
  answering,
  answerAwaitingReconciliation,
  answeredBy,
  onApprove,
  onDecide,
  onAnswer,
}: {
  approval: PendingApproval | null;
  input: PendingInput | null;
  /** The revision of the confirmed requirements, 0 before any confirmation. */
  confirmedRevision: number;
  disabled: boolean;
  approving: boolean;
  answering: boolean;
  answerAwaitingReconciliation: boolean;
  answeredBy?: string;
  onApprove: (callId: string, approved: boolean) => void;
  onDecide: (callId: string, decision: RequirementsDecision) => void;
  onAnswer: (callId: string, answers: readonly UserInputAnswer[]) => void;
}) {
  const designChanges = useCardDesign();
  // The Brief on the Flow tab holds the card, so the dock leaves out its pointer while the Brief shows.
  const card = useBriefOnScreen() ? null : (input?.requirements ?? null);
  const questions = input && !input.requirements ? input : null;
  if (!approval && !card && !questions) return null;
  const inputDisabled = disabled || answerAwaitingReconciliation;
  const submitting = answering || answerAwaitingReconciliation;
  return (
    <div className={PENDING_ACTION_DOCK}>
      {approval && (
        <ToolApprovalCard
          toolName={approval.toolName}
          argumentsJson={approval.argumentsJson}
          disabled={disabled}
          isSubmitting={approving}
          onApprove={() => onApprove(approval.callId, true)}
          onReject={() => onApprove(approval.callId, false)}
        />
      )}
      {input && card && (
        <RequirementsCard
          key={input.callId}
          requirements={card}
          version={confirmedRevision + 1}
          disabled={inputDisabled}
          isSubmitting={submitting}
          answeredBy={answeredBy}
          designChanges={designChanges}
          onDecide={(decision) => onDecide(input.callId, decision)}
        />
      )}
      {questions && (
        <PendingQuestionBatch
          key={questions.callId}
          questions={questions.questions}
          disabled={inputDisabled}
          isSubmitting={submitting}
          answeredBy={answeredBy}
          onSubmit={(answers) => onAnswer(questions.callId, answers)}
        />
      )}
    </div>
  );
}
