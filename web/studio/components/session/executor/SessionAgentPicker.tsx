// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { PopoverCard, POPOVER_ITEM, usePopover } from "@/components/ui/Popover";
import { RemoteAgentDialog } from "@/components/session/remoteagent/RemoteAgentDialog";
import { setSessionExecutor } from "@/lib/api/agent";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { agentModelLabel, AGENT_MODEL_OPTIONS, type AgentModel } from "@/lib/session/agent-types";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { timestampToISO } from "@/lib/shared/protobuf";

/** SQL last-seen information is presentation only; the canonical executor owns authority. */
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

type PickerProps = {
  model: AgentModel | null;
  contextModel: AgentModel | null;
  effort: string;
  mockSelectable: boolean;
  canManage: boolean;
  modelDisabled: boolean;
  onSelect: (model: AgentModel) => void;
};

/** One control beside the composer chooses the agent first and then the Superverse model. */
export function SessionAgentPicker(props: PickerProps) {
  const state = useRemoteStatus();
  return (
    <AgentPickerMenu
      key={`${state.session?.projectId}:${state.executor?.epoch}:${state.executor?.generation}:${state.remote?.connectionId ?? "platform"}:${state.session?.status}:${props.canManage}`}
      {...state}
      {...props}
    />
  );
}

