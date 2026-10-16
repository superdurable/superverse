// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { stepById, stepByType, type FlowModel, type StepModel } from "@/components/flow/canvas/model/flow-model";
import { activeStepTypes, stepReason, type Reason, type RunOverlay } from "@/components/flow/canvas/model/run-overlay";
import { DetailPanel, stepDefinition } from "./StepDefinitionPanel";
import { framingNeighbours, NO_FRAMING_NEIGHBOURS } from "@/components/flow/canvas/camera/camera";
import { ArrowDefs } from "@/components/flow/canvas/render/RoutedEdge";
import { Stage } from "@/components/flow/canvas/render/Stage";
import { groupOfStep } from "@/components/flow/canvas/layout/layout-flow";
import { stageId } from "@/components/flow/canvas/layout/stage-card";
import type { Box, Direction, Scene } from "@/components/flow/canvas/layout/scene";
import { useStageScene } from "./use-stage-scene";
import { detourMessage, hasDetour, isFailureHandling, runDetourSources, withDetours } from "./failure-detours";
import { CanvasControls } from "./CanvasControls";
import { runFailure, type RunFailure } from "./run-failure";
import { CanvasLegend } from "./CanvasLegend";
import type { StepGroup } from "@/components/flow/canvas/model/step-groups";
import { actionableSteps, type ActionableStep } from "@/components/flow/canvas/model/actionable-steps";
import { stepContext, type StepContextView } from "@/components/flow/canvas/model/step-context";
import { PaneResizeHandle, paneWidthStyle, useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { STEP_PANEL_PANE } from "@/components/flow/frame/pane-bounds";
import { cx } from "@/components/ui/cx";
import { EMPTY } from "@/components/flow/frame/workspace-styles";

/** The selected Step's run status, so a host drawer can label it without owning selection. */
export interface SelectedStepStatus {
  stepType: string;
  /** The why-line the Step card shows, or null for a healthy Step. */
  reason: Reason | null;
  /** True when this is the Step the run is waiting on. */
  isBlocking: boolean;
  /** This Step's failure, or the run's latest one when this Step did not fail. */
  failure: RunFailure | null;
}

/** A text mark on a Step's card, such as a design's "new" or "changed"; never colour alone. */
export type StepMarkText = "new" | "changed";

/**
 * A stage carries its Steps' marks: "new" when every marked member is new and none is unmarked, else
 * "changed" when any member is marked. An unwritten Step is drawn dashed, as a planned one is.
 */
function markedScene(
  scene: Scene,
  marks: ReadonlyMap<string, StepMarkText>,
  groups: StepGroup[],
  unwritten: ReadonlySet<string>,
): Scene {
  const markOf = (box: Box): StepMarkText | undefined => {
    if (box.kind === "step") return marks.get(box.id);
    if (box.kind !== "stage") return undefined;
    const members = groups.find((group) => stageId(group.id) === box.id)?.stepIds ?? [];
    const memberMarks = members.map((id) => marks.get(id));
    if (memberMarks.every((mark) => mark === undefined)) return undefined;
    return memberMarks.every((mark) => mark === "new") ? "new" : "changed";
  };
  const writtenState = (box: Box): Partial<Box> =>
    box.kind === "step" && unwritten.has(box.id) ? { emphasis: "planned", unwritten: true } : {};
  return { ...scene, boxes: scene.boxes.map((box) => ({ ...box, mark: markOf(box), ...writtenState(box) })) };
}

/**
 * The canvas of one decoded Flow: its main story, stages, failure detours, selection and Step panel, with an
 * optional run overlay. It reads no catalog and no Flow API, so an implemented Flow and a design draw alike.
 */
export function FlowGraphCanvas({
  flowType,
  flow,
  groups,
  fitKey,
  overlay = null,
  marks,
  unwritten,
  notice,
  renderPanel,
  onActionable,
  onSelectedStepStatus,
  onStepContext,
  deselectKey = 0,
  refocusKey = 0,
  focusBlockingStep = false,
  markNow = false,
  showStepPanel = true,
  isRunLoaded = true,
  initialDirection = "tb",
}: {
  flowType: string;
  flow: FlowModel;
  groups: StepGroup[];
  /** Changes when a new drawing should fit the view; opening a stage or a panel keeps the reader's place. */
  fitKey: string;
  /** The way the Flow first reads; the reader's own choice then stays. */
  initialDirection?: Direction;
  /** False while the run that `overlay` will show is still loading, so the last drawing stays on screen. */
  isRunLoaded?: boolean;
  overlay?: RunOverlay | null;
  /** Text marks by Step id. */
  marks?: ReadonlyMap<string, StepMarkText>;
  /** The ids of Steps whose code is not written yet, such as a design's while the agent builds it. */
  unwritten?: ReadonlySet<string>;
  /** A line under the controls, such as a failed run read. */
  notice?: ReactNode;
  /** The selected Step's panel in place of its definition, for a view that explains Steps its own way. */
  renderPanel?: (step: StepModel, close: () => void) => ReactNode;
  deselectKey?: number;
  /** Bumped when the open run is picked again: the reader's selection goes and the view returns to the run. */
  refocusKey?: number;
  focusBlockingStep?: boolean;
  markNow?: boolean;
  showStepPanel?: boolean;
  onSelectedStepStatus?: (status: SelectedStepStatus | null) => void;
  onStepContext?: (context: StepContextView | null) => void;
  onActionable?: (steps: ActionableStep[]) => void;
}) {
  const canvasRef = useRef<HTMLDivElement>(null);
  // A Flow reads top-down, as Zapier draws it, unless its host says otherwise; then the reader chooses.
  const [direction, setDirection] = useState<Direction>(initialDirection);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  /** Auto-focus selects a Step for the reader; only their own click should fade the rest. */
  const [selectedByReader, setSelectedByReader] = useState(false);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  /** The Step whose failure detour is drawn: the last selected Step that can fail. */
  const [detourFor, setDetourFor] = useState<string | null>(null);
  const stepPanelPane = useCollapsibleColumn(STEP_PANEL_PANE);

  /** The Scene and the stages the reader opened in place, each opened or closed without losing their place. */
  const stages = useStageScene({ flow, groups, direction, run: overlay });
  /** The main story, with the failure detours of the selected Step and of every one the run went through. */
  const scene = useMemo(() => {
    if (stages.scene === null) return null;
    const sources = [
      ...new Set([...(overlay ? runDetourSources(flow, overlay) : []), ...(detourFor ? [detourFor] : [])]),
    ];
    const drawn = withDetours(
      stages.scene,
      flow,
      { direction, run: overlay, groups, openStages: stages.openStages },
      sources,
    );
    return marks || unwritten ? markedScene(drawn, marks ?? new Map(), groups, unwritten ?? new Set()) : drawn;
  }, [detourFor, direction, flow, groups, marks, overlay, stages.openStages, stages.scene, unwritten]);
  // Nothing selected, so no detour: however the selection was cleared.
  if (selectedId === null && detourFor !== null) setDetourFor(null);

  const selectedStep = selectedId === null ? null : (stepById(flow, selectedId) ?? null);

  /** The Step the run is actually waiting on, which is what an Admin came to see. */
  const blockingStepType = overlay ? (activeStepTypes(overlay)[0] ?? null) : null;
  const blockingStep = blockingStepType === null ? null : (stepByType(flow, blockingStepType) ?? null);
  const focusNodeId = focusBlockingStep ? (blockingStep?.id ?? null) : null;

  // A host's deselect, or the open run picked again, drops the selection before the run's Step is revealed.
  const [observedDeselect, setObservedDeselect] = useState(`${deselectKey}|${refocusKey}`);
  if (`${deselectKey}|${refocusKey}` !== observedDeselect) {
    setObservedDeselect(`${deselectKey}|${refocusKey}`);
    setSelectedByReader(false);
    setSelectedId(null);
    setSelectedGroupId(null);
    setDetourFor(null);
  }

  // Choosing a run should land on where it stopped, once, without fighting later clicks.
  const [revealedFor, setRevealedFor] = useState("");
  const revealKey = `${fitKey}|${blockingStepType}|${String(focusBlockingStep)}|${refocusKey}`;
  if (blockingStep !== null && revealedFor !== revealKey) {
    setRevealedFor(revealKey);
    setSelectedId(focusNodeId);
    setSelectedByReader(false);
    setSelectedGroupId(null);
    // The stage that holds where the run is opens, so its "now" ring is on a Step; failure handling
    // the run is in is drawn as its detour instead.
    const holding = isFailureHandling(flow, blockingStep.id) ? undefined : groupOfStep(groups, blockingStep.id);
    if (holding) stages.revealStage(holding.id);
  }

  useEffect(() => {
    onActionable?.(actionableSteps(flow, overlay));
  }, [flow, onActionable, overlay]);

  useEffect(() => {
    onStepContext?.(selectedStep ? stepContext(flow, selectedStep, groups) : null);
  }, [flow, groups, onStepContext, selectedStep]);

  useEffect(() => {
    if (!onSelectedStepStatus) return;
    if (!selectedStep || !overlay) {
      onSelectedStepStatus(null);
      return;
    }
    onSelectedStepStatus({
      stepType: selectedStep.stepType,
      reason: stepReason(overlay, selectedStep.stepType),
      isBlocking: selectedStep.stepType === blockingStepType,
      failure: runFailure(flow, overlay, selectedStep.stepType),
    });
  }, [blockingStepType, flow, onSelectedStepStatus, overlay, selectedStep]);

  const panel = useMemo(() => (selectedStep ? stepDefinition(flow, selectedStep) : null), [flow, selectedStep]);

  // Stable, so a re-render that changes no Scene does not rebuild the stage's nodes. Selecting a Step
  // that can fail draws its detour; selecting inside the detour keeps it; anything else closes it.
  const selectStep = useCallback(
    (id: string | null) => {
      setSelectedId(id);
      setSelectedByReader(id !== null);
      setSelectedGroupId(null);
      if (id !== null && isFailureHandling(flow, id)) return;
      setDetourFor(id !== null && hasDetour(flow, id) ? id : null);
    },
    [flow],
  );
  /** A box's label selects its stage; its ⊖ closes it. */
  const { collapseStage, openStage: openStageInPlace } = stages;
  const pickBand = useCallback((groupId: string) => {
    setSelectedGroupId(groupId);
    setSelectedId(null);
  }, []);
  const closeStage = useCallback(
    (groupId: string) => {
      collapseStage(groupId);
      setSelectedGroupId(null);
    },
    [collapseStage],
  );
  /** Opening a stage marks its box, so the eye lands where the card was. */
  const openStage = useCallback(
    (boxId: string) => {
      openStageInPlace(boxId);
      const group = groups.find((g) => stageId(g.id) === boxId);
      setSelectedGroupId(group?.id ?? null);
      setSelectedId(null);
    },
    [groups, openStageInPlace],
  );
  const legend = useMemo(() => (scene ? <CanvasLegend scene={scene} /> : null), [scene]);

  /** The Step the camera frames, with the cards of the Steps around it. */
  // Picking a run frames its Step; Collapsed, Expanded or a new direction then return to the opening view.
  const runPick = `${fitKey}|${refocusKey}`;
  const [picked, setPicked] = useState({ runPick, view: stages.view, isLeft: false });
  if (picked.runPick !== runPick) setPicked({ runPick, view: stages.view, isLeft: false });
  else if (picked.view === "" && stages.view !== "") setPicked({ ...picked, view: stages.view });
  else if (!picked.isLeft && stages.view !== picked.view) setPicked({ ...picked, isLeft: true });
  const runStepId = focusNodeId ?? (markNow ? (blockingStep?.id ?? null) : null);
  const targetId = picked.isLeft ? null : runStepId;
  const targetNeighbours = useMemo(
    () => (scene && targetId !== null ? framingNeighbours(flow, groups, scene, targetId) : NO_FRAMING_NEIGHBOURS),
    [flow, groups, scene, targetId],
  );
  // The drawing settles once its run has loaded, the stage holding its Step is open and the layout is done.
  const isSettled = stages.isSettled && isRunLoaded && (blockingStep === null || revealedFor === revealKey);

  if (!scene) return <div className={EMPTY}>Drawing the flow…</div>;

  const ownsPanel = showStepPanel && panel !== null && selectedStep !== null;
  const closePanel = () => setSelectedId(null);

  return (
    // min-h-0: a flex or grid item would otherwise refuse to shrink below its content and push the
    // canvas past the viewport instead of filling it exactly.
    <div
      className="relative flex min-h-0 min-w-0 flex-1 flex-col"
      ref={canvasRef}
      style={ownsPanel ? paneWidthStyle(stepPanelPane) : undefined}
    >
      <ArrowDefs />
      {/*
       * Top right and sized to its content, so it does not read as a banner. One row that scrolls
       * sideways if it must: the pane reserves a fixed inset for it, and a wrapped bar would grow
       * exactly when the canvas is narrowest and cover the first Step. Translucent, so the drawing
       * is never fully hidden behind its own switches.
       */}
      <div
        className={cx(
          "absolute top-2.5 z-5 flex w-max [scrollbar-width:none] items-center gap-2 overflow-x-auto rounded-lg border border-line-faint bg-bg/88 px-2.25 py-1.5 backdrop-blur-[6px] [&::-webkit-scrollbar]:hidden",
          // Beside the Step panel while it is open, never under it.
          ownsPanel
            ? "right-[calc(var(--v2-panel-w,290px)+12px)] max-w-[calc(100%-var(--v2-panel-w,290px)-24px)]"
            : "right-3 max-w-[calc(100%-24px)]",
        )}
      >
        <CanvasControls
          stages={stages.hasStages ? stages.state : null}
          direction={direction}
          onCollapseAll={stages.collapseAll}
          onExpandAll={stages.expandAll}
          onDirection={setDirection}
        />
      </div>
      {notice}
      <Stage
        // A new Flow type enters afresh; another run of it moves into place.
        key={flowType}
        isSettled={isSettled}
        targetNeighbours={targetNeighbours}
        dimUnrelated={selectedByReader}
        focusNodeId={targetId}
        nowNodeId={markNow ? (blockingStep?.id ?? null) : null}
        scene={scene}
        direction={direction}
        selectedId={selectedId}
        selectedGroupId={selectedGroupId}
        insetRightPx={ownsPanel ? stepPanelPane.width : null}
        onBand={pickBand}
        onCloseStage={closeStage}
        onOpenStage={openStage}
        onSelect={selectStep}
        legend={legend}
        cue={stages.cue}
        fitKey={`${fitKey}|${stages.view}|${refocusKey}`}
      />
      <p className="sr-only" aria-live="polite">
        {stages.cue?.message}
      </p>
      <p className="sr-only" aria-live="polite">
        {detourFor ? detourMessage(flow, detourFor) : ""}
      </p>
      {ownsPanel ? (
        <>
          {/*
           * The handle pins itself to the right edge of its positioned parent, so this zero-width
           * strip ends where the panel begins and the handle straddles the panel's inner edge.
           */}
          <div className="absolute inset-y-0 right-[var(--v2-panel-w,290px)] z-7 w-0 translate-x-1">
            <PaneResizeHandle
              column={stepPanelPane}
              targetRef={canvasRef}
              measureRef={canvasRef}
              ariaLabel="Resize the Step detail panel"
            />
          </div>
          {renderPanel ? (
            renderPanel(selectedStep, closePanel)
          ) : (
            <DetailPanel key={panel.stepType} model={panel} flowType={flowType} onClose={closePanel} />
          )}
        </>
      ) : null}
    </div>
  );
}
