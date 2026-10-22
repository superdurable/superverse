// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo } from "react";
import { Badge, type BadgeTone } from "@/components/ui/badge/Badge";
import type {
  ActivityStreamEvent,
  ConfirmedRequirements,
  LiveTextEntry,
  SequencedMessage,
} from "@/lib/session/agent-types";
import {
  RecordedRequirementsLine,
  recordedRequirementsVersions,
  startRecordedKey,
} from "@/components/agent/interaction/RecordedRequirements";
import { ConversationClockProvider } from "./ConversationClockProvider";
import { ConversationMessage, type NotificationLinks } from "./ConversationMessage";
import { MarkdownContent } from "./MarkdownContent";
import { EarlierActivity, WorkLog } from "./WorkLog";
import {
  buildConversationPresentation,
  type ConversationPendingWait,
  type ConversationQuestionItem,
  type ConversationStreamState,
} from "./conversation-presentation";

export interface ConversationViewProps {
  messages: readonly SequencedMessage[];
  reasoning: readonly LiveTextEntry[];
  activities: readonly ActivityStreamEvent[];
  assistant: LiveTextEntry | null;
  pendingWaits: readonly ConversationPendingWait[];
  isModelRunning: boolean;
  isExecutionLive: boolean;
  streamState: ConversationStreamState;
  /** Where the page lets a person act on a notification; a notice without links shows none. */
  notificationLinks?: NotificationLinks;
  /** The current version of the requirements, whose recording each turn that recorded a version shows once. */
  confirmedRequirements?: ConfirmedRequirements | null;
  /** The call of a requirements card a Session still waits at, which is not recorded yet. */
  waitingRequirementsCallId?: string | null;
  /** The entry beside which the Session's start recorded version 1 itself, as one created from an example does. */
  requirementsStartSequence?: number | null;
}

/** How a question of the history ended, in words, and its badge tone. */
function questionState(item: ConversationQuestionItem): { label: string; tone: BadgeTone } {
  switch (item.status) {
    case "pending":
      return { label: "Waiting for your answer", tone: "busy" };
    case "answered":
      return { label: "Answered", tone: "pass" };
    case "cancelled":
      return { label: "Cancelled", tone: "neutral" };
    case "failed":
      return { label: "No longer waiting", tone: "neutral" };
  }
}

/** A question the agent asked: how it ended and its first question. */
function QuestionCard({ item }: { item: ConversationQuestionItem }) {
  const { label, tone } = questionState(item);
  return (
    <article
      aria-label="Question"
      className="flex flex-col items-start gap-1.5 rounded-lg border border-line-soft bg-panel px-3 py-2.5 text-body text-ink-dim"
      data-status={item.status}
    >
      <Badge tone={tone}>{label}</Badge>
      {item.target ? <p className="wrap-anywhere">{item.target}</p> : null}
    </article>
  );
}

/** Whether a message is a system marker, which its turn's work log shows instead of the conversation. */
function isMarker(entry: SequencedMessage): boolean {
  return entry.message.role === "system" && !entry.message.executorChanged && entry.message.content !== "";
}

function hasUnfinishedTools(messages: readonly SequencedMessage[]): boolean {
  const results = new Set(messages.map((entry) => entry.message.toolCallId).filter(Boolean));
  return messages.some((entry) => entry.message.toolCalls.some((call) => !results.has(call.id)));
}

export function ConversationView({
  messages,
  reasoning,
  activities,
  assistant,
  pendingWaits,
  isModelRunning,
  isExecutionLive,
  streamState,
  notificationLinks,
  confirmedRequirements = null,
  waitingRequirementsCallId = null,
  requirementsStartSequence = null,
}: ConversationViewProps) {
  const unfinishedTools = useMemo(() => hasUnfinishedTools(messages), [messages]);
  const isLive =
    isExecutionLive &&
    (isModelRunning || pendingWaits.length > 0 || (assistant !== null && !assistant.isComplete) || unfinishedTools);
  const presentation = useMemo(
    () =>
      buildConversationPresentation({ messages, reasoning, activities, pendingWaits, isModelRunning, isExecutionLive }),
    [activities, isModelRunning, isExecutionLive, messages, pendingWaits, reasoning],
  );
  const recorded = useMemo(
    () =>
      recordedRequirementsVersions(
        messages,
        confirmedRequirements,
        waitingRequirementsCallId,
        requirementsStartSequence,
      ),
    [confirmedRequirements, messages, requirementsStartSequence, waitingRequirementsCallId],
  );

  return (
    <ConversationClockProvider active={isLive}>
      <section className="flex flex-col gap-4" aria-label="Conversation">
        <EarlierActivity entries={presentation.earlierActivity} />
        {presentation.turns.map((turn) => (
          <section className="flex flex-col gap-4" aria-label="Conversation turn" key={turn.key}>
            {turn.messages
              .filter((entry) => !isMarker(entry))
              .map((entry) => (
                <ConversationMessage entry={entry} key={entry.sequence} notificationLinks={notificationLinks} />
              ))}
            <WorkLog turn={turn} markers={turn.messages.filter(isMarker)} />
            {turn.messages.flatMap(({ sequence, message }) =>
              [startRecordedKey(sequence), ...message.toolCalls.map((call) => call.id)].flatMap((key) => {
                const version = recorded.get(key);
                return version ? [<RecordedRequirementsLine recorded={version} key={key} />] : [];
              }),
            )}
            {turn.questions.map((question) => (
              <QuestionCard item={question} key={question.key} />
            ))}
          </section>
        ))}
        {assistant?.value ? (
          <div className="text-body" aria-busy={streamState === "streaming"} data-testid="live-assistant-message">
            <MarkdownContent value={assistant.value} />
          </div>
        ) : null}
      </section>
    </ConversationClockProvider>
  );
}
