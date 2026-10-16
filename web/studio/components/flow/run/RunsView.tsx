// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useState, type ReactNode } from "react";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { FlowCanvas, type SelectedStepStatus } from "@/components/flow/canvas/FlowCanvas";
import type { ActionableStep } from "@/components/flow/canvas/model/actionable-steps";
import type { StepContextView } from "@/components/flow/canvas/model/step-context";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { PaneResizeHandle, paneWidthStyle, useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { PaneRail } from "@/components/ui/pane/ListColumn";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { LIST_BESIDE_CASE, WORKSPACE_BODY } from "@/components/flow/frame/workspace-styles";
import { DRAWER_PANE } from "@/components/flow/frame/pane-bounds";
import { StartFlowDialog } from "@/components/flow/run/start/StartFlowDialog";
import { FlowFrame } from "@/components/flow/frame/FlowFrame";
import { RUN_COPY } from "@/components/flow/run/detail/run-copy";
import { RunDetailDrawer } from "@/components/flow/run/detail/RunDetailDrawer";
import { fallbackRunTitle } from "@/components/flow/run/detail/ui-slots";
import { RunListColumn, useRunListColumn, type RunListEvents } from "./RunListColumn";

/**
 * Runs: pick a run and see the whole Flow with the Step it is at ringed "now"; click a Step to open
 * the drawer with its state, the run's Actions and what a builder may do there. The list folds to a
 * rail of status icons. Under all processes the list holds every Flow type's runs, and the canvas
 * draws the selected run's own Flow. `events`, what the environment's triggers received, sits around
 * the list; `empty` replaces the list's empty text while nothing narrows it.
 */
export function RunsView({ events, empty }: { events?: RunListEvents; empty?: ReactNode }) {
  const { column, shellRef, bodyRef } = useRunListColumn("runs");
  const { route, search } = column;
  const { flowId, catalog } = route;
  const entry = column.shownEntry;
  const host = useFlowHost();
  const { definitionUpdateKey, handleDefinitionError } = useFlowCatalog();
  const [selectedStepStatus, setSelectedStepStatus] = useState<SelectedStepStatus | null>(null);
  const [stepView, setStepView] = useState<StepContextView | null>(null);
  const [actionable, setActionable] = useState<ActionableStep[]>([]);
  const [summary, setSummary] = useState<FlowSummary | null>(null);
  const [tick, setTick] = useState(0);
  /** Bumped when a retry started the open run again, so the canvas reads it at once. */
  const [runReloadKey, setRunReloadKey] = useState(0);
  /** Bumped to tell the canvas to drop its Step selection. */
  const [deselectKey, setDeselectKey] = useState(0);
  /** Bumped when the open run is picked again: the canvas returns to where the run is. */
  const [refocusKey, setRefocusKey] = useState(0);
  const [startOpen, setStartOpen] = useState(false);
  const [startNotice, setStartNotice] = useState("");
  const drawerPane = useCollapsibleColumn(DRAWER_PANE);
  const onLive = host.environment === "live";

  // The canvas owns the run poll; the drawer refreshes on the same beat.
  const onTick = useCallback(() => setTick((previous) => previous + 1), []);

  // A new Flow type, definition or start target closes a Start dialog opened for the old one.
  const startEntry = route.entry ?? (route.allProcesses ? undefined : catalog.flows[0]);
  const startTarget = JSON.stringify([
    startEntry?.flowType,
    catalog.definitionRevision,
    catalog.startFlow?.targetRevision,
    definitionUpdateKey,
  ]);
  const [observedStartTarget, setObservedStartTarget] = useState(startTarget);
  if (observedStartTarget !== startTarget) {
    setObservedStartTarget(startTarget);
    setStartOpen(false);
    setStartNotice("");
  }

  // The drawer opens on a Step click, and only then.
  const showDrawer = stepView !== null;
  // The selected run's name, as its row says it; before the list has it, its process and start.
  const runTitle =
    column.selectedTitle ??
    (summary?.flowId === flowId && summary ? fallbackRunTitle(summary.flowType, summary.startTime) : "");
  const canStart = Boolean(catalog.startFlow?.enabled && startEntry?.definition.start);
  const startLabel = onLive ? RUN_COPY.liveRun : RUN_COPY.testRun;
  return (
    <FlowFrame className="min-h-0" ref={shellRef} style={paneWidthStyle(column.pane, drawerPane)}>
      <div
        className={cx(
          WORKSPACE_BODY,
          showDrawer ? "grid-cols-[var(--v2-list-w,250px)_minmax(0,1fr)_var(--v2-drawer-w,290px)]" : LIST_BESIDE_CASE,
        )}
        data-has-drawer={showDrawer ? "true" : undefined}
        ref={bodyRef}
      >
        <RunListColumn
          column={column}
          shellRef={shellRef}
          bodyRef={bodyRef}
          heading={
            onLive ? RUN_COPY.liveRunsHeading : host.environment ? RUN_COPY.previewRunsHeading : RUN_COPY.runsHeading
          }
          emptyText={RUN_COPY.noRuns}
          empty={empty}
          events={events}
          onRefocus={() => setRefocusKey((key) => key + 1)}
          headerAction={
            canStart ? (
              <Button className="ml-auto" variant="secondary" size="sm" onClick={() => setStartOpen(true)}>
                <Glyph name="runs" className="size-3.5" />
                {startLabel}
              </Button>
            ) : undefined
          }
        />
        <section
          className="@container/canvas relative flex min-h-0 min-w-0 flex-col bg-canvas"
          aria-label="Flow definition"
          data-pick-scope={flowId ? `${onLive ? "Live run" : "Test run"} ${flowId}` : undefined}
        >
          {entry ? (
            <>
              {flowId && (
                // Beside the canvas controls when the canvas has room for both, else on a line below them.
                <div className="pointer-events-none absolute top-2.5 left-3 z-5 flex max-w-[calc(100%-32rem)] min-w-0 items-center gap-2 text-[12.5px] text-ink-faint @max-[54rem]/canvas:top-14 @max-[54rem]/canvas:max-w-[calc(100%-1.5rem)]">
                  <span
                    className="min-w-0 truncate rounded-lg border border-line-soft bg-panel px-2.25 py-1"
                    title={flowId}
                  >
                    <span className="text-ink">{runTitle || flowId}</span> · {onLive ? "Live" : "Preview"}
                  </span>
                  {!showDrawer && (
                    <span className="shrink-0 rounded-lg border border-line-soft bg-panel px-2.25 py-1">
                      {RUN_COPY.clickStep}
                    </span>
                  )}
                </div>
              )}
              <FlowCanvas
                flowId={flowId}
                flowType={entry.flowType}
                onActionable={setActionable}
                onSelectedStepStatus={setSelectedStepStatus}
                onStepContext={setStepView}
                deselectKey={deselectKey}
                refocusKey={refocusKey}
                markNow
                onSummary={setSummary}
                showStepPanel={false}
                onTick={onTick}
                reloadKey={runReloadKey}
              />
            </>
          ) : (
            <p className="m-auto max-w-sm p-8 text-center text-[13.5px] text-ink-faint">{RUN_COPY.selectPrompt}</p>
          )}
        </section>
        {showDrawer && entry ? (
          <>
            <PaneResizeHandle
              column={drawerPane}
              targetRef={shellRef}
              measureRef={bodyRef}
              ariaLabel={RUN_COPY.resizeDrawer}
              onToggle={drawerPane.toggle}
            />
            {drawerPane.isCollapsed ? (
              <div className="flex flex-col border-l border-line-faint bg-panel pt-2 dark:bg-transparent">
                <PaneRail glyph="eye" label={RUN_COPY.expandDrawer} onExpand={drawerPane.expand} />
              </div>
            ) : (
              <RunDetailDrawer
                actionable={actionable}
                selectedStepStatus={selectedStepStatus}
                definition={entry.definition}
                definitionRevision={catalog.definitionRevision}
                flowId={flowId}
                title={runTitle}
                flowStatusCode={column.selectedFlow?.flowStatusCode}
                flowType={entry.flowType}
                reloadKey={tick}
                stepContext={stepView}
                summary={summary?.flowId === flowId ? summary : null}
                onClose={() => setDeselectKey((previous) => previous + 1)}
                onStopped={search.runSearch}
                onRetried={() => {
                  setRunReloadKey((key) => key + 1);
                  search.runSearch();
                }}
                onStranded={column.rememberStranded}
              />
            )}
          </>
        ) : null}
      </div>
      {startNotice && (
        <div
          className="fixed right-4.5 bottom-4.5 z-30 max-w-[min(520px,calc(100vw-36px))] rounded-lg border border-pass-rule bg-panel px-3.5 py-2.75 text-ink shadow-(--shadow-lift)"
          role="status"
        >
          {startNotice}
        </div>
      )}
      {startOpen && startEntry?.definition.start && catalog.startFlow?.enabled && (
        <StartFlowDialog
          definition={startEntry.definition.start}
          definitionRevision={catalog.definitionRevision}
          flowType={startEntry.flowType}
          target={catalog.startFlow}
          live={onLive}
          onClose={() => setStartOpen(false)}
          onDefinitionChanged={handleDefinitionError}
          onStarted={(startedFlowID, words) => {
            setStartOpen(false);
            setStartNotice(words);
            search.runSearch();
            route.open(startedFlowID);
          }}
        />
      )}
    </FlowFrame>
  );
}
