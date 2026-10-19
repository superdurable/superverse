// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { searchFlows, type SearchFilter } from "@/lib/api/flow/flow-run";
import type { FlowRuntimeCatalogEntry, FlowSearchRow } from "@/lib/api/flow/flow-types";
import { absorb, nothingHeld, type Liveness } from "@/components/flow/run/detail/read-liveness";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";
import { RUN_PAGE_SIZE, readGroupPages, tallyHealth, type RunHealth } from "./run-groups";
import {
  FLOW_SEARCH_OUTCOME_FAILURE,
  FLOW_SEARCH_OUTCOME_SUCCESS,
  FLOW_SEARCH_OUTCOME_UNCHANGED,
  FlowSearchBackoff,
} from "./flow-search-backoff";

function isRateLimited(error: unknown): error is ApiError {
  return error instanceof ApiError && error.status === 429;
}

/** What one read holds: the list's runs and, when asked for, each group's runs and count. */
interface SearchRead {
  flows: FlowSearchRow[];
  health: RunHealth | null;
}

export interface FlowSearch {
  flows: FlowSearchRow[];
  /** Each group's runs and count, read beside the list; null on the Inbox and before the first read. */
  health: RunHealth | null;
  loading: boolean;
  /** What the reader currently knows, which is not what the last request returned. */
  liveness: Liveness;
  /** Counts the reads that answered, so a view can tell a read that began after its change. */
  reads: number;
  runSearch: () => void;
}

export function useFlowSearch({
  entries,
  definitionRevision,
  filters,
  workQueueOnly,
  health,
}: {
  /** The Flow types to search: the selected one, or every one for all processes, merged by start time. */
  entries: readonly FlowRuntimeCatalogEntry[];
  definitionRevision: string;
  /** Owned by whatever renders the search controls, so there is one source of scope. */
  filters: readonly SearchFilter[];
  /** The Inbox scope: only runs whose current state awaits a declared Action. */
  workQueueOnly: boolean;
  /** Also read each group's page, one search per group and Flow type, on the same beat as the list. */
  health: boolean;
}): FlowSearch {
  const flowTypes = entries.map((entry) => entry.flowType).join(",");
  const client = useFlowRequestScope(flowTypes);
  const { definitionUpdateKey, handleDefinitionError } = useFlowCatalog();
  const [held, setHeld] = useState(() => nothingHeld<SearchRead>());
  const [reads, setReads] = useState(0);
  const [loading, setLoading] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const backoff = useRef<FlowSearchBackoff | null>(null);
  useEffect(() => () => pending.current?.abort(), [flowTypes]);

  // A quiet read is the timer's: it neither shows "Searching…" nor cuts short a read the reader asked for.
  const search = useCallback(
    async (quiet: boolean) => {
      if (flowTypes === "" || (quiet && pending.current)) {
        if (quiet) backoff.current?.settled(FLOW_SEARCH_OUTCOME_UNCHANGED);
        return;
      }
      pending.current?.abort();
      const controller = new AbortController();
      pending.current = controller;
      if (!quiet) setLoading(true);
      try {
        const groupFailures: unknown[] = [];
        const results = await Promise.all(
          flowTypes.split(",").map((flowType) => {
            const read = (scope: { workQueueOnly: boolean; filters: readonly SearchFilter[] }) =>
              searchFlows(
                client,
                { flowType, ...scope, pageSize: RUN_PAGE_SIZE },
                { definitionRevision, signal: controller.signal },
              );
            return Promise.all([
              read({ workQueueOnly, filters }),
              health ? readGroupPages(read, filters, (error) => groupFailures.push(error)) : null,
            ]);
          }),
        );
        controller.signal.throwIfAborted();
        // Each page is newest first; merged, the newest runs of every Flow type come first.
        const flows = results
          .flatMap(([result]) => result.flows)
          .sort((a, b) => Date.parse(b.startTime ?? "") - Date.parse(a.startTime ?? ""));
        const groups = results.flatMap(([, pages]) => (pages ? [pages] : []));
        const value = { flows, health: health ? tallyHealth(groups) : null };
        setHeld((prior) => absorb(prior, { state: "ok", value }));
        setReads((count) => count + 1);
        // A failed group page leaves only its group unknown, but still slows the next quiet read.
        const rateLimitedGroup = groupFailures.find(isRateLimited);
        if (groupFailures.length === 0) {
          backoff.current?.settled(FLOW_SEARCH_OUTCOME_SUCCESS);
        } else {
          backoff.current?.settled(
            quiet || rateLimitedGroup ? FLOW_SEARCH_OUTCOME_FAILURE : FLOW_SEARCH_OUTCOME_UNCHANGED,
            rateLimitedGroup?.retryAfter,
          );
        }
      } catch (failedSearch) {
        if (controller.signal.aborted) return;
        const rateLimited = isRateLimited(failedSearch);
        backoff.current?.settled(
          quiet || rateLimited ? FLOW_SEARCH_OUTCOME_FAILURE : FLOW_SEARCH_OUTCOME_UNCHANGED,
          rateLimited ? failedSearch.retryAfter : undefined,
        );
        if (handleDefinitionError(failedSearch)) {
          setHeld(nothingHeld<SearchRead>());
          return;
        }
        // Keep the rows that were true a moment ago; absorb marks them stale.
        setHeld((prior) =>
          absorb(rateLimited && prior.value === null ? { ...prior, value: { flows: [], health: null } } : prior, {
            state: "unreachable",
            reason: errorMessage(failedSearch, String(failedSearch)),
          }),
        );
      } finally {
        if (pending.current === controller) {
          pending.current = null;
          setLoading(false);
        }
      }
    },
    [client, definitionRevision, filters, flowTypes, handleDefinitionError, health, workQueueOnly],
  );

  const runSearch = useCallback(() => void search(false), [search]);

  // `filters` only changes when the reader submits, so this cannot fire per keystroke.
  useEffect(() => runSearch(), [definitionUpdateKey, runSearch]);

  useEffect(() => {
    const poller = new FlowSearchBackoff(() => {
      if (document.visibilityState === "visible") void search(true);
      else poller.settled(FLOW_SEARCH_OUTCOME_UNCHANGED);
    });
    backoff.current = poller;
    poller.start();
    return () => {
      poller.stop();
      if (backoff.current === poller) backoff.current = null;
    };
  }, [search]);

  return {
    flows: held.value?.flows ?? [],
    health: held.value?.health ?? null,
    loading,
    liveness: held.liveness,
    reads,
    runSearch,
  };
}
