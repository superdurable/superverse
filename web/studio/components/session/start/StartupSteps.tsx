// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";
import { RunningMark } from "@/components/ui/wait/LongWait";
import { AgentStatus, type AgentDescription, type SequencedMessage } from "@/lib/session/agent-types";

/** What a new Project goes through before the agent first answers, in order, as a person would say it. */
const STEPS = [
  { step: "project", label: "Create your project" },
  { step: "agent", label: "Start the agent" },
  { step: "workspace", label: "Get a workspace ready" },
  { step: "reading", label: "Read your description" },
] as const;

export type StartupStep = (typeof STEPS)[number]["step"];

/** How long the shown step has run, as "12 s" or "1 min 5 s". */
function elapsed(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  return seconds < 60 ? `${seconds} s` : `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
}

/** How long the current step has shown, from when it first rendered, ticking every second. */
function Elapsed() {
  const [since] = useState(() => Date.now());
  const [now, setNow] = useState(since);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return <span className="text-caption text-ink-faint">· {elapsed(now - since)}</span>;
}

/**
 * The steps before a new Project's agent first answers, each done, current (with how long it has run) or still
 * to come, so a wait that is real infrastructure work reads as progress rather than one line. On the creation pages
 * (`mark`) the running mark heads the card: at once while the Project is created, after the shared long-wait delay
 * from a later step. The chat never shows the mark.
 */
export function StartupSteps({ current, mark = false }: { current: StartupStep; mark?: boolean }) {
  const index = STEPS.findIndex((item) => item.step === current);
  return (
    <div className="mx-auto grid w-full max-w-sm gap-2.5 rounded-lg border border-line-soft bg-panel p-3 text-body">
      {mark && <RunningMark size="panel" chain="startup" delay={current === "project" ? 0 : undefined} />}
      <ol aria-label="Setting up" role="status" className="grid gap-1.5" data-startup-step={current}>
        {STEPS.map(({ step, label }, at) => (
          <li
            key={step}
            className={cx(
              "flex items-center gap-2",
              at < index && "text-ink-dim",
              at === index && "text-ink",
              at > index && "text-ink-faint",
            )}
          >
            {at < index ? (
              <Glyph name="check" className="size-3.5 text-pass" />
            ) : at === index ? (
              <Spinner className="h-3.5 w-3.5" />
            ) : (
              <span aria-hidden="true" className="size-3.5" />
            )}
            <span>{label}</span>
            {at === index && <Elapsed />}
          </li>
        ))}
      </ol>
    </div>
  );
}

/**
 * The chat's startup status: before the agent's first answer, the steps with the current one; afterwards the
 * workspace line when a later tool waits for one. A Session's lifecycle message comes first either way.
 */
export function StartupStatus({
  messages,
  description,
  lifecycle,
  workspacePreparing,
}: {
  messages: readonly SequencedMessage[];
  description: AgentDescription | null;
  lifecycle: string | null;
  workspacePreparing: boolean;
}) {
  const answered =
    Boolean(description?.pendingUserInput) || messages.some(({ message }) => message.role === "assistant");
  const agentReady = description !== null && description.status !== AgentStatus.INITIALIZING;
  const step: StartupStep | null = answered
    ? null
    : !agentReady
      ? "agent"
      : workspacePreparing
        ? "workspace"
        : "reading";
  return (
    <>
      {lifecycle && (
        <div className="rounded border border-line-soft bg-panel p-3 text-[12px] text-ink-dim">{lifecycle}</div>
      )}
      {step ? (
        <StartupSteps current={step} />
      ) : (
        workspacePreparing && (
          <div className="rounded border border-line-soft bg-panel p-3 text-[12px] text-ink-dim">
            <Spinner className="mr-2 inline h-3.5 w-3.5" /> Getting the workspace ready…
          </div>
        )
      )}
    </>
  );
}
