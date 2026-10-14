// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { getFlowSummary } from "@/lib/api/flow/flow-diagnostics";
import { useFlowRouteParams } from "@/lib/flow/FlowNavigationProvider";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { FlowFrame } from "@/components/flow/frame/FlowFrame";
import { RunDiagnosticsView } from "./RunDiagnosticsView";
import { errorMessage } from "@/lib/api/transport/api-error";
import { Banner } from "@/components/ui/Banner";

/**
 * Everything one run recorded, addressed by Flow ID and optionally a run: Time Travel moves
 * along the run chain, while the Runs view only shows the current run. The
 * Flow type comes from the run's own summary, because the link may come from another Flow.
 */
export function DiagnosticsView() {
  const client = useFlowApi();
  const { flowId, runId } = useFlowRouteParams();
  const [resolvedRunID, setResolvedRunID] = useState(runId);
  const [resolvedFlowType, setResolvedFlowType] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void getFlowSummary(client, flowId, runId, controller.signal)
      .then((summary) => {
        if (controller.signal.aborted) return;
        setResolvedRunID(summary.runId);
        setResolvedFlowType(summary.flowType);
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(loadError, "Could not find a run for this Flow ID."));
        }
      });
    return () => controller.abort();
  }, [client, flowId, runId]);

  const flowType = resolvedFlowType || undefined;
  return (
    // A size container, so the sticky sidebar inside can measure itself against this scroller.
    <FlowFrame plane="page" className="@container-size overflow-y-auto">
      <header className="flex items-baseline gap-2.5 border-b border-line-faint px-5 py-2.5">
        <FlowLink className="text-[11px] text-accent hover:text-ink" to={{ surface: "runs", flowType, flowId }}>
          ← Run
        </FlowLink>
        <span className="text-[11px] tracking-[0.06em] text-ink uppercase">Diagnostics</span>
        <span className="text-[10px] text-ink-faint">
          Everything the run recorded. The Run view shows what it means.
        </span>
      </header>
      {error && <Banner className="m-5">{error}</Banner>}
      {!error && !resolvedRunID && (
        <div className="grid min-h-60 place-items-center p-10 text-center text-ink-faint">Finding the current run…</div>
      )}
      {resolvedRunID && (
        <RunDiagnosticsView
          key={`${flowId}:${resolvedRunID}`}
          breadcrumb={
            <div className="mb-5 flex gap-1.75 overflow-hidden text-[10.5px] whitespace-nowrap text-ink-faint">
              <FlowLink className="hover:text-ink" to={{ surface: "runs", flowType }}>
                {resolvedFlowType}
              </FlowLink>
              <span>/</span>
              <FlowLink className="hover:text-ink" to={{ surface: "runs", flowType, flowId }}>
                {flowId}
              </FlowLink>
              <span>/</span>
              <span className="truncate font-mono">{resolvedRunID}</span>
            </div>
          }
          flowId={flowId}
          runId={resolvedRunID}
        />
      )}
    </FlowFrame>
  );
}
