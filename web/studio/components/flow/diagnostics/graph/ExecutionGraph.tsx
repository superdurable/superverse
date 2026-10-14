// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
  type ReactFlowInstance,
} from "@xyflow/react";
import { buildStepGraph, stepGraphSelection, type StepGraphNode } from "./step-graph";
import type { FlowHistoryEvent, FlowState } from "@/lib/api/flow/flow-types";
import {
  defaultGraphZoom,
  graphBounds,
  layoutGraph,
  minimumGraphZoom,
  type SizedGraphNode,
} from "./execution-graph-layout";
import { graphNodeDimensions, graphNodeTypes, nodeHandles, nodeLabel, type StepNodeData } from "./ExecutionGraphNode";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

const graphEdgeColor = "var(--color-mark)";
const previousStepColor = "var(--color-accent)";
const nextStepColor = "var(--color-busy)";

type GraphNodeModel = Node<StepNodeData> & SizedGraphNode;

const NODE = cx("rounded-[10px] bg-panel text-left text-ink-dim");

/** The node's border by its status; a Source or a canceled Step keeps the neutral line. */
const NODE_BORDER: Partial<Record<StepGraphNode["status"], string>> = {
  Active: cx("border-accent"),
  Pending: cx("border-mark-faint"),
  Completed: cx("border-pass"),
  Waiting: cx("border-clock"),
  Failed: cx("border-fail"),
};

/** The selected Step, the Steps that led to it and the Steps it started. */
const NODE_RELATION = {
  current: cx("border shadow-[0_0_0_6px_rgba(111,160,54,0.24),0_14px_30px_rgba(79,129,39,0.2)] outline-2 outline-cta"),
  previous: cx("border-2 shadow-[0_0_0_3px_rgba(63,120,168,0.2),0_11px_25px_rgba(38,88,130,0.14)]"),
  next: cx("border-2 shadow-[0_0_0_3px_rgba(193,111,36,0.2),0_11px_25px_rgba(128,76,28,0.14)]"),
  none: cx("border shadow-[0_8px_20px_rgba(28,51,45,0.1)]"),
};

const LEGEND = [
  ["Source", cx("border border-child bg-raised")],
  ["Active / Pending", cx("bg-accent")],
  ["Waiting", cx("bg-clock")],
  ["Failed", cx("bg-fail")],
  ["Canceled", cx("bg-mark")],
  ["SubFlow", cx("border border-accent-rule bg-raised")],
] as const;

