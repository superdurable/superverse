// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { cx } from "@/components/ui/cx";
import type { AgentNotification, NotificationLink, SequencedMessage } from "@/lib/session/agent-types";
import { formatMonthDayTime } from "@/lib/shared/format";
import { reportedProblem } from "@/lib/shared/problem/problem";
import { ProblemPanel, type RemedyActions } from "@/components/ui/problem/ProblemPanel";
import { MarkdownContent } from "./MarkdownContent";

/** The links a page offers for a notification, such as where to configure what a failed deployment lacks. */
export type NotificationLinks = (notification: AgentNotification) => readonly NotificationLink[];

/** A marker of the conversation, such as a compaction: its text between two rules, never a chat bubble. */
function ConversationMarker({ entry }: { entry: SequencedMessage }) {
  const { content, createdAt } = entry.message;
  return (
    <div
      className="flex items-center gap-3 text-xs text-ink-faint"
      data-message-sequence={entry.sequence}
      data-testid="conversation-marker"
      title={formatMonthDayTime(createdAt)}
    >
      <span aria-hidden="true" className="h-px flex-1 bg-line-soft" />
      <span className="max-w-[80%] text-center wrap-anywhere">{content}</span>
      <span aria-hidden="true" className="h-px flex-1 bg-line-soft" />
    </div>
  );
}

/** What a delivery notification is about, by its source; tasks and connectors deliver nothing. */
function deliverySubject(source: AgentNotification["source"]): string {
  switch (source) {
    case "preview":
      return "Preview";
    case "release":
      return "Live release";
    case "deployment":
      return "Live deployment";
    case "subagent":
    case "command":
    case "connector":
      return "Delivery";
  }
}

/** The title of a notification notice by what it reports. */
function notificationTitle(notification: AgentNotification): string {
  switch (notification.kind) {
    case "task":
      return notification.taskStatus === "lost" ? "Background task lost" : "Background task finished";
    case "delivery": {
      const subject = deliverySubject(notification.source);
      if (notification.outcome === "ready") return `${subject} is ready`;
      return notification.gapCount > 0 ? `${subject} needs configuration` : `${subject} failed`;
    }
    case "connector":
      switch (notification.connectorEvent) {
        case "pull_request_failed":
          return `Pull request of connector ${notification.slug} could not be opened`;
        case "release_candidate_published":
          return `Release candidate of connector ${notification.slug} published`;
        case "release_candidate_failed":
          return `Release candidate of connector ${notification.slug} failed`;
        case null:
          return "Connector update";
      }
  }
}

/** The links a delivery notice offers besides its fix. */
function NoticeLinks({ links }: { links: readonly NotificationLink[] }) {
  if (links.length === 0) return null;
  return (
    <p className="mt-1 flex flex-wrap gap-x-3">
      {links.map((link) => (
        <Link key={link.href} className="text-accent hover:underline" href={link.href}>
          {link.label}
        </Link>
      ))}
    </p>
  );
}

/**
 * A delivery that did not reach READY, as a problem: what happened, the failure's message, the fix the
 * remedy names among the page's links, the configuration it lacks and the details folded, with the
 * page's other links below. The commit appears only in the details; the version names it.
 */
function DeliveryProblem({
  notification,
  links,
}: {
  notification: AgentNotification;
  links: readonly NotificationLink[];
}) {
  const hidden = notification.gapCount - notification.gaps.length;
  const problem = reportedProblem(notificationTitle(notification), {
    ...notification,
    gaps: hidden > 0 ? [...notification.gaps, `and ${hidden} more`] : notification.gaps,
  });
  const actions: RemedyActions = Object.fromEntries(
    links.flatMap((link) => (link.remedy ? [[link.remedy, { href: link.href }]] : [])),
  );
  return (
    <>
      <ProblemPanel className="mt-1" problem={problem} actions={actions} />
      <NoticeLinks links={links.filter((link) => link.remedy !== problem.remedy)} />
    </>
  );
}

