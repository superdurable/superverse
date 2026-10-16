// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { AgentStatus, type RequirementsDecision } from "@/lib/session/agent-types";
import { useProjectSession, useSessionResource } from "@/lib/session/SessionProvider";
import type { ShownDesign } from "@/lib/session/design/use-flow-design";
import { useBriefShown } from "@/lib/session/design/use-brief-on-screen";
import { designPhaseLabel } from "@/lib/shared/design-phase";
import { processName } from "@/lib/shared/format";
import { designChangeLines, ProcessBrief } from "@/components/flow/scope/flow-design";
import { LazyDesignFlowView } from "@/components/flow/scope/LazyFlowViews";
import { cx } from "@/components/ui/cx";
import { NotReady } from "@/components/ui/NotReady";

const MOCK_ANSWER = "On the mock model, the mock composer in the chat sends the answer.";

/** Counts the Brief as on screen, so the chat leaves out its pointer to it. */
function BriefShown() {
  useBriefShown();
  return null;
}

/**
 * The Process Brief of the card whose design is shown, or of a card without one: the waiting card with Confirm
 * and build, or the confirmed one. Change something… asks for changes to a waiting card, and otherwise sends a
 * message to the agent. It alone reads the Session's commands, whose streamed text then never redraws the canvas.
 */
function BriefHost({
  design,
  changes,
  drawing,
  stopped = false,
}: {
  design?: ShownDesign;
  changes: readonly string[];
  drawing?: ReactNode;
  /** The confirmed card's agent stopped short of building, which the host says above the Brief. */
  stopped?: boolean;
}) {
  const session = useProjectSession();
  const [failed, setFailed] = useState(false);
  const description = session.snapshot?.description ?? null;
  const confirmed = description?.confirmedRequirements ?? null;
  const pending = description?.pendingUserInput?.kind === "requirements" ? description.pendingUserInput : null;
  const waits = design ? design.phase === "waiting" : pending !== null;
  const waiting = waits ? pending : null;
  const card = waits ? waiting?.requirements : confirmed?.spec;
  if (!card) return drawing ?? null;
  const callId = waiting?.callId ?? null;
  const mock = description?.executor?.mode !== "remote" && description?.contextModel === "mock";
  // As in the chat: an answer stays in flight until the Snapshot takes it.
  const reconciling = callId !== null && session.answeredQuestionCallId === callId;
  const ready = description !== null && description.status !== AgentStatus.INITIALIZING;
  const answer = (decision: RequirementsDecision) => {
    if (callId === null) return Promise.reject(new Error("No card waits"));
    setFailed(false);
    return session.decideRequirements(callId, decision).catch((reason: unknown) => {
      setFailed(true);
      throw reason;
    });
  };
  const send = (text: string) => {
    setFailed(false);
    return session.send(text).catch((reason: unknown) => {
      setFailed(true);
      throw reason;
    });
  };
  const actions = mock
    ? undefined
    : {
        disabled:
          session.mutating ||
          session.status !== "SESSION_STATUS_ACTIVE" ||
          !ready ||
          !session.snapshotCurrent ||
          reconciling,
        submitting: (callId !== null && session.answeringQuestionCallId === callId) || reconciling,
        error: failed ? session.commandError : null,
        onConfirm:
          callId === null
            ? undefined
            : () => void answer({ isConfirmed: true, requestedChanges: "" }).catch(() => undefined),
        onChange: (text: string) =>
          callId === null ? send(text) : answer({ isConfirmed: false, requestedChanges: text }),
      };
  return (
    <>
      <BriefShown />
      <ProcessBrief
        key={callId ?? "confirmed"}
        card={card}
        version={(confirmed?.revision ?? 0) + (waiting ? 1 : 0)}
        status={callId !== null ? "waiting" : stopped ? "stopped" : "building"}
        designSummary={design?.summary}
        outline={design?.outline}
        design={design?.body}
        changes={changes}
        drawing={drawing}
        actions={actions}
        note={mock && callId !== null ? MOCK_ANSWER : undefined}
      />
    </>
  );
}

/** What the Flow tab says above a confirmed card's Brief until its processes are built. */
const CONFIRMED_NOTE = {
  building: "The agent is building it. The processes show here once they are built.",
  stalled:
    "Not built yet: the agent's last turn ended before the processes were built. Ask it in the chat to carry on; its last turn's More details show what failed.",
} as const;

/**
 * The Process Brief of a card that carries no Flow design, which the Flow tab shows alone: the same summary
 * without a drawing, while it waits and once it is confirmed, saying whether the agent is still building what
 * it describes.
 */
export function CardBrief({ state }: { state: "waiting" | keyof typeof CONFIRMED_NOTE }) {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      {state !== "waiting" && (
        <p
          className={cx(
            "border-b border-line-faint px-5 py-2.5 text-body",
            state === "stalled" ? "bg-busy-subtle text-busy" : "bg-panel text-ink-dim",
          )}
          role="status"
          data-card-brief-state={state}
        >
          {CONFIRMED_NOTE[state]}
        </p>
      )}
      <BriefHost changes={[]} stopped={state === "stalled"} />
    </div>
  );
}

/**
 * A designed Flow on the Flow tab, in place of the implemented one it changes: the Process Brief of its card
 * with the design drawn below its summary, left to right with its stages closed, while the card waits and
 * after it is confirmed. Its Steps are compared with the current source only when that source is the Session
 * head's.
 */
export function DesignSurface({ design, flowType }: { design: ShownDesign; flowType: string }) {
  const { currentSource } = useProcessDefinitions();
  const head = useSessionResource()?.resource?.headCommit;
  const current = currentSource && currentSource.commit === head ? currentSource.graphs : null;
  const body = design.body;
  const changes = useMemo(() => (body && current ? designChangeLines(body, current) : []), [body, current]);
  const drawing = body ? (
    <LazyDesignFlowView
      flowType={flowType}
      design={body}
      phase={design.phase}
      current={current}
      features={design.features}
      direction="lr"
    />
  ) : design.error ? (
    <NotReady kind="error" heading="The Flow design did not load" retry>
      {design.error}
    </NotReady>
  ) : (
    <NotReady kind="placeholder" heading="Reading the Flow design">
      The agent&apos;s design of {processName(flowType)} is loading.
    </NotReady>
  );
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <BriefHost design={design} changes={changes} drawing={drawing} />
    </div>
  );
}

/** Above all processes while a design is shown: its label, its summary and a link to each designed Flow. */
export function DesignBar({ design, hrefOf }: { design: ShownDesign; hrefOf: (flowType: string) => string }) {
  return (
    <div
      role="status"
      aria-label="Flow design"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-line-soft bg-panel px-3 py-2 text-[12.5px]"
    >
      <strong className="font-semibold text-accent">{designPhaseLabel(design.phase)}</strong>
      <span className="text-ink-dim">{design.error ?? design.summary}</span>
      {design.body?.flows.map(({ flowType }) => (
        <Link key={flowType} href={hrefOf(flowType)} className="text-accent hover:underline">
          {processName(flowType)}
        </Link>
      ))}
      {design.error && (
        <button type="button" className="text-accent hover:underline" onClick={design.retry}>
          Try again
        </button>
      )}
    </div>
  );
}
