// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowSummary, FlowSearchRow } from "@/lib/api/flow/flow-types";

export function usesCurrentDefinition(
  value: Pick<FlowSummary | FlowSearchRow, "definitionBinding" | "definitionRevision"> | null | undefined,
  revision?: string,
): boolean {
  return Boolean(revision && value?.definitionBinding === "CURRENT" && value.definitionRevision === revision);
}
export const unavailableDefinitionMessage =
  "The definition for this execution is unavailable in this environment. Open Diagnostics to inspect its original history and state.";
