// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useMemo } from "react";
import { safeDecode } from "@/components/flow/canvas/model/decode-flow-definition";
import type { StepModel } from "@/components/flow/canvas/model/flow-model";
import { groupsFromDefinition } from "@/components/flow/canvas/model/step-groups";
import { FlowGraphCanvas } from "@/components/flow/canvas/FlowGraphCanvas";
import type { Direction } from "@/components/flow/canvas/layout/scene";
import { EMPTY } from "@/components/flow/frame/workspace-styles";
import { compareFlow, decodeDesignedFlow, type CurrentGraphs } from "./design-compare";
import type { FlowDesign } from "./design-graph";
import type { DesignPhase } from "@/lib/shared/design-phase";
import { processName } from "@/lib/shared/format";
import { DesignHeading } from "./DesignHeading";
import { DesignStepPanel } from "./DesignStepPanel";

const NONE_WRITTEN: readonly string[] = [];

/**
 * One designed Flow on the Flow tab, drawn by the canvas that draws implemented Flows. Its Steps are marked
 * new or changed against the current source when it is the Session head's and holds the Flow; a process the
 * project never built would be new throughout, so it carries no marks. While the agent builds it, a Step
 * whose code is not written yet, neither by the agent's writes nor in the compared source, is drawn dashed,
 * and the host's `heading` shows on the canvas's corner with the count of written ones; once built, every
 * Step is solid and the drawing stands alone. It has no runs, so it offers no Runs, Start or Action control.
 */
export function DesignFlowView({
  flowType,
  design,
  phase,
  current,
  features,
  written = NONE_WRITTEN,
  direction,
  heading,
}: {
  flowType: string;
  design: FlowDesign;
  phase: DesignPhase;
  /** The current source's graphs; null when they are not the Session head's, so nothing is marked. */
  current: CurrentGraphs;
  features: readonly { id: string; text: string }[];
  /** The designed Steps whose declaration the agent wrote, each `Flow.Step`. */
  written?: readonly string[];
  /** The way it first reads, which the reader may change; the canvas's own default when absent. */
  direction?: Direction;
  /** The process title on the canvas's corner while the agent builds it; absent once built. */
  heading?: string;
}) {
  const designed = design.flows.find((flow) => flow.flowType === flowType);
  const decoded = useMemo(() => (designed ? decodeDesignedFlow(designed) : null), [designed]);
  const groups = useMemo(() => (decoded ? groupsFromDefinition(decoded.graph, decoded.model) : []), [decoded]);
  const comparison = useMemo(() => {
    if (!decoded || current === null) return null;
    const graph = current.get(flowType);
    return compareFlow(decoded.model, graph ? safeDecode(graph) : null);
  }, [current, decoded, flowType]);
  // A Step the compared source already holds needs no code; without a comparison only the agent's writes count.
  const unwritten = useMemo(() => {
    if (!decoded || phase !== "building") return new Set<string>();
    const done = new Set(
      written.filter((name) => name.startsWith(`${flowType}.`)).map((name) => name.slice(flowType.length + 1)),
    );
    const missing = decoded.model.steps.filter(
      (step) => !done.has(step.stepType) && (comparison === null || comparison.marks.get(step.id) === "new"),
    );
    return new Set(missing.map((step) => step.id));
  }, [comparison, decoded, flowType, phase, written]);
  const renderPanel = useCallback(
    (step: StepModel, close: () => void) => {
      const designedStep = designed?.steps.find((item) => item.stepType === step.stepType);
      return designedStep && designed ? (
        <DesignStepPanel key={step.stepType} step={designedStep} flow={designed} features={features} onClose={close} />
      ) : null;
    },
    [designed, features],
  );
  if (!designed || !decoded)
    return <div className={EMPTY}>The design of {processName(flowType)} could not be drawn.</div>;

  const total = decoded.model.steps.length;
  // Marks only say something about a process the compared source already holds.
  const marks = comparison && current?.has(flowType) ? comparison.marks : undefined;
  return (
    <section
      aria-label="Flow design"
      className="@container/canvas relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-canvas"
      data-design-phase={phase}
    >
      {heading !== undefined && phase === "building" && (
        <DesignHeading title={heading} status={`Building · ${total - unwritten.size} of ${total} steps written`} />
      )}
      <FlowGraphCanvas
        flowType={flowType}
        flow={decoded.model}
        groups={groups}
        marks={marks}
        unwritten={unwritten}
        renderPanel={renderPanel}
        fitKey={`design|${design.digest}|${flowType}`}
        initialDirection={direction}
        opensWhole
      />
    </section>
  );
}
