// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { agentModelView } from "./agent-model";
import { listAgentModels } from "@/lib/api/agent";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { RpcOutput } from "@/lib/shared/rpc-methods";

export type AgentModelChoice = RpcOutput<"ListAgentModels">["models"][number];

/** The page's server-authorized catalog; failed reads offer no inferred models. */
export function useAgentModels() {
  const [catalog, setCatalog] = useState<{ models: readonly AgentModelChoice[]; error: string | null } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const read = (attempt: number) => {
      listAgentModels(controller.signal).then(
        ({ models }) => {
          if (!controller.signal.aborted) setCatalog({ models, error: null });
        },
        (reason) => {
          if (controller.signal.aborted) return;
          setCatalog({ models: [], error: errorMessage(reason, "Models are unavailable") });
          if (attempt < 3) timer = setTimeout(() => read(attempt + 1), 1000 * 2 ** attempt);
        },
      );
    };
    read(0);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);
  return catalog;
}

/** Local verified providers use the same scenario composer as the mock identity. */
export function usesMockEndpoint(
  catalog: ReturnType<typeof useAgentModels>,
  model: ReturnType<typeof agentModelView> | undefined,
) {
  return (
    model === "mock" ||
    !!catalog?.models.some((choice) => choice.mockEndpoint && agentModelView(choice.model) === model)
  );
}
