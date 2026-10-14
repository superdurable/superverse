// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { StopFlowDialog } from "@/components/flow/diagnostics/StopFlowDialog";
import { formatDate, formatDuration } from "@/lib/shared/format";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";
import { RUN_COPY } from "./run-copy";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { CASE_STATUS, FACT, FACT_TERM, FACTS, LINK_OUT } from "@/components/flow/frame/workspace-styles";

/** One line per fact: the header stays short so the Actions below it stay in view. */
const FACT_DATUM = cx("min-w-0 truncate text-xs text-ink-dim");

/** The selected run's identity and timing, with Stop while it is open and a way into Diagnostics. */
export function RunHeader({
  flowId,
  summary,
  onStopped,
}: {
  flowId: string;
  summary: FlowSummary | null;
  onStopped: () => void;
}) {
  const [stopOpen, setStopOpen] = useState(false);
  // Stopping a run, like rerunning it from a Step, is for those who build the Project.
  const host = useFlowHost();
  const isOpen = isOpenFlowStatusCode(summary?.flowStatusCode);
  return (
    <div className="sticky top-0 z-1 border-b border-line-faint bg-bg px-4 pt-3 pb-2.5">
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="min-w-0 truncate font-mono text-sm font-semibold text-ink" title={flowId}>
          {flowId}
        </span>
        {summary && <span className={CASE_STATUS}>{summary.flowStatus}</span>}
      </div>
      {summary && (
        <dl className={cx(FACTS, "mt-2")}>
          <div className={FACT}>
            <dt className={FACT_TERM}>Run</dt>
            <dd className={cx(FACT_DATUM, "font-mono")}>{summary.runId}</dd>
          </div>
          <div className={FACT}>
            <dt className={FACT_TERM}>Started</dt>
            <dd className={FACT_DATUM}>{formatDate(summary.startTime)}</dd>
          </div>
          <div className={FACT}>
            <dt className={FACT_TERM}>{isOpen ? RUN_COPY.elapsed : RUN_COPY.closed}</dt>
            <dd className={FACT_DATUM}>
              {isOpen ? formatDuration(summary.startTime, null) : formatDate(summary.closeTime)}
            </dd>
          </div>
        </dl>
      )}
      <div className="mt-2.5 flex items-center gap-3">
        {summary && isOpen && host.canIntervene && (
          <Button variant="danger" size="sm" onClick={() => setStopOpen(true)}>
            {RUN_COPY.stop}
          </Button>
        )}
        <FlowLink className={LINK_OUT} to={{ surface: "diagnostics", flowId, runId: summary?.runId }}>
          Diagnostics
        </FlowLink>
      </div>
      {summary && stopOpen && (
        <StopFlowDialog summary={summary} onClose={() => setStopOpen(false)} onStopped={onStopped} />
      )}
    </div>
  );
}
