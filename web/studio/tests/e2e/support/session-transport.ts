// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test, type Page } from "@playwright/test";
import type { Protocol } from "playwright-core/types/protocol";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import type { SessionStatus__Output } from "@/lib/generated/protobuf/superverse/v2/SessionStatus";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { ENDED_SESSION_STATUSES, expectNewestFinalReplySequence } from "./project";

/** One Session Watch (EventSource request) of one document; the Project addresses its one Session. */
type SessionWatch = { projectId: string; frameId: string; loaderId: string; snapshots: number; frames: number };
type LifecycleEntry = { kind: string; at: number; requestId?: string } & Partial<SessionWatch>;
/**
 * What the observer learned of one Session from its Watch frames: identities and counts, never text.
 * modelSequences maps each model call (source) to the message sequence it will commit, textSources holds
 * the calls that streamed assistant text, the counts are diagnostics only, and tracking stops once the
 * bounded source budget is exceeded.
 */
type Observation = {
  lastSequence: number;
  status: string;
  sessionStatus: SessionStatus__Output;
  pendingUserInput: boolean;
  modelSequences: Map<string, number>;
  textSources: Set<string>;
  textFrames: number;
  modelEvents: number;
  tracking: boolean;
};

const MODEL_LIFECYCLE = new Set(["KIND_MODEL_STARTED", "KIND_MODEL_COMPLETED", "KIND_MODEL_FAILED"]);

/**
 * The value of `promise`, or a rejection with `message` after `milliseconds`. Racing cannot cancel the
 * promise, so its late rejection is observed rather than left unhandled.
 */
export async function withDeadline<T>(promise: Promise<T>, milliseconds: number, message: string): Promise<T> {
  void promise.catch(() => {});
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => (timer = setTimeout(() => reject(new Error(message)), milliseconds))),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

