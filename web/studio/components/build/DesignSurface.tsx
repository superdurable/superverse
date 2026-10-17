// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useMemo, type ReactNode } from "react";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { useProjectSession, useSessionResource } from "@/lib/session/SessionProvider";
import type { ShownDesign } from "@/lib/session/design/use-flow-design";
import { designPhaseLabel } from "@/lib/shared/design-phase";
import { processName } from "@/lib/shared/format";
import { designChangeLines, DraftBrief, ProcessBrief } from "@/components/flow/scope/flow-design";
import { LazyDesignFlowView } from "@/components/flow/scope/LazyFlowViews";
import { NotReady } from "@/components/ui/NotReady";

/** How to answer a card a Session still waits at from before cards stopped waiting. */
const OLD_CARD_NOTE = "Reply in the chat to build or change it.";

/**
 * The Flow tab's overview of the current version of the requirements while its design is drafted or built:
 * the drawing with the title and status on its corner, the summary line and More details under it. Once the
 * design is built the drawing stands alone, until the processes built from it replace it; a new version
 * with a design brings the overview back for that version. The person changes the card only in the chat.
 * While the agent writes the next card, its draft takes the place.
 */
function BriefHost({
  design,
  changes,
  drawing,
  waiting,
  stopped = false,
}: {
  design?: ShownDesign;
  changes: readonly string[];
  drawing?: ReactNode;
  /** What the canvas says for a card without a design. */
  waiting?: string;
  /** The agent's last turn ended short of building the card. */
  stopped?: boolean;
}) {
  const session = useProjectSession();
  const description = session.snapshot?.description ?? null;
  const current = description?.confirmedRequirements ?? null;
  if (session.requirementsDraft) return <DraftBrief draft={session.requirementsDraft} />;
  if (!current || design?.phase === "built") return drawing ?? null;
  return (
    <ProcessBrief
      key={String(current.revision)}
      card={current.spec}
      status={stopped ? "stopped" : "building"}
      outline={design?.outline}
      design={design?.body}
      changes={changes}
      drawing={drawing}
      waiting={waiting}
      note={description?.pendingUserInput?.kind === "requirements" ? OLD_CARD_NOTE : undefined}
    />
  );
}

/** What the Flow tab says in place of a drawing for a recorded card without a design, until it is built. */
const CARD_NOTE = {
  building: "The agent is building it. The processes show here once they are built.",
  stalled:
    "Not built yet: the agent's last turn ended before the processes were built. Ask it in the chat to carry on; its last turn's More details show what failed.",
} as const;

/** The overview of a card that carries no Flow design: the same summary and More details, with no drawing. */
export function CardBrief({ state }: { state: keyof typeof CARD_NOTE }) {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <BriefHost changes={[]} waiting={CARD_NOTE[state]} stopped={state === "stalled"} />
    </div>
  );
}

/**
 * A designed Flow on the Flow tab, in place of the implemented one it changes, drawn left to right with its
 * stages closed from the moment the agent records the card, under the overview while it is built. Its Steps
 * are compared with the current source only when that source is the Session head's.
 */
export function DesignSurface({ design, flowType }: { design: ShownDesign; flowType: string }) {
  const { currentSource } = useProcessDefinitions();
  const head = useSessionResource()?.resource?.headCommit;
  const title = useProjectSession().snapshot?.description?.confirmedRequirements?.spec.title;
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
      written={design.writtenSteps}
      direction="lr"
      heading={title}
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