function AgentPickerMenu({
  session,
  executor,
  remote,
  seen,
  online,
  model,
  contextModel,
  effort,
  mockSelectable,
  canManage,
  modelDisabled,
  onSelect,
}: ReturnType<typeof useRemoteStatus> & PickerProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  const [level, setLevel] = useState<"agents" | "models" | "remote">("agents");
  const [connect, setConnect] = useState(false);
  const action = useSingleFlight<"platform">();
  const wasOpen = useRef(false);
  const wasConnecting = useRef(false);
  useEffect(() => {
    if (wasOpen.current && !popover.open && !connect && document.activeElement === document.body)
      trigger.current?.focus();
    wasOpen.current = popover.open;
  }, [connect, popover.open]);
  useEffect(() => {
    if (popover.open) card.current?.querySelector<HTMLButtonElement>("button")?.focus();
  }, [level, popover.open]);
  useEffect(() => {
    // Native <dialog> restores focus when its cleanup closes it. Focus after that cleanup.
    if (wasConnecting.current && !connect) trigger.current?.focus();
    wasConnecting.current = connect;
  }, [connect]);
  if (!session?.resource?.agent?.description) return null;
  const owner = remote?.owner?.email ?? "another Builder";
  const label = remote
    ? `My Codex · ${online ? "online" : "offline"} · connected by ${owner}`
    : `Superverse agent · ${model === null ? "Not selected yet" : agentModelLabel(model)}`;
  const active = session.status === "SESSION_STATUS_ACTIVE";
  const waiting = session.resource.agent.description.pendingUserInput
    ? "Waiting for your answer"
    : session.resource.agent.description.pendingApproval
      ? "Waiting for approval"
      : "";
  const liveness = online
    ? waiting || "Online"
    : seen
      ? `Offline · last seen ${new Date(seen).toLocaleString()}`
      : "Offline · last seen unavailable";
  const switchPending = !remote && contextModel !== null && contextModel !== model;
  const close = () => {
    popover.close();
    setLevel("agents");
    trigger.current?.focus();
  };
  const switchToPlatform = () =>
    action.run("platform", async (signal) => {
      if (!remote || !executor) return;
      try {
        await setSessionExecutor({ projectId: session.projectId, basisEpoch: executor.epoch }, signal);
      } catch (reason) {
        await session.refresh(signal);
        throw reason;
      }
      signal.throwIfAborted();
      await session.refresh(signal, true);
      close();
    });
  return (
    <span
      ref={anchor}
      className="relative inline-flex max-w-full min-w-0 shrink-0"
      data-session-executor={remote ? "remote" : "platform"}
    >
      <button
        ref={trigger}
        type="button"
        aria-label={`Agent: ${label}`}
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        title={label}
        onClick={() => {
          setLevel("agents");
          popover.toggle();
        }}
        className="flex max-w-[min(22rem,70vw)] items-center gap-1 rounded-md border border-line-soft bg-panel px-2 py-1 text-[12px] text-ink-dim hover:bg-hover focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
      >
        {switchPending && (
          <span className="size-1.5 shrink-0 rounded-full bg-accent" aria-label="Model changes next turn" />
        )}
        <span className="truncate">{label}</span>
        <Glyph name="chevronDown" className="size-3.5 shrink-0" />
      </button>
      {popover.open && (
        <PopoverCard
          anchorRef={anchor}
          cardRef={card}
          label="Choose agent"
          align="end"
          autoFocus
          tabIndex={-1}
          className="w-80 max-w-[90vw] p-2"
        >
          {level === "agents" ? (
            <div className="grid gap-1">
              <p className="px-2 py-1 text-xs font-medium text-ink-faint">Agent</p>
              <button type="button" className={POPOVER_ITEM} onClick={() => setLevel("models")}>
                Superverse agent · {model === null ? "Not selected yet" : agentModelLabel(model)}
              </button>
              <button type="button" className={POPOVER_ITEM} onClick={() => setLevel("remote")}>
                My local agent · {remote ? liveness.toLowerCase() : "not connected"}
              </button>
            </div>
          ) : level === "models" ? (
            <div className="grid gap-1">
              <Button size="sm" variant="quiet" onClick={() => setLevel("agents")}>
                ← Agent
              </Button>
              {remote && canManage && active && (
                <Button size="sm" disabled={action.busy !== null} onClick={() => void switchToPlatform()}>
                  {action.busy ? "Switching…" : "Use the Superverse agent"}
                </Button>
              )}
              {!remote &&
                AGENT_MODEL_OPTIONS.filter(({ value }) => value !== "mock" || mockSelectable || model === "mock").map(
                  (option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={POPOVER_ITEM}
                      aria-current={option.value === model ? "page" : undefined}
                      disabled={!canManage || modelDisabled}
                      onClick={() => {
                        onSelect(option.value);
                        close();
                      }}
                    >
                      <span>
                        {option.label}
                        <span className="block text-xs text-ink-faint">{option.description}</span>
                      </span>
                    </button>
                  ),
                )}
              {!remote && contextModel !== null && (
                <p className="px-2 text-xs text-ink-faint">
                  Current context: {agentModelLabel(contextModel)}
                  {effort ? `, ${effort} effort` : ""}
                  {switchPending ? " · change takes effect next turn" : ""}
                </p>
              )}
              {action.error && (
                <p role="alert" className="px-2 text-xs text-fail">
                  {action.error}
                </p>
              )}
            </div>
          ) : (
            <div className="grid gap-2 text-[12px] text-ink-dim">
              <Button size="sm" variant="quiet" onClick={() => setLevel("agents")}>
                ← Agent
              </Button>
              <p role="status" data-remote-liveness={remote ? (online ? "online" : "offline") : "none"}>
                {remote ? `${liveness} · connected by ${owner}` : "No local agent is running this Session."}
              </p>
              {canManage && active && (
                <>
                  <p className="text-ink-faint">
                    Changing agents stops the current work. Background commands keep running.
                  </p>
                  <Button
                    size="sm"
                    onClick={() => {
                      popover.close();
                      setConnect(true);
                    }}
                  >
                    Connect
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
          )}
        </PopoverCard>
      )}
      {connect && canManage && active && (
        <RemoteAgentDialog projectId={session.projectId} onClose={() => setConnect(false)} />
      )}
    </span>
  );
}

/** Offline keeps the remote executor and the ordinary queue, steer and Stop controls. */
export function RemoteAgentOfflineNotice() {
  const { remote, online, session } = useRemoteStatus();
  if (!remote || online || session?.status !== "SESSION_STATUS_ACTIVE") return null;
  return (
    <p
      className="rounded-md border border-line-soft bg-panel p-3 text-[12px] text-ink-dim"
      role="status"
      data-remote-offline
    >
      Codex is offline; messages are kept until it reconnects. Use the agent picker to reconnect or use the Superverse
      agent.
    </p>
  );
}
