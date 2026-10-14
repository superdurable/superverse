// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { FlowLink } from "@/components/flow/frame/FlowLink";
import { cx } from "@/components/ui/cx";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { formatDate } from "@/lib/shared/format";
import { backendErrorLabel, durabilityLabel, pendingStepMethodPhaseLabel } from "@/lib/flow/enum-labels";
import { eventTitle, eventTypeLabel } from "@/components/flow/diagnostics/event/EventDetails";
import { previousRunId, stepContext, stepMethod } from "@/lib/api/flow/flow-history";
import { asRecord, isRecord } from "@/lib/shared/json";

type EventTone = "fork" | "pending" | "failed" | "waiting" | "completed" | "closed" | "neutral";
type Method = "wait-for" | "execute" | "other";
/** selected is the reader's event; linked sits at either end of the lit link; counterpart is the other end. */
type CardState = "plain" | "linked" | "counterpart" | "selected";

function eventTone(event: FlowHistoryEvent): EventTone {
  if (event.type === "TimeTravelFork") return "fork";
  if (event.type.endsWith("Pending")) return "pending";
  if (event.type.endsWith("Failed")) return "failed";
  if (event.type === "StepWaitForCompleted") return "waiting";
  if (event.type.endsWith("Completed")) return "completed";
  if (event.type === "FlowClosed") return "closed";
  return "neutral";
}

const CARD = cx(
  "mb-3.75 rounded-xl bg-raised p-3.25 transition-[border-color,box-shadow,transform] duration-140 max-md:col-start-2",
);

const LINKED = {
  other: cx("border border-pass-rule shadow-[0_8px_22px_rgba(79,129,39,0.09)]"),
  "wait-for": cx(
    "border border-busy-rule bg-linear-to-r from-busy-subtle to-raised to-38% shadow-[0_8px_22px_rgba(128,88,23,0.1)]",
  ),
  execute: cx(
    "border border-pass-rule bg-linear-to-r from-pass-subtle to-raised to-38% shadow-[0_8px_22px_rgba(79,129,39,0.1)]",
  ),
} satisfies Record<Method, string>;

const COUNTERPART = {
  other: cx(
    "border border-pass-rule shadow-[0_8px_22px_rgba(79,129,39,0.09)] before:border-door-rule before:bg-door-subtle before:text-door",
  ),
  "wait-for": cx(
    "border border-busy-rule bg-linear-to-r from-busy-subtle to-raised to-38% shadow-[0_0_0_3px_rgba(211,151,50,0.2),0_10px_25px_rgba(128,88,23,0.12)] before:border-busy-rule before:bg-busy-subtle before:text-busy",
  ),
  execute: cx(
    "border border-cta bg-linear-to-r from-pass-subtle to-raised to-38% shadow-[0_0_0_3px_rgba(111,160,54,0.2),0_10px_25px_rgba(79,129,39,0.12)] before:border-pass-rule before:bg-pass-subtle before:text-pass",
  ),
} satisfies Record<Method, string>;

const COUNTERPART_BADGE = cx(
  "relative before:absolute before:-top-2.5 before:right-3 before:z-1 before:rounded-full before:border before:px-1.75 before:py-0.75 before:text-[8px] before:font-extrabold before:tracking-[0.04em] before:uppercase before:content-['Linked_event']",
);

const SELECTED = {
  other: cx("shadow-[inset_5px_0_0_var(--color-cta),0_0_0_4px_rgba(111,160,54,0.16),0_12px_28px_rgba(79,129,39,0.13)]"),
  "wait-for": cx(
    "bg-linear-to-r from-busy-subtle to-raised to-46% shadow-[inset_5px_0_0_var(--color-busy-rule),0_0_0_4px_rgba(211,151,50,0.24),0_13px_30px_rgba(128,88,23,0.16)]",
  ),
  execute: cx(
    "bg-linear-to-r from-pass-subtle to-raised to-46% shadow-[inset_5px_0_0_var(--color-pass-rule),0_0_0_4px_rgba(111,160,54,0.24),0_13px_30px_rgba(79,129,39,0.16)]",
  ),
} satisfies Record<Method, string>;

function cardClass(method: Method, state: CardState): string {
  if (state === "selected") return cx(CARD, "relative -translate-y-px border-2 border-accent", SELECTED[method]);
  if (state === "counterpart") return cx(CARD, COUNTERPART_BADGE, COUNTERPART[method]);
  if (state === "linked") return cx(CARD, LINKED[method]);
  return cx(CARD, "border border-line-faint");
}

const DOT = cx(
  "z-1 mt-4.25 ml-2 size-2.5 rounded-full border-2 border-line-faint bg-raised transition-[border-color,box-shadow,transform] duration-140 max-md:ml-0.75",
);

const DOT_RING = {
  fork: cx("shadow-[0_0_0_1px_var(--color-door)]"),
  pending: cx("shadow-[0_0_0_1px_var(--color-child)]"),
  failed: cx("shadow-[0_0_0_1px_var(--color-fail)]"),
  waiting: cx("shadow-[0_0_0_1px_var(--color-busy-rule)]"),
  completed: cx("shadow-[0_0_0_1px_var(--color-pass)]"),
  closed: cx("shadow-[0_0_0_1px_var(--color-mark)]"),
  neutral: cx("shadow-[0_0_0_1px_var(--color-mark)]"),
} satisfies Record<EventTone, string>;

