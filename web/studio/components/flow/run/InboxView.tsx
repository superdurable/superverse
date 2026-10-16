// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { RUNNING_FLOW_STATUS_CODE, SEARCH_STATUS_VALUES } from "@/lib/api/flow/flow-status";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { FlowFrame } from "@/components/flow/frame/FlowFrame";
import { INBOX_COPY } from "@/components/flow/run/detail/run-copy";
import { RunListColumn, useRunListColumn } from "./RunListColumn";
import { EMPTY_RUN_QUERY } from "./run-search-query";
import { paneWidthStyle } from "@/components/ui/pane/PaneResizeHandle";
import { SelectedRunPanel } from "@/components/flow/run/detail/SelectedRunPanel";
import { cx } from "@/components/ui/cx";
import {
  CASE_COLUMN,
  CLOSE,
  EMPTY,
  LINK_OUT,
  LIST_BESIDE_CASE,
  WORKSPACE_BODY,
} from "@/components/flow/frame/workspace-styles";

/**
 * Inbox: what has arrived for the reader, with no canvas. It shares the run list and case panel
 * with Runs; what differs is the default scope (open runs only) and that evidence comes before the
 * Actions. "See the process" opens the same run in Runs. Under all processes the list holds every
 * Flow type's waiting items, merged by time. An Action that leaves its run with nothing to act on
 * clears the item: the list is read again without it, and the next item opens.
 */
export function InboxView() {
  const { column, shellRef, bodyRef } = useRunListColumn("inbox", {
    initialQuery: { ...EMPTY_RUN_QUERY, status: SEARCH_STATUS_VALUES[RUNNING_FLOW_STATUS_CODE] },
    workQueueOnly: true,
  });
  const { route } = column;
  const { flowId, catalog } = route;
  // Under all processes the selected item's own Flow type draws its panel.
  const entry = column.shownEntry;
  const host = useFlowHost();
  /** The confirmation of the last Action, shown above the item it left open. */
  const [done, setDone] = useState<{ text: string; shownFor: string } | null>(null);
  /** The item an Action cleared, kept out of the list until a read that began after the Action. */
  const [cleared, setCleared] = useState<{ flowId: string; reads: number } | null>(null);
  const hiddenFlowId = cleared && cleared.reads === column.search.reads ? cleared.flowId : undefined;

  const onActed = (actedOn: string, outcome: { action: string; waiting: boolean }) => {
    if (outcome.waiting) {
      setDone({ text: INBOX_COPY.done(outcome.action), shownFor: actedOn });
      column.search.runSearch();
      return;
    }
    const next = column.search.flows.find((flow) => flow.flowId !== actedOn && flow.flowId !== hiddenFlowId);
    setDone({ text: INBOX_COPY.done(outcome.action), shownFor: next?.flowId ?? "" });
    setCleared({ flowId: actedOn, reads: column.search.reads });
    route.open(next?.flowId);
    column.search.runSearch();
  };

  return (
    <FlowFrame className="min-h-0" ref={shellRef} style={paneWidthStyle(column.pane)}>
      <div className={cx(WORKSPACE_BODY, LIST_BESIDE_CASE)} ref={bodyRef}>
        <RunListColumn
          column={column}
          shellRef={shellRef}
          bodyRef={bodyRef}
          heading={INBOX_COPY.appName}
          headerNote={INBOX_COPY.liveNote}
          emptyText={INBOX_COPY.clear}
          hiddenFlowId={hiddenFlowId}
        />
        <section className={CASE_COLUMN} aria-label={flowId || INBOX_COPY.appName}>
          {done && done.shownFor === flowId && (
            <p
              className="mx-4 mt-3 flex items-baseline gap-2 rounded-lg bg-pass-subtle px-2.5 py-2 text-caption text-pass"
              role="status"
              data-testid="inbox-done"
            >
              <span aria-hidden="true">✓</span>
              {done.text}
            </p>
          )}
          {flowId && entry && host.environment === "live" && (
            <p
              className="mx-4 mt-3 flex items-start gap-2 rounded-lg bg-busy-subtle px-2.5 py-2 text-caption text-busy"
              data-testid="inbox-live-note"
            >
              <span aria-hidden="true">●</span>
              {INBOX_COPY.liveItem}
            </p>
          )}
          {flowId && entry ? (
            <SelectedRunPanel
              key={`${entry.flowType}:${flowId}`}
              definition={entry.definition}
              definitionRevision={catalog.definitionRevision}
              flowId={flowId}
              title={column.selectedTitle}
              flowStatusCode={column.selectedFlow?.flowStatusCode}
              flowType={entry.flowType}
              order="evidence-first"
              onActed={(outcome) => onActed(flowId, outcome)}
              onStranded={column.rememberStranded}
              footer={
                <>
                  <FlowLink
                    className={LINK_OUT}
                    to={{ surface: "runs", flowType: route.allProcesses ? undefined : entry.flowType, flowId }}
                  >
                    {INBOX_COPY.seeProcess}
                  </FlowLink>
                  <button aria-label={INBOX_COPY.close} className={CLOSE} onClick={() => route.open()} type="button">
                    ✕
                  </button>
                </>
              }
            />
          ) : (
            // An empty list already says nothing is waiting.
            column.search.flows.some((flow) => flow.flowId !== hiddenFlowId) && (
              <p className={EMPTY}>{INBOX_COPY.selectPrompt}</p>
            )
          )}
        </section>
      </div>
    </FlowFrame>
  );
}
