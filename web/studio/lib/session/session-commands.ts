// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useMemo, type Dispatch, type RefObject } from "react";
import {
  answerQuestions,
  deleteQueuedMessage,
  interruptAgent,
  resolveToolApproval,
  sendMessage,
  steerQueuedMessage,
  updateSessionModel,
} from "@/lib/api/agent";
import { ApiError, errorMessage, newRequestId } from "@/lib/api/transport/api-error";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { isSessionExecutionStopped } from "@/lib/shared/session-command-error";
import type { AgentModel, AgentSnapshot, UserInputAnswer } from "./agent-types";
import { validatePendingAnswers } from "./agent-view";
import { agentModelInput } from "./model/agent-model";
import type { SessionAction } from "./session-reducer";

// The Agent commands of the page Session, and how a failed one reads for the person: a definite rejection or
// an uncertain result.

/** What the commands need of their owner: its one-at-a-time command runner and its latest Snapshot. */
interface CommandOwner {
  projectId: string;
  mutate: (kind: string, command: (signal: AbortSignal) => Promise<void>) => Promise<void>;
  dispatch: Dispatch<SessionAction>;
  latestSnapshot: RefObject<AgentSnapshot | null>;
  loadHistory: (mode: "page" | "beginning") => Promise<void>;
}

/** The Agent commands of the page Session, each run through the owner's `mutate`. */
export function useSessionCommands({ projectId, mutate, dispatch, latestSnapshot, loadHistory }: CommandOwner) {
  return useMemo(() => {
    const session = { projectId };
    // Answers the pending question batch; the Agent takes the answer or refuses it as not admitted.
    const answerPending = async (callId: string, input: Required<RpcInput<"AnswerQuestions">>["input"]) => {
      dispatch({ type: "answeringQuestions", callId });
      try {
        await mutate("answer", (signal) => answerQuestions({ ...session, input }, signal));
        dispatch({ type: "questionsAnswered", callId });
      } finally {
        dispatch({ type: "answeringQuestions", callId: null });
      }
    };
    return {
      // A send is a new message with a new ID. The Agent does not deduplicate by message ID, so reusing
      // one for a retry would queue a second copy under the same ID.
      send: (text: string, attachmentIds: readonly string[] = []) =>
        mutate("send", (signal) =>
          sendMessage({ ...session, messageId: newRequestId(), text, attachmentIds: [...attachmentIds] }, signal),
        ),
      // A change to what the agent builds is a message, with its files; while a turn runs (`steer`) it also
      // steers that turn, which applies it at its next safe boundary. A steer the Agent no longer admits,
      // because the turn ended and took the message, changes nothing: the message is delivered either way.
      change: (text: string, steer: boolean, attachmentIds: readonly string[] = []) =>
        mutate("send", async (signal) => {
          const messageId = newRequestId();
          await sendMessage({ ...session, messageId, text, attachmentIds: [...attachmentIds] }, signal);
          if (steer) await steerQueuedMessage({ ...session, messageId }, signal).catch(unlessNotAdmitted);
        }),
      steer: (messageId: string) => mutate("steer", (signal) => steerQueuedMessage({ ...session, messageId }, signal)),
      deleteMessage: (messageId: string) =>
        mutate("delete", (signal) => deleteQueuedMessage({ ...session, messageId }, signal)),
      answerQuestions: async (callId: string, answers: readonly UserInputAnswer[]) => {
        const pending = latestSnapshot.current?.description.pendingUserInput;
        if (pending?.callId !== callId) throw new Error("These Agent questions are no longer pending");
        validatePendingAnswers(answers, pending.questions);
        return answerPending(callId, {
          callId,
          answers: answers.map(({ questionId, answer }) => ({ questionId, answer })),
        });
      },
      // Taken at any time: the next turn starts on the model. The revision is the one the person saw;
      // a stale one is refused as not admitted.
      updateModel: (model: AgentModel, expectedRevision: number) =>
        mutate("model", (signal) =>
          updateSessionModel({ ...session, model: agentModelInput(model), expectedRevision }, signal),
        ),
      approveTool: (callId: string, approved: boolean) =>
        mutate("approval", (signal) => resolveToolApproval({ ...session, callId, isApproved: approved }, signal)),
      // Stops the turn the person saw: a later turn refuses a stale Stop as not admitted.
      stop: async () => {
        const turn = latestSnapshot.current?.description.turn ?? 0;
        await mutate("stop", (signal) => interruptAgent({ ...session, turn }, signal));
        dispatch({ type: "stopRequested" });
      },
      loadEarlier: () => loadHistory("page"),
      jumpToBeginning: () => loadHistory("beginning"),
    };
  }, [dispatch, latestSnapshot, loadHistory, mutate, projectId]);
}

/** The text of a failed Agent command: definitely rejected, or uncertain until the person checks. */
export function commandFailureText(reason: unknown): string {
  const info = reason instanceof ApiError ? reason.errorInfo : undefined;
  const detail = errorMessage(reason, "Unexpected session error");
  if (
    info?.reason === "AGENT_COMMAND_NOT_DISPATCHED" ||
    info?.reason === "AGENT_COMMAND_NOT_ADMITTED" ||
    isSessionExecutionStopped(info)
  )
    return `This request was rejected. ${detail}`;
  return `Command result is uncertain. Confirm the current state before sending again. ${detail}`;
}

/** Rethrows a failure unless the Agent definitely did not admit the command, which then changed nothing. */
function unlessNotAdmitted(reason: unknown): void {
  if (!(reason instanceof ApiError && reason.errorInfo?.reason === "AGENT_COMMAND_NOT_ADMITTED")) throw reason;
}

/** The reason of a failure that found the Session's engine execution stopped, or null. */
export function stoppedExecutionReason(reason: unknown): string | null {
  const info = reason instanceof ApiError ? reason.errorInfo : undefined;
  return info && isSessionExecutionStopped(info) ? info.reason : null;
}
