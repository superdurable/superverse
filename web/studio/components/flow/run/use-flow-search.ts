// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import { searchFlows, type SearchFilter } from "@/lib/api/flow/flow-run";
import type { FlowRuntimeCatalogEntry, FlowSearchRow } from "@/lib/api/flow/flow-types";
import { absorb, nothingHeld, type Liveness } from "@/components/flow/run/detail/read-liveness";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";

/** The run list shows one page per Flow type; the newest runs come first from the server. */
const PAGE_SIZE = 50;

export interface FlowSearch {
  flows: FlowSearchRow[];
  loading: boolean;
  /** What the reader currently knows, which is not what the last request returned. */
  liveness: Liveness;
  runSearch: () => void;
}

export function useFlowSearch({
  entries,
  definitionRevision,
  filters,
  workQueueOnly,
}: {
  /** The Flow types to search: the selected one, or every one for all processes, merged by start time. */
  entries: readonly FlowRuntimeCatalogEntry[];
  definitionRevision: string;
  /** Owned by whatever renders the search controls, so there is one source of scope. */
  filters: readonly SearchFilter[];
  /** The Inbox scope: only runs whose current state awaits a declared Action. */
  workQueueOnly: boolean;
}): FlowSearch {
  const flowTypes = entries.map((entry) => entry.flowType).join(",");
  const client = useFlowRequestScope(flowTypes);
  const { definitionUpdateKey, handleDefinitionError } = useFlowCatalog();
  const [held, setHeld] = useState(() => nothingHeld<FlowSearchRow[]>());
  const [loading, setLoading] = useState(false);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), [flowTypes]);

  const search = useCallback(async () => {
    if (flowTypes === "") return;
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    setLoading(true);
    try {
      const results = await Promise.all(
        flowTypes
          .split(",")
          .map((flowType) =>
            searchFlows(
              client,
              { flowType, workQueueOnly, filters, pageSize: PAGE_SIZE },
              { definitionRevision, signal: controller.signal },
            ),
          ),
      );
      controller.signal.throwIfAborted();
      // Each page is newest first; merged, the newest runs of every Flow type come first.
      const flows = results
        .flatMap((result) => result.flows)
        .sort((a, b) => Date.parse(b.startTime ?? "") - Date.parse(a.startTime ?? ""));
      setHeld((prior) => absorb(prior, { state: "ok", value: flows }));
    } catch (failedSearch) {
      if (controller.signal.aborted) return;
      if (handleDefinitionError(failedSearch)) {
        setHeld(nothingHeld<FlowSearchRow[]>());
        return;
      }
      // Keep the rows that were true a moment ago; absorb marks them stale.
      setHeld((prior) =>
        absorb(prior, {
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
  }, [client, definitionRevision, filters, flowTypes, handleDefinitionError, workQueueOnly]);

  const runSearch = useCallback(() => void search(), [search]);

  // `filters` only changes when the reader submits, so this cannot fire per keystroke.
  useEffect(() => runSearch(), [definitionUpdateKey, runSearch]);

  return {
    flows: held.value ?? [],
    loading,
    liveness: held.liveness,
    runSearch,
  };
}
