// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Handle, Position, type Node, type NodeProps, type NodeTypes } from "@xyflow/react";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { ConditionIcon } from "@/components/flow/diagnostics/value/DetailRecords";
import { cx } from "@/components/ui/cx";
import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { asRecord, asRecordArray, stringField, type UnknownRecord } from "@/lib/shared/json";
import { waitingConditionOf, type StepGraphNode } from "./step-graph";

interface GraphHandle {
  id: "topology" | "subflow";
  type: "source" | "target";
  position: Position;
}

export interface StepNodeData extends Record<string, unknown> {
  label: React.ReactNode;
  model: StepGraphNode;
  handles: GraphHandle[];
}

type MethodStatus = "Started" | "Waiting" | "Running" | "Pending" | "Completed" | "Failed" | "Canceled" | "Not started";
type MethodName = "WaitFor" | "Execute";

const METHOD = cx(
  "flex min-w-0 flex-col items-start gap-0.75 px-2.75 py-2.25 text-left only:col-span-full focus-visible:z-1 focus-visible:outline-2 focus-visible:outline-cta disabled:cursor-default disabled:opacity-60",
);

/**
 * Execute is a card; WaitFor is an arrow, its outline drawn by its own fill showing around the inset
 * ::before that paints its face.
 */
const METHOD_SHAPE = {
  WaitFor: cx(
    "relative isolate bg-clock pr-5 text-clock [clip-path:polygon(0_0,calc(100%-14px)_0,100%_50%,calc(100%-14px)_100%,0_100%)] *:relative *:z-1 before:absolute before:inset-px before:z-0 before:bg-raised before:[clip-path:inherit] focus-visible:-outline-offset-4 enabled:hover:before:bg-panel",
  ),
  Execute: cx("rounded-lg border border-pass bg-raised text-pass focus-visible:-outline-offset-2"),
} satisfies Record<MethodName, string>;

/** A failed, canceled or pending method says so in its status hue; otherwise the method's own hue shows. */
const STATUS_INK: Partial<Record<MethodStatus, string>> = {
  Failed: cx("text-fail"),
  Canceled: cx("text-ink-faint"),
  Pending: cx("text-child"),
};

const ELLIPSIS = cx("overflow-hidden text-ellipsis whitespace-nowrap");

const CONDITION = cx("flex min-w-0 items-center gap-1 text-[9px] whitespace-nowrap not-italic");

function conditionCount(condition: UnknownRecord, key: string): number {
  return asRecordArray(condition[key]).length;
}

function channelNames(condition: UnknownRecord): string[] {
  return asRecordArray(condition.channelConditions).flatMap((entry) => stringField(entry.channelName) || []);
}

function skipsWaitFor(node: StepGraphNode): boolean {
  return asRecord(node.active?.movement?.stepOptions ?? node.movement?.stepOptions).skipWaitFor === true;
}

function waitForStatus(node: StepGraphNode): MethodStatus {
  if (node.waitFor?.type === "StepWaitForFailed") return "Failed";
  if (node.pendingWaitFor) return "Pending";
  if (node.active?.phase === "Waiting") return "Waiting";
  if (node.waitFor) return "Started";
  if (node.active) return "Running";
  if (node.status === "Canceled") return "Canceled";
  return "Not started";
}

function executeStatus(node: StepGraphNode): MethodStatus {
  if (node.execute?.type === "StepExecuteFailed") return "Failed";
  if (node.pendingExecute) return "Pending";
  if (node.execute) return "Completed";
  if (node.active?.phase === "Active" && node.waitFor) return "Running";
  if (node.status === "Canceled") return "Canceled";
  return "Not started";
}

