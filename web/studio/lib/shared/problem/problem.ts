// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import { FailureRemedy, type FailureRemedy__Output } from "@/lib/generated/protobuf/superverse/v2/FailureRemedy";
import { stepName } from "@/lib/shared/format";
import { isRecord } from "@/lib/shared/json";
import { versionLabel } from "@/lib/shared/version/version";

/** What the reader can do about a problem: the remedy the server declared with its code or reason. */
export type Remedy = Exclude<FailureRemedy__Output, "FAILURE_REMEDY_UNSPECIFIED">;

/**
 * The button of each remedy and what the panel says when its page offers no action for it. The
 * Record over the generated enum makes a new remedy a compile error here.
 */
export const REMEDIES = {
  FAILURE_REMEDY_OPEN_SETUP: { label: "Open Setup", hint: "Complete Setup, then Apply." },
  FAILURE_REMEDY_APPLY_AGAIN: { label: "Try again", hint: "Try again." },
  FAILURE_REMEDY_ASK_AGENT: { label: "Ask the Agent", hint: "Ask the Agent to fix the application." },
  FAILURE_REMEDY_REAUTHORIZE: { label: "Reauthorize", hint: "Authorize the connection again." },
  FAILURE_REMEDY_FREE_WORKSPACE: {
    label: "Ask the Agent to free space",
    hint: "Free space in the workspace, then start again.",
  },
  FAILURE_REMEDY_PUBLISH_AGAIN: { label: "Publish again", hint: "Publish again." },
  FAILURE_REMEDY_CONTACT_OPERATOR: {
    label: "Copy details for the operator",
    hint: "An operator must look at this; send them the technical details.",
  },
  FAILURE_REMEDY_CANCEL_DEPLOYMENT: {
    label: "Cancel the deployment",
    hint: "The build is published; cancel its deployment instead.",
  },
} as const satisfies Record<Remedy, { label: string; hint: string }>;

/**
 * What happened, by the phase a failure records: a Preview command kind names the operation; any
 * other phase is a Step type, which reads in words.
 */
const PHASE_OPERATIONS: Readonly<Record<string, string>> = {
  "prepare-preview-source": "Building processes",
  "start-preview": "Starting the Preview",
  "deploy-preview": "Updating the Preview to the pushed version",
  "stop-preview": "Stopping the Preview",
  reclaim: "Reclaiming the Preview",
  release: "Releasing what the Preview started",
};

/** The fallback of an error that names no remedy: a platform problem with its raw details folded. */
const PLATFORM_PROBLEM = "Something went wrong on the platform.";

/**
 * A problem in the one format Studio shows: what happened, why, the remedy that fixes it with any
 * configuration gaps to complete, and the technical details folded below.
 */
export interface Problem {
  title: string;
  why: string;
  remedy: Remedy;
  gaps: readonly string[];
  /** The version the problem concerns, as "V12 · subject"; empty when it names none. */
  version: string;
  details: ProblemDetails;
}

/** The technical details: the code, the phase, the truncated cause, the commit and when it happened. */
interface ProblemDetails {
  code: string;
  phase: string;
  cause: string;
  commit: string;
  at: string;
}

/** What a page knows about where a problem happened, beyond the record itself. */
export interface ProblemContext {
  /** The operation in words, such as "Starting the Preview", when the phase does not name it. */
  operation: string;
  gaps?: readonly string[];
  version?: CommitVersion__Output | null;
  commit?: string;
  at?: string;
}

/** The remedy the server declared, or the platform fallback when it declared none. */
function remedyOf(value: unknown): Remedy {
  return typeof value === "string" &&
    value !== FailureRemedy.FAILURE_REMEDY_UNSPECIFIED &&
    Object.hasOwn(REMEDIES, value)
    ? (value as Remedy)
    : FailureRemedy.FAILURE_REMEDY_CONTACT_OPERATOR;
}

/** A failure's message split into the code's message, which says why, and the cause the server appended. */
function splitCause(message: string): { why: string; cause: string } {
  const at = message.indexOf(" Cause: ");
  return at < 0 ? { why: message, cause: "" } : { why: message.slice(0, at), cause: message.slice(at + 8) };
}