const SELECTED_DOT = {
  other: undefined,
  "wait-for": cx("shadow-[0_0_0_4px_rgba(211,151,50,0.28)]"),
  execute: cx("shadow-[0_0_0_4px_rgba(111,160,54,0.28)]"),
} satisfies Record<Method, string | undefined>;

function dotClass(tone: EventTone, method: Method, selected: boolean, linked: boolean): string {
  const ring =
    (selected && SELECTED_DOT[method]) || (linked ? cx("shadow-[0_0_0_3px_rgba(143,201,61,0.32)]") : DOT_RING[tone]);
  return cx(DOT, ring, linked && "scale-135");
}

const TYPE_INK: Partial<Record<EventTone, string>> = { failed: cx("text-fail"), completed: cx("text-pass") };

function executionSummary(event: FlowHistoryEvent): string {
  const context = stepContext(event);
  return [context.stepType, context.stepExecutionId].filter(Boolean).join(" · ");
}

/**
 * One event of the Timeline: its time, its dot on the rail and its card. The card carries
 * `data-event-card`, which the selected-event connector and the link layout measure.
 */
export function TimelineEventRow({
  event,
  flowId,
  eventNumber,
  relativeSeconds,
  state,
  cardRef,
  onSelect,
}: {
  event: FlowHistoryEvent;
  flowId: string;
  eventNumber: number | undefined;
  relativeSeconds: number | null;
  state: CardState;
  cardRef: (node: HTMLDivElement | null) => void;
  onSelect: () => void;
}) {
  const tone = eventTone(event);
  const method: Method = stepMethod(event) ?? "other";
  const previousRun = previousRunId(event);
  const summary = executionSummary(event);
  return (
    <article
      className="grid cursor-pointer grid-cols-[145px_var(--timeline-link-rail-width,88px)_minmax(0,1fr)] max-md:grid-cols-[calc(var(--timeline-link-rail-width,88px)-14px)_minmax(0,1fr)]"
      data-event-id={event.eventId}
      onClick={onSelect}
    >
      <div className="flex flex-col gap-1.25 pt-3.25 pr-2 pb-7 text-right text-[10px] text-ink-faint max-md:col-start-2 max-md:flex-row max-md:p-0 max-md:pb-1.5 max-md:text-left">
        <b className="font-semibold">{formatDate(event.eventTime)}</b>
        {relativeSeconds !== null && <span>+{relativeSeconds}s</span>}
      </div>
      <div className="relative flex justify-start before:absolute before:inset-y-0 before:left-3.25 before:w-px before:bg-line-soft max-md:col-start-1 max-md:row-span-2 max-md:row-start-1 max-md:before:left-2">
        <span className={dotClass(tone, method, state === "selected", state !== "plain")} />
      </div>
      <div className={cardClass(method, state)} data-event-card="" ref={cardRef}>
        {/* On a phone the type badge wraps under the title instead of overflowing the card. */}
        <header className="flex justify-between gap-x-3.75 gap-y-1.5 max-md:flex-wrap">
          <div>
            <span className="font-mono text-[10px] text-ink-faint">#{eventNumber}</span>
            <h3 className="ml-2 inline text-xs font-semibold text-ink-dim">
              {previousRun ? (
                <FlowLink
                  className="text-inherit underline decoration-pass/35 underline-offset-3 hover:text-pass hover:decoration-current"
                  title={previousRun}
                  to={{ surface: "diagnostics", flowId, runId: previousRun }}
                >
                  {event.type === "TimeTravelFork" ? "Time Travel fork" : "Flow continued"}
                </FlowLink>
              ) : (
                eventTitle(event)
              )}
            </h3>
            {summary && <p className="mt-1.5 text-[11px] text-ink-faint">{summary}</p>}
          </div>
          <span
            className={cx(
              "self-start rounded-md bg-panel px-1.5 py-1 text-[9px] font-extrabold whitespace-nowrap",
              TYPE_INK[tone] ?? "text-ink-faint",
            )}
          >
            {eventTypeLabel(event)}
          </span>
        </header>
        <EventHighlights event={event} />
      </div>
    </article>
  );
}

function pendingPhase(event: FlowHistoryEvent): string {
  if (!event.type.endsWith("Pending")) return "";
  const phase = pendingStepMethodPhaseLabel(event.payload.phase);
  return phase === "Unspecified" ? "Pending" : phase;
}

const HIGHLIGHT = cx("rounded-md bg-panel px-1.75 py-1.25 text-[10px] text-ink-faint");

function EventHighlights({ event }: { event: FlowHistoryEvent }) {
  const context = stepContext(event);
  const failure = asRecord(event.payload.output).failure;
  const activityPhase = pendingPhase(event);
  if (!isRecord(event.payload.context) && !isRecord(failure) && !activityPhase) return null;
  const errorText = backendErrorLabel(asRecord(failure).backendError);
  return (
    <div className="mt-2.5 flex flex-wrap gap-1.75">
      {activityPhase && (
        <span className={HIGHLIGHT}>
          Activity phase <b>{activityPhase}</b>
        </span>
      )}
      {context.durability !== undefined && (
        <span className={HIGHLIGHT}>
          Durability <b>{durabilityLabel(context.durability)}</b>
        </span>
      )}
      {context.finalAttempt !== undefined && (
        <span className={HIGHLIGHT}>
          Final attempt <b>{String(context.finalAttempt)}</b>
        </span>
      )}
      {typeof errorText === "string" && errorText && (
        <span className="rounded-md bg-fail-subtle px-1.75 py-1.25 text-[10px] text-fail">{errorText}</span>
      )}
    </div>
  );
}