function MethodSection({
  name,
  status,
  event,
  children,
  onSelect,
}: {
  name: MethodName;
  status: MethodStatus;
  event?: FlowHistoryEvent;
  children?: React.ReactNode;
  onSelect: (event: FlowHistoryEvent) => void;
}) {
  return (
    <button
      className={cx(METHOD, METHOD_SHAPE[name], "nodrag nopan")}
      disabled={!event}
      type="button"
      onClick={(clickEvent) => {
        clickEvent.stopPropagation();
        if (event) onSelect(event);
      }}
    >
      <span className="text-[9px] font-extrabold text-ink-faint">{name}</span>
      <strong className={cx("text-[10px]", STATUS_INK[status])}>{status}</strong>
      {children}
    </button>
  );
}

function StepNodeLabel({ node, onSelect }: { node: StepGraphNode; onSelect: (event: FlowHistoryEvent) => void }) {
  const condition = waitingConditionOf(node);
  const timerCount = conditionCount(condition, "timerConditions");
  const channelCount = conditionCount(condition, "channelConditions");
  const subFlowCount = conditionCount(condition, "subFlowConditions");
  const channels = [...new Set(channelNames(condition))];
  const channelSummary =
    channels.length > 0 ? channels.join(", ") : `${channelCount} channel${channelCount === 1 ? "" : "s"}`;
  const showWaitFor = Boolean(
    node.waitFor || node.pendingWaitFor || (!skipsWaitFor(node) && (node.active || node.isPlanned) && !node.execute),
  );
  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-2 gap-y-0.75 border-b border-line-soft px-3 pt-2.5 pb-2">
        <span className="col-span-full text-[10px] font-extrabold text-ink-faint">{node.status}</span>
        <b className={cx(ELLIPSIS, "text-xs")}>{node.label}</b>
        <code
          className={cx(ELLIPSIS, "rounded-sm bg-raised px-1 text-[9px] text-ink-dim")}
          title={
            node.isPlanned
              ? node.status === "Canceled"
                ? "Canceled before a Step event was recorded"
                : "No Step event has been recorded"
              : undefined
          }
        >
          {node.isPlanned ? "Execution ID unavailable" : node.id}
        </code>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] items-stretch gap-4 px-2.5 pt-2.25 pb-2.5">
        {showWaitFor && (
          <MethodSection
            name="WaitFor"
            status={waitForStatus(node)}
            event={node.waitFor ?? node.pendingWaitFor}
            onSelect={onSelect}
          >
            {(timerCount > 0 || channelCount > 0 || subFlowCount > 0) && (
              <small className="mt-0.5 flex w-full min-w-0 flex-col gap-1.5">
                {timerCount > 0 && (
                  <i className={CONDITION} title={`${timerCount} timer condition${timerCount === 1 ? "" : "s"}`}>
                    <ConditionIcon kind="timer" className="size-3 shrink-0" />
                    {timerCount} timer{timerCount === 1 ? "" : "s"}
                  </i>
                )}
                {channelCount > 0 && (
                  <i className={CONDITION} title={channelSummary}>
                    <ConditionIcon kind="channel" className="size-3 shrink-0" />
                    <span className="overflow-hidden font-semibold text-ellipsis">{channelSummary}</span>
                  </i>
                )}
                {subFlowCount > 0 && (
                  <i className={CONDITION} title={`${subFlowCount} SubFlow condition${subFlowCount === 1 ? "" : "s"}`}>
                    <ConditionIcon kind="subflow" className="size-3 shrink-0" />
                    {subFlowCount} SubFlow{subFlowCount === 1 ? "" : "s"}
                  </i>
                )}
              </small>
            )}
          </MethodSection>
        )}
        <MethodSection
          name="Execute"
          status={executeStatus(node)}
          event={node.execute ?? node.pendingExecute}
          onSelect={onSelect}
        />
      </div>
    </div>
  );
}

