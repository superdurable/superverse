// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// What the Session chat shows for a Session snapshot: lifecycle text, composer gating, pending messages
// and the links of its notices.
import { needsConfiguration, type NotificationLinks, type PendingMessageQueueItem } from "@/components/agent";
import type {
  AgentDescription,
  NotificationLink,
  PendingUserMessage,
  SequencedMessage,
} from "@/lib/session/agent-types";
import type { KnownSessionStatus } from "@/lib/session/session-status";
import { withoutMockMarkersInMessages } from "@/lib/session/mock-scenarios";
import { studioRoutes } from "@/lib/shared/studio-routes";

/** What the Agent waits for the user to answer. */
export type PendingInputKind = NonNullable<AgentDescription["pendingUserInput"]>["kind"];

/** What the agent is doing, as a person would say it; empty while it waits for a message. */
export function agentStatusLabel(value: AgentDescription["status"] | undefined): string {
  switch (value) {
    case undefined:
      return "Connecting…";
    case "initializing":
      return "Starting the agent…";
    case "waiting_for_message":
      return "";
    case "compacting_context":
      return "Summarizing the conversation so far…";
    case "calling_model":
      return "Thinking…";
    case "routing_tool":
    case "executing_tool":
      return "Working…";
    case "waiting_for_tool_approval":
      return "Waiting for your approval";
    case "waiting_for_timer":
      return "Waiting before it goes on…";
  }
}

/**
 * The Project's one Session is archived only by the Project's deletion, and FAILED only when its
 * engine execution stopped, which an operator recovers.
 */
export function lifecycleMessage(status: KnownSessionStatus | null): string | null {
  switch (status) {
    case "SESSION_STATUS_ARCHIVED":
      return "This Project is being deleted.";
    case "SESSION_STATUS_FAILED":
      return "The agent stopped unexpectedly. An operator must recover it; your conversation and code are kept.";
    case "SESSION_STATUS_ACTIVE":
    case null:
      return null;
  }
}

/**
 * Where a person acts on a delivery notice of the Project, each fix named by the remedy it carries
 * out: Setup for what the configuration lacks, the Interface to start the Preview again, the
 * Release to publish again, and this conversation for a change to the application. A Preview notice
 * also shows the Preview, a Release notice opens the Release and a Deployment notice the Release it
 * deployed; a Release ID is its source commit. All read stored state, so none wakes a Sandbox.
 */
export function notificationLinks(projectId: string): NotificationLinks {
  return (notification): NotificationLink[] => {
    if (notification.kind !== "delivery") return [];
    const agent = studioRoutes.projectSurface(projectId, "flow");
    const fixes: NotificationLink[] = [
      { label: "Open Setup", href: studioRoutes.previewConfiguration(projectId), remedy: "FAILURE_REMEDY_OPEN_SETUP" },
      { label: "Ask the Agent", href: agent, remedy: "FAILURE_REMEDY_ASK_AGENT" },
      { label: "Ask the Agent", href: agent, remedy: "FAILURE_REMEDY_FREE_WORKSPACE" },
      {
        label: "Reauthorize",
        href: studioRoutes.previewConfiguration(projectId),
        remedy: "FAILURE_REMEDY_REAUTHORIZE",
      },
    ];
    // A notice that lacks settings and declares no remedy is completed in Setup all the same.
    const remedy =
      notification.remedy === "FAILURE_REMEDY_UNSPECIFIED" && needsConfiguration(notification)
        ? "FAILURE_REMEDY_OPEN_SETUP"
        : notification.remedy;
    const fix = fixes.filter((link) => link.remedy === remedy);
    switch (notification.source) {
      case "preview": {
        const preview = studioRoutes.projectSurface(projectId, "interface");
        return [
          ...fix,
          ...(notification.remedy === "FAILURE_REMEDY_APPLY_AGAIN"
            ? [{ label: "Try again", href: preview, remedy: "FAILURE_REMEDY_APPLY_AGAIN" as const }]
            : []),
          { label: "Show the Preview", href: preview },
        ];
      }
      case "release":
      case "deployment": {
        const release = studioRoutes.publishingDeployment(projectId, notification.commit);
        return [
          ...fix.map((link) =>
            link.remedy === "FAILURE_REMEDY_OPEN_SETUP"
              ? { ...link, href: studioRoutes.setup(projectId, "live", notification.commit) }
              : link,
          ),
          ...(notification.remedy === "FAILURE_REMEDY_PUBLISH_AGAIN"
            ? [{ label: "Publish again", href: release, remedy: "FAILURE_REMEDY_PUBLISH_AGAIN" as const }]
            : []),
          { label: notification.source === "release" ? "Open the Release" : "Open the Deployment", href: release },
        ];
      }
      case "subagent":
      case "command":
      case "connector":
        return [];
    }
  };
}

