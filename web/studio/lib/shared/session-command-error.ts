// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { isRecord } from "./json";

/** Go refused an Agent command before running it. */
export const AGENT_COMMAND_NOT_ADMITTED: ErrorInfo__Output = {
  reason: "AGENT_COMMAND_NOT_ADMITTED",
  domain: "superverse.v2",
  metadata: {},
};

/** The BFF rejected an Agent command before dispatching it. */
export const AGENT_COMMAND_NOT_DISPATCHED: ErrorInfo__Output = {
  reason: "AGENT_COMMAND_NOT_DISPATCHED",
  domain: "superverse.v2",
  metadata: {},
};

/**
 * The reasons of a Session whose engine execution stopped while it still reads ACTIVE: every
 * operation that needs it running fails with one, and Studio presents the Session as FAILED.
 */
const SESSION_EXECUTION_STOPPED_REASONS: ReadonlySet<unknown> = new Set([
  "SESSION_EXECUTION_FAILED",
  "SESSION_EXECUTION_CANCELED",
  "SESSION_EXECUTION_TERMINATED",
]);

/** The ErrorInfo of an operation that found the Session's engine execution stopped. */
export function isSessionExecutionStopped(info: ErrorInfo__Output | undefined): boolean {
  return info !== undefined && SESSION_EXECUTION_STOPPED_REASONS.has(info.reason);
}

/**
 * The only structured error details the business BFF lets through. The BFF is
 * their one gate; the browser reads what it forwards. They describe command
 * admission and are never a substitute lifecycle or Agent snapshot:
 * - AGENT_COMMAND_NOT_ADMITTED / AGENT_COMMAND_NOT_DISPATCHED: the Agent
 *   definitely did not take the command.
 * - SESSION_EXECUTION_FAILED / _CANCELED / _TERMINATED: the Session's engine
 *   execution stopped, so nothing that needs it running was taken.
 */
export function commandErrorInfo(value: unknown): ErrorInfo__Output | undefined {
  if (!isRecord(value) || value.domain !== "superverse.v2") return;
  if (
    isRecord(value.metadata) &&
    (value.reason === "AGENT_COMMAND_NOT_ADMITTED" ||
      value.reason === "AGENT_COMMAND_NOT_DISPATCHED" ||
      SESSION_EXECUTION_STOPPED_REASONS.has(value.reason)) &&
    typeof value.reason === "string"
  )
    return { reason: value.reason, domain: value.domain, metadata: {} };
}
