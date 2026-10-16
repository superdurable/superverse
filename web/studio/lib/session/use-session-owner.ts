// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { getSession, listArchivedMessages } from "@/lib/api/session";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { watch } from "@/lib/api/transport/watch";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import { delay, isAbortError } from "@/lib/shared/async";
import { timestampToISO } from "@/lib/shared/protobuf";
import { commandFailureText, stoppedExecutionReason, useSessionCommands } from "./session-commands";
import { AgentStatus, EventStream, type AgentSnapshot, type SequencedMessage } from "./agent-types";
import { agentActivityView, agentHistoryView, agentSnapshotView } from "./agent-view";
import { requirementsDraftView } from "./draft/requirements-draft";
import { initialSessionState, sessionReducer } from "./session-reducer";
import { isStaleSessionSnapshot, sessionStatus, type KnownSessionStatus } from "./session-status";

/*
 * The page's one owner of its Project's Session: its snapshot, its single Watch
 * and its Agent commands. Watch snapshots own durable state; Get reads recover
 * after focus, reconnect or a failed command, and never replace a Watch
 * snapshot that arrived while they were in flight. Commands return admission
 * only.
 */

const STREAM_RECONNECT_MILLISECONDS = 750;
// ProjectFlow starts the Session as the Project becomes READY. Until the Session
// exists its read is NOT_FOUND, which repeats every second for at most 30 seconds.
const SESSION_START_WAIT_MILLISECONDS = 30_000;
const SESSION_START_RETRY_MILLISECONDS = 1_000;
export function useSessionOwner(projectId: string) {
  const [state, dispatch] = useReducer(sessionReducer, initialSessionState);
  const sessionStartDeadline = useRef<number | null>(null);
  const [isDocumentVisible, setDocumentVisible] = useState(
    () => typeof document === "undefined" || document.visibilityState === "visible",
  );
  const refreshInFlight = useRef(false);
  const refreshControllers = useRef(new Set<AbortController>());
  const refreshWaiters = useRef<Array<() => void>>([]);
  const historyController = useRef<AbortController | null>(null);
  const historyCursor = useRef<number | null>(null);
  const historyWasLoaded = useRef(false);
  const status = useRef<KnownSessionStatus | null>(null);
  const latestSnapshot = useRef<AgentSnapshot | null>(null);
  const mutationController = useRef<AbortController | null>(null);
  const progressCursor = useRef("");
  const snapshotVersion = useRef(0);
  const resourceRevision = useRef<string | null>(null);
  // Bumped when this owner's effects end, so late responses of an older run are dropped.
  const epoch = useRef(0);

  // A failure that found the engine execution stopped turns the Session FAILED at once; the
  // next snapshot, which joins the same engine status, confirms it.
  const noteStoppedExecution = useCallback((reason: unknown) => {
    const stopped = stoppedExecutionReason(reason);
    if (stopped === null) return;
    status.current = "SESSION_STATUS_FAILED";
    dispatch({ type: "executionStopped", reason: stopped });
  }, []);

  const applySnapshot = useCallback(
    (resource: SessionSnapshot__Output) => {
      if (resource.projectId !== projectId) throw new Error("Session snapshot scope mismatch");
      const snapshot = resource.agent ? agentSnapshotView(resource.agent) : null;
      if (isStaleSessionSnapshot(resourceRevision.current, resource.revision, latestSnapshot.current, snapshot)) return;
      latestSnapshot.current = snapshot;
      resourceRevision.current = resource.revision;
      status.current = sessionStatus(resource);
      snapshotVersion.current++;
      if (!historyWasLoaded.current) historyCursor.current = snapshot?.history.nextBeforeSequence ?? null;
      dispatch({ type: "resource", resource, snapshot, hasEarlierMessages: historyCursor.current !== null });
    },
    [projectId],
  );

  // One Get at a time; a later caller waits for the current read to finish.
  const refresh = useCallback(
    async (signal?: AbortSignal, propagateFailure = false) => {
      const started = epoch.current;
      const isCurrent = () => !signal?.aborted && started === epoch.current;
      if (!isCurrent()) return;
      while (refreshInFlight.current) {
        await new Promise<void>((resolve) => refreshWaiters.current.push(resolve));
        if (!isCurrent()) return;
      }
      refreshInFlight.current = true;
      const controller = new AbortController();
      const abort = () => controller.abort();
      signal?.addEventListener("abort", abort, { once: true });
      refreshControllers.current.add(controller);
      try {
        const requestedVersion = snapshotVersion.current;
        const snapshot = await getSession({ projectId }, controller.signal);
        // A full Watch snapshot received while Get was in flight wins.
        if (isCurrent() && snapshotVersion.current === requestedVersion) applySnapshot(snapshot);
      } catch (reason) {
        const missing = reason instanceof ApiError && reason.status === 404;
        if (isCurrent() && missing && Date.now() < (sessionStartDeadline.current ?? 0)) {
          // The Session is still starting: read again shortly.
          dispatch({ type: "sessionStarting" });
        } else if (isCurrent() && !isAbortError(reason)) {
          dispatch({
            type: "error",
            error: missing
              ? "The project's agent did not start. Reload the page; if this persists, the project needs an operator."
              : errorMessage(reason, "Unexpected session error"),
            unavailable: reason instanceof ApiError && [401, 403, 404].includes(reason.status),
          });
        }
        if (propagateFailure && isCurrent()) throw reason;
      } finally {
        signal?.removeEventListener("abort", abort);
        refreshControllers.current.delete(controller);
        refreshInFlight.current = false;
        const waiters = refreshWaiters.current;
        refreshWaiters.current = [];
        for (const resolve of waiters) resolve();
      }
    },
    [applySnapshot, projectId],
  );

  // Read on mount, and again whenever the page comes back: focus, history
  // navigation or the network returning.
  useEffect(() => {
    sessionStartDeadline.current ??= Date.now() + SESSION_START_WAIT_MILLISECONDS;
    const started = ++epoch.current;
    const reads = refreshControllers.current;
    const controller = new AbortController();
    void refresh(controller.signal);
    const onResume = () => void refresh(controller.signal);
    const resumeEvents = ["focus", "pageshow", "online"] as const;
    for (const name of resumeEvents) window.addEventListener(name, onResume);
    return () => {
      epoch.current = started + 1;
      controller.abort();
      for (const read of reads) read.abort();
      mutationController.current?.abort();
      historyController.current?.abort();
      for (const name of resumeEvents) window.removeEventListener(name, onResume);
    };
  }, [refresh]);

  useEffect(() => {
    const update = () => {
      const visible = document.visibilityState === "visible";
      setDocumentVisible(visible);
      if (visible) void refresh();
    };
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, [refresh]);

  // While the Session is starting, its read repeats until it finds the Session.
  useEffect(() => {
    if (state.sessionStartReads === 0) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => void refresh(controller.signal), SESSION_START_RETRY_MILLISECONDS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [refresh, state.sessionStartReads]);

  // The one Session Watch: open while the page is visible and the Session is
  // readable; after a failure it reconnects from its last cursor and re-reads.
  useEffect(() => {
    if (state.unavailable || !isDocumentVisible) return;
    const controller = new AbortController();
    dispatch({ type: "streamConnecting" });
    void watch(
      "WatchSession",
      { projectId },
      {
        resumeCursor: progressCursor.current,
        signal: controller.signal,
        onFrame: ({ snapshot, progress, resumeCursor }) => {
          if (snapshot) {
            applySnapshot(snapshot);
          } else if (progress) {
            const position = {
              resumeToken: resumeCursor,
              createdAt: timestampToISO(progress.createdAt) ?? "",
              source: progress.source,
            };
            if (progress.taskId && progress.activity) {
              dispatch({
                type: "subagentActivity",
                taskId: progress.taskId,
                event: agentActivityView(progress.activity),
              });
            } else if (progress.content === "activity" && progress.activity) {
              dispatch({
                type: "activityEvent",
                event: { ...position, kind: "activity", value: agentActivityView(progress.activity) },
              });
            } else if (progress.content === "assistantText") {
              dispatch({
                type: "textEvent",
                stream: EventStream.ASSISTANT,
                event: { ...position, kind: "assistant_text", value: progress.assistantText ?? "" },
              });
            } else if (progress.content === "reasoningSummary") {
              dispatch({
                type: "textEvent",
                stream: EventStream.REASONING,
                event: { ...position, kind: "reasoning_summary", value: progress.reasoningSummary ?? "" },
              });
            } else if (progress.content === "requirementsDraft" && progress.requirementsDraft) {
              dispatch({ type: "requirementsDraft", draft: requirementsDraftView(progress.requirementsDraft) });
            }
          }
          progressCursor.current = resumeCursor;
          dispatch({ type: "streamConnected" });
        },
      },
    ).catch(async () => {
      if (controller.signal.aborted) return;
      await delay(STREAM_RECONNECT_MILLISECONDS, controller.signal);
      if (controller.signal.aborted) return;
      dispatch({ type: "streamFailed" });
      void refresh();
    });
    return () => controller.abort();
  }, [applySnapshot, isDocumentVisible, refresh, projectId, state.streamGeneration, state.unavailable]);

  // Archived history pages load backwards from the oldest loaded message.
  const loadHistory = useCallback(
    async (mode: "page" | "beginning") => {
      let beforeSequence = historyCursor.current;
      if (beforeSequence === null || latestSnapshot.current === null || historyController.current) return;
      const started = epoch.current;
      const controller = new AbortController();
      historyController.current = controller;
      const isCurrent = () =>
        !controller.signal.aborted && started === epoch.current && historyController.current === controller;
      const messages: SequencedMessage[] = [];
      const accept = () => {
        historyWasLoaded.current = true;
        historyCursor.current = beforeSequence;
        dispatch({ type: "archived", messages, hasEarlierMessages: beforeSequence !== null });
      };
      dispatch({ type: "historyLoading", loading: true, mode, loadedCount: 0 });
      try {
        do {
          const pageSize = mode === "page" ? 50 : 200;
          const page = agentHistoryView(
            await listArchivedMessages({ projectId, beforeSequence, pageSize }, controller.signal),
          );
          if (!isCurrent()) return;
          messages.unshift(...page.messages);
          beforeSequence = page.nextBeforeSequence;
          dispatch({ type: "historyLoading", loading: true, mode, loadedCount: messages.length });
        } while (mode === "beginning" && beforeSequence !== null);
        accept();
      } catch (reason) {
        if (isCurrent()) {
          if (messages.length > 0) accept();
          noteStoppedExecution(reason);
          if (!isAbortError(reason))
            dispatch({ type: "commandError", error: errorMessage(reason, "Unexpected session error") });
        }
      } finally {
        if (historyController.current === controller) {
          historyController.current = null;
          if (started === epoch.current) dispatch({ type: "historyLoading", loading: false });
        }
      }
    },
    [noteStoppedExecution, projectId],
  );

  /**
   * One Agent command at a time, only while the Agent can take it. The receipt
   * confirms admission, not the resulting interaction: the Watch stays live and
   * its next snapshot owns the visible result. A failed command re-reads the
   * Session and is never replayed automatically.
   */
  const mutate = useCallback(
    async (kind: string, command: (signal: AbortSignal) => Promise<void>) => {
      const description = latestSnapshot.current?.description;
      const rejection =
        mutationController.current ||
        status.current !== "SESSION_STATUS_ACTIVE" ||
        !description ||
        description.status === AgentStatus.INITIALIZING
          ? "Project session is not ready to accept this command"
          : null;
      if (rejection !== null) {
        dispatch({ type: "commandError", error: rejection });
        throw new Error(rejection);
      }
      const started = epoch.current;
      const controller = new AbortController();
      mutationController.current = controller;
      dispatch({ type: "mutating", mutating: true, kind });
      const isCurrent = () => !controller.signal.aborted && started === epoch.current;
      try {
        await command(controller.signal);
      } catch (reason) {
        if (isCurrent()) {
          noteStoppedExecution(reason);
          void refresh();
          dispatch({ type: "commandError", error: commandFailureText(reason) });
        }
        throw reason;
      } finally {
        if (mutationController.current === controller) mutationController.current = null;
        if (isCurrent()) dispatch({ type: "mutating", mutating: false });
      }
    },
    [noteStoppedExecution, refresh],
  );

  const commands = useSessionCommands({ projectId, mutate, dispatch, latestSnapshot, loadHistory });

  const resourceStatus = useMemo(() => (state.resource ? sessionStatus(state.resource) : null), [state.resource]);
  return { ...state, ...commands, status: resourceStatus, projectId, refresh };
}
