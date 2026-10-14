// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Flow status labels by status code. They are also the `executionStatus` values a run search sends,
 * which the server compares verbatim, so they must not be replaced with display text; history
 * payloads use `flowStatusLabel` from lib/flow/enum-labels.ts instead.
 */
export const SEARCH_STATUS_VALUES = {
  0: "Unspecified",
  1: "Running",
  2: "Completed",
  3: "Failed",
  4: "Timed out",
  5: "Terminated",
  6: "Canceled",
  7: "Continued as new",
} as const;

export type FlowStatus = (typeof SEARCH_STATUS_VALUES)[keyof typeof SEARCH_STATUS_VALUES];

/** Running is the only open status. Decisions use the code, because label tables differ. */
export const RUNNING_FLOW_STATUS_CODE = 1;

export function isOpenFlowStatusCode(flowStatusCode: number | undefined): boolean {
  return flowStatusCode === RUNNING_FLOW_STATUS_CODE;
}

/** Codes 2 through 7 are the closed statuses, from Completed to Continued as new. */
export function isTerminalFlowStatusCode(flowStatusCode: number): boolean {
  return flowStatusCode >= 2 && flowStatusCode <= 7;
}