export function ExecutionGraph({
  flowId,
  events,
  state,
  selectedEvent,
  onSelectEvent,
}: {
  flowId: string;
  events: FlowHistoryEvent[];
  state: FlowState | null;
  selectedEvent: FlowHistoryEvent | null;
  onSelectEvent: (event: FlowHistoryEvent | null) => void;
}) {
  const graphCanvas = useRef<HTMLDivElement>(null);
  const [canvasWidth, setCanvasWidth] = useState(0);
  const [isMiniMapExpanded, setIsMiniMapExpanded] = useState(false);
  const [flowInstance, setFlowInstance] = useState<ReactFlowInstance<GraphNodeModel, Edge> | null>(null);
  const graph = useMemo(
    () => buildStepGraph(events, state?.activeStepExecutions ?? [], flowId),
    [events, flowId, state],
  );
  const selection = useMemo(
    () => stepGraphSelection(graph.nodes, graph.edges, selectedEvent),
    [graph.edges, graph.nodes, selectedEvent],
  );
  const nodeByID = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes]);
  const subFlowParentIDs = useMemo(
    () => new Set(graph.nodes.filter((node) => node.kind === "subflow").map((node) => node.parentStepId)),
    [graph.nodes],
  );
  const edges = useMemo<Edge[]>(
    () =>
      graph.edges.map((edge) => {
        const isIncoming = selection.incomingEdgeIDs.has(edge.id);
        const isOutgoing = selection.outgoingEdgeIDs.has(edge.id);
        const isSubFlow = nodeByID.get(edge.target)?.kind === "subflow";
        const color = isIncoming ? previousStepColor : isOutgoing ? nextStepColor : graphEdgeColor;
        return {
          ...edge,
          type: "smoothstep",
          sourceHandle: isSubFlow ? "subflow" : "topology",
          targetHandle: isSubFlow ? "subflow" : "topology",
          markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18, color },
          style: {
            stroke: color,
            strokeWidth: isIncoming || isOutgoing ? 3 : 1.5,
            strokeDasharray: isSubFlow ? "6 4" : undefined,
            filter: isIncoming
              ? "drop-shadow(0 1px 2px rgba(38, 88, 130, 0.28))"
              : isOutgoing
                ? "drop-shadow(0 1px 2px rgba(128, 76, 28, 0.28))"
                : undefined,
          },
          zIndex: isIncoming || isOutgoing ? 2 : 0,
        };
      }),
    [graph.edges, nodeByID, selection],
  );
  const nodes = useMemo(() => {
    const raw: GraphNodeModel[] = graph.nodes.map((node) => {
      const dimensions = graphNodeDimensions(node);
      const handles = nodeHandles(node, subFlowParentIDs.has(node.id));
      return {
        id: node.id,
        type: "graph",
        position: { x: 0, y: 0 },
        className: cx(
          NODE,
          NODE_BORDER[node.status] ?? "border-line-soft",
          NODE_RELATION[
            node.id === selection.selectedStepExecutionID
              ? "current"
              : selection.previousStepExecutionIDs.has(node.id)
                ? "previous"
                : selection.nextStepExecutionIDs.has(node.id)
                  ? "next"
                  : "none"
          ],
        ),
        width: dimensions.width,
        height: dimensions.height,
        style: dimensions,
        handles: handles.map((handle) => ({ ...handle, x: 0, y: 0, ...dimensions })),
        data: {
          model: node,
          handles,
          label: nodeLabel(node, flowId, onSelectEvent),
        },
      };
    });
    return layoutGraph(raw, edges);
  }, [edges, flowId, graph.nodes, onSelectEvent, selection, subFlowParentIDs]);
  const bounds = useMemo(() => graphBounds(nodes), [nodes]);
  const initialZoom = defaultGraphZoom(bounds.width, canvasWidth);

  useEffect(() => {
    const canvas = graphCanvas.current;
    if (!canvas) return undefined;
    const updateWidth = () => setCanvasWidth(canvas.clientWidth);
    updateWidth();
    const observer = new ResizeObserver(updateWidth);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!flowInstance || canvasWidth <= 0) return;
    void flowInstance.setViewport({
      x: Math.max(0, (canvasWidth - bounds.width * initialZoom) / 2),
      y: 0,
      zoom: initialZoom,
    });
  }, [bounds.width, canvasWidth, flowInstance, initialZoom]);

  if (!nodes.length)
    return (
      <div className="grid min-h-60 place-items-center rounded-lg border border-line-faint bg-panel p-10 text-center">
        <h3 className="m-0 text-[13px] font-semibold text-ink-dim">No execution topology loaded</h3>
      </div>
    );
  return (
    <div className="rounded-lg border border-line-faint bg-panel p-4.5">
      <div className="mb-3.25 flex items-center justify-between gap-5 max-md:flex-col max-md:items-start">
        <h2 className="m-0 text-sm font-semibold tracking-normal text-ink">Execution graph</h2>
        <div className="flex flex-wrap justify-end gap-2.5 text-[10px] text-ink-faint">
          {LEGEND.map(([label, swatch]) => (
            <span className="inline-flex items-center gap-1.25" key={label}>
              <i className={cx("size-2 rounded-full", swatch)} />
              {label}
            </span>
          ))}
        </div>
      </div>
      <div
        className="overflow-hidden rounded-xl border border-line-soft bg-canvas"
        ref={graphCanvas}
        style={{ height: Math.max(650, bounds.height * initialZoom) }}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={graphNodeTypes}
          minZoom={minimumGraphZoom}
          maxZoom={2}
          elementsSelectable={false}
          nodesConnectable={false}
          nodesDraggable={false}
          preventScrolling={false}
          zoomOnScroll={false}
          onInit={setFlowInstance}
          onNodeClick={(_, node) => {
            const model = node.data.model;
            if (model.kind === "subflow") return;
            onSelectEvent(model.execute ?? model.pendingExecute ?? model.waitFor ?? model.pendingWaitFor ?? null);
          }}
        >
          <Background gap={22} size={1} color="var(--color-line-soft)" />
          <Controls position="top-right" />
          <div
            className={cx(
              "absolute right-3 bottom-6.5 z-5",
              isMiniMapExpanded ? "pointer-events-auto h-37.5 w-50" : "pointer-events-none",
            )}
          >
            {isMiniMapExpanded && (
              <MiniMap
                ariaLabel="Execution graph Mini Map"
                className="m-0 overflow-hidden rounded-lg border border-line-soft"
                maskColor="color-mix(in oklab, var(--color-canvas) 65%, transparent)"
                nodeColor="var(--color-mark-faint)"
                pannable
                position="top-left"
                zoomable
                nodeStrokeWidth={2}
              />
            )}
            <Button
              aria-expanded={isMiniMapExpanded}
              aria-label={isMiniMapExpanded ? "Collapse Mini Map" : "Expand Mini Map"}
              className={cx(
                "nodrag nopan pointer-events-auto",
                isMiniMapExpanded && "absolute top-1.75 right-1.75 z-6 shadow-[0_3px_12px_rgba(28,51,45,0.34)]",
              )}
              onClick={() => setIsMiniMapExpanded((expanded) => !expanded)}
              size="sm"
              title={isMiniMapExpanded ? "Collapse Mini Map" : "Expand Mini Map"}
              variant={isMiniMapExpanded ? "primary" : "secondary"}
            >
              {isMiniMapExpanded ? "Close ×" : "Mini Map"}
            </Button>
          </div>
        </ReactFlow>
      </div>
      {selectedEvent && <div className="mt-2.5 text-[11px] text-ink-faint">Selected {selectedEvent.type}</div>}
    </div>
  );
}
