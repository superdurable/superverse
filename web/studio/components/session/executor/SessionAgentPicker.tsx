// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { Glyph } from "@/components/ui/Glyph";
import { PopoverCard, POPOVER_ITEM, usePopover } from "@/components/ui/Popover";
import { RemoteAgentDialog } from "@/components/session/remoteagent/RemoteAgentDialog";
import { confirmCredentialSwitch } from "./credential-switch";
import { ProviderModelPicker } from "./ProviderModelPicker";
import { setSessionExecutor } from "@/lib/api/agent";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { agentModelLabel, type AgentModel } from "@/lib/session/agent-types";
import { useRemoteAgentStatus } from "./use-remote-agent-status";
import { useModelChoice } from "@/lib/session/model/use-model-choice";

type PickerProps = {
  model: AgentModel | null;
  contextModel: AgentModel | null;
  effort: string;
  mockSelectable: boolean;
  canManage: boolean;
  modelDisabled: boolean;
  modelRevision: number;
  onSelect: (model: AgentModel) => Promise<void>;
};

/**
 * One quiet control on the composer's row, named by the model alone (or the local agent), chooses the agent
 * first and then the Platform model; its accessible name and hint say the whole of it.
 */
export function SessionAgentPicker(props: PickerProps) {
  const state = useRemoteAgentStatus();
  return <AgentPickerMenu key={state.session?.projectId} {...state} {...props} />;
}

/** The binding does not distinguish every provider, so the display stays agent-neutral. */
const LOCAL_AGENT = "Local agent";

