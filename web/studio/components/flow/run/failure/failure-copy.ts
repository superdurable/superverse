// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { RunFailure } from "@/components/flow/canvas/run-failure";
import { SEARCH_STATUS_VALUES, type FlowStatus } from "@/lib/api/flow/flow-status";

/** What happens now, and whether it waits for a person (`stuck`) or the request carries on by itself. */
interface FailureOutlook {
  text: string;
  stuck: boolean;
}

export const FAILURE_COPY = {
  happened: "What happened",
  happensNow: "What happens now",
  region: "What went wrong",
  error: "Error",
  code: "Code",
  tries: "Tries",
  step: "Step",
  runFailed: "This request ended as failed.",
  runFailedNow: "It does not go on by itself.",
} as const;

/**
 * Errors whose usual words say the same thing in plain ones. Dex reports timeouts and unreachable
 * services in the engine's or the network library's words, whatever the Step was doing.
 */
const KNOWN_CAUSES: readonly { pattern: RegExp; words: string }[] = [
  { pattern: /deadline exceeded|timed out|timeout/i, words: "it did not finish in time" },
  { pattern: /connection refused|connection reset|no such host|unreachable/i, words: "it could not connect" },
];

/** The longest cause the sentence repeats; anything longer is left to More details. */
const CAUSE_LIMIT = 120;

/**
 * The cause in the error's own words: Go wraps an error as "operation: cause", so the last part says
 * what went wrong. A bare code or a long dump is left out of the sentence.
 */
function plainCause(error: string | undefined): string {
  if (!error) return "";
  const known = KNOWN_CAUSES.find((cause) => cause.pattern.test(error));
  if (known) return known.words;
  const line = error.split("\n")[0].trim();
  const cause = (line.split(": ").at(-1) ?? "").trim().replace(/[.!]+$/, "");
  if (cause.length > CAUSE_LIMIT || !/[a-z]+\s+[a-z]+/i.test(cause)) return "";
  return /^[A-Z]{2,}\b/.test(cause) ? cause : cause.charAt(0).toLowerCase() + cause.slice(1);
}

/** One sentence from the Step title and the failure, such as "Refund in Stripe failed: the card was declined." */
export function whatHappened(failure: RunFailure): string {
  const cause = plainCause(failure.error);
  const how = failure.method === "waitFor" ? "failed while waiting" : "failed";
  return `${failure.title} ${how}${cause ? `: ${cause}` : ""}.`;
}

/** The failure for the agent: its Step and the engine's words, as one line. */
export function failureLine(failure: RunFailure): string {
  const error = failure.error?.trim().replace(/[.!]*$/, ".");
  return error ? `${failure.title} failed: ${error}` : `${failure.title} failed.`;
}

/** After a failure that led on to another Step: the rest of the sentence, how the request ended or that it goes on. */
const AFTER_HANDLING: Record<FlowStatus, FailureOutlook | null> = {
  Unspecified: null,
  Running: { text: " and is still going.", stuck: false },
  Completed: { text: " and finished.", stuck: false },
  Failed: { text: ", but it still ended as failed and needs someone to look at it.", stuck: true },
  "Timed out": { text: ", but it ran out of time and needs someone to look at it.", stuck: true },
  Terminated: { text: " and was stopped later.", stuck: false },
  Canceled: { text: " and was stopped later.", stuck: false },
  "Continued as new": null,
};

/** After a failure that nothing picked up, by how the request stands. */
const AFTER_NOTHING: Record<FlowStatus, FailureOutlook | null> = {
  Unspecified: null,
  Running: { text: "The request is still running, so this may still change.", stuck: false },
  Completed: { text: "The request finished anyway.", stuck: false },
  Failed: { text: "The request stopped here and needs someone to retry this step or fix the process.", stuck: true },
  "Timed out": { text: "The request ran out of time and stopped. It needs someone to look at it.", stuck: true },
  Terminated: { text: "The request was stopped, so it goes no further.", stuck: false },
  Canceled: { text: "The request was stopped, so it goes no further.", stuck: false },
  "Continued as new": null,
};

function statusOf(flowStatusCode: number | undefined): FlowStatus | null {
  return flowStatusCode !== undefined && Object.hasOwn(SEARCH_STATUS_VALUES, flowStatusCode)
    ? SEARCH_STATUS_VALUES[flowStatusCode as keyof typeof SEARCH_STATUS_VALUES]
    : null;
}

/**
 * What happens now, from the run alone: whether failure handling took over (the definition routes the
 * failure to the Step the run went on to) or the request stopped and needs someone. Null while the
 * run's status is unknown, so nothing is guessed.
 */
export function whatHappensNow(failure: RunFailure, flowStatusCode: number | undefined): FailureOutlook | null {
  const status = statusOf(flowStatusCode);
  if (status === null) return null;
  if (failure.next === null) return AFTER_NOTHING[status];
  const after = AFTER_HANDLING[status];
  if (after === null) return null;
  const lead = failure.next.viaFailureRoute
    ? `Failure handling took over: the request went on to ${failure.next.title}`
    : `The request went on to ${failure.next.title}`;
  return { text: `${lead}${after.text}`, stuck: after.stuck };
}

/** Whether the run as a whole failed, so its failure leads the drawer whichever Step is shown. */
export function runEndedFailed(flowStatusCode: number | undefined): boolean {
  return statusOf(flowStatusCode) === "Failed";
}
