// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Where a Flow design stands, which its label on the Flow tab says: its card waits for confirmation, the
 * agent builds it, or the push that built it is in and the Preview's processes are not built from it yet.
 */
export type DesignPhase = "waiting" | "building" | "built";

export function designPhaseLabel(phase: DesignPhase): string {
  if (phase === "waiting") return "Design · waiting for your confirmation";
  return phase === "building" ? "Design · building" : "Design · built, building processes";
}