/** Green while the local agent was seen in the last 90 seconds, red otherwise. */
function LivenessDot({ online }: { online: boolean }) {
  return <span aria-hidden className={`size-2 shrink-0 rounded-full ${online ? "bg-pass" : "bg-fail"}`} />;
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
  canManage,
  modelDisabled,
  modelRevision,
  onSelect,
}: ReturnType<typeof useRemoteAgentStatus> & PickerProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  const [opened, setOpened] = useState(false);
  const [connect, setConnect] = useState(false);
  const action = useSingleFlight<"platform" | "local">();
  const choice = useModelChoice(modelRevision, onSelect, () => session?.refresh(undefined, true) ?? Promise.resolve());
  const wasOpen = useRef(false);
  const wasConnecting = useRef(false);
  useEffect(() => {
    if (wasOpen.current && !popover.open && !connect && document.activeElement === document.body)
      trigger.current?.focus();
    wasOpen.current = popover.open;
  }, [connect, popover.open]);
  useEffect(() => {
    if (popover.open) card.current?.querySelector<HTMLButtonElement>("button")?.focus();
  }, [popover.open]);
  useEffect(() => {
    // Native <dialog> restores focus when its cleanup closes it. Focus after that cleanup.
    if (wasConnecting.current && !connect) trigger.current?.focus();
    wasConnecting.current = connect;
  }, [connect]);
  if (!session?.resource?.agent?.description) return null;
  const owner = remote?.owner?.email ?? "another Builder";
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
  const label = remote
    ? `${LOCAL_AGENT} · ${liveness.toLowerCase()} · connected by ${owner}`
    : `Platform agent · ${model === null ? "Not selected yet" : agentModelLabel(model)}`;
  const active = session.status === "SESSION_STATUS_ACTIVE";
  const switchable = canManage && active;
  const switchPending = !remote && contextModel !== null && contextModel !== model;
  const close = () => {
    popover.close();
    trigger.current?.focus();
  };
  const switchToPlatform = () =>
    action.run("platform", async (signal) => {
      if (!remote || !executor) return;
      try {
        await setSessionExecutor(
          { projectId: session.projectId, basisEpoch: executor.epoch, connectionId: "" },
          signal,
        );
      } catch (reason) {
        await session.refresh(signal);
        throw reason;
      }
      signal.throwIfAborted();
      await session.refresh(signal, true);
      close();
    });
  const resumeLocal = () =>
    action.run("local", async (signal) => {
      if (!executor?.suspended) return;
      try {
        await setSessionExecutor(
          { projectId: session.projectId, basisEpoch: executor.epoch, connectionId: executor.suspended.connectionId },
          signal,
        );
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
          setOpened(true);
          popover.toggle();
        }}
        className="flex h-7 max-w-[min(14rem,50vw)] items-center gap-1 rounded-[7px] px-1.5 text-[12px] text-ink-dim hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none aria-expanded:bg-hover aria-expanded:text-ink"
      >
        {switchPending && (
          <span className="size-1.5 shrink-0 rounded-full bg-accent" aria-label="Model changes next turn" />
        )}
        {remote && <LivenessDot online={online} />}
        <span className="truncate">
          {remote
            ? `${LOCAL_AGENT}${online ? "" : " · offline"}`
            : `${model === null ? "Choose a model" : agentModelLabel(model)}${executor?.suspended ? (online ? " · local idle" : " · local offline") : ""}`}
        </span>
        <Glyph name="chevronDown" className="size-3.5 shrink-0" />
      </button>
      {(popover.open || opened) && (
        <PopoverCard
          anchorRef={anchor}
          cardRef={card}
          label="Choose agent"
          align="end"
          autoFocus={popover.open}
          tabIndex={-1}
          className={`w-80 max-w-[90vw] p-2 ${popover.open ? "" : "hidden"}`}
        >
          <div className="grid gap-1 text-[12px]" onClickCapture={confirmCredentialSwitch}>
            {remote && (
              <div
                className="rounded-md bg-accent-wash px-2.25 py-1.75"
                data-remote-liveness={online ? "online" : "offline"}
              >
                <p className="flex items-center gap-2 text-ink">
                  <LivenessDot online={online} />
                  <span className="font-medium">{LOCAL_AGENT}</span>
                  <span className="ml-auto text-xs text-ink-faint">Current</span>
                </p>
                <p role="status" className="mt-0.5 pl-4 text-xs text-ink-faint">
                  {liveness} · connected by {owner}
                </p>
              </div>
            )}
            {remote ? (
              switchable && (
                <button
                  type="button"
                  className={POPOVER_ITEM}
                  disabled={action.busy !== null}
                  onClick={() => void switchToPlatform()}
                >
                  {action.busy ? "Switching…" : "Use the Platform agent"}
                </button>
              )
            ) : (
              <>
                <p className="flex items-center px-2.25 pt-1 text-ink">
                  <span className="font-medium">Platform agent</span>
                  <span className="ml-auto text-xs text-ink-faint">Current</span>
                </p>
                <ProviderModelPicker
                  model={model}
                  canManage={canManage}
                  disabled={modelDisabled || choice.pending}
                  onSelect={(value) => {
                    choice.select(value);
                    close();
                  }}
                />
                {choice.pending && (
                  <p role="status" className="px-2.25 text-xs text-ink-faint">
                    Changing model…
                  </p>
                )}
                {contextModel !== null && (
                  <p className="px-2.25 text-xs text-ink-faint">
                    Current context: {agentModelLabel(contextModel)}
                    {effort ? `, ${effort} effort` : ""}
                    {switchPending ? " · change takes effect next turn" : ""}
                  </p>
                )}
              </>
            )}
            {switchable && (
              <>
                <hr className="my-1 border-line-faint" />
                {!remote && executor?.suspended && (
                  <>
                    <button
                      type="button"
                      className={POPOVER_ITEM}
                      disabled={action.busy !== null}
                      data-discard-credential
                      onClick={() => void resumeLocal()}
                    >
                      {action.busy === "local" ? "Switching…" : "Use the local agent"}
                    </button>
                    <p className="px-2.25 text-xs text-ink-faint">
                      {online ? "Connected · idle while the Platform agent is active" : "Paired · offline"} ·{" "}
                      {executor.suspended.binding?.hostLabel || executor.suspended.owner?.email || "Local agent"}
                    </p>
                  </>
                )}
                <button
                  type="button"
                  className={POPOVER_ITEM}
                  data-discard-credential
                  onClick={() => {
                    popover.close();
                    setConnect(true);
                  }}
                >
                  {remote ? "Connect another local agent…" : "Connect a local agent…"}
                </button>
                <p className="px-2.25 text-xs text-ink-faint">
                  Changing agents stops the current work. Background commands keep running.
                </p>
              </>
            )}
            {action.error && (
              <p role="alert" className="px-2.25 text-xs text-fail">
                {action.error}
              </p>
            )}
          </div>
        </PopoverCard>
      )}
      {connect && switchable && <RemoteAgentDialog projectId={session.projectId} onClose={() => setConnect(false)} />}
    </span>
  );
}

/** Offline keeps the remote executor and the ordinary queue, steer and Stop controls. */
export function RemoteAgentOfflineNotice() {
  const { remote, executor, online, session } = useRemoteAgentStatus();
  if (session?.status !== "SESSION_STATUS_ACTIVE") return null;
  if (!remote && executor?.suspended) {
    return (
      <p
        className="rounded-md border border-line-soft bg-panel p-3 text-[12px] text-ink-dim"
        role="status"
        data-remote-idle
      >
        The Platform agent is active. Your local agent is {online ? "connected and idle" : "offline"}; choose “Use the
        local agent” in the agent picker to resume this connection.
      </p>
    );
  }
  if (!remote || online) return null;
  return (
    <p
      className="rounded-md border border-line-soft bg-panel p-3 text-[12px] text-ink-dim"
      role="status"
      data-remote-offline
    >
      Your local agent is offline; messages are kept until it reconnects. Use the agent picker to reconnect or use the
      Platform agent.
    </p>
  );
}
