// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { usesCurrentDefinition } from "@/lib/flow/definition-binding";
import { formatTimeOfDay } from "@/lib/shared/format";
import type { FlowRuntimeCatalog, FlowSearchRow } from "@/lib/api/flow/flow-types";
import { processName } from "@/lib/shared/format";
import { ListColumn } from "@/components/ui/pane/ListColumn";
import { useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { LIST_PANE } from "@/components/flow/frame/pane-bounds";
import { LIVENESS_COPY, RUN_COPY } from "@/components/flow/run/detail/run-copy";
import { useFlowTypeRoute } from "./use-flow-type-route";
import { useStrandedRuns } from "@/components/flow/run/detail/read-liveness";
import { groupRuns, type RunGroupKey } from "./run-groups";
import { GROUP_EMPTY, RunHealthStrip } from "./RunHealthStrip";
import { RunFilter } from "./RunSearch";
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
    health: surface === "runs",
  });
  /** The group the health strip narrows the list to, or null for every run. */
  const [picked, setPicked] = useState<RunGroupKey | null>(null);
  const pick = useCallback((key: RunGroupKey) => setPicked((prior) => (prior === key ? null : key)), []);
  const { strandedFlowIDs, rememberStranded } = useStrandedRuns();
  // A run started elsewhere, such as Try it, is selected before the list holds it: read the list once
  // more for it; a run the search still misses waits for the next quiet re-read.
  const { loading, runSearch } = search;
  // A run the strip's group shows may be older than the list's own page.
  const known = [...search.flows, ...(picked ? (search.health?.groups[picked]?.flows ?? []) : [])];
  const unlisted = Boolean(route.flowId) && !loading && !known.some((flow) => flow.flowId === route.flowId);
  const reread = useRef("");
  useEffect(() => {
    if (!unlisted || reread.current === route.flowId) return;
    reread.current = route.flowId;
    runSearch();
  }, [route.flowId, unlisted, runSearch]);
  const shellRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const pane = useCollapsibleColumn(LIST_PANE);
  const selectedFlow = known.find((flow) => flow.flowId === route.flowId);
  const column = {
    surface,
    route,
    runQuery,
    search,
    picked,
    pick,
    strandedFlowIDs,
    rememberStranded,
    pane,
    selectedFlow,
    /** The selected run's name as its row says it, once the list holds the run. */
    selectedTitle: selectedFlow ? runRow(selectedFlow, runSlots(selectedFlow, route.catalog)).title : undefined,
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

/** Where the run's Flow puts its Attributes; a run of another definition revision fills no slot. */
function runSlots(flow: FlowSearchRow, catalog: FlowRuntimeCatalog): UISlots {
  const entry = catalog.flows.find((candidate) => candidate.flowType === flow.flowType);
  return entry && usesCurrentDefinition(flow, catalog.definitionRevision) ? fieldsBySlot(entry.definition) : new Map();
}

/** What the environment's triggers received, beside its runs: a problem line above them and the log below. */
export interface RunListEvents {
  /** One line when recent events did not arrive, with what fixes it; nothing otherwise. */
  notice: ReactNode;
  /** The events themselves, folded at the foot of the list. */
  log: ReactNode;
}

/**
 * One run list keeps Runs and the Inbox in the same visual language: the runs of the picked Process or
 * of all of them, grouped by what they need, with the search folded behind Filter. Runs leads with a
 * health strip whose groups also narrow the list. Folded, it is a rail. What either view differs on
 * arrives as a prop: `events` puts what the environment's triggers received around the runs, and
 * `empty` replaces the empty text while nothing narrows the list.
 */
export function RunListColumn({
  column,
  shellRef,
  bodyRef,
  heading,
  emptyText,
  empty,
  headerNote,
  headerAction,
  events,
  hiddenFlowId,
  onRefocus,
}: {
  column: RunListColumnState;
  shellRef: RefObject<HTMLElement | null>;
  bodyRef: RefObject<HTMLElement | null>;
  heading: string;
  emptyText: string;
  /** Shown instead of `emptyText` when there are no runs and no filter narrows the list. */
  empty?: ReactNode;
  headerNote?: string;
  headerAction?: ReactNode;
  events?: RunListEvents;
  /** A run the view knows has left the list, kept out until the next read says so too. */
  hiddenFlowId?: string;
  /** Called when the open run is picked again, so its canvas can return to where the run is. */
  onRefocus?: () => void;
}) {
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
      {column.surface === "runs" && !nothingYet(column) && (
        <RunHealthStrip health={column.search.health} picked={column.picked} onPick={column.pick} />
      )}
      {events?.notice}
      <RunList column={column} emptyText={emptyText} empty={empty} hiddenFlowId={hiddenFlowId} onRefocus={onRefocus} />
      {events?.log}
    </ListColumn>
  );
}

