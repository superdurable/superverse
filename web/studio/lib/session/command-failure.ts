// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { isSessionExecutionStopped } from "@/lib/shared/session-command-error";

// How a failed Session operation reads for the person: a definite rejection or an uncertain result.

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
export function unlessNotAdmitted(reason: unknown): void {
  if (!(reason instanceof ApiError && reason.errorInfo?.reason === "AGENT_COMMAND_NOT_ADMITTED")) throw reason;
}

/** The reason of a failure that found the Session's engine execution stopped, or null. */
export function stoppedExecutionReason(reason: unknown): string | null {
  const info = reason instanceof ApiError ? reason.errorInfo : undefined;
  return info && isSessionExecutionStopped(info) ? info.reason : null;
}
