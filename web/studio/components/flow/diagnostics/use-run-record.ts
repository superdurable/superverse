// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";
import { hydrateBlobs, VALUE_BLOB_UNAVAILABLE } from "@/lib/api/flow/flow-blobs";
import {
  deleteFlowChannelMessage,
  getFlowState,
  getFlowSummary,
  loadRunHistory,
  waitFlowHistory,
} from "@/lib/api/flow/flow-diagnostics";
import { isTerminalFlowStatusCode } from "@/lib/api/flow/flow-status";
import type { FlowHistoryEvent, FlowState, FlowSummary } from "@/lib/api/flow/flow-types";
import { errorMessage } from "@/lib/api/transport/api-error";
import { isAbortError } from "@/lib/shared/async";

/**
 * One run as Diagnostics reads it: its summary, semantic history and, while it is open, live state.
 * With `live` on, a long-poll on the history wakes a reload each time the run records an event.
 * Stored Value blobs load in the background: the state's on every read, an event's when a view asks
 * through `hydrateEvent`, after which `history` carries the hydrated copy in place of the event.
 */
export function useRunRecord(flowId: string, runId: string, live: boolean) {
  const api = useFlowRequestScope(`${flowId}:${runId}`);
  const [summary, setSummary] = useState<FlowSummary | null>(null);
  const [recordedHistory, setRecordedHistory] = useState<FlowHistoryEvent[]>([]);
  const [state, setState] = useState<FlowState | null>(null);
  const [nextInternalEventId, setNextInternalEventId] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [waitCycle, setWaitCycle] = useState(0);
  const [hydratedEvents, setHydratedEvents] = useState<Record<number, FlowHistoryEvent>>({});
  const [deletingChannelMessage, setDeletingChannelMessage] = useState("");
  const blobCache = useRef(new Map<string, unknown>());
  /** Events whose hydration is loading or done; a failed one leaves so a later request retries it. */
  const requestedEventIDs = useRef(new Set<number>());

  const addWarning = useCallback((warning: string) => {
    setWarnings((current) => (current.includes(warning) ? current : [...current, warning]));
  }, []);

  /** A closed run has no live state. An open run's state shows at once, then again with its blobs. */
  const loadState = useCallback(
    async (statusCode: number) => {
      if (isTerminalFlowStatusCode(statusCode)) {
        setState(null);
        return;
      }
      try {
        const rawState = await getFlowState(api, flowId, runId);
        setState(rawState);
        const hydrated = await hydrateBlobs(flowId, rawState, blobCache.current, api);
        setState(hydrated.value);
        if (hydrated.error) addWarning(hydrated.error);
      } catch (stateError) {
        setError(errorMessage(stateError, "State query failed"));
      }
    },
    [api, addWarning, flowId, runId],
  );

  /** Reads the summary, then the history after `fromInternalEventId` (all of it from 0) and the state. */
  const reload = useCallback(
    async (fromInternalEventId: number) => {
      const latest = await getFlowSummary(api, flowId, runId);
      setSummary(latest);
      await Promise.all([
        loadRunHistory(api, flowId, runId, fromInternalEventId).then((page) => {
          setRecordedHistory((current) =>
            inEventOrder(fromInternalEventId > 0 ? [...current, ...page.events] : page.events),
          );
          setNextInternalEventId(page.nextInternalEventId);
        }),
        loadState(latest.flowStatusCode),
      ]);
    },
    [api, flowId, loadState, runId],
  );

  const refresh = useCallback(async () => {
    setError("");
    try {
      await reload(0);
    } catch (refreshError) {
      setError(errorMessage(refreshError, "Run refresh failed"));
    }
  }, [reload]);

  useEffect(() => {
    let active = true;
    void Promise.resolve()
      .then(() => (active ? refresh() : undefined))
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [refresh]);

  useEffect(() => {
    if (!live || !summary || isTerminalFlowStatusCode(summary.flowStatusCode) || nextInternalEventId <= 0) return;

    // Any change that would make this wait stale re-runs the effect, whose cleanup aborts it.
    const controller = new AbortController();
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    const scheduleRetry = () => {
      retryTimer = setTimeout(() => {
        if (!controller.signal.aborted) setWaitCycle((current) => current + 1);
      }, 1000);
    };
    void waitFlowHistory(api, flowId, runId, nextInternalEventId, controller.signal)
      .then(async (wait) => {
        if (controller.signal.aborted) return;
        // A wait that reached its deadline without a new event waits again at once.
        if (wait === "deadline") {
          setWaitCycle((current) => current + 1);
          return;
        }
        if (wait === "unavailable") {
          scheduleRetry();
          return;
        }
        await reload(nextInternalEventId);
      })
      .catch((waitError) => {
        if (isAbortError(waitError)) return;
        setError(errorMessage(waitError, "History wait failed"));
        scheduleRetry();
      });
    return () => {
      controller.abort();
      if (retryTimer) clearTimeout(retryTimer);
    };
  }, [api, flowId, live, nextInternalEventId, reload, runId, summary, waitCycle]);

  const hydrateEvent = useCallback(
    async (event: FlowHistoryEvent) => {
      if (requestedEventIDs.current.has(event.eventId)) return;
      requestedEventIDs.current.add(event.eventId);
      try {
        const hydrated = await hydrateBlobs(flowId, event, blobCache.current, api);
        setHydratedEvents((current) => ({ ...current, [event.eventId]: hydrated.value }));
        if (hydrated.error) addWarning(hydrated.error);
      } catch {
        requestedEventIDs.current.delete(event.eventId);
        addWarning(VALUE_BLOB_UNAVAILABLE);
      }
    },
    [api, addWarning, flowId],
  );

  const history = useMemo(
    () => recordedHistory.map((event) => hydratedEvents[event.eventId] ?? event),
    [recordedHistory, hydratedEvents],
  );

  /** Removes one pending Channel message, then rereads the live state that lists the Channel. */
  const deleteChannelMessage = useCallback(
    async (statusCode: number, channelName: string, messageId: string) => {
      setDeletingChannelMessage(messageId);
      setError("");
      try {
        await deleteFlowChannelMessage(api, { flowId, runId, channelName, messageId });
      } catch (deleteError) {
        setError(errorMessage(deleteError, "Channel deletion failed"));
      } finally {
        setDeletingChannelMessage("");
        await loadState(statusCode);
      }
    },
    [api, flowId, loadState, runId],
  );

  return {
    summary,
    history,
    state,
    loading,
    error,
    warnings,
    refresh,
    hydrateEvent,
    deletingChannelMessage,
    deleteChannelMessage,
  };
}

/** Each event once, in event order: a long-poll append can repeat events the history already holds. */
function inEventOrder(events: FlowHistoryEvent[]): FlowHistoryEvent[] {
  return [...new Map(events.map((event) => [event.eventId, event])).values()].sort(
    (left, right) => left.eventId - right.eventId,
  );
}
