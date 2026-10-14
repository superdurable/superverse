// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { isRecord } from "./json";

/**
 * A revision fence that no longer held: the resource changed after the page read it, so the write did not
 * run. Studio says so and reads the resource again.
 */
export const REVISION_CHANGED: ErrorInfo__Output = {
  reason: "REVISION_CHANGED",
  domain: "superverse.v2",
  metadata: {},
};

/** Whether `value`, an ErrorInfo as the BFF or the browser received it, is REVISION_CHANGED. */
export function isRevisionChanged(value: unknown): boolean {
  return isRecord(value) && value.domain === REVISION_CHANGED.domain && value.reason === REVISION_CHANGED.reason;
}
