// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import Link from "next/link";
import { Fragment, useMemo, type ReactNode } from "react";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { cx } from "@/components/ui/cx";
import { NotReady } from "@/components/ui/NotReady";
import { processCards, type ProcessCard } from "./process-cards";

/** Step chips for a process without stages. */
const MAX_STEPS = 8;

/** Its stages in reading order, or for a process without stages, its first Steps. */
function Outline({ card }: { card: ProcessCard }) {
  const chips = card.stages.length > 0 ? card.stages : card.steps.slice(0, MAX_STEPS).map((step) => step.label);
  return (
    <span className="flex flex-wrap items-center gap-1 text-[11.5px] text-ink-dim">
      {chips.map((label, index) => (
        <Fragment key={`${label}-${index}`}>
          {index > 0 && <span className="text-ink-faint">›</span>}
          <span
            className={cx(
              "rounded-md bg-band-0 px-1.5 py-0.5",
              card.stages.length > 0 ? "font-medium text-ink" : "text-ink-dim",
            )}
          >
            {label}
          </span>
        </Fragment>
      ))}
      {card.stages.length === 0 && card.steps.length > MAX_STEPS && (
        <span className="text-ink-faint">and {card.steps.length - MAX_STEPS} more</span>
      )}
    </span>
  );
}

/**
 * A process's one-line summary. No Flow Definition carries one yet: `dex:explanation` describes Steps,
 * not the Flow, so the line says so until the analyser emits one for the Flow.
 */
function Summary() {
  return (
    <span className="text-[12.5px] text-ink-faint italic" title="Shown here once the Flow definition describes it">
      No one-line summary yet
    </span>
  );
}

/**
 * The Flow tab without a picked Process: the project map, one card per Process of the version shown
 * with its Steps and who acts at each, an arrow where one Process starts another, a marker on a
 * Process whose definition differs between Preview and Live, and, once they are read, its runs
 * waiting for a person in the version shown. A card opens its Process.
 */
export function ProjectMap({
  names,
  hrefOf,
  changed,
  changedLabel,
  waiting,
  banner,
}: {
  /** People's names of the Flow Types. */
  names: (flowType: string) => string;
  hrefOf: (flowType: string) => string;
  changed: ReadonlySet<string>;
  /** "changed" on Preview, "differs on Preview" on Live. */
  changedLabel: string;
  /** A Process's runs waiting for a person in the version shown, in words; absent while unknown. */
  waiting?: (flowType: string) => string;
  /** A note beside the crumb, such as that Live is read only here. */
  banner?: ReactNode;
}) {
  const { definitions, ready, error } = useFlowCatalog();
  const cards = useMemo(() => processCards(definitions), [definitions]);
  if (!ready)
    return (
      <NotReady kind="placeholder" heading="Loading the Flow">
        Reading the Flow Definitions of this version.
      </NotReady>
    );
  if (error)
    return (
      <NotReady kind="error" heading="The flow did not load" retry>
        {error}
      </NotReady>
    );
  if (cards.length === 0)
    return (
      <NotReady kind="empty" heading="No processes yet">
        Describe what this project should do in the chat. The first version appears here.
      </NotReady>
    );
  const changedHere = cards.filter((card) => changed.has(card.flowType));
  return (
    <div
      className="relative min-h-0 flex-1 overflow-auto bg-canvas bg-[radial-gradient(var(--color-line-soft)_1px,transparent_1.2px)] bg-size-[18px_18px]"
      data-testid="project-map"
    >
      <div className="pointer-events-none sticky top-0 left-0 z-2 flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5 text-[12.5px] text-ink-faint">
        <div className="pointer-events-auto flex flex-wrap items-center gap-1.5">
          <span className="rounded-lg border border-line-soft bg-panel px-2.25 py-1">
            All processes · {cards.length}
          </span>
          {changedHere.length > 0 && (
            <span className="flex items-center gap-1 rounded-lg border border-line-soft bg-panel px-2.25 py-1">
              {changedLabel === "changed" ? "changed since Live:" : "differs on Preview:"}
              {changedHere.map((card) => (
                <span key={card.flowType} className="rounded-full bg-busy-subtle px-1.5 text-[10.5px] text-busy">
                  {names(card.flowType)}
                </span>
              ))}
            </span>
          )}
          {banner}
        </div>
      </div>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(19.5rem,1fr))] items-start gap-5 px-6 pt-4 pb-10">
        {cards.map((card) => {
          const waitsForSomeone = card.steps.filter((step) => ["person", "outside", "waits"].includes(step.who)).length;
          return (
            <Link
              key={card.flowType}
              href={hrefOf(card.flowType)}
              className="flex min-w-0 flex-col gap-2.5 rounded-xl border border-line-soft bg-panel p-3.5 text-left hover:border-line hover:shadow-(--shadow-lift) focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
              data-process={card.flowType}
              data-pick={names(card.flowType)}
            >
              <span className="flex flex-col">
                <span className="flex items-center gap-2">
                  <b className="text-[15px] font-semibold text-ink">{names(card.flowType)}</b>
                  {changed.has(card.flowType) && (
                    <span className="rounded-full bg-busy-subtle px-1.5 text-[10.5px] font-medium text-busy">
                      {changedLabel}
                    </span>
                  )}
                </span>
                <span className="font-mono text-[11px] text-ink-faint">{card.flowType}</span>
              </span>
              <Summary />
              <Outline card={card} />
              <span className="flex flex-col gap-0.5 text-xs text-ink-faint">
                <span>
                  {card.steps.length} step{card.steps.length === 1 ? "" : "s"}
                  {card.stages.length > 0 ? ` · ${card.stages.length} stages` : ""}
                  {waitsForSomeone > 0 ? ` · ${waitsForSomeone} wait for someone` : ""}
                </span>
                {card.apps.length > 0 && <span className="text-ink-dim">{card.apps.join(" · ")}</span>}
                {card.starts.length > 0 && (
                  <span className="text-child">↳ starts {card.starts.map(names).join(", ")}</span>
                )}
              </span>
              {waiting && (
                <span
                  className="border-t border-line-faint pt-1.5 text-xs text-ink-faint"
                  data-testid="process-waiting"
                >
                  {waiting(card.flowType)}
                </span>
              )}
            </Link>
          );
        })}
      </div>
      <p className="-mt-4 pb-6 text-center text-[12.5px] text-ink-faint">
        Click a process to see its steps. The Flow shows the version picked in the bar.
      </p>
    </div>
  );
}
