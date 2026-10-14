// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

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
 * Flow type's waiting items, merged by time.
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
        />
        <section className={CASE_COLUMN} aria-label={flowId || INBOX_COPY.appName}>
          {flowId && entry && host.environment === "live" && (
            <p
              className="mx-4 mt-3 flex items-start gap-2 rounded-lg bg-busy-subtle px-2.5 py-2 text-[12.5px] text-busy"
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
              flowStatusCode={column.selectedFlow?.flowStatusCode}
              flowType={entry.flowType}
              order="evidence-first"
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
            <p className={EMPTY}>{INBOX_COPY.selectPrompt}</p>
          )}
        </section>
      </div>
    </FlowFrame>
  );
}