/** What happened: the operation of the phase, needing configuration when that owner must act. */
function titleOf(failure: Failure__Output, operation: string): string {
  const named = PHASE_OPERATIONS[failure.phase];
  const what = named ?? operation;
  if (failure.owner === "FAILURE_OWNER_CONFIGURATION") return `${what} needs configuration`;
  return named || !failure.phase ? `${what} failed` : `${what} failed at "${stepName(failure.phase)}"`;
}

/** The problem of a failure a resource recorded. */
export function failureProblem(failure: Failure__Output, context: ProblemContext): Problem {
  const { why, cause } = splitCause(failure.message);
  const remedy = remedyOf(failure.remedy);
  return {
    title: titleOf(failure, context.operation),
    why: why || PLATFORM_PROBLEM,
    remedy,
    gaps: context.gaps ?? [],
    version: versionLabel(context.version, ""),
    details: { code: failure.code, phase: failure.phase, cause, commit: context.commit ?? "", at: context.at ?? "" },
  };
}

/** What a delivery notice reports of a failure: the parts of its problem it carries itself. */
export interface ReportedFailure {
  message: string;
  remedy: string;
  code: string;
  phase: string;
  commit: string;
  occurredAt: string;
  gaps: readonly string[];
  version: string;
}

/** The problem a notice reports under title, such as a Session's delivery notice. */
export function reportedProblem(title: string, report: ReportedFailure): Problem {
  const { why, cause } = splitCause(report.message);
  return {
    title,
    why: why || PLATFORM_PROBLEM,
    remedy: remedyOf(report.remedy),
    gaps: report.gaps,
    version: report.version,
    details: { code: report.code, phase: report.phase, cause, commit: report.commit, at: report.occurredAt },
  };
}

/** The ErrorInfo an error carries, as ApiError holds what the BFF let cross. */
function errorInfoOf(reason: unknown): ErrorInfo__Output | undefined {
  if (!isRecord(reason) || !isRecord(reason.errorInfo)) return undefined;
  return reason.errorInfo as unknown as ErrorInfo__Output;
}

/** Whether reason is a server's answer, as ApiError carries its HTTP status, rather than Studio's own error. */
function isServerAnswer(reason: unknown): boolean {
  return isRecord(reason) && typeof reason.status === "number";
}

/**
 * The problem of a refused or failed request: the server's message and the remedy its ErrorInfo
 * reason declares. A server answer without one is a platform problem whose own message is folded;
 * an error Studio raised itself, such as a check before the request or a network failure, says why
 * in its own words and is tried again.
 */
export function errorProblem(reason: unknown, context: ProblemContext): Problem {
  const message = reason instanceof Error ? reason.message : String(reason);
  const info = errorInfoOf(reason);
  const declared = info?.metadata?.remedy;
  const own = !declared && reason instanceof Error && !isServerAnswer(reason);
  const shown = Boolean(declared) || own;
  return {
    title: `${context.operation} failed`,
    why: shown ? message : PLATFORM_PROBLEM,
    remedy: own ? FailureRemedy.FAILURE_REMEDY_APPLY_AGAIN : remedyOf(declared),
    gaps: context.gaps ?? [],
    version: versionLabel(context.version, ""),
    details: {
      code: info?.reason ?? "",
      phase: "",
      cause: shown ? "" : message,
      commit: context.commit ?? "",
      at: context.at ?? new Date().toISOString(),
    },
  };
}

/**
 * What a configuration that is not READY and lists no gap says: "Cannot check: {reason}", with the
 * first reason the server gave for not checking it.
 */
export function cannotCheckText(validationErrors: readonly string[]): string {
  return `Cannot check: ${validationErrors[0] || "the server gave no reason"}`;
}

/** The plain text of a problem's details, which the operator remedy copies. */
export function problemDetailsText(problem: Problem): string {
  const { code, phase, cause, commit, at } = problem.details;
  return [
    `What happened: ${problem.title}`,
    `Why: ${problem.why}`,
    code && `Code: ${code}`,
    phase && `Phase: ${phase}`,
    commit && `Commit: ${commit}`,
    at && `Time: ${at}`,
    cause && `Cause: ${cause}`,
  ]
    .filter(Boolean)
    .join("\n");
}
