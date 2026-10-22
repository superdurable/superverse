// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import type { AgentNotification, NotificationLink, SequencedMessage } from "@/lib/session/agent-types";
import { formatMonthDayTime } from "@/lib/shared/format";
import { reportedProblem } from "@/lib/shared/problem/problem";
import { ProblemPanel, type RemedyActions } from "@/components/ui/problem/ProblemPanel";
import { MessageAttachments } from "@/components/agent/attachments/MessageAttachments";
import { MarkdownContent } from "./MarkdownContent";

/** The links a page offers for a notification, such as where to configure what a failed deployment lacks. */
export type NotificationLinks = (notification: AgentNotification) => readonly NotificationLink[];

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

/**
 * Whether a delivery notice reports settings the Project lacks, which Setup completes, rather than a
 * failure: its owner is the configuration, its remedy is Setup, or it names what is missing.
 */
export function needsConfiguration(notification: AgentNotification): boolean {
  return (
    notification.kind === "delivery" &&
    notification.outcome !== "ready" &&
    (notification.owner === "configuration" ||
      notification.remedy === "FAILURE_REMEDY_OPEN_SETUP" ||
      notification.gapCount > 0)
  );
}

/** The title of a notification notice by what it reports. */
function notificationTitle(notification: AgentNotification): string {
  switch (notification.kind) {
    case "task":
      return notification.taskStatus === "lost" ? "Background task lost" : "Background task finished";
    case "delivery": {
      const subject = deliverySubject(notification.source);
      if (notification.outcome === "ready") return `${subject} is ready`;
      if (needsConfiguration(notification))
        return notification.source === "preview"
          ? "Preview needs a few settings before it can start"
          : `${subject} needs a few settings first`;
      return `${subject} failed`;
    }
    case "connector":
      switch (notification.connectorEvent) {
        case "pull_request_failed":
          return `Pull request of connector ${notification.slug} could not be opened`;
        case "release_candidate_published":
          return `Release candidate of connector ${notification.slug} published`;
        case "release_candidate_failed":
          return `Release candidate of connector ${notification.slug} failed`;
        case "checks_failed":
          return `Library CI of connector ${notification.slug} failed`;
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
    links.flatMap((link) =>
      link.remedy ? [[link.remedy, link.onClick ? { onClick: link.onClick } : { href: link.href }]] : [],
    ),
  );
  return (
    <>
      <ProblemPanel className="mt-1" problem={problem} actions={actions} />
      <NoticeLinks links={links.filter((link) => link.remedy !== problem.remedy)} />
    </>
  );
}

/** A technical line of a notice's details, shown only when it has a value. */
function Detail({ name, value }: { name: string; value: string }) {
  return value ? (
    <>
      <dt>{name}</dt>
      <dd className="wrap-anywhere whitespace-pre-wrap">{value}</dd>
    </>
  ) : null;
}

/**
 * A delivery that waits for settings, in plain words: what it still needs, the Setup link that
 * completes it and the page's other links, with the server's reason, the code, the phase and the
 * commit one click away.
 */
function ConfigurationNeeded({
  notification,
  links,
}: {
  notification: AgentNotification;
  links: readonly NotificationLink[];
}) {
  const hidden = notification.gapCount - notification.gaps.length;
  const setup = links.find((link) => link.remedy === "FAILURE_REMEDY_OPEN_SETUP");
  const why = notification.message.split(" Cause: ");
  return (
    <div className="mt-1 grid gap-1.5">
      {notification.gaps.length > 0 && (
        <>
          <p>It still needs:</p>
          <ul className="list-disc pl-5" aria-label="Missing configuration">
            {[...notification.gaps, ...(hidden > 0 ? [`and ${hidden} more`] : [])].map((gap) => (
              <li key={gap} className="wrap-anywhere">
                {gap}
              </li>
            ))}
          </ul>
        </>
      )}
      {setup && (
        <div data-testid="problem-fix">
          <Link href={setup.href} className={buttonStyle("accent", "sm")}>
            {setup.label}
          </Link>
        </div>
      )}
      <NoticeLinks links={links.filter((link) => link !== setup)} />
      <MoreDetails surface="chat.notice">
        <dl className="grid grid-cols-[auto_1fr] gap-x-2 font-mono text-ink-faint" data-testid="problem-details">
          <Detail name="Why" value={why[0] ?? ""} />
          <Detail name="Code" value={notification.code} />
          <Detail name="Phase" value={notification.phase} />
          <Detail name="Version" value={notification.version} />
          <Detail name="Commit" value={notification.commit} />
          <Detail name="Time" value={notification.occurredAt} />
          <Detail name="Cause" value={why.slice(1).join(" Cause: ")} />
        </dl>
      </MoreDetails>
    </div>
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
  const configuration = needsConfiguration(notification);
  return (
    <div
      role="status"
      className={cx(
        "rounded-lg border px-3 py-2 text-caption text-ink-dim",
        !isDelivery && "border-line-soft bg-well",
        isDelivery && notification.outcome === "failure" && "border-fail-rule bg-fail-subtle",
        isDelivery && notification.outcome !== "failure" && "border-busy-rule bg-busy-subtle",
      )}
      data-message-sequence={entry.sequence}
      data-testid="notification-notice"
      data-notification-kind={notification.kind}
      data-notification-source={notification.source}
    >
      <div
        className={cx(
          "flex items-center justify-between gap-3",
          configuration ? "text-body text-ink" : "text-[11px] text-ink-faint",
        )}
      >
        <strong className="font-semibold" data-testid={configuration ? "problem-what" : undefined}>
          {notificationTitle(notification)}
        </strong>
        <time className="shrink-0 text-caption text-ink-faint" dateTime={notification.occurredAt}>
          {formatMonthDayTime(notification.occurredAt)}
        </time>
      </div>
      {configuration ? (
        <ConfigurationNeeded notification={notification} links={links} />
      ) : isDelivery && notification.outcome !== "ready" ? (
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
 * A durable user or assistant message with its time, or a notification as a notice; a system marker
 * and a tool result render nothing, because the turn's work log shows them.
 */
export function ConversationMessage({
  entry,
  notificationLinks,
}: {
  entry: SequencedMessage;
  notificationLinks?: NotificationLinks;
}) {
  const { role, content, createdAt, notification, answeredInputCallId } = entry.message;
  if (entry.message.executorChanged)
    return (
      <p role="status" className="border-t border-line-soft pt-3 text-caption text-ink-dim" data-executor-changed>
        {content}
      </p>
    );
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
        {isUser && <MessageAttachments attachments={entry.message.attachments} className="mb-1.5 justify-end" />}
        {isUser ? (
          (content || entry.message.attachments.length === 0) && (
            <div
              className={cx(
                "w-fit max-w-full rounded-[14px_14px_4px_14px] border border-line-soft bg-well px-3 py-2.5 text-[13px] leading-relaxed wrap-anywhere",
                !answeredInputCallId && "whitespace-pre-wrap",
              )}
            >
              {/* An answer to a pending input is the Session's Markdown of the answers; anything else is the user's own text. */}
              {answeredInputCallId ? <MarkdownContent value={content} /> : content}
            </div>
          )
        ) : (
          <div className="text-[13px]">
            {remoteReply && (
              <span className="text-[11px] text-ink-faint">
                Local agent · Remote reply{entry.message.imported ? " · Imported" : ""}
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