/**
 * A Session notification as a notice, never a user bubble: an automated report of something that
 * happened outside the Agent's turn, which the Agent reads. A delivery is the platform variant: a
 * failure the application owns reads as a failure, any other outcome as a notice.
 */
function NotificationNotice({
  entry,
  notification,
  links,
}: {
  entry: SequencedMessage;
  notification: AgentNotification;
  links: readonly NotificationLink[];
}) {
  const isDelivery = notification.kind === "delivery";
  return (
    <div
      role="status"
      className={cx(
        "rounded-lg border px-3 py-2 text-xs text-ink-dim",
        !isDelivery && "border-line-soft bg-well",
        isDelivery && notification.outcome === "failure" && "border-fail-rule bg-fail-subtle",
        isDelivery && notification.outcome !== "failure" && "border-busy-rule bg-busy-subtle",
      )}
      data-message-sequence={entry.sequence}
      data-testid="notification-notice"
      data-notification-kind={notification.kind}
      data-notification-source={notification.source}
    >
      <div className="flex items-center justify-between gap-3 text-[11px] text-ink-faint">
        <strong className="font-semibold">{notificationTitle(notification)}</strong>
        <time dateTime={notification.occurredAt}>{formatMonthDayTime(notification.occurredAt)}</time>
      </div>
      {isDelivery && notification.outcome !== "ready" ? (
        <DeliveryProblem notification={notification} links={links} />
      ) : (
        <>
          <p className="mt-1 wrap-anywhere">{entry.message.content}</p>
          {isDelivery && notification.version && <p className="mt-1 text-ink-faint">{notification.version}</p>}
          {isDelivery && <NoticeLinks links={links} />}
        </>
      )}
    </div>
  );
}

/**
 * A durable user or assistant message with its time, a notification as a notice, or a system marker
 * as a divider line; a tool result renders nothing, because the work log shows its call.
 */
export function ConversationMessage({
  entry,
  notificationLinks,
}: {
  entry: SequencedMessage;
  notificationLinks?: NotificationLinks;
}) {
  const { role, content, createdAt, notification, answeredInputCallId } = entry.message;
  if (role === "system") return content ? <ConversationMarker entry={entry} /> : null;
  if (role === "notification")
    return notification ? (
      <NotificationNotice entry={entry} notification={notification} links={notificationLinks?.(notification) ?? []} />
    ) : null;
  if (role !== "user" && role !== "assistant") return null;
  const isUser = role === "user";
  const remoteReply = !isUser && Boolean(entry.message.via);
  return (
    <article
      aria-label={`${isUser ? "User" : "Assistant"} message`}
      data-message-sequence={entry.sequence}
      className={cx(
        "group/message flex min-w-0 flex-col",
        isUser ? "w-fit max-w-[min(76%,42rem)] items-end self-end" : "w-full items-start",
      )}
    >
      <div
        className={cx("flex w-full min-w-0 flex-col", isUser && "items-end")}
        data-testid="conversation-message-content"
      >
        {isUser ? (
          <div
            className={cx(
              "w-fit max-w-full rounded-[14px_14px_4px_14px] border border-line-soft bg-well px-3 py-2.5 text-[13px] leading-relaxed wrap-anywhere",
              !answeredInputCallId && "whitespace-pre-wrap",
            )}
          >
            {/* An answer to a pending input is the Session's Markdown of the answers; anything else is the user's own text. */}
            {answeredInputCallId ? <MarkdownContent value={content} /> : content}
          </div>
        ) : (
          <div className="text-[13px]">
            {remoteReply && (
              <span className="text-[11px] text-ink-faint">
                Codex · Remote reply{entry.message.imported ? " · Imported" : ""}
              </span>
            )}
            {content && <MarkdownContent value={content} />}
          </div>
        )}
      </div>
      <time
        className={cx(
          "mt-1 h-[1.25em] overflow-hidden text-[11px] leading-[1.25] text-ink-faint opacity-0 transition-opacity group-focus-within/message:opacity-100 group-hover/message:opacity-100",
          isUser && "self-end",
        )}
        dateTime={createdAt}
      >
        {formatMonthDayTime(createdAt)}
      </time>
    </article>
  );
}
