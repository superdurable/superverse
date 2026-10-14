// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useLayoutEffect, useMemo, type ReactNode } from "react";
import { configurationDraftQueue, type ConfigurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import {
  createFlowApiClient,
  createFlowRequestOwner,
  type FlowApiClient,
  type ObservedFlowTarget,
} from "@/lib/api/transport/flow-api-client";

const FlowApiContext = createContext<FlowApiClient | null>(null);
const BasePathContext = createContext<string | null>(null);

export function FlowApiProvider({
  basePath,
  deploymentId,
  workspaceRequestId,
  generation,
  children,
}: ObservedFlowTarget & {
  basePath: string;
  children: ReactNode;
}) {
  const client = useMemo(
    () => createFlowApiClient(basePath, { deploymentId, workspaceRequestId, generation }),
    [basePath, deploymentId, workspaceRequestId, generation],
  );
  useLayoutEffect(() => {
    client.activate();
    return () => client.dispose();
  }, [client]);
  return (
    <BasePathContext.Provider value={basePath}>
      <FlowApiContext.Provider value={client}>{children}</FlowApiContext.Provider>
    </BasePathContext.Provider>
  );
}

export function useFlowApi(): FlowApiClient {
  const client = useContext(FlowApiContext);
  if (!client) throw new Error("Flow views require an explicit project API scope");
  return client;
}

export function useFlowRequestScope(identity = ""): FlowApiClient {
  const parent = useFlowApi();
  const owner = useMemo(() => createFlowRequestOwner(parent, identity), [parent, identity]);
  useLayoutEffect(() => {
    owner.activate();
    return () => owner.dispose();
  }, [owner]);
  return owner;
}

/** One configuration host's Flow API: its client and base path, as Setup holds Preview's and the next publish's at once. */
export interface FlowApiScopeBinding {
  client: FlowApiClient;
  basePath: string;
}

/** The scope the views under it use, to hand to views that also sit under another scope. */
export function useFlowApiScope(): FlowApiScopeBinding {
  const client = useFlowApi();
  const basePath = useContext(BasePathContext);
  if (!basePath) throw new Error("Flow views require an explicit project API scope");
  return useMemo(() => ({ client, basePath }), [client, basePath]);
}

/**
 * Puts an existing scope under the views it wraps, such as Setup's Next publish cells beside Preview's;
 * the provider that created the client keeps owning it.
 */
export function FlowApiScope({ scope, children }: { scope: FlowApiScopeBinding; children: ReactNode }) {
  return (
    <BasePathContext.Provider value={scope.basePath}>
      <FlowApiContext.Provider value={scope.client}>{children}</FlowApiContext.Provider>
    </BasePathContext.Provider>
  );
}

/** The autosave queue of the configuration scope these Flow views edit, one per scope in this tab. */
export function useConfigurationDraftQueue(): ConfigurationDraftQueue {
  const basePath = useContext(BasePathContext);
  if (!basePath) throw new Error("Configuration editors require an explicit project API scope");
  return configurationDraftQueue(basePath);
}