export function mapPendingMessages(
  queued: readonly PendingUserMessage[],
  steered: readonly PendingUserMessage[],
): PendingMessageQueueItem[] {
  return [
    ...steered.map((message): PendingMessageQueueItem => ({
      id: message.messageId,
      content: message.value.content,
      label: "Steering",
      kind: "steered",
    })),
    ...queued.map((message): PendingMessageQueueItem => ({
      id: message.messageId,
      content: message.value.content,
      label: "Queued",
      kind: "queued",
    })),
  ];
}

/** What the composer invites, or what the person must do before sending another message. */
export function composerPlaceholder({
  pendingInputKind,
  answerAwaitingReconciliation,
  sessionStatus,
  viewingLive,
}: {
  pendingInputKind: PendingInputKind | null;
  answerAwaitingReconciliation: boolean;
  sessionStatus: KnownSessionStatus | null;
  /** The person views Live, where nothing is built. */
  viewingLive: boolean;
}): string {
  if (pendingInputKind !== null) {
    if (answerAwaitingReconciliation) return "Waiting for the agent to accept your answer…";
    switch (pendingInputKind) {
      case "requirements":
        return "Reply or ask for a change…";
      case "question":
        return "Answer the agent's questions before sending another message";
    }
  }
  switch (sessionStatus) {
    case "SESSION_STATUS_ARCHIVED":
      return "This Project is being deleted";
    case "SESSION_STATUS_FAILED":
      return "The agent stopped unexpectedly";
    case "SESSION_STATUS_ACTIVE":
    case null:
      break;
  }
  return viewingLive ? "Ask for a change. It's built on Preview, not Live." : "Ask for a change…";
}

/** Why the composer cannot send now, or null when it can. A pending question explains itself in the placeholder. */
export function composerDisabledReason({
  productInteractive,
  lifecycle,
  agentReady,
  snapshotCurrent,
  blockedByPending,
  placeholder,
}: {
  productInteractive: boolean;
  lifecycle: string | null;
  agentReady: boolean;
  snapshotCurrent: boolean;
  blockedByPending: boolean;
  placeholder: string;
}): string | null {
  if (!productInteractive) return lifecycle ?? "Waiting for the session to become available…";
  if (!agentReady) return "Starting the agent…";
  if (!snapshotCurrent) return "Could not read the latest session state; waiting for the next update…";
  if (blockedByPending) return placeholder;
  return null;
}

/**
 * What the pending message queue does with a message: steer it into the running turn, delete it, or
 * take it out of the queue and back into the composer to edit it.
 */
export function pendingQueueActions(
  queued: readonly PendingUserMessage[],
  commands: {
    steer: (messageId: string) => Promise<unknown>;
    deleteMessage: (messageId: string) => Promise<unknown>;
  },
  setComposerText: (text: string) => void,
) {
  return {
    onSteer: (messageId: string) => void commands.steer(messageId).catch(() => undefined),
    onEdit: (messageId: string) => {
      const message = queued.find((candidate) => candidate.messageId === messageId);
      if (!message) return;
      void commands
        .deleteMessage(messageId)
        .then(() => setComposerText(message.value.content))
        .catch(() => undefined);
    },
    onDelete: (messageId: string) => void commands.deleteMessage(messageId).catch(() => undefined),
  };
}

/**
 * The conversation as the chat shows it: without mock markers, and without notices that the template's own
 * commit lacks settings. The Preview tries the commit the Project started from before the agent pushes
 * anything, and the person never asked for that version.
 */
export function shownChatMessages(messages: readonly SequencedMessage[], templateCommit: string): SequencedMessage[] {
  const shown = withoutMockMarkersInMessages(messages);
  if (!templateCommit) return shown;
  return shown.filter(({ message }) => {
    const notification = message.notification;
    return !(notification && needsConfiguration(notification) && notification.commit === templateCommit);
  });
}
