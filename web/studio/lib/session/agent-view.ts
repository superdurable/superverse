// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { AgentBackgroundCommand__Output } from "@/lib/generated/protobuf/superverse/v2/AgentBackgroundCommand";
import type { AgentConfirmedRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentConfirmedRequirements";
import type { SessionExecutor__Output } from "@/lib/generated/protobuf/superverse/v2/SessionExecutor";
import type { AgentDescription__Output } from "@/lib/generated/protobuf/superverse/v2/AgentDescription";
import type { AgentEvent__Output } from "@/lib/generated/protobuf/superverse/v2/AgentEvent";
import type { AgentHistoryPage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentHistoryPage";
import type { AgentMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentMessage";
import { agentModelView } from "./model/agent-model";
import type { AgentModelCallMetrics__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModelCallMetrics";
import type { AgentNotification__Output } from "@/lib/generated/protobuf/superverse/v2/AgentNotification";
import type { AgentPendingUserMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentPendingUserMessage";
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import type { AgentSequencedMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentSequencedMessage";
import type { AgentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AgentSnapshot";
import type { AgentSubagent__Output } from "@/lib/generated/protobuf/superverse/v2/AgentSubagent";
import type { AgentTodoList__Output } from "@/lib/generated/protobuf/superverse/v2/AgentTodoList";
import type { AgentToolExecutionMetrics__Output } from "@/lib/generated/protobuf/superverse/v2/AgentToolExecutionMetrics";
import type { ConnectorContribution__Output } from "@/lib/generated/protobuf/superverse/v2/ConnectorContribution";
import { enumText, required, type EnumText } from "@/lib/shared/protobuf";
import { versionLabel } from "@/lib/shared/version/version";
import { agentAttachmentView } from "./attachments/attachment-view";

/*
 * The browser view of the Agent protobuf. Decoded uint64/int64 values arrive as
 * decimal strings and become numbers here, and an unspecified enum or missing
 * required message is a server defect, not a state to render. The
 * transport-free Agent UI keeps its lowercase literals: each prefixed enum
 * value maps to its lowercase domain value, and back when a command is sent.
 */

function agentInteger(value: string | number): number {
  if (typeof value === "string" && !/^-?\d+$/.test(value)) throw new Error("Invalid Agent integer");
  const result = Number(value);
  if (!Number.isSafeInteger(result)) throw new Error("Agent integer exceeds browser precision");
  return result;
}

function nullableInteger(value: string | number | null | undefined): number | null {
  return value === null || value === undefined ? null : agentInteger(value);
}

/** The Agent UI literal of an enum value, such as "waiting_for_message" for STATUS_WAITING_FOR_MESSAGE. */
type AgentLiteral<V extends string, P extends string> = Lowercase<Exclude<EnumText<V, P>, "">>;

function agentLiteral<P extends string, V extends `${P}_${string}`>(value: V, prefix: P, name: string) {
  const text = enumText(value, prefix);
  if (!text) throw new Error(`Agent ${name} is unavailable`);
  return text.toLowerCase() as AgentLiteral<V, P>;
}

export function agentSnapshotView(value: AgentSnapshot__Output) {
  return {
    history: agentHistoryView(required(value.history, "Agent history")),
    description: agentDescriptionView(required(value.description, "Agent description")),
    queued: value.queued.map(agentPendingMessageView),
    steered: value.steered.map(agentPendingMessageView),
  };
}

export function agentHistoryView(value: AgentHistoryPage__Output) {
  return {
    messages: value.messages.map(agentSequencedMessageView),
    nextBeforeSequence: nullableInteger(value.nextBeforeSequence),
  };
}

function agentSequencedMessageView(value: AgentSequencedMessage__Output) {
  return {
    sequence: agentInteger(value.sequence),
    message: agentMessageView(required(value.message, "Agent message")),
  };
}

function agentMessageView(value: AgentMessage__Output) {
  return {
    role: agentLiteral(value.role, "ROLE", "message role"),
    content: value.content,
    attachments: value.attachments.map(agentAttachmentView),
    ...(value.via ? { via: value.via } : {}),
    ...(value.imported ? { imported: value.imported } : {}),
    ...(value.executorChanged ? { executorChanged: value.executorChanged } : {}),
    toolCalls: value.toolCalls.map((call) => ({ id: call.id, name: call.name, target: call.target })),
    toolCallId: value.toolCallId ?? null,
    toolName: value.toolName ?? null,
    answeredInputCallId: value.answeredInputCallId ?? null,
    createdAt: value.createdAt,
    startedAt: value.startedAt ?? null,
    modelCall: value.modelCall ? agentModelCallMetricsView(value.modelCall) : null,
    toolExecution: value.toolExecution ? agentToolExecutionMetricsView(value.toolExecution) : null,
    notification: value.notification ? agentNotificationView(value.notification) : null,
  };
}

/**
 * A Session notification: an automated report, never a user message; its report is for the model
 * only. A delivery names its outcome, its owner, the failure's code, phase, redacted message and
 * remedy, the commit with its version, the instance, build or deployment it concerns and at most
 * eight configuration gaps; an evolve notification names its review and whether it starts the agent's
 * review or tells it a person's decision.
 */
function agentNotificationView(value: AgentNotification__Output) {
  return {
    id: value.id,
    kind: agentLiteral(value.kind, "KIND", "notification kind"),
    source: agentLiteral(value.source, "SOURCE", "notification source"),
    occurredAt: value.occurredAt,
    taskId: value.taskId,
    taskStatus:
      value.taskStatus === "TASK_STATUS_UNSPECIFIED"
        ? null
        : agentLiteral(value.taskStatus, "TASK_STATUS", "task status"),
    summary: value.summary,
    outcome:
      value.outcome === "OUTCOME_UNSPECIFIED" ? null : agentLiteral(value.outcome, "OUTCOME", "delivery outcome"),
    owner: value.owner === "OWNER_UNSPECIFIED" ? null : agentLiteral(value.owner, "OWNER", "delivery owner"),
    code: value.code,
    phase: value.phase,
    commit: value.commit,
    resourceId: value.resourceId,
    gaps: value.gaps,
    gapCount: value.gapCount,
    message: value.message,
    remedy: value.remedy,
    version: versionLabel(value.version, ""),
    slug: value.slug,
    connectorEvent:
      value.connectorEvent === "CONNECTOR_EVENT_UNSPECIFIED"
        ? null
        : agentLiteral(value.connectorEvent, "CONNECTOR_EVENT", "connector event"),
    reviewId: value.reviewId,
    evolvePhase:
      value.evolvePhase === "EVOLVE_PHASE_UNSPECIFIED"
        ? null
        : agentLiteral(value.evolvePhase, "EVOLVE_PHASE", "evolve phase"),
  };
}

function agentModelCallMetricsView(value: AgentModelCallMetrics__Output) {
  return {
    model: value.model,
    servedModel: value.servedModel,
    effort: value.effort,
    durationMs: agentInteger(value.durationMs),
    firstEventMs: nullableInteger(value.firstEventMs),
    inputTokens: nullableInteger(value.inputTokens),
    cacheReadInputTokens: nullableInteger(value.cacheReadInputTokens),
    cacheCreationInputTokens: nullableInteger(value.cacheCreationInputTokens),
    outputTokens: nullableInteger(value.outputTokens),
    thinkingTokens: nullableInteger(value.thinkingTokens),
    droppedThinkingBlocks: agentInteger(value.droppedThinkingBlocks),
  };
}

function agentToolExecutionMetricsView(value: AgentToolExecutionMetrics__Output) {
  return {
    outcome: agentLiteral(value.outcome, "OUTCOME", "tool outcome"),
    durationMs: nullableInteger(value.durationMs),
    exitCode: nullableInteger(value.exitCode),
    resultBytes: agentInteger(value.resultBytes),
    omittedBytes: nullableInteger(value.omittedBytes),
    resultCode: value.resultCode,
  };
}

function agentPendingMessageView(value: AgentPendingUserMessage__Output) {
  const message = required(value.value, "pending Agent message");
  return {
    messageId: value.messageId,
    value: { content: message.content, attachments: message.attachments.map(agentAttachmentView) },
  };
}

function agentDescriptionView(value: AgentDescription__Output) {
  const { pendingApproval, pendingTimer, pendingUserInput } = value;
  return {
    status: agentLiteral(value.status, "STATUS", "status"),
    waitingInputRound: agentInteger(value.waitingInputRound),
    model: agentModelView(value.model),
    modelRevision: agentInteger(value.modelRevision),
    ...(value.executor ? { executor: sessionExecutorView(value.executor) } : {}),
    contextModel: agentModelView(value.contextModel),
    effort: value.effort,
    firstRetainedSequence: agentInteger(value.firstRetainedSequence),
    lastSequence: agentInteger(value.lastSequence),
    pendingApproval: pendingApproval && {
      callId: pendingApproval.callId,
      toolName: pendingApproval.toolName,
      argumentsJson: pendingApproval.argumentsJson,
      startedAt: pendingApproval.startedAt ?? null,
    },
    pendingTimer: pendingTimer && {
      callId: pendingTimer.callId,
      durationSeconds: agentInteger(pendingTimer.durationSeconds),
      reason: pendingTimer.reason,
      startedAt: pendingTimer.startedAt ?? null,
    },
    pendingUserInput: pendingUserInput && {
      callId: pendingUserInput.callId,
      kind: agentLiteral(pendingUserInput.kind, "KIND", "pending input kind"),
      requirements: pendingUserInput.requirements && agentRequirementsView(pendingUserInput.requirements),
      questions: pendingUserInput.questions.map((question) => ({
        id: question.id,
        header: question.header,
        question: question.question,
        options: question.options.map((option) => ({ label: option.label, description: option.description })),
      })),
      startedAt: pendingUserInput.startedAt ?? null,
    },
    todoList: value.todoList ? agentTodoListView(value.todoList) : null,
    confirmedRequirements: value.confirmedRequirements && agentConfirmedRequirementsView(value.confirmedRequirements),
    pendingQueuedMessageCount: agentInteger(value.pendingQueuedMessageCount),
    pendingSteeredMessageCount: agentInteger(value.pendingSteeredMessageCount),
    availableTools: value.availableTools,
    turn: agentInteger(value.turn),
    backgroundCommands: value.backgroundCommands.map(agentBackgroundCommandView),
    subagents: value.subagents.map(agentSubagentView),
    mockProgress: value.mockProgress && {
      scenario: value.mockProgress.scenario,
      input: value.mockProgress.input,
      nextInput: value.mockProgress.nextInput,
    },
    startingExample: value.startingExample && {
      exampleId: value.startingExample.exampleId,
      choice: agentLiteral(value.startingExample.choice, "CHOICE", "starting example choice"),
    },
  };
}

/** A background command of the Agent as the tasks panel shows it; its command text and output never leave the Flow. */
function agentBackgroundCommandView(value: AgentBackgroundCommand__Output) {
  return {
    taskId: value.taskId,
    description: value.description,
    status: agentLiteral(value.status, "STATUS", "background command status"),
    endReason:
      value.endReason === "END_REASON_UNSPECIFIED" ? null : agentLiteral(value.endReason, "END_REASON", "end reason"),
    exitCode: nullableInteger(value.exitCode),
    outputBytes: agentInteger(value.outputBytes),
    startedAt: value.startedAt,
    endedAt: value.endedAt ?? null,
  };
}

/**
 * One connector the Session contributes to the connectors library, as the tasks panel shows it: its
 * branch, its status, its draft pull request and its head's release candidate, read from the
 * contribution's own Flow.
 */
export function connectorContributionView(value: ConnectorContribution__Output) {
  const pullRequestNumber = agentInteger(value.pullRequestNumber);
  return {
    slug: value.slug,
    module: value.module,
    kind: agentLiteral(value.kind, "KIND", "contribution kind"),
    status: agentLiteral(value.status, "STATUS", "contribution status"),
    branch: value.branch,
    location: agentLiteral(value.location, "LOCATION", "contribution location"),
    pullRequest: pullRequestNumber > 0 ? { number: pullRequestNumber, url: value.pullRequestUrl } : null,
    failure: value.failure,
    description: value.description,
    releaseCandidate:
      value.releaseCandidateStatus === "RELEASE_CANDIDATE_STATUS_UNSPECIFIED"
        ? null
        : {
            version: value.releaseCandidateVersion,
            status: agentLiteral(value.releaseCandidateStatus, "RELEASE_CANDIDATE_STATUS", "release candidate status"),
            failure: value.releaseCandidateFailure,
          },
  };
}

/** A subagent of the Agent as the tasks panel shows it; its prompt and report never leave the Flows. */
function agentSubagentView(value: AgentSubagent__Output) {
  return {
    taskId: value.taskId,
    kind: agentLiteral(value.kind, "KIND", "subagent kind"),
    description: value.description,
    status: agentLiteral(value.status, "STATUS", "subagent status"),
    startedAt: value.startedAt,
    finishedAt: value.finishedAt ?? null,
    summary: value.summary,
    changedFiles: value.changedFiles,
    insertions: value.insertions,
    deletions: value.deletions,
    lost: value.lost,
  };
}

function agentRequirementsView(value: AgentRequirements__Output) {
  return {
    title: value.title,
    summary: value.summary,
    features: value.features.map((feature) => ({ id: feature.id, text: feature.text })),
    connections: value.connections.map((connection) => ({
      need: connection.need,
      connector: connection.connector,
      operations: connection.operations,
      status: agentLiteral(connection.status, "STATUS", "connection status"),
      userSetup: connection.userSetup,
    })),
    userSetup: value.userSetup,
    acceptance: value.acceptance,
    design: value.design
      ? { digest: value.design.digest, summary: value.design.summary, outline: value.design.outline }
      : null,
    designBuilt: value.designBuilt,
    designBuiltCommit: value.designBuiltCommit,
    designWrittenSteps: value.designWrittenSteps,
  };
}

function agentConfirmedRequirementsView(value: AgentConfirmedRequirements__Output) {
  return {
    revision: agentInteger(value.revision),
    spec: agentRequirementsView(required(value.spec, "confirmed requirements")),
    confirmedAt: value.confirmedAt,
  };
}

function agentTodoListView(value: AgentTodoList__Output) {
  return {
    revision: agentInteger(value.revision),
    items: value.items.map((item) => ({
      content: item.content,
      status: agentLiteral(item.status, "STATUS", "todo status"),
    })),
  };
}

export function agentActivityView(value: AgentEvent__Output) {
  const consumption = value.inputConsumption;
  return {
    kind: agentLiteral(value.kind, "KIND", "activity kind"),
    message: value.message,
    callId: value.callId ?? null,
    toolName: value.toolName ?? null,
    target: value.target,
    messageSequence: nullableInteger(value.messageSequence),
    attempt: nullableInteger(value.attempt),
    inputConsumption: consumption && {
      queuedMessageIds: consumption.queuedMessageIds,
      steeredMessageIds: consumption.steeredMessageIds,
    },
  };
}

/** Current execution authority; legacy snapshots have no executor and mean platform epoch zero. */
function sessionExecutorView(value: SessionExecutor__Output) {
  const remote = value.remote;
  return {
    mode: agentLiteral(value.mode, "SESSION_EXECUTOR_MODE", "executor mode"),
    epoch: agentInteger(value.epoch),
    generation: agentInteger(value.generation),
    remote: remote && {
      connectionId: remote.connectionId,
      owner: required(remote.owner, "remote executor owner"),
      binding: required(remote.binding, "remote executor binding"),
      reported: agentInteger(remote.reported),
      activatedAt: remote.activatedAt,
      lastSeenAt: remote.lastSeenAt ?? null,
    },
  };
}

/** Keep answer order aligned with the pending question batch. */
export function validatePendingAnswers(
  answers: readonly { questionId: string; answer: string }[],
  questions: readonly { id: string }[],
) {
  if (
    answers.length !== questions.length ||
    answers.some((answer, index) => answer.questionId !== questions[index]?.id || !answer.answer.trim())
  ) {
    throw new Error("Every pending question must be answered in order");
  }
}
