// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** Shared bounded policy for production reads and real-dependency browser drivers. */
export const READ_POLICY = {
  maxRetries: 3,
  backoffMilliseconds: 500,
  budgetMilliseconds: 60_000,
  retryStatuses: [503, 504] as readonly number[],
} as const;

export function readRetryDelay(attempt: number): number {
  return READ_POLICY.backoffMilliseconds * 2 ** attempt * (0.75 + Math.random() * 0.5);
}
