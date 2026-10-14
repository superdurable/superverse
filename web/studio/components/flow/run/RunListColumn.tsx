// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useId, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { usesCurrentDefinition } from "@/lib/flow/definition-binding";
import { formatTimeOfDay } from "@/lib/shared/format";
import type { FlowSearchRow } from "@/lib/api/flow/flow-types";
import { processName } from "@/lib/shared/format";
import { ListColumn } from "@/components/ui/pane/ListColumn";
import { useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { LIST_PANE } from "@/components/flow/frame/pane-bounds";
import { LIVENESS_COPY, RUN_COPY, SEARCH_COPY } from "@/components/flow/run/detail/run-copy";
import { useFlowTypeRoute } from "./use-flow-type-route";
import { useStrandedRuns } from "@/components/flow/run/detail/read-liveness";
import { groupRuns } from "./run-groups";
import { RunSearch } from "./RunSearch";
import { useRunQuery, type RunQuery } from "./run-search-query";
import { fieldsBySlot, runRow, type UISlots } from "@/components/flow/run/detail/ui-slots";
import { useFlowSearch } from "./use-flow-search";
import { cx } from "@/components/ui/cx";
import {
  LABEL,
  LIST_ITEM,
  LIST_ROW,
  LIST_ROW_STATE,
  LIST_ROW_TITLE,
  LIST_SCROLL,
} from "@/components/flow/frame/workspace-styles";

/**
 * What Runs and the Inbox share around their run list: the route, the search scope and its
 * results, the runs learned to be stranded, and the list column's width. The shell and body refs
 * come back beside the column state rather than inside it, so reading the column during render
 * never reads a ref.
 */
export function useRunListColumn(
  surface: "runs" | "inbox",
  options: { initialQuery?: RunQuery; workQueueOnly?: boolean } = {},
) {
  const route = useFlowTypeRoute(surface);
  const runQuery = useRunQuery(
    route.allProcesses ? NO_INDEXED_ATTRIBUTES : route.entry?.definition,
    options.initialQuery,
  );
  const search = useFlowSearch({
    entries: route.allProcesses ? route.catalog.flows : route.entry ? [route.entry] : [],
    definitionRevision: route.catalog.definitionRevision,
    filters: runQuery.appliedFilters,
    workQueueOnly: options.workQueueOnly ?? false,
  });
  const { strandedFlowIDs, rememberStranded } = useStrandedRuns();
  const shellRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const pane = useCollapsibleColumn(LIST_PANE);
  const selectedFlow = search.flows.find((flow) => flow.flowId === route.flowId);
  const column = {
    surface,
    route,
    runQuery,
    search,
    strandedFlowIDs,
    rememberStranded,
    pane,
    selectedFlow,
    /** The Flow type of the selected run, which the canvas and the drawer draw: under all processes, the run's own. */
    shownEntry:
      route.entry ??
      (selectedFlow ? route.catalog.flows.find((entry) => entry.flowType === selectedFlow.flowType) : undefined),
  };
  return { column, shellRef, bodyRef };
}

export type RunListColumnState = ReturnType<typeof useRunListColumn>["column"];

/** All processes search no Indexed Attribute: each Flow type declares its own. */
const NO_INDEXED_ATTRIBUTES = { indexedAttributes: [] };

/**
 * One run list keeps Runs and the Inbox in the same visual language: the search scope, and the runs,
 * open ones first, of the picked Process or of all of them. Folded, it is a rail of one status icon
 * per run. What either view differs on arrives as a prop. With `events`, a Runs | Events switch heads
 * the list, and Events shows what the environment's triggers received instead of the runs; the
 * events mount only while shown.
 */
export function RunListColumn({
  column,
  shellRef,
  bodyRef,
  heading,
  emptyText,
  headerNote,
  headerAction,
  events,
  onRefocus,
}: {
  column: RunListColumnState;
  shellRef: RefObject<HTMLElement | null>;
  bodyRef: RefObject<HTMLElement | null>;
  heading: string;
  emptyText: string;
  headerNote?: string;
  headerAction?: ReactNode;
  /** The events the environment's triggers received, beside its runs. */
  events?: ReactNode;
  /** Called when the open run is picked again, so its canvas can return to where the run is. */
  onRefocus?: () => void;
}) {
  const [view, setView] = useState<ListView>("runs");
  const ids = useId();
  return (
    <ListColumn
      heading={heading}
      headingTitle={RUN_COPY.selectPrompt}
      note={headerNote}
      action={headerAction}
      expandTitle={RUN_COPY.expandList}
      resizeLabel={RUN_COPY.resizeList}
      foldLabel={RUN_COPY.foldList}
      pane={column.pane}
      shellRef={shellRef}
      bodyRef={bodyRef}
      railGlyph={column.surface === "inbox" ? "inbox" : "runs"}
    >
      {events !== undefined && <ListViewSwitch view={view} onChange={setView} ids={ids} />}
      {events !== undefined && view === "events" ? (
        <div
          role="tabpanel"
          id={`${ids}-events`}
          aria-labelledby={`${ids}-events-tab`}
          className={LIST_SCROLL}
          data-zone="events"
        >
          {events}
        </div>
      ) : (
        <RunList
          column={column}
          emptyText={emptyText}
          panel={events !== undefined ? ids : null}
          onRefocus={onRefocus}
        />
      )}
    </ListColumn>
  );
}

/** What the list column shows: the runs, or the events the triggers received. */
type ListView = "runs" | "events";
const LIST_VIEWS: readonly { view: ListView; label: string }[] = [
  { view: "runs", label: "Runs" },
  { view: "events", label: "Events" },
];

/** Runs | Events, two tabs that the arrow keys move between, as the bar's switch looks. */
function ListViewSwitch({ view, onChange, ids }: { view: ListView; onChange: (view: ListView) => void; ids: string }) {
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const next = view === "runs" ? "events" : "runs";
    onChange(next);
    document.getElementById(`${ids}-${next}-tab`)?.focus();
  };
  return (
    <div
      role="tablist"
      aria-label="Runs or events"
      className="mb-2.5 flex shrink-0 gap-0.5 rounded-[9px] bg-well p-0.75"
    >
      {LIST_VIEWS.map((entry) => (
        <button
          key={entry.view}
          type="button"
          role="tab"
          id={`${ids}-${entry.view}-tab`}
          aria-selected={view === entry.view}
          aria-controls={`${ids}-${entry.view}`}
          tabIndex={view === entry.view ? 0 : -1}
          onClick={() => onChange(entry.view)}
          onKeyDown={onKeyDown}
          className="h-7 flex-1 rounded-[7px] border border-transparent text-[13px] text-ink-faint hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none aria-selected:border-line aria-selected:bg-panel aria-selected:font-semibold aria-selected:text-ink"
        >
          {entry.label}
        </button>
      ))}
    </div>
  );
}

