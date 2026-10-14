// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { getFlowCatalog, getFlowDefinitions } from "@/lib/api/flow/flow-run";
import type { FlowDefinitionCatalog, FlowRuntimeCatalog } from "@/lib/api/flow/flow-types";
import { useFlowApi } from "./FlowApiProvider";

interface FlowCatalogValue {
  ready: boolean;
  flowsAvailable: boolean;
  catalog: FlowRuntimeCatalog | null;
  definitions: FlowDefinitionCatalog | null;
  error: string;
  /** Says why an operation was refused and its form closed: the definition or deployment changed. */
  notice: string;
  definitionUpdateKey: number;
  handleDefinitionError: (error: unknown) => boolean;
}

const FlowCatalogContext = createContext<FlowCatalogValue | null>(null);

export function FlowCatalogProvider({ children }: { children: ReactNode }) {
  const [catalog, setCatalog] = useState<FlowRuntimeCatalog | null>(null);
  const [definitions, setDefinitions] = useState<FlowDefinitionCatalog | null>(null);
  const [runtimeCatalogLoaded, setRuntimeCatalogLoaded] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const [definitionUpdateKey, setDefinitionUpdateKey] = useState(0);
  const [notice, setNotice] = useState("");
  const client = useFlowApi();

  const loadCatalog = useCallback(
    async (signal?: AbortSignal) => {
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const [nextDefinitions, nextCatalog] = await Promise.all([
          getFlowDefinitions(client, signal),
          getFlowCatalog(client, signal)
            .then((value) => ({ ok: true as const, value }))
            .catch(() => ({
              ok: false as const,
              value: { enabled: false, flows: [], definitionRevision: "" },
            })),
        ]);
        const revisionsMatch =
          !nextCatalog.ok || nextDefinitions.definitionRevision === nextCatalog.value.definitionRevision;
        if (revisionsMatch) {
          setDefinitions(nextDefinitions);
          setCatalog(nextCatalog.value);
          setRuntimeCatalogLoaded(nextCatalog.ok);
          setError("");
          return;
        }
      }
      throw new Error("Flow Definition changed repeatedly while loading the catalog");
    },
    [client],
  );

  useEffect(() => {
    const controller = new AbortController();
    void Promise.resolve()
      .then(() => {
        controller.signal.throwIfAborted();
        return loadCatalog(controller.signal);
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(loadError, "Flow catalog failed to load"));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setReady(true);
      });
    return () => controller.abort();
  }, [loadCatalog]);

  const handleDefinitionError = useCallback(
    (failedRequest: unknown) => {
      if (
        !(failedRequest instanceof ApiError) ||
        !["FLOW_DEFINITION_CHANGED", "START_TARGET_CHANGED"].includes(failedRequest.code ?? "")
      ) {
        return false;
      }
      setNotice("Flow Definition or deployment updated. Review the refreshed target and confirm the operation again.");
      setDefinitionUpdateKey((current) => current + 1);
      void loadCatalog().catch((loadError: unknown) => {
        setError(errorMessage(loadError, "Flow catalog failed to reload"));
      });
      return true;
    },
    [loadCatalog],
  );

  const value = useMemo<FlowCatalogValue>(
    () => ({
      ready,
      flowsAvailable: runtimeCatalogLoaded && definitions?.configured === true,
      catalog,
      definitions,
      error,
      notice,
      definitionUpdateKey,
      handleDefinitionError,
    }),
    [catalog, definitionUpdateKey, definitions, error, handleDefinitionError, notice, runtimeCatalogLoaded, ready],
  );

  return <FlowCatalogContext.Provider value={value}>{children}</FlowCatalogContext.Provider>;
}

// A verified immutable source catalog is sufficient for a read-only design.
// It does not grant runtime operations or make Runs/Inbox ready.
export function DesignCatalogProvider({
  catalog,
  definitions,
  children,
}: {
  catalog: FlowRuntimeCatalog;
  definitions: FlowDefinitionCatalog;
  children: ReactNode;
}) {
  const value = useMemo<FlowCatalogValue>(
    () => ({
      ready: true,
      flowsAvailable: false,
      catalog,
      definitions,
      error: "",
      notice: "",
      definitionUpdateKey: 0,
      handleDefinitionError: () => false,
    }),
    [catalog, definitions],
  );
  return <FlowCatalogContext.Provider value={value}>{children}</FlowCatalogContext.Provider>;
}

export function useFlowCatalog() {
  const value = useContext(FlowCatalogContext);
  if (!value) throw new Error("useFlowCatalog must be used inside FlowCatalogProvider");
  return value;
}
