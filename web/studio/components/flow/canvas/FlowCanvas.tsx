// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useMemo, useState, type ComponentProps } from "react";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";
import { usesCurrentDefinition, unavailableDefinitionMessage } from "@/lib/flow/definition-binding";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { safeDecode } from "@/components/flow/canvas/model/decode-flow-definition";
import type { RunOverlay } from "@/components/flow/canvas/model/run-overlay";
import { loadCurrentRun } from "@/components/flow/canvas/model/run-overlay-from-history";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { groupsFromDefinition } from "@/components/flow/canvas/model/step-groups";
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";
import { errorMessage } from "@/lib/api/transport/api-error";
import { cx } from "@/components/ui/cx";
import { EMPTY, ERROR, LINK } from "@/components/flow/frame/workspace-styles";
import { FlowGraphCanvas } from "./FlowGraphCanvas";

export type { SelectedStepStatus } from "./FlowGraphCanvas";

/** How often an open run is re-read. */
const RUN_POLL_MS = 5000;

export function FlowCanvas(props: ComponentProps<typeof FlowCanvasContent>) {
  // Keyed by Flow type only: another run of the same Flow moves the drawing into place instead of redrawing it.
  return <FlowCanvasContent key={props.flowType} {...props} />;
}

type GraphCanvasProps = ComponentProps<typeof FlowGraphCanvas>;

/**
 * A Flow type's valid definition from the catalog, drawn by FlowGraphCanvas, with the run that `flowId`
 * names overlaid and re-read while it is open. A run of another definition is never drawn on this one.
 */
function FlowCanvasContent({
  flowType,
  flowId = "",
  onSummary,
  onTick,
  ...canvas
}: Pick<
  GraphCanvasProps,
  | "onActionable"
  | "onSelectedStepStatus"
  | "onStepContext"
  | "deselectKey"
  | "refocusKey"
  | "focusBlockingStep"
  | "markNow"
  | "showStepPanel"
> & {
  flowType: string;
  flowId?: string;
  onSummary?: (summary: FlowSummary | null) => void;
  /** Fired on every run poll, so a host can refresh on the same beat. */
  onTick?: () => void;
}) {
  const client = useFlowRequestScope(`${flowType}:${flowId}`);
  const { definitions } = useFlowCatalog();
  const [runSummary, setRunSummary] = useState<FlowSummary | null>(null);
  const [runError, setRunError] = useState("");
  const [overlay, setOverlay] = useState<RunOverlay | null>(null);
  /** The run whose history last loaded or failed to; the drawing waits for the one shown. */
  const [loadedFlowId, setLoadedFlowId] = useState("");
  // Another run: its predecessor's overlay and error go at once; its summary stays, so the canvas stays mounted.
  const [observedFlowId, setObservedFlowId] = useState(flowId);
  if (observedFlowId !== flowId) {
    setObservedFlowId(flowId);
    setOverlay(null);
    setRunError("");
  }

  useEffect(() => {
    if (!flowId) return undefined;
    const controller = new AbortController();
    let timer: number | undefined;
    const load = async () => {
      try {
        const run = await loadCurrentRun(client, flowId, controller.signal);
        if (controller.signal.aborted) return;
        setRunSummary(run.summary);
        setOverlay(run.overlay);
        setRunError("");
        setLoadedFlowId(flowId);
        onSummary?.(run.summary);
        onTick?.();
        if (isOpenFlowStatusCode(run.summary.flowStatusCode) && timer === undefined) {
          timer = window.setInterval(() => {
            void load();
          }, RUN_POLL_MS);
        }
      } catch (loadError: unknown) {
        if (!controller.signal.aborted) {
          setLoadedFlowId(flowId);
          setRunError(errorMessage(loadError, "Run history failed to load"));
        }
      }
    };
    void load();
    return () => {
      controller.abort();
      if (timer !== undefined) window.clearInterval(timer);
    };
  }, [client, flowId, onSummary, onTick]);

  const selected = useMemo(
    () =>
      definitions?.definitions.find(
        (definition) => definition.flowName === flowType && definition.schemaVersion === "2.0" && definition.valid,
      ),
    [definitions, flowType],
  );

  const bound = !flowId || usesCurrentDefinition(runSummary, definitions?.definitionRevision);
  const flow = useMemo(() => (selected && bound ? safeDecode(selected.graph) : null), [bound, selected]);

  const groups = useMemo(() => {
    if (!selected || !flow) return [];
    return groupsFromDefinition(selected.graph, flow);
  }, [flow, selected]);

  if (!definitions) return <div className={EMPTY}>Loading Flow definition…</div>;
  if (flowId && !bound)
    return (
      <div className={EMPTY} role="status">
        {runSummary ? unavailableDefinitionMessage : runError || "Loading execution definition…"}{" "}
        <FlowLink className={LINK} to={{ surface: "diagnostics", flowId, runId: runSummary?.runId }}>
          Diagnostics
        </FlowLink>
      </div>
    );
  if (!selected || !flow) {
    return <div className={EMPTY}>No valid Flow Definition Graph 2.0 file for this Flow type.</div>;
  }

  return (
    <FlowGraphCanvas
      {...canvas}
      flowType={flowType}
      flow={flow}
      groups={groups}
      overlay={overlay}
      isRunLoaded={!flowId || loadedFlowId === flowId}
      notice={runError ? <p className={cx(ERROR, "mx-3")}>{runError}</p> : null}
      // Opening or closing a stage, or the Step panel, keeps the reader's place, so neither refits the view.
      fitKey={`${flowType}|${flowId}|${selected.file}`}
    />
  );
}
