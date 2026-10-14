// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/Button";
import { setSessionExecutor } from "@/lib/api/agent";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { timestampToISO } from "@/lib/shared/protobuf";

/** Uses only the canonical resource's SQL last-seen join; the browser never infers authority from liveness. */
function useRemoteStatus() {
  const session = useSessionResource();
  const executor = session?.resource?.agent?.description?.executor;
  const remote = executor?.mode === "SESSION_EXECUTOR_MODE_REMOTE" ? executor.remote : null;
  const seen = timestampToISO(remote?.lastSeenAt);
  const connection = remote?.connectionId;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!connection) return;
    const timer = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(timer);
  }, [connection]);
  const online = seen !== null && Number.isFinite(Date.parse(seen)) && now - Date.parse(seen) <= 90_000;
  return { session, executor, remote, seen, online };
}

/** Studio owns the menu and commands. Connect opens the single RAPAIR-owned dialog supplied by its parent. */
export function SessionExecutorStatus({ canManage, onConnect }: { canManage: boolean; onConnect: () => void }) {
  const state = useRemoteStatus();
  return (
    <ExecutorMenu
      key={`${state.session?.projectId}:${state.executor?.epoch}:${state.executor?.generation}:${state.remote?.connectionId ?? "platform"}:${state.session?.status}:${canManage}`}
      {...state}
      canManage={canManage}
      onConnect={onConnect}
    />
  );
}

function ExecutorMenu({
  session,
  executor,
  remote,
  seen,
  online,
  canManage,
  onConnect,
}: ReturnType<typeof useRemoteStatus> & { canManage: boolean; onConnect: () => void }) {
  const action = useSingleFlight<"platform">();
  if (!session?.resource?.agent?.description) return null;
  const active = session.status === "SESSION_STATUS_ACTIVE";
  const owner = remote?.owner?.email ?? "another Builder";
  const label = remote ? `Codex via Orca · connected by ${owner}` : "Superverse agent";
  const description = session.resource.agent.description;
  const waiting = description?.pendingUserInput
    ? "Waiting for your answer"
    : description?.pendingApproval
      ? "Waiting for approval"
      : "";
  const working = online && !waiting && description?.status !== "STATUS_WAITING_FOR_MESSAGE";
  const liveness = online
    ? waiting || (working ? "Working" : "Online")
    : seen
      ? `Offline · last seen ${new Date(seen).toLocaleString()}`
      : "Offline · last seen unavailable";
  const switchToPlatform = () =>
    action.run("platform", async (signal) => {
      if (!remote || !executor) return;
      // Keep the protobuf's decimal string intact. The server CAS rejects a stale epoch.
      try {
        await setSessionExecutor({ projectId: session.projectId, basisEpoch: executor.epoch }, signal);
      } catch (reason) {
        // A refusal or lost response needs canonical reconciliation, never an automatic retry.
        await session.refresh(signal);
        throw reason;
      }
      signal.throwIfAborted();
      await session.refresh(signal, true);
    });
  return (
    <details className="relative text-[12px] text-ink-dim" data-session-executor={remote ? "remote" : "platform"}>
      <summary
        className="cursor-pointer rounded-md px-2 py-1 focus-visible:outline-2 focus-visible:outline-accent"
        aria-label={`Executor: ${label}`}
      >
        {label}
        {remote ? ` · ${online ? waiting.toLowerCase() || (working ? "working" : "online") : "offline"}` : ""}
      </summary>
      <div className="absolute right-0 z-20 mt-1 flex w-80 max-w-[85vw] flex-col gap-2 rounded-lg border border-line-soft bg-panel p-3 shadow-lg">
        {remote && (
          <p role="status" data-remote-liveness={online ? "online" : "offline"}>
            {liveness}
            {remote.binding?.hostLabel ? ` · ${remote.binding.hostLabel}` : ""}
          </p>
        )}
        {canManage && active && (
          <>
            <p className="text-ink-faint">Changing agents stops the current work. Background commands keep running.</p>
            <Button size="sm" disabled={action.busy !== null} onClick={onConnect}>
              Connect my agent
            </Button>
            {remote && (
              <Button size="sm" disabled={action.busy !== null} onClick={() => void switchToPlatform()}>
                {action.busy ? "Switching…" : "Use the Superverse agent"}
              </Button>
            )}
          </>
        )}
        {action.error && (
          <p role="alert" className="text-fail">
            {action.error}
          </p>
        )}
      </div>
    </details>
  );
}

/** Offline keeps the remote executor and the ordinary queue/steer/Stop controls. */
export function RemoteAgentOfflineNotice() {
  const { remote, online, session } = useRemoteStatus();
  if (!remote || online || session?.status !== "SESSION_STATUS_ACTIVE") return null;
  return (
    <p
      className="rounded-md border border-line-soft bg-panel p-3 text-[12px] text-ink-dim"
      role="status"
      data-remote-offline
    >
      Codex is offline; messages are kept until it reconnects. Use the executor menu to reconnect or use the Superverse
      agent.
    </p>
  );
}
