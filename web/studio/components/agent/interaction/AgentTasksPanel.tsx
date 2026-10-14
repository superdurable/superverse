// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { Eyebrow } from "@/components/ui/Eyebrow";
import {
  isSubagentRunning,
  type BackgroundCommand,
  type ConnectorContribution,
  type Subagent,
} from "@/lib/session/agent-types";
import { formatPaddedDuration } from "@/lib/shared/format";

/** The current time, read again each second while something runs. */
function useSecondClock(running: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [running]);
  return now;
}

/** How long a task ran: until it ended, or until now while it runs. */
function taskDuration(startedAt: string, endedAt: string | null, now: number): string | null {
  return formatPaddedDuration((endedAt ? Date.parse(endedAt) : now) - Date.parse(startedAt));
}

/** An output size as a person reads it. */
function formatOutputBytes(bytes: number): string {
  if (bytes < 1_024) return `${bytes} B`;
  if (bytes < 1_048_576) return `${(bytes / 1_024).toFixed(1)} KB`;
  return `${(bytes / 1_048_576).toFixed(1)} MB`;
}

/** What one background command's line says: its state, as the Session recorded it. */
function taskStatusText(command: BackgroundCommand, stopping: boolean): string {
  switch (command.status) {
    case "running":
      return stopping ? "Stopping" : "Running";
    case "stopped":
      return "Stopped";
    case "lost":
      return "Lost before it finished";
    case "exited":
      break;
  }
  if (command.endReason === "lifetime_limit") return "Stopped at its time limit";
  if (command.endReason === "output_limit") return "Stopped after too much output";
  return command.exitCode === 0 ? "Finished" : `Failed (exit code ${command.exitCode ?? "unknown"})`;
}

/** What one subagent's status says, as the Session recorded it. */
const SUBAGENT_STATUS_TEXT = {
  starting: "Starting",
  running: "Running",
  completed: "Finished",
  failed: "Failed",
  stopped: "Stopped",
  timed_out: "Stopped at its time limit",
  merged: "Merged",
  discarded: "Discarded",
} as const satisfies Record<Subagent["status"], string>;

/** What one connector contribution's status says, as its own Flow recorded it. */
const CONTRIBUTION_STATUS_TEXT = {
  pull_request_pending: "Opening a draft pull request",
  unpublished: "Built on a local branch; this environment opens no pull request",
  pull_request_failed: "Pull request could not be opened",
  draft: "Draft pull request, waiting for review",
  closed: "Closed",
} as const satisfies Record<ConnectorContribution["status"], string>;

/**
 * One connector contribution: the connector and, below it, its status or the link to its pull
 * request once open, since a status is a sentence that a line beside the name would squeeze, and
 * the release candidate of its head.
 */
function ContributionRow({ contribution }: { contribution: ConnectorContribution }) {
  return (
    <li
      className="flex flex-col gap-0.5"
      data-contribution-slug={contribution.slug}
      data-contribution-status={contribution.status}
    >
      <span className="min-w-0 wrap-anywhere text-ink-dim">
        Connector {contribution.slug}
        <span className="ml-2 text-[11px] text-ink-faint">{contribution.kind}</span>
      </span>
      <span className="text-[11px] wrap-anywhere text-ink-faint">
        {contribution.pullRequest ? (
          <a className="underline" href={contribution.pullRequest.url} rel="noreferrer" target="_blank">
            Pull request #{contribution.pullRequest.number}
          </a>
        ) : (
          CONTRIBUTION_STATUS_TEXT[contribution.status]
        )}
      </span>
      {contribution.releaseCandidate && (
        <span
          className="text-[11px] wrap-anywhere text-ink-faint"
          data-release-candidate-status={contribution.releaseCandidate.status}
        >
          {contribution.releaseCandidate.status === "published"
            ? `Release candidate ${contribution.releaseCandidate.version}`
            : `Release candidate failed: ${contribution.releaseCandidate.failure}`}
        </span>
      )}
    </li>
  );
}

function TaskRow({ command, now, stopping }: { command: BackgroundCommand; now: number; stopping: boolean }) {
  const details = [
    taskStatusText(command, stopping),
    taskDuration(command.startedAt, command.endedAt, now),
    command.outputBytes > 0 ? `${formatOutputBytes(command.outputBytes)} output` : null,
  ].filter(Boolean);
  return (
    <li
      className="flex items-baseline justify-between gap-3"
      data-task-id={command.taskId}
      data-task-status={command.status}
    >
      <span className="min-w-0 wrap-anywhere text-ink-dim">{command.description}</span>
      <span className="shrink-0 text-[11px] text-ink-faint">{details.join(" · ")}</span>
    </li>
  );
}

