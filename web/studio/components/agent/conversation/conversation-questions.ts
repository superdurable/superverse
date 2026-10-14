// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The Agent's request_user_input and confirm_requirements calls of the history. The history carries
// no tool arguments, so a past request shows only its target, the first question or the card's title;
// the pending questions and the card come from the Agent's description.
import type { AgentToolCall } from "@/lib/session/agent-types";
import type { ConversationQuestionItem, PresentationContext } from "./conversation-presentation";

const REQUIREMENTS_TOOL = "confirm_requirements";
/** The tools whose call waits for the user's answer. */
export const QUESTION_TOOLS: ReadonlySet<string> = new Set(["request_user_input", REQUIREMENTS_TOOL]);

export function questionItem(call: AgentToolCall, context: PresentationContext): ConversationQuestionItem {
  const answered = context.messages.some((entry) => entry.message.answeredInputCallId === call.id);
  const activities = context.activity.byCall.get(call.id) ?? [];
  const cancelled = activities.some((entry) => entry.value.kind === "user_input_cancelled");
  const failed =
    context.resultByCall.get(call.id)?.message.toolExecution?.outcome === "known_failure" ||
    activities.some((entry) => entry.value.kind === "tool_failed");
  return {
    key: `question-${call.id}`,
    callId: call.id,
    isRequirements: call.name === REQUIREMENTS_TOOL,
    target: call.target,
    status: answered
      ? "answered"
      : failed
        ? "failed"
        : cancelled
          ? "cancelled"
          : context.pendingWaitByCall.has(call.id)
            ? "pending"
            : "failed",
  };
}