/** No run at all, as far as the list knows: four zeros would only stand in the way of the first one. */
function nothingYet({ search, runQuery, picked }: RunListColumnState): boolean {
  return search.flows.length === 0 && runQuery.activeFilters === 0 && picked === null;
}

/** The search scope and the runs, grouped: those that need a person first, then in progress, failed and done. */
function RunList({
  column,
  emptyText,
  empty,
  hiddenFlowId,
  onRefocus,
}: {
  column: RunListColumnState;
  emptyText: string;
  empty?: ReactNode;
  hiddenFlowId?: string;
  /** Called when the open run is picked again, so its canvas can return to where the run is. */
  onRefocus?: () => void;
}) {
  const { route, runQuery, search, strandedFlowIDs, picked } = column;
  // A picked group lists its own page, which can hold runs older than the list's; unread, the list's own runs of it.
  const waiting = column.surface === "inbox" ? "all" : (search.health?.waiting ?? null);
  const shown = picked
    ? (search.health?.groups[picked]?.flows ??
      groupRuns(search.flows, waiting).find((group) => group.key === picked)?.flows ??
      [])
    : search.flows;
  const flows = hiddenFlowId ? shown.filter((flow) => flow.flowId !== hiddenFlowId) : shown;
  const groups = groupRuns(flows, waiting);
  const settled = search.liveness !== "loading" && search.liveness !== "unreachable" && search.liveness !== "stale";
  const showEmpty =
    settled && flows.length === 0 && empty !== undefined && runQuery.activeFilters === 0 && picked === null;
  const stateText = !settled
    ? LIVENESS_COPY[search.liveness as "loading" | "unreachable" | "stale"]
    : flows.length === 0 && !showEmpty
      ? picked
        ? GROUP_EMPTY[picked]
        : emptyText
      : "";
  return (
    <div className="flex min-h-0 flex-auto flex-col">
      <RunFilter
        activeFilters={runQuery.activeFilters}
        busy={search.loading}
        definition={route.allProcesses ? NO_INDEXED_ATTRIBUTES : (route.entry?.definition ?? NO_INDEXED_ATTRIBUTES)}
        query={runQuery.query}
        onChange={runQuery.setQuery}
        onClear={runQuery.clear}
        onSubmit={runQuery.submit}
      />
      {stateText !== "" && (
        <p
          className="mt-1.5 mb-2.5 text-caption/normal text-ink-faint data-[liveness=stale]:text-fail data-[liveness=unreachable]:text-fail"
          data-liveness={search.liveness}
        >
          {stateText}
        </p>
      )}
      {showEmpty && <div className="mt-1.5 mb-2.5">{empty}</div>}
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
                    slots={runSlots(flow, route.catalog)}
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
        className={cx(LIST_ROW_TITLE, row.titleIsFallback && "col-span-full", stranded ? "text-ink-faint" : "text-ink")}
        title={flow.flowId}
      >
        {row.title}
      </span>
      {/* A fallback title already names the process and the start time. */}
      {!row.titleIsFallback && (
        <span className="text-right text-[11px] whitespace-nowrap text-ink-faint">
          {formatTimeOfDay(flow.startTime)}
        </span>
      )}
      <span className={LIST_ROW_STATE}>
        {process && !row.titleIsFallback ? `${process} · ` : ""}
        {row.status}
      </span>
      {stranded && <span className="col-span-full text-[11px] text-fail">{LIVENESS_COPY.strandedRow}</span>}
    </button>
  );
}