/**
 * One subagent: its task and kind, its status and how long it ran, and below it the newest live
 * status line while it runs, or its summary and the files its saved changes touch once it ended.
 */
function SubagentRow({
  subagent,
  line,
  now,
  stopping = false,
}: {
  subagent: Subagent;
  line: string | undefined;
  now: number;
  stopping?: boolean;
}) {
  const isRunning = isSubagentRunning(subagent);
  const files = subagent.changedFiles;
  return (
    <li className="flex flex-col gap-0.5" data-task-id={subagent.taskId} data-task-status={subagent.status}>
      <span className="flex items-baseline justify-between gap-3">
        <span className="min-w-0 wrap-anywhere text-ink-dim">
          {subagent.description}
          <span className="ml-2 text-[11px] text-ink-faint">{subagent.kind}</span>
        </span>
        <span className="shrink-0 text-[11px] text-ink-faint">
          {subagent.lost
            ? "Lost, no report"
            : isRunning && stopping
              ? "Stopping"
              : SUBAGENT_STATUS_TEXT[subagent.status]}
          {subagent.startedAt ? ` · ${taskDuration(subagent.startedAt, subagent.finishedAt, now) ?? ""}` : ""}
        </span>
      </span>
      {isRunning && line && <span className="truncate text-[11px] text-ink-faint">{line}</span>}
      {!isRunning && subagent.summary && (
        <span className="text-[11px] wrap-anywhere text-ink-faint">{subagent.summary}</span>
      )}
      {!isRunning && files > 0 && (
        <span className="text-[11px] text-ink-faint">
          {files} {files === 1 ? "file" : "files"} changed (+{subagent.insertions} −{subagent.deletions})
        </span>
      )}
    </li>
  );
}

/**
 * The Agent's background tasks, read-only: its subagents and background commands, the running ones
 * and the finished ones under a disclosure, and the connectors it contributes. Each line comes from
 * the Session's record or a contribution's own, and a running subagent's live line from its
 * best-effort activity; the Stop button beside the composer stops them all, so a task has no button
 * of its own.
 */
export function AgentTasksPanel({
  commands,
  subagents,
  subagentLines,
  contributions,
  stopping = false,
}: {
  commands: readonly BackgroundCommand[];
  subagents: readonly Subagent[];
  subagentLines: ReadonlyMap<string, string>;
  contributions: readonly ConnectorContribution[];
  /** The Session admitted a Stop, so the running tasks are ending. */
  stopping?: boolean;
}) {
  const now = useSecondClock(
    commands.some((command) => command.status === "running") || subagents.some(isSubagentRunning),
  );
  if (commands.length === 0 && subagents.length === 0 && contributions.length === 0) return null;
  const runningAgents = subagents.filter(isSubagentRunning);
  const finishedAgents = subagents.filter((subagent) => !isSubagentRunning(subagent)).reverse();
  const running = commands.filter((command) => command.status === "running");
  const finished = commands.filter((command) => command.status !== "running").reverse();
  const finishedCount = finishedAgents.length + finished.length;
  return (
    <section aria-label="Background tasks" className="rounded-lg border border-line-soft bg-panel p-3 text-[12px]">
      <Eyebrow>Background tasks</Eyebrow>
      {runningAgents.length + running.length > 0 && (
        <ul className="mt-2 flex flex-col gap-1">
          {runningAgents.map((subagent) => (
            <SubagentRow
              key={subagent.taskId}
              line={subagentLines.get(subagent.taskId)}
              subagent={subagent}
              now={now}
              stopping={stopping}
            />
          ))}
          {running.map((command) => (
            <TaskRow command={command} key={command.taskId} now={now} stopping={stopping} />
          ))}
        </ul>
      )}
      {contributions.length > 0 && (
        <ul aria-label="Connector contributions" className="mt-2 flex flex-col gap-1">
          {contributions.map((contribution) => (
            <ContributionRow contribution={contribution} key={contribution.slug} />
          ))}
        </ul>
      )}
      {finishedCount > 0 && (
        <details className="mt-2">
          <summary className="cursor-pointer text-ink-faint">{finishedCount} finished</summary>
          <ul className="mt-1 flex flex-col gap-1">
            {finishedAgents.map((subagent) => (
              <SubagentRow key={subagent.taskId} line={undefined} subagent={subagent} now={now} />
            ))}
            {finished.map((command) => (
              <TaskRow command={command} key={command.taskId} now={now} stopping={stopping} />
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}