/** Observe actual browser network traffic. No interception, replacement, or provider fixtures. */
export async function observeSessionTransport(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  let closing = false;
  let closePromise: Promise<void> | undefined;
  let detachPromise: Promise<void> | undefined;
  const ensureOpen = () => {
    if (closing) throw new Error("Session observer closed before reconnect verification completed");
  };
  // An unconfirmed detach leaves actual context/browser disposal with Playwright.
  const detachOnce = () =>
    (detachPromise ??= withDeadline(
      cdp.detach(),
      5000,
      "Session observer detach exceeded 5 seconds; remote CDP cleanup is unconfirmed",
    ));
  const active = new Map<string, SessionWatch>();
  let document: { frameId: string; loaderId: string } | undefined;
  let documentVersion = 0;
  const retiredLoaders = new Set<string>();
  const retiredRequests = new Set<string>();
  const lifecycle: LifecycleEntry[] = [];
  const watchesStarted = new Map<string, number>();
  // The message sequence of each Project's Session, by Project ID.
  const observations = new Map<string, Observation>();
  const changed = new Set<() => void>();
  let version = 0;
  const notify = () => {
    version++;
    for (const wake of [...changed]) wake();
  };
  const waitForChange = (observedVersion: number, timeout: number) =>
    new Promise<void>((resolve) => {
      if (version !== observedVersion) return resolve();
      const done = () => {
        clearTimeout(timer);
        changed.delete(done);
        resolve();
      };
      const timer = setTimeout(done, timeout);
      changed.add(done);
    });
  const uncommittedSource = (projectId: string, afterSequence: number) => {
    const state = observations.get(projectId);
    if (!state) return undefined;
    for (const source of state.textSources) {
      if ((state.modelSequences.get(source) ?? 0) > Math.max(state.lastSequence, afterSequence))
        return { state, source };
    }
    return undefined;
  };
  const violations: string[] = [];
  const violation = (message: string) => {
    if (!violations.includes(message)) violations.push(message);
  };
  const record = (entry: Omit<LifecycleEntry, "at">) => {
    if (lifecycle.length < 512) lifecycle.push({ ...entry, at: Date.now() });
    else violation("Session lifecycle observation exceeded its bounded budget");
  };
  const loaderKey = (frameId: string, loaderId: string) => JSON.stringify([frameId, loaderId]);
  const commitDocument = (frameId: string, loaderId: string) => {
    if (!frameId || !loaderId) return violation("Main document has no frame/loader identity");
    if (document?.frameId === frameId && document.loaderId === loaderId) return;
    if (document) {
      if (retiredLoaders.size >= 128) return violation("Session document observation exceeded its bounded budget");
      retiredLoaders.add(loaderKey(document.frameId, document.loaderId));
      for (const [requestId, watch] of active) {
        if (watch.frameId !== document.frameId || watch.loaderId !== document.loaderId) continue;
        if (retiredRequests.size >= 512) {
          violation("Retired Session Watch observation exceeded its bounded budget");
          continue;
        }
        // A committed new document ends this document's EventSource lifetime.
        // Keep a tombstone: any later frame is a violation, never ignored.
        retiredRequests.add(requestId);
        active.delete(requestId);
        record({ kind: "document-retired-watch", requestId, ...watch });
      }
    }
    document = { frameId, loaderId };
    record({ kind: "document-committed", frameId, loaderId });
    notify();
  };
  const onFrameNavigated = ({ frame, type }: Protocol.Page.frameNavigatedPayload) => {
    if (frame.parentId) return;
    documentVersion++;
    // The pinned Playwright Chromium disables BFCache. A restored document
    // needs its own lifecycle proof; it must not silently reuse retired watches.
    if (type === "BackForwardCacheRestore") violation("Unexpected BFCache restore during Session observation");
    commitDocument(frame.id, frame.loaderId);
  };
  const onRequestWillBeSent = (event: Protocol.Network.requestWillBeSentPayload) => {
    const url = new URL(event.request.url);
    if (/^\/api\/(agent|control-plane)\//.test(url.pathname))
      violation("A removed standalone Agent or resource route was requested");
    const projectId = url.searchParams.get("projectId");
    if (url.pathname !== "/api/rpc/WatchSession" || event.type !== "EventSource" || !projectId) return;
    if (!event.frameId || !event.loaderId) return violation("Session Watch has no frame/loader identity");
    if (retiredLoaders.has(loaderKey(event.frameId, event.loaderId)) || retiredRequests.has(event.requestId))
      return violation("A retired document started another Session Watch");
    if (active.size >= 512) return violation("Active Session Watch observation exceeded its bounded budget");
    // A new loader's request may precede frameNavigated. Keep both until the
    // real commit retires only the prior document, never the incoming watch.
    const watch = { projectId, frameId: event.frameId, loaderId: event.loaderId, snapshots: 0, frames: 0 };
    active.set(event.requestId, watch);
    record({ kind: "watch-started", requestId: event.requestId, ...watch });
    watchesStarted.set(projectId, (watchesStarted.get(projectId) ?? 0) + 1);
  };
  const observeSnapshot = (watch: SessionWatch, snapshot: SessionSnapshot__Output) => {
    const complete = ["agent", "workspace", "preview"].every((field) => Object.hasOwn(snapshot, field));
    if (snapshot.projectId !== watch.projectId || typeof snapshot.revision !== "string" || !complete)
      violation("Session Watch did not preserve the full protobuf snapshot");
    if (!snapshot.agent) return;
    const previous = observations.get(watch.projectId);
    const description = snapshot.agent.description;
    if (!description) return violation("Agent snapshot has no description");
    const committedSequence = Number(description.lastSequence);
    const valid = Number.isSafeInteger(committedSequence) && committedSequence >= 0;
    if (!valid || (previous && committedSequence < previous.lastSequence))
      violation("Session committed message sequence decreased or is invalid");
    const state: Observation = {
      lastSequence: Math.max(previous?.lastSequence ?? 0, committedSequence),
      status: description.status,
      sessionStatus: snapshot.status,
      pendingUserInput: description.pendingUserInput != null,
      modelSequences: previous?.modelSequences ?? new Map(),
      textSources: previous?.textSources ?? new Set(),
      textFrames: previous?.textFrames ?? 0,
      modelEvents: previous?.modelEvents ?? 0,
      tracking: previous?.tracking ?? true,
    };
    for (const [source, sequence] of state.modelSequences) {
      if (sequence > state.lastSequence) continue;
      state.modelSequences.delete(source);
      state.textSources.delete(source);
    }
    observations.set(watch.projectId, state);
  };
  const onEventSourceMessageReceived = (event: Protocol.Network.eventSourceMessageReceivedPayload) => {
    if (retiredRequests.has(event.requestId)) {
      violation("A Session Watch delivered a frame after its document was retired");
      record({ kind: "retired-watch-frame", requestId: event.requestId });
      return notify();
    }
    const watch = active.get(event.requestId);
    if (!watch) return;
    try {
      const { snapshot, progress }: RpcOutput<"WatchSession"> = JSON.parse(event.data);
      if (watch.frames === 0 && !snapshot) violation("Session Watch did not begin with a snapshot");
      watch.frames++;
      if (snapshot) {
        if (++watch.snapshots === 1) record({ kind: "watch-first-snapshot", requestId: event.requestId, ...watch });
        observeSnapshot(watch, snapshot);
      }
      const state = observations.get(watch.projectId);
      if (!progress || !state?.tracking) return;
      const activity = progress.content === "activity" ? progress.activity : null;
      if (activity && MODEL_LIFECYCLE.has(activity.kind) && activity.messageSequence != null) {
        state.modelSequences.set(progress.source, Number(activity.messageSequence));
        state.modelEvents++;
      }
      // Independent Streams may deliver text before its model activity. Keep
      // only the source identity, never the text, and correlate either order.
      if (progress.content === "assistantText" && progress.assistantText) {
        state.textSources.add(progress.source);
        state.textFrames++;
      }
      if (state.modelSequences.size > 1024 || state.textSources.size > 1024) {
        violation("Session observation exceeded its bounded source budget");
        state.modelSequences.clear();
        state.textSources.clear();
        state.tracking = false;
      }
    } catch {
      violation("Session Watch delivered invalid JSON");
    } finally {
      notify();
    }
  };
  const ended = (requestId: string, kind: string) => {
    const watch = active.get(requestId);
    if (watch || retiredRequests.has(requestId)) record({ kind, requestId, ...watch });
    active.delete(requestId);
    notify();
  };
  const onLoadingFinished = (event: Protocol.Network.loadingFinishedPayload) =>
    ended(event.requestId, "network-finished");
  const onLoadingFailed = (event: Protocol.Network.loadingFailedPayload) => ended(event.requestId, "network-failed");
  cdp.on("Page.frameNavigated", onFrameNavigated);
  cdp.on("Network.requestWillBeSent", onRequestWillBeSent);
  cdp.on("Network.eventSourceMessageReceived", onEventSourceMessageReceived);
  cdp.on("Network.loadingFinished", onLoadingFinished);
  cdp.on("Network.loadingFailed", onLoadingFailed);
  const stopObserving = () => {
    if (closing) return;
    closing = true;
    cdp.off("Page.frameNavigated", onFrameNavigated);
    cdp.off("Network.requestWillBeSent", onRequestWillBeSent);
    cdp.off("Network.eventSourceMessageReceived", onEventSourceMessageReceived);
    cdp.off("Network.loadingFinished", onLoadingFinished);
    cdp.off("Network.loadingFailed", onLoadingFailed);
    // Preserve the final evidence, while releasing each local wait's timer.
    notify();
  };
  try {
    await cdp.send("Page.enable");
    const versionBeforeRead = documentVersion;
    const { frameTree } = await cdp.send("Page.getFrameTree");
    // A navigation event received while the query was pending is newer than
    // this initialization read and must not be overwritten by it.
    if (documentVersion === versionBeforeRead) commitDocument(frameTree.frame.id, frameTree.frame.loaderId);
    await cdp.send("Network.enable");
  } catch (error) {
    stopObserving();
    await detachOnce().catch((cleanupError: unknown) => {
      throw new AggregateError([error, cleanupError], "Session observer initialization and detach failed");
    });
    throw error;
  }
  const watchesWithSnapshot = (projectId: string) =>
    [...active.values()].filter((value) => value.projectId === projectId && value.snapshots > 0).length;
  return {
    async reconnectDuringModelText(projectId: string, afterSequence: number) {
      ensureOpen();
      expect(Number.isSafeInteger(afterSequence) && afterSequence >= 0).toBe(true);
      // Cancel the browser's existing resource loads during a real provider
      // turn. Offline emulation does not reliably close an established SSE.
      // This exercises Studio's Watch error/reconnect path without a reload,
      // canned frames, interception, or provider substitution.
      const deadline = Date.now() + 5 * 60_000;
      for (;;) {
        ensureOpen();
        const observedVersion = version;
        expect(violations).toEqual([]);
        const state = observations.get(projectId);
        const diagnostic = `text frames=${state?.textFrames ?? 0}, model events=${state?.modelEvents ?? 0}`;
        if (state?.pendingUserInput)
          throw new Error(
            `Real Agent requested user input before the reconnect check (${diagnostic}); no response or text is synthesized`,
          );
        if (state && ENDED_SESSION_STATUSES.has(state.sessionStatus))
          throw new Error(`Session reached ${state.sessionStatus} before the reconnect check (${diagnostic})`);
        const candidate = uncommittedSource(projectId, afterSequence);
        if (candidate && (await page.getByTestId("live-assistant-message").isVisible())) {
          ensureOpen();
          // Recheck after the browser round trip. A snapshot may have committed
          // the text while the DOM visibility read was pending.
          const current = uncommittedSource(projectId, afterSequence);
          if (current?.state === candidate.state && current.source === candidate.source) break;
        }
        if (Date.now() >= deadline || page.isClosed())
          throw new Error(`No visible, correlated uncommitted Agent text was available to interrupt (${diagnostic})`);
        // Event-driven except for a short rendering allowance after real text.
        // A historical observation alone never qualifies the interruption.
        await waitForChange(observedVersion, Math.min(candidate ? 50 : 1000, deadline - Date.now()));
      }
      const interrupted = [...active].filter(([, value]) => value.projectId === projectId).map(([id]) => id);
      expect(interrupted).toHaveLength(1);
      const prior = watchesStarted.get(projectId) ?? 0;
      ensureOpen();
      await cdp.send("Page.stopLoading");
      ensureOpen();
      // A new Watch may already have opened by the time CDP reports the old
      // cancellation. Require the exact interrupted request to have ended.
      await expect.poll(() => interrupted.filter((requestId) => active.has(requestId)).length).toBe(0);
      await expect.poll(() => watchesStarted.get(projectId) ?? 0).toBeGreaterThan(prior);
      await expect.poll(() => watchesWithSnapshot(projectId)).toBe(1);
      expect(violations).toEqual([]);
    },
    async assertCompletedTextReconciled(projectId: string, afterSequence: number, completed: SessionSnapshot__Output) {
      const sequence = expectNewestFinalReplySequence(completed, projectId, afterSequence);
      // This Session's committed message sequence never decreases.
      // The authoritative snapshot confirms the durable final reply.
      await expect
        .poll(
          () => {
            const observed = observations.get(projectId);
            return (
              observed !== undefined &&
              observed.lastSequence >= sequence &&
              observed.status === "STATUS_WAITING_FOR_MESSAGE"
            );
          },
          { timeout: 30_000 },
        )
        .toBe(true);
      await expect(page.getByTestId("live-assistant-message")).toHaveCount(0);
      const committed = page
        .getByRole("article", { name: "Assistant message", exact: true })
        .and(page.locator(`[data-message-sequence="${sequence}"]`));
      await expect(committed).toHaveCount(1);
      await expect
        .poll(
          async () => (await committed.getByTestId("conversation-message-content").textContent())?.trim().length ?? 0,
        )
        .toBeGreaterThan(0);
      expect(violations).toEqual([]);
    },
    async assertSingleWatch(projectId: string) {
      await expect.poll(() => watchesWithSnapshot(projectId)).toBe(1);
      await expect.poll(() => active.size).toBe(1);
      expect(violations).toEqual([]);
    },
    close() {
      if (!closePromise) {
        stopObserving();
        closePromise = (async () => {
          // Every step runs; their failures are reported together.
          const failures: unknown[] = [];
          const attempt = (step: () => unknown) =>
            Promise.resolve()
              .then(step)
              .catch((error: unknown) => void failures.push(error));
          await attempt(() => expect(violations).toEqual([]));
          // Body-only attachment is local. Save it before awaiting remote CDP.
          const watches = [...active].map(([requestId, watch]) => ({ requestId, ...watch }));
          const body = JSON.stringify({ document, lifecycle, active: watches, violations });
          await attempt(() => test.info().attach("session-watch-lifecycle", { contentType: "application/json", body }));
          await attempt(detachOnce);
          if (failures.length === 1) throw failures[0];
          if (failures.length > 1) throw new AggregateError(failures, "Session observer cleanup or validation failed");
        })();
      }
      return closePromise;
    },
  };
}