function SubFlowNodeLabel({ flow }: { flow: StepGraphNode }) {
  return (
    <FlowLink
      className="nodrag nopan flex min-h-20.5 flex-col justify-center gap-1 px-3 py-2.5 text-inherit no-underline hover:bg-accent-subtle focus-visible:outline-3 focus-visible:-outline-offset-3 focus-visible:outline-cta"
      to={{ surface: "diagnostics", flowId: flow.flowId ?? "" }}
      aria-label={`Open SubFlow ${flow.flowId ?? ""}`}
    >
      <span className="flex items-center gap-1.25 text-[10px] font-extrabold text-accent">
        <ConditionIcon kind="subflow" className="size-3 shrink-0" />
        SubFlow · {flow.subFlowStatus}
      </span>
      <code className={cx(ELLIPSIS, "text-[9px]")} title={flow.flowId}>
        {flow.flowId}
      </code>
      {flow.reusePolicy && <small className={cx(ELLIPSIS, "text-[9px] text-ink-faint")}>{flow.reusePolicy}</small>}
    </FlowLink>
  );
}

/** Where Steps began: the Flow start, linking the previous run it continued from, or an RPC. */
function SourceNodeLabel({ node, flowId }: { node: StepGraphNode; flowId: string }) {
  return (
    <div className="flex min-h-19.5 flex-col justify-center gap-1 px-3 py-2.5">
      <span className="text-[10px] font-extrabold text-ink-faint">Source</span>
      {node.previousRunId ? (
        <>
          <b className={cx(ELLIPSIS, "text-xs")}>
            <FlowLink
              className="nodrag nopan text-inherit underline decoration-pass/35 underline-offset-3 hover:text-pass hover:decoration-current"
              title={node.previousRunId}
              to={{ surface: "diagnostics", flowId, runId: node.previousRunId }}
            >
              Continued from previous run
            </FlowLink>
          </b>
          <code className={cx(ELLIPSIS, "text-[9px]")} title={node.previousRunId}>
            {node.previousRunId}
          </code>
        </>
      ) : (
        <b className={cx(ELLIPSIS, "text-xs")}>{node.label}</b>
      )}
    </div>
  );
}

/** The label of a node in the execution graph of Flow `flowId`; selecting a Step method shows its event. */
export function nodeLabel(
  node: StepGraphNode,
  flowId: string,
  onSelect: (event: FlowHistoryEvent) => void,
): React.ReactNode {
  if (node.kind === "step") return <StepNodeLabel node={node} onSelect={onSelect} />;
  if (node.kind === "subflow") return <SubFlowNodeLabel flow={node} />;
  return <SourceNodeLabel node={node} flowId={flowId} />;
}

function GraphNode({ data }: NodeProps<Node<StepNodeData>>) {
  return (
    <div className="size-full">
      <div className="h-full overflow-hidden rounded-[10px]">{data.label}</div>
      {data.handles.map((handle) => (
        <Handle
          className="pointer-events-none size-2 border-none opacity-0"
          id={handle.id}
          key={`${handle.type}:${handle.id}`}
          position={handle.position}
          type={handle.type}
        />
      ))}
    </div>
  );
}

export const graphNodeTypes: NodeTypes = { graph: GraphNode };

export function graphNodeDimensions(node: StepGraphNode): { height: number; width: number } {
  if (node.kind !== "step") return { height: 90, width: 220 };
  return {
    height: node.execute && !node.waitFor && !node.pendingWaitFor ? 126 : 158,
    width: 300,
  };
}

/**
 * Where edges attach: a Step takes topology edges at its top and bottom and, when it started
 * SubFlows, sends SubFlow edges from its right; a SubFlow receives on its left; a source only sends.
 */
export function nodeHandles(node: StepGraphNode, hasSubFlowChildren: boolean): GraphHandle[] {
  if (node.kind === "subflow") return [{ id: "subflow", type: "target", position: Position.Left }];
  if (node.kind === "source") return [{ id: "topology", type: "source", position: Position.Bottom }];
  return [
    { id: "topology", type: "target", position: Position.Top },
    { id: "topology", type: "source", position: Position.Bottom },
    ...(hasSubFlowChildren ? [{ id: "subflow", type: "source", position: Position.Right } as const] : []),
  ];
}
