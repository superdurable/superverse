// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { renewWorkspace } from "@/lib/api/session";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { isWorkspacePaused, workspaceDeadline } from "@/lib/session/session-status";
import { formatRemainingTime } from "@/lib/shared/format";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { POPOVER_ITEM, PopoverCard, usePopover } from "@/components/ui/Popover";
import { PhaseTimings } from "@/components/ui/timings/PhaseTimings";

/** The phases of a workspace start in order: the Sandbox becoming ready, then the repository checkout. */
const WORKSPACE_PHASE_LABELS: Readonly<Record<string, string>> = {
  provision: "Starting the Sandbox",
  checkout: "Checking out the repository",
};

type SessionConnectionState = "complete" | "connected" | "connecting" | "reconnecting";
function useConnectionState() {
  const session = useSessionResource();
  const active = session?.status === "SESSION_STATUS_ACTIVE";
  const state: SessionConnectionState = !active
    ? "complete"
    : session.streamState === "idle"
      ? "connecting"
      : session.streamState;
  return { state };
}

function connectionLabel(state: SessionConnectionState): string {
  if (state === "connected") return "Live updates connected";
  if (state === "reconnecting") {
    return "Live updates reconnecting; durable history remains current";
  }
  return "Connecting to live updates";
}

export function SessionConnectionIndicator() {
  const { state } = useConnectionState();
  if (state === "complete") return null;
  const connected = state === "connected";
  const label = connectionLabel(state);
  return (
    <span
      aria-label={label}
      className={connected ? "h-1.5 w-1.5 shrink-0 rounded-full bg-pass" : "h-1.5 w-1.5 shrink-0 rounded-full bg-busy"}
      data-session-connection={state}
      role="status"
      title={label}
    />
  );
}

/** The clock mark beside the workspace's time. */
function ClockMark() {
  return (
    <svg aria-hidden="true" className="h-3 w-3" fill="none" viewBox="0 0 16 16">
      <circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 4.75V8l2.25 1.5" stroke="currentColor" strokeLinecap="round" strokeWidth="1.5" />
    </svg>
  );
}

/**
 * The Session's workspace: how long the ready Sandbox stays after the agent last used it, as a button
 * that opens "Keep running for 7 days", or that the Session paused it and kept its files. Nothing
 * while no workspace is ready or paused.
 */
export function WorkspaceStatusIndicator() {
  const resource = useSessionResource()?.resource ?? null;
  const deadline = resource ? workspaceDeadline(resource) : null;
  const paused = resource !== null && isWorkspacePaused(resource);
  const now = useMinuteClock(deadline);
  const anchor = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);

  if (paused) {
    const label = "Workspace paused; your files are kept";
    return (
      <span
        aria-label={label}
        className="inline-flex shrink-0 items-center gap-1 text-[11px] text-ink-faint"
        data-workspace-status="paused"
        role="status"
        title={label}
      >
        <svg aria-hidden="true" className="h-3 w-3" fill="none" viewBox="0 0 16 16">
          <path d="M6 4.5v7M10 4.5v7" stroke="currentColor" strokeLinecap="round" strokeWidth="1.5" />
        </svg>
        Paused
      </span>
    );
  }
  if (!deadline) return null;
  const remaining = formatRemainingTime(deadline, now);
  const label = `Workspace kept for ${remaining} more; the agent's work extends it`;
  return (
    <div ref={anchor} className="relative flex shrink-0">
      <button
        type="button"
        aria-label={label}
        title={label}
        aria-expanded={popover.open}
        aria-haspopup="dialog"
        onClick={popover.toggle}
        className="inline-flex items-center gap-1 rounded-md px-1.25 py-0.5 text-[11px] text-ink-faint tabular-nums hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none aria-expanded:bg-hover aria-expanded:text-ink"
        data-workspace-status="ready"
        data-testid="workspace-timer"
      >
        <ClockMark />
        {remaining}
      </button>
      {popover.open && (
        <PopoverCard anchorRef={anchor} label="Keep the workspace running" className="w-72">
          <WorkspaceKeepRunning remaining={remaining} />
        </PopoverCard>
      )}
    </div>
  );
}

/**
 * What the timer's popover holds: how long the ready workspace stays, and "Keep running for 7 days",
 * which moves the Sandbox deadline to seven days from now.
 */
function WorkspaceKeepRunning({ remaining }: { remaining: string }) {
  const session = useSessionResource();
  const action = useSingleFlight<"renew">();
  const [notRenewed, setNotRenewed] = useState(false);
  if (!session) return null;
  const renew = () =>
    action.run("renew", async (signal) => {
      setNotRenewed(!(await renewWorkspace({ projectId: session.projectId }, signal)));
      await session.refresh(signal, true);
    });
  return (
    <div data-workspace-keep-running>
      <p className="px-2.25 pt-1.5 pb-2 text-[12.5px]/snug text-ink-dim tabular-nums">
        The agent&apos;s workspace stays {remaining} more after its last work. Its files are kept either way.
      </p>
      <button
        type="button"
        disabled={action.busy !== null}
        onClick={() => void renew()}
        className={cx(POPOVER_ITEM, "disabled:opacity-50")}
      >
        <Glyph name="clock" />
        {action.busy !== null ? "Keeping it running…" : "Keep running for 7 days"}
      </button>
      {(action.error || notRenewed) && (
        <p role="alert" className="px-2.25 pt-1 pb-1.5 text-[12px] text-fail">
          {action.error || "The workspace is not running; the agent's next change starts it again."}
        </p>
      )}
    </div>
  );
}

/**
 * Where the time of the current Sandbox instance's start went, opened on demand. Nothing while the instance
 * provisions, whose phases the Session receives with its outcome, or once it is reclaimed.
 */
export function WorkspaceStartup() {
  const workspace = useSessionResource()?.resource?.workspace;
  const settled = workspace?.status === "WORKSPACE_STATUS_READY" || workspace?.status === "WORKSPACE_STATUS_FAILED";
  if (!workspace || !settled || workspace.phaseTimings.length === 0) return null;
  return (
    <details className="shrink-0 px-3 py-1 text-[12px] text-ink-faint" data-workspace-startup>
      <summary className="cursor-pointer">Workspace startup</summary>
      <PhaseTimings title="Startup phases" timings={workspace.phaseTimings} labels={WORKSPACE_PHASE_LABELS} />
    </details>
  );
}

/** The current time, read again every minute while a deadline counts down. */
function useMinuteClock(deadline: string | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!deadline) return;
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, [deadline]);
  return now;
}
