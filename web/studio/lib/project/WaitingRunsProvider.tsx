// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { getFlowCatalog, searchFlows } from "@/lib/api/flow/flow-run";
import { RUNNING_FLOW_STATUS_CODE, SEARCH_STATUS_VALUES } from "@/lib/api/flow/flow-status";
import { createFlowApiClient, type ObservedFlowTarget } from "@/lib/api/transport/flow-api-client";
import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import { flowApiRoutes, type ProjectEnvironment } from "@/lib/shared/studio-routes";

/** The Inbox reads one page of this many waiting runs per Flow Type; a full page counts as "50+". */
const WAITING_PAGE = 50;
/** Waiting runs change as people act; the frame reads them again this often, and on every surface change. */
const REREAD_MS = 60_000;
/** A run keeps its Action permission history after it closes, so only running ones are waiting, as in the Inbox. */
const RUNNING = { field: "executionStatus", operator: "eq", values: [SEARCH_STATUS_VALUES[RUNNING_FLOW_STATUS_CODE]] };

/** Where the waiting runs are read: the running Preview or the Live deployment, with what identifies it. */
interface WaitingRunsTarget {
  basePath: string;
  observed: ObservedFlowTarget;
  /** Changes when the instance or deployment behind basePath changes. */
  key: string;
}

/** Runs waiting for a person, by Flow Type, in the version the bar shows; null while unknown. */
type WaitingRuns = ReadonlyMap<string, number> | null;

const WaitingRunsContext = createContext<WaitingRuns>(null);

/** The waiting runs of the version shown: the Live deployment, or the Preview while it is READY. */
export function waitingRunsTarget(
  projectId: string,
  environment: ProjectEnvironment,
  live: ProjectLive__Output | null,
  preview: PreviewSnapshot__Output | null | undefined,
): WaitingRunsTarget | null {
  if (environment === "live")
    return (
      live && {
        basePath: flowApiRoutes.liveRuntime(projectId, live.releaseId),
        observed: { deploymentId: live.deploymentId },
        key: `live:${live.deploymentId}:${live.revision}`,
      }
    );
  if (preview?.status !== "PREVIEW_STATUS_READY" || !preview.definitionRevision) return null;
  return {
    basePath: flowApiRoutes.previewRuntime(projectId),
    observed: { workspaceRequestId: preview.workspaceRequestId, generation: preview.generation },
    key: `preview:${preview.workspaceRequestId}:${preview.servingRevision}`,
  };
}

/**
 * Reads, per Flow Type of target's catalog, the runs whose current state awaits a declared Action,
 * as the Inbox does. Without a target (Preview not running, nothing Live), or when a read fails, the
 * counts are unknown and every place that shows them stays absent.
 */
export function useReadWaitingRuns(target: WaitingRunsTarget | null, rereadKey: string): WaitingRuns {
  const [tick, setTick] = useState(0);
  const [held, setHeld] = useState<{ key: string; counts: Map<string, number> } | null>(null);
  const read = target === null ? null : JSON.stringify([target, rereadKey, tick]);
  useEffect(() => {
    const timer = window.setInterval(() => setTick((value) => value + 1), REREAD_MS);
    return () => window.clearInterval(timer);
  }, []);
  useEffect(() => {
    if (read === null) return;
    const [{ basePath, observed, key }] = JSON.parse(read) as [WaitingRunsTarget];
    const client = createFlowApiClient(basePath, observed);
    const controller = new AbortController();
    void (async () => {
      const catalog = await getFlowCatalog(client, controller.signal);
      const flowTypes = [...new Set(catalog.flows.map((flow) => flow.flowType))];
      const pages = await Promise.all(
        flowTypes.map((flowType) =>
          searchFlows(
            client,
            { flowType, workQueueOnly: true, filters: [RUNNING], pageSize: WAITING_PAGE },
            { definitionRevision: catalog.definitionRevision, signal: controller.signal },
          ),
        ),
      );
      if (!controller.signal.aborted)
        setHeld({ key, counts: new Map(flowTypes.map((flowType, index) => [flowType, pages[index].flows.length])) });
    })().catch(() => {
      if (!controller.signal.aborted) setHeld(null);
    });
    return () => {
      controller.abort();
      client.dispose();
    };
  }, [read]);
  return target !== null && held?.key === target.key ? held.counts : null;
}

export function WaitingRunsProvider({ runs, children }: { runs: WaitingRuns; children: ReactNode }) {
  return <WaitingRunsContext.Provider value={runs}>{children}</WaitingRunsContext.Provider>;
}

export function useWaitingRuns(): WaitingRuns {
  return useContext(WaitingRunsContext);
}

/** A count as the frame shows it: a full page reads as "50+". */
export function waitingCount(count: number): string {
  return count >= WAITING_PAGE ? `${WAITING_PAGE}+` : String(count);
}
