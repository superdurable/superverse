// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowHistoryEvent, FlowState, FlowSummary } from "@/lib/api/flow/flow-types";
import { cx } from "@/components/ui/cx";
import { SemanticEventDetails } from "@/components/flow/diagnostics/event/EventDetails";
import { SelectedEventSummary } from "@/components/flow/diagnostics/timeline/SelectedEventPanel";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { LiveFlowState } from "./LiveFlowState";

/** An Overview card, and the heading every card opens with. */
const OVERVIEW_CARD = cx("min-w-0 rounded-lg border border-line-faint bg-panel p-4.5");
const CARD_HEADING = cx("m-0 text-sm font-semibold tracking-normal text-ink");

const COLUMN = cx("flex min-w-0 flex-col gap-4");

export function FlowOverview({
  summary,
  events,
  state,
  selectedEvent,
  deletingChannelMessage,
  onDeleteChannelMessage,
}: {
  summary: FlowSummary;
  events: FlowHistoryEvent[];
  state: FlowState | null;
  selectedEvent: FlowHistoryEvent | null;
  deletingChannelMessage: string;
  onDeleteChannelMessage: (channelName: string, messageId: string) => Promise<void>;
}) {
  const started = events.find((event) => event.type === "FlowStartedOrContinued");
  const closed = events.findLast((event) => event.type === "FlowClosed");
  const startKind = started?.payload.initialStart ? "Initial start" : "Continued run";

  return (
    <div className="grid grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)] items-start gap-4 max-md:grid-cols-1">
      <div className={COLUMN}>
        <LiveFlowState
          summary={summary}
          state={state}
          closed={closed}
          deletingChannelMessage={deletingChannelMessage}
          onDeleteChannelMessage={onDeleteChannelMessage}
        />

        <section className={OVERVIEW_CARD}>
          <Eyebrow className="mb-1.5">Run input</Eyebrow>
          <h2 className={CARD_HEADING}>{startKind}</h2>
          {started ? (
            <div className="mt-2.5 flex flex-col gap-2.5">
              <SemanticEventDetails event={started} parentFlowId={summary.flowId} showStartHeading={false} />
            </div>
          ) : (
            <p className="mt-2 text-xs text-ink-faint">The start event is not in the loaded history page.</p>
          )}
        </section>
      </div>

      <div className={COLUMN}>
        <section className={OVERVIEW_CARD}>
          <SelectedEventSummary
            event={selectedEvent}
            history={events}
            parentFlowId={summary.flowId}
            variant="overview"
          />
        </section>

        <section className={OVERVIEW_CARD}>
          <Eyebrow className="mb-1.5">Identity</Eyebrow>
          <h2 className={CARD_HEADING}>Execution</h2>
          <dl className="mt-3.5">
            {(
              [
                ["Flow ID", summary.flowId, true],
                ["Run ID", summary.runId, true],
                ["First run ID", summary.firstRunId, true],
                ["Request ID", summary.requestId, true],
                ["Flow type", summary.flowType, false],
              ] as const
            ).map(([label, value, mono]) => (
              <div
                className="grid grid-cols-[120px_minmax(0,1fr)] gap-3.5 border-b border-line-faint py-2.25 last:border-b-0"
                key={label}
              >
                <dt className="text-[11px] text-ink-faint">{label}</dt>
                <dd className={cx("m-0 truncate text-xs text-ink-dim", mono && "font-mono")}>{value || "—"}</dd>
              </div>
            ))}
          </dl>
        </section>
      </div>
    </div>
  );
}
