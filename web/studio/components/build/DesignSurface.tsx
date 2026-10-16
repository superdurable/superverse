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
import { AgentStatus } from "@/lib/session/agent-types";
import { useProjectSession, useSessionResource } from "@/lib/session/SessionProvider";
import type { ShownDesign } from "@/lib/session/design/use-flow-design";
import { designPhaseLabel } from "@/lib/shared/design-phase";
import { processName } from "@/lib/shared/format";
import { designChangeLines, ProcessBrief } from "@/components/flow/scope/flow-design";
import { LazyDesignFlowView } from "@/components/flow/scope/LazyFlowViews";
import { cx } from "@/components/ui/cx";
import { NotReady } from "@/components/ui/NotReady";

const MOCK_CHANGE = "On the mock model, the mock composer in the chat sends the scenario's next input.";
const ANSWER_FIRST = "The agent waits for your answers in the chat; answer them, then change the brief.";

/**
 * The Process Brief of the current version of the requirements, whose design is shown or which has none. The
 * agent builds from it at once; Change something… sends a message, which steers the turn while one runs, and
 * the agent records a revised version and builds what changed. It alone reads the Session's commands, whose
 * streamed text then never redraws the canvas.
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
  /** The agent's last turn ended short of building the card, which the host says above the Brief. */
  stopped?: boolean;
}) {
  const session = useProjectSession();
  const [failed, setFailed] = useState(false);
  const description = session.snapshot?.description ?? null;
  const current = description?.confirmedRequirements ?? null;
  if (!current) return drawing ?? null;
  const mock = description?.executor?.mode !== "remote" && description?.contextModel === "mock";
  const ready = description !== null && description.status !== AgentStatus.INITIALIZING;
  // Questions block every message until they are answered; a card a Session still waits at takes it as its reply.
  const questions = description?.pendingUserInput?.kind === "question";
  const turnRunning = description !== null && description.status !== AgentStatus.WAITING_FOR_MESSAGE;
  const change = (text: string) => {
    setFailed(false);
    return session.change(text, turnRunning).catch((reason: unknown) => {
      setFailed(true);
      throw reason;
    });
  };
  const actions =
    mock || questions
      ? undefined
      : {
          disabled:
            session.mutating || session.status !== "SESSION_STATUS_ACTIVE" || !ready || !session.snapshotCurrent,
          submitting: session.mutationKind === "send",
          error: failed ? session.commandError : null,
          onChange: change,
        };
  return (
    <ProcessBrief
      key={String(current.revision)}
      card={current.spec}
      version={current.revision}
      status={design?.phase === "built" ? "built" : stopped ? "stopped" : "building"}
      designSummary={design?.summary}
      outline={design?.outline}
      design={design?.body}
      changes={changes}
      drawing={drawing}
      actions={actions}
      note={mock ? MOCK_CHANGE : questions ? ANSWER_FIRST : undefined}
    />
  );
}

/** What the Flow tab says above a recorded card's Brief until its processes are built. */
const CARD_NOTE = {
  building: "The agent is building it. The processes show here once they are built.",
  stalled:
    "Not built yet: the agent's last turn ended before the processes were built. Ask it in the chat to carry on; its last turn's More details show what failed.",
} as const;

/**
 * The Process Brief of a card that carries no Flow design, which the Flow tab shows alone: the same summary
 * without a drawing, saying whether the agent is still building what it describes.
 */
export function CardBrief({ state }: { state: keyof typeof CARD_NOTE }) {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <p
        className={cx(
          "border-b border-line-faint px-5 py-2.5 text-body",
          state === "stalled" ? "bg-busy-subtle text-busy" : "bg-panel text-ink-dim",
        )}
        role="status"
        data-card-brief-state={state}
      >
        {CARD_NOTE[state]}
      </p>
      <BriefHost changes={[]} stopped={state === "stalled"} />
    </div>
  );
}

/**
 * A designed Flow on the Flow tab, in place of the implemented one it changes: the Process Brief of its card
 * with the design drawn below its summary, left to right with its stages closed, from the moment the agent
 * records the card. Its Steps are compared with the current source only when that source is the Session
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
