// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useAccount } from "@/lib/account/AccountProvider";
import type { AgentModel } from "@/lib/session/agent-types";
import type { MockScenarioCatalog } from "@/lib/session/mock-scenarios";

/**
 * The model a new Project's Session starts on, as the creation page preselects it: the platform
 * administrator starts on the mock model where the server offers it, and every other account, or
 * the administrator where the server offers none, on Sonnet. It is null while the administrator's
 * answer, the server's offer of the mock model, is still being read.
 */
export function useStartingModel(catalog: MockScenarioCatalog | null): AgentModel | null {
  const isAdmin = useAccount()?.isAdmin ?? false;
  if (!isAdmin) return "sonnet-5.5";
  if (catalog === null) return null;
  return catalog.selectable ? "mock" : "sonnet-5.5";
}
