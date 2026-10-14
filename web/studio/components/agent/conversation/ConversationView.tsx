// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo } from "react";
import { cx } from "@/components/ui/cx";
import type { ActivityStreamEvent, LiveTextEntry, SequencedMessage } from "@/lib/session/agent-types";
import { ConversationClockProvider } from "./ConversationClockProvider";
import { ConversationMessage, type NotificationLinks } from "./ConversationMessage";
import { MarkdownContent } from "./MarkdownContent";
import { OperationStatus, WorkLog } from "./WorkLog";
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
}

/** A bordered block of the transcript: live assistant text and activity disclosures. */
const CARD = cx("rounded-lg border border-line-soft bg-panel px-3 py-2.5");

function QuestionCard({ item }: { item: ConversationQuestionItem }) {
  return (
    <article
      className="my-3 rounded-xl border border-line-soft bg-panel px-4 py-3.5 text-xs/normal text-ink-dim"
      data-status={item.status}
    >
      <div className="flex items-center justify-between gap-3 text-[11px] text-ink-faint">
        <strong className="font-semibold">
          {item.isRequirements ? "Assistant asked you to confirm the requirements" : "Assistant requested input"}
        </strong>
        <OperationStatus value={item.status} />
      </div>
      {item.target ? <p className="mt-2 wrap-anywhere">{item.target}</p> : null}
    </article>
  );
}

function ActivityDetails({
  entries,
  label = "Historical activity",
}: {
  entries: readonly (ActivityStreamEvent | LiveTextEntry)[];
  label?: string;
}) {
  if (entries.length === 0) return null;
  return (
    <details className={cx(CARD, "text-xs text-ink-dim")}>
      <summary className="cursor-pointer">
        {label} · {entries.length} event
        {entries.length === 1 ? "" : "s"}
      </summary>
      <ol className="mt-2 list-decimal pl-5">
        {entries.map((entry, index) => (
          <li key={("resumeToken" in entry ? entry.resumeToken : entry.source) + String(index)}>
            {typeof entry.value === "string" ? entry.value : entry.value.message}
          </li>
        ))}
      </ol>
    </details>
  );
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

  return (
    <ConversationClockProvider active={isLive}>
      <section className="flex flex-col gap-4" aria-label="Conversation">
        <ActivityDetails entries={presentation.earlierActivity} />
        {presentation.turns.map((turn) => (
          <section className="flex flex-col gap-4" aria-label="Conversation turn" key={turn.key}>
            {turn.messages.map((entry) => (
              <ConversationMessage entry={entry} key={entry.sequence} notificationLinks={notificationLinks} />
            ))}
            <ActivityDetails entries={turn.historicalActivities} />
            <WorkLog turn={turn} />
            {turn.fallbacks.map((event) => (
              <p className="text-xs text-ink-faint" key={event.resumeToken}>
                {event.value.message}
              </p>
            ))}
            {turn.questions.map((question) => (
              <QuestionCard item={question} key={question.key} />
            ))}
            <ActivityDetails entries={turn.liveActivities} label="Current activity" />
          </section>
        ))}
        {assistant?.value ? (
          <div className={cx(CARD, "text-[13px]")} data-testid="live-assistant-message">
            <span className="text-[11px] text-ink-faint">Assistant · {streamState}</span>
            <MarkdownContent value={assistant.value} />
          </div>
        ) : null}
      </section>
    </ConversationClockProvider>
  );
}