/** The search scope and the runs, grouped, open ones first. */
function RunList({
  column,
  emptyText,
  panel,
  onRefocus,
}: {
  column: RunListColumnState;
  emptyText: string;
  /** The id stem of the Runs tab that labels this list, when the list is one of two views. */
  panel: string | null;
  /** Called when the open run is picked again, so its canvas can return to where the run is. */
  onRefocus?: () => void;
}) {
  const { route, runQuery, search, strandedFlowIDs } = column;
  const slotsOf = (flowType: string) => {
    const entry = route.catalog.flows.find((candidate) => candidate.flowType === flowType);
    return entry ? fieldsBySlot(entry.definition) : new Map();
  };
  const groups = groupRuns(search.flows);
  const stateText =
    search.liveness === "loading" || search.liveness === "unreachable" || search.liveness === "stale"
      ? LIVENESS_COPY[search.liveness]
      : search.flows.length === 0
        ? emptyText
        : "";
  return (
    <div
      className="flex min-h-0 flex-auto flex-col"
      {...(panel ? { role: "tabpanel", id: `${panel}-runs`, "aria-labelledby": `${panel}-runs-tab` } : {})}
    >
      <section className="mb-2.5 rounded-md border border-line-faint bg-panel px-2.25 pt-2 pb-0.5" data-zone="find">
        <h3 className={cx(LABEL, "mb-1.5 font-semibold")}>{SEARCH_COPY.findHeading}</h3>
        <RunSearch
          busy={search.loading}
          definition={route.allProcesses ? NO_INDEXED_ATTRIBUTES : (route.entry?.definition ?? NO_INDEXED_ATTRIBUTES)}
          query={runQuery.query}
          onChange={runQuery.setQuery}
          onClear={runQuery.clear}
          onSubmit={runQuery.submit}
        />
      </section>
      {stateText !== "" && (
        <p
          className="mt-1.5 mb-2.5 text-xs/normal text-ink-faint data-[liveness=stale]:text-fail data-[liveness=unreachable]:text-fail"
          data-liveness={search.liveness}
        >
          {stateText}
        </p>
      )}
      <div className={LIST_SCROLL} data-zone="list">
        {groups.map((group) => (
          <section
            className="not-first:mt-3 not-first:border-t not-first:border-line-soft not-first:pt-2.5"
            data-group={group.key}
            key={group.key}
          >
            {groups.length > 1 && <h3 className={cx(LABEL, "mb-0.5 font-medium")}>{group.label}</h3>}
            <ul>
              {group.flows.map((flow) => (
                <li
                  className={LIST_ITEM}
                  data-selected={flow.flowId === route.flowId ? "true" : undefined}
                  data-stranded={strandedFlowIDs.has(flow.flowId) ? "true" : undefined}
                  key={flow.flowId}
                >
                  <RunRowButton
                    flow={flow}
                    slots={
                      usesCurrentDefinition(flow, route.catalog.definitionRevision) ? slotsOf(flow.flowType) : new Map()
                    }
                    process={route.allProcesses ? processName(flow.flowType) : null}
                    stranded={strandedFlowIDs.has(flow.flowId)}
                    onSelect={(flowId) => {
                      if (flowId === route.flowId) onRefocus?.();
                      route.open(flowId);
                    }}
                  />
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  );
}

/**
 * Three things: what names the run, when it started, where it has got to.
 *
 * What the Flow declares for `recommendation` and `reason` is deliberately left to the drawer — a
 * list of reasons stops being a list.
 */
function RunRowButton({
  flow,
  slots,
  process,
  stranded,
  onSelect,
}: {
  flow: FlowSearchRow;
  slots: UISlots;
  /** The run's Process, named on each row while all processes are listed. */
  process: string | null;
  stranded: boolean;
  onSelect: (flowID: string) => void;
}) {
  const row = runRow(flow, slots);
  return (
    <button className={LIST_ROW} onClick={() => onSelect(flow.flowId)} type="button" data-pick={`run ${flow.flowId}`}>
      <span
        className={cx(LIST_ROW_TITLE, row.titleIsFlowID && "font-mono", stranded ? "text-ink-faint" : "text-ink")}
        title={flow.flowId}
      >
        {row.title}
      </span>
      <span className="text-right text-[11px] whitespace-nowrap text-ink-faint">{formatTimeOfDay(flow.startTime)}</span>
      <span className={LIST_ROW_STATE}>
        {process ? `${process} · ` : ""}
        {row.status}
      </span>
      {stranded && <span className="col-span-full text-[11px] text-fail">{LIVENESS_COPY.strandedRow}</span>}
    </button>
  );
}
