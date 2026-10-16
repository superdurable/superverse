// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { renewWorkspace } from "@/lib/api/session";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { isWorkspacePaused, workspaceDeadline } from "@/lib/session/session-status";
import { formatRemainingTime } from "@/lib/shared/format";
import { Button } from "@/components/ui/Button";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { PhaseTimings } from "@/components/ui/timings/PhaseTimings";

/** The phases of a workspace start in order: the Sandbox becoming ready, then the repository checkout. */
const WORKSPACE_PHASE_LABELS: Readonly<Record<string, string>> = {
  provision: "Starting the Sandbox",
  checkout: "Checking out the repository",
};

type SessionConnectionState = "complete" | "connected" | "connecting" | "reconnecting";
function useConnectionState(): SessionConnectionState {
  const session = useSessionResource();
  const active = session?.status === "SESSION_STATUS_ACTIVE";
  return !active ? "complete" : session.streamState === "idle" ? "connecting" : session.streamState;
}

function connectionLabel(state: SessionConnectionState): string {
  if (state === "connected") return "Live updates connected";
  if (state === "reconnecting") {
    return "Live updates reconnecting; durable history remains current";
  }
  return "Connecting to live updates";
}

/** An amber dot beside the chat's heading while live updates reconnect; nothing while they are live. */
export function SessionConnectionIndicator() {
  const state = useConnectionState();
  if (state !== "reconnecting") return null;
  const label = connectionLabel(state);
  return (
    <span
      aria-label={label}
      className="h-1.5 w-1.5 shrink-0 rounded-full bg-busy"
      data-session-connection={state}
      role="status"
      title={label}
    />
  );
}

/**
 * The chat header's More details, a card below the link: whether live updates are connected, how
 * long the workspace is kept with Keep running for 7 days, and where the time of its last start went.
 * Nothing once the Session is no longer active and has no workspace to tell about.
 */
export function SessionDetails() {
  const state = useConnectionState();
  const workspace = useSessionResource()?.resource?.workspace;
  if (state === "complete" && !workspace) return null;
  return (
    <MoreDetails surface="chat.header" className="relative flex items-center">
      <div className="absolute top-full right-0 z-20 mt-1.5 flex w-72 max-w-[85vw] flex-col gap-2 rounded-lg border border-line-soft bg-panel p-3 text-caption text-ink-dim shadow-(--shadow-lift)">
        {state !== "complete" && <p data-session-connection-detail={state}>{connectionLabel(state)}</p>}
        <WorkspaceKept />
        <WorkspaceStartup />
      </div>
    </MoreDetails>
  );
}

/**
 * The Session's workspace: how long the ready Sandbox stays after the agent last used it, with "Keep
 * running for 7 days", which moves the Sandbox deadline to seven days from now, or that the Session
 * paused it and kept its files. Nothing while no workspace is ready or paused.
 */
function WorkspaceKept() {
  const session = useSessionResource();
  const resource = session?.resource ?? null;
  const deadline = resource ? workspaceDeadline(resource) : null;
  const paused = resource !== null && isWorkspacePaused(resource);
  const now = useMinuteClock(deadline);
  const action = useSingleFlight<"renew">();
  const [notRenewed, setNotRenewed] = useState(false);
  if (paused) {
    return (
      <p data-workspace-status="paused" role="status">
        Workspace paused; your files are kept.
      </p>
    );
  }
  if (!session || !deadline) return null;
  const renew = () =>
    action.run("renew", async (signal) => {
      setNotRenewed(!(await renewWorkspace({ projectId: session.projectId }, signal)));
      await session.refresh(signal, true);
    });
  return (
    <div className="flex flex-col items-start gap-1.5" data-workspace-status="ready" data-workspace-keep-running>
      <p className="tabular-nums" data-testid="workspace-timer">
        Workspace kept for {formatRemainingTime(deadline, now)} more; the agent&apos;s work extends it. Its files are
        kept either way.
      </p>
      <Button size="sm" disabled={action.busy !== null} onClick={() => void renew()}>
        {action.busy !== null ? "Keeping it running…" : "Keep running for 7 days"}
      </Button>
      {(action.error || notRenewed) && (
        <p role="alert" className="text-fail">
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
function WorkspaceStartup() {
  const workspace = useSessionResource()?.resource?.workspace;
  const settled = workspace?.status === "WORKSPACE_STATUS_READY" || workspace?.status === "WORKSPACE_STATUS_FAILED";
  if (!workspace || !settled || workspace.phaseTimings.length === 0) return null;
  return (
    <details className="text-ink-faint" data-workspace-startup>
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
