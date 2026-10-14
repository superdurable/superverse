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
import { EMPTY } from "@/components/flow/frame/workspace-styles";
import { compareFlow, decodeDesignedFlow, type CurrentGraphs } from "./design-compare";
import type { FlowDesign } from "./design-graph";
import { designPhaseLabel, type DesignPhase } from "@/lib/shared/design-phase";
import { DesignStepPanel } from "./DesignStepPanel";

/**
 * One designed Flow on the Flow tab, drawn by the canvas that draws implemented Flows, under its Design
 * label. Its Steps are marked new or changed against the current source when it is the Session head's.
 * It has no runs, so it offers no Runs, Start or Action control.
 */
export function DesignFlowView({
  flowType,
  design,
  phase,
  current,
  features,
}: {
  flowType: string;
  design: FlowDesign;
  phase: DesignPhase;
  /** The current source's graphs; null when they are not the Session head's, so nothing is marked. */
  current: CurrentGraphs;
  features: readonly { id: string; text: string }[];
}) {
  const designed = design.flows.find((flow) => flow.flowType === flowType);
  const decoded = useMemo(() => (designed ? decodeDesignedFlow(designed) : null), [designed]);
  const groups = useMemo(() => (decoded ? groupsFromDefinition(decoded.graph, decoded.model) : []), [decoded]);
  const comparison = useMemo(() => {
    if (!decoded || current === null) return null;
    const graph = current.get(flowType);
    return compareFlow(decoded.model, graph ? safeDecode(graph) : null);
  }, [current, decoded, flowType]);
  const renderPanel = useCallback(
    (step: StepModel, close: () => void) => {
      const designedStep = designed?.steps.find((item) => item.stepType === step.stepType);
      return designedStep && designed ? (
        <DesignStepPanel key={step.stepType} step={designedStep} flow={designed} features={features} onClose={close} />
      ) : null;
    },
    [designed, features],
  );
  if (!designed || !decoded) return <div className={EMPTY}>The design of {flowType} could not be drawn.</div>;

  const removed = [...design.removedFlows, ...(comparison?.removedSteps ?? []), ...(comparison?.removedActions ?? [])];
  return (
    <section
      aria-label="Flow design"
      className="@container/canvas relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-canvas"
      data-design-phase={phase}
    >
      {/* A strip above the drawing, never over it, so the label cannot cover the first Step. */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-line-soft bg-panel px-3 py-2 text-[12.5px]">
        <strong className="font-semibold text-accent">{designPhaseLabel(phase)}</strong>
        <span className="text-ink-dim">{designed.purpose}</span>
        {removed.length > 0 && <span className="text-ink-dim">Removes {removed.join(", ")}.</span>}
        {current === null && (
          <span className="text-ink-faint">
            Not compared: build the processes of the current version to mark what changes.
          </span>
        )}
      </div>
      <FlowGraphCanvas
        flowType={flowType}
        flow={decoded.model}
        groups={groups}
        marks={comparison?.marks}
        renderPanel={renderPanel}
        fitKey={`design|${design.digest}|${flowType}`}
      />
    </section>
  );
}
