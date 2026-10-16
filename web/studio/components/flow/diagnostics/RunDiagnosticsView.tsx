// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { FlowLink } from "@/components/flow/frame/FlowLink";
import { usesCurrentDefinition } from "@/lib/flow/definition-binding";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { formatDate, formatDuration } from "@/lib/shared/format";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { isTerminalFlowStatusCode } from "@/lib/api/flow/flow-status";
import { readStoredPreference, STORAGE_KEYS, writeStoredPreference } from "@/lib/browser/storage";
import { PaneResizeHandle, paneWidthStyle, useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { SELECTED_EVENT_PANE } from "@/components/flow/frame/pane-bounds";
import { FlowOverview } from "./FlowOverview";
import { SelectedEventPanel } from "@/components/flow/diagnostics/timeline/SelectedEventPanel";
import { TimeTravelDialog } from "./TimeTravelDialog";
import { StopFlowDialog } from "./StopFlowDialog";
import { ExecutionGraph } from "@/components/flow/diagnostics/graph/ExecutionGraph";
import { SelectedEventConnector } from "@/components/flow/diagnostics/timeline/SelectedEventConnector";
import { StreamPanel } from "./StreamPanel";
import { Timeline } from "@/components/flow/diagnostics/timeline/Timeline";
import { useRunRecord } from "./use-run-record";
import { Banner } from "@/components/ui/Banner";
import { Button, buttonStyle } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { cx } from "@/components/ui/cx";

const RUN_TABS = [
  ["overview", "Overview"],
  ["steps", "Execution graph"],
  ["timeline", "Timeline"],
  ["stream", "Streams"],
] as const;

type RunTab = (typeof RUN_TABS)[number][0];

function storedRunTab(): RunTab {
  const stored = readStoredPreference(STORAGE_KEYS.diagnosticsTab);
  return RUN_TABS.find(([id]) => id === stored)?.[0] ?? "steps";
}

/** The run status in the hue of who must act; an unlisted status stays neutral. */
const STATUS_INK: Partial<Record<string, string>> = {
  running: cx("text-accent"),
  completed: cx("text-pass"),
  failed: cx("text-fail"),
  terminated: cx("text-fail"),
  canceled: cx("text-busy"),
  "timed-out": cx("text-busy"),
};

function StatusBadge({ status }: { status: string }) {
  return (
    <span
      className={cx(
        "inline-flex items-center self-start rounded-full bg-raised px-2 py-1.25 text-[10px] font-extrabold tracking-[0.03em] uppercase",
        STATUS_INK[status.toLowerCase().replaceAll(" ", "-")] ?? "text-ink-faint",
      )}
    >
      {status || "Unknown"}
    </span>
  );
}

/** One cell of the summary strip; the strip's 1px gap draws the dividers between cells. */
function SummaryCell({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-18 min-w-0 flex-col justify-center gap-1.75 bg-panel p-3.25">
      <Eyebrow>{label}</Eyebrow>
      {children}
    </div>
  );
}

const SUMMARY_VALUE = cx("truncate text-xs text-ink");

export function RunDiagnosticsView({
  flowId,
  runId,
  breadcrumb,
}: {
  flowId: string;
  runId: string;
  /** Supplied by the host shell, which owns where "up" goes. */
  breadcrumb: ReactNode;
}) {
  const { catalog } = useFlowCatalog();
  // Stopping a run and rerunning it from an event are for those who build the Project, as in the run drawer.
  const { canIntervene } = useFlowHost();
  const [live, setLive] = useState(true);
  const {
    summary,
    history,
    state,
    loading,
    error,
    warnings,
    refresh,
    hydrateEvent,
    deletingChannelMessage,
    deleteChannelMessage,
  } = useRunRecord(flowId, runId, live);
  const [tab, setTab] = useState<RunTab>(storedRunTab);
  const [selectedEvent, setSelectedEvent] = useState<FlowHistoryEvent | null>(null);
  /** The open Time Travel dialog and the event it starts from, or null while it is closed. */
  const [timeTravel, setTimeTravel] = useState<{ event: FlowHistoryEvent | null } | null>(null);
  const [stopOpen, setStopOpen] = useState(false);
  const sidebar = useCollapsibleColumn(SELECTED_EVENT_PANE);
  const runContent = useRef<HTMLDivElement>(null);

  // Until the reader picks an event the newest one is shown, always as its hydrated copy.
  const selected = useMemo(() => {
    const shown = selectedEvent ?? history.at(-1);
    return shown ? (history.find((event) => event.eventId === shown.eventId) ?? shown) : null;
  }, [history, selectedEvent]);

  useEffect(() => {
    if (selected) void hydrateEvent(selected);
  }, [hydrateEvent, selected]);

  useEffect(() => {
    if (tab !== "overview") return;
    const startEvent = history.find((event) => event.type === "FlowStartedOrContinued");
    if (startEvent) void hydrateEvent(startEvent);
  }, [hydrateEvent, history, tab]);

  const continuedToRunId = useMemo(() => {
    const closed = history.findLast((event) => event.type === "FlowClosed");
    const nextRunId = closed?.payload.continuedToRunId;
    return typeof nextRunId === "string" ? nextRunId : "";
  }, [history]);

  if (loading && !summary)
    return <div className="grid min-h-60 place-items-center p-10 text-center text-ink-faint">Loading flow run…</div>;

  return (
    <div className="mx-auto w-full max-w-[1500px] px-10 pt-3 pb-7 max-md:px-4">
      <section>
        {breadcrumb}
        <div className="flex items-center justify-between gap-5 max-md:flex-col max-md:items-start">
          <div className="min-w-0">
            <Eyebrow className="mb-1.5">{summary?.flowType || "Flow execution"}</Eyebrow>
            <h1 className="m-0 text-xl/tight font-semibold tracking-tight text-ink">{flowId}</h1>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 max-md:justify-start">
            {continuedToRunId && (
              <FlowLink
                className={buttonStyle("accent")}
                to={{ surface: "diagnostics", flowId, runId: continuedToRunId }}
              >
                Next run
              </FlowLink>
            )}
            <label className="inline-flex items-center gap-1.75 px-2.5 py-2 text-xs font-semibold text-ink-faint">
              <input
                className="accent-cta"
                type="checkbox"
                checked={live}
                onChange={(event) => setLive(event.target.checked)}
              />
              Live
            </label>
            <Button onClick={() => void navigator.clipboard.writeText(window.location.href)}>Copy link</Button>
            <Button onClick={() => void refresh()}>Refresh</Button>
            {canIntervene && (
              <Button
                variant="danger"
                disabled={!summary || isTerminalFlowStatusCode(summary.flowStatusCode)}
                onClick={() => setStopOpen(true)}
              >
                Stop
              </Button>
            )}
            {canIntervene && (
              <Button variant="danger" onClick={() => setTimeTravel({ event: selectedEvent ? selected : null })}>
                Time Travel
              </Button>
            )}
          </div>
        </div>
        {summary && (
          <div className="mt-5.5 grid grid-cols-6 gap-px overflow-hidden rounded-lg border border-line-faint bg-line-faint max-lg:grid-cols-3 max-md:grid-cols-2">
            <SummaryCell label="Status">
              <StatusBadge status={summary.flowStatus} />
            </SummaryCell>
            <SummaryCell label="Started">
              <b className={SUMMARY_VALUE}>{formatDate(summary.startTime)}</b>
            </SummaryCell>
            <SummaryCell label="Closed">
              <b className={SUMMARY_VALUE}>{formatDate(summary.closeTime)}</b>
            </SummaryCell>
            <SummaryCell label="Duration">
              <b className={SUMMARY_VALUE}>{formatDuration(summary.startTime, summary.closeTime)}</b>
            </SummaryCell>
            <SummaryCell label="Run chain">
              <b className={SUMMARY_VALUE}>{new Set([summary.firstRunId, summary.runId].filter(Boolean)).size}</b>
            </SummaryCell>
            <SummaryCell label="Semantic events">
              <b className={SUMMARY_VALUE}>{history.length}</b>
            </SummaryCell>
          </div>
        )}
      </section>

      {summary && !usesCurrentDefinition(summary, catalog?.definitionRevision) && (
        <Banner tone="busy" role="status" className="mt-3.5">
          The original definition is unavailable. This diagnostic graph uses recorded execution events only; current
          definitions are not applied.
        </Banner>
      )}
      {error && <Banner className="mt-3.5">{error}</Banner>}
      {warnings.map((warning) => (
        <Banner tone="busy" className="mt-3.5" key={warning}>
          {warning}
        </Banner>
      ))}

      <div className="mt-5.5 flex gap-0.75 border-b border-line-faint max-md:overflow-x-auto" role="tablist">
        {RUN_TABS.map(([id, label]) => (
          <button
            key={id}
            className={cx(
              "relative shrink-0 px-3.75 py-2.75 text-[11.5px] font-bold hover:text-ink",
              tab === id
                ? "text-ink after:absolute after:inset-x-2.5 after:-bottom-px after:h-0.5 after:bg-accent"
                : "text-ink-faint",
            )}
            onClick={() => {
              setTab(id);
              writeStoredPreference(STORAGE_KEYS.diagnosticsTab, id);
            }}
          >
            {label}
          </button>
        ))}
      </div>

      <div
        className={cx(
          "relative grid gap-4.5 pt-4.5",
          tab === "overview"
            ? "grid-cols-1"
            : "grid-cols-[minmax(0,1fr)_var(--run-sidebar-width,340px)] max-lg:grid-cols-1",
        )}
        ref={runContent}
        style={paneWidthStyle(sidebar)}
      >
        <SelectedEventConnector contentRef={runContent} selectedEvent={selectedEvent} enabled={tab === "timeline"} />
        <section className="min-w-0">
          {tab === "overview" && summary && (
            <FlowOverview
              summary={summary}
              events={history}
              state={state}
              selectedEvent={selected}
              deletingChannelMessage={deletingChannelMessage}
              onDeleteChannelMessage={(channelName, messageId) =>
                deleteChannelMessage(summary.flowStatusCode, channelName, messageId)
              }
            />
          )}
          {tab === "steps" && (
            <ExecutionGraph
              flowId={flowId}
              events={history}
              state={state}
              selectedEvent={selectedEvent}
              onSelectEvent={setSelectedEvent}
            />
          )}
          {tab === "timeline" && (
            <Timeline flowId={flowId} events={history} selectedEvent={selectedEvent} onSelectEvent={setSelectedEvent} />
          )}
          {tab === "stream" && summary && <StreamPanel flowId={flowId} flowType={summary.flowType} />}
        </section>
        {(tab === "steps" || tab === "timeline") && (
          // The resize handle places itself in the grid gap before the sidebar (PaneResizeHandle).
          <aside className="relative min-w-0">
            <PaneResizeHandle
              column={sidebar}
              targetRef={runContent}
              measureRef={runContent}
              ariaLabel="Resize selected event panel"
              onToggle={sidebar.toggle}
            />
            <SelectedEventPanel selectedEvent={selected} history={history} parentFlowId={flowId} />
          </aside>
        )}
      </div>

      {summary && timeTravel && canIntervene && (
        <TimeTravelDialog
          summary={summary}
          events={history}
          initialEvent={timeTravel.event}
          onClose={() => setTimeTravel(null)}
        />
      )}
      {summary && stopOpen && canIntervene && (
        <StopFlowDialog summary={summary} onClose={() => setStopOpen(false)} onStopped={() => void refresh()} />
      )}
    </div>
  );
}
