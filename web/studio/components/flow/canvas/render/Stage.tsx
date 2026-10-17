// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import {
  Background,
  BackgroundVariant,
  type Edge,
  type Node,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
} from "@xyflow/react";
import { useCallback, useEffect, useMemo, useRef, useState, type JSX, type ReactNode } from "react";
import type { Direction, Scene } from "@/components/flow/canvas/layout/scene";
import { placementKey } from "@/components/flow/canvas/layout/anchor";
import { LEGEND_BOX_H, LEGEND_W, legendAnchor } from "./legend-anchor";
import { EDGE_MARKER, RoutedEdge, edgeAttributes, type RoutedEdgeData } from "./RoutedEdge";
import { anchorHandles, NODE_TYPES, type BoxData } from "./SceneNodes";
import { cameraMotion, entranceStyle, type FramingNeighbours } from "@/components/flow/canvas/camera/camera";
import { useCamera, useRunTravel, useSettledDrawing } from "@/components/flow/canvas/camera/use-camera";
import { chromeCompensation, edgeContrastBoost, FIT_FLOOR, fitOptions, MAX_ZOOM, MIN_ZOOM, OPEN_FLOOR } from "./zoom";
import { ZoomControls } from "./ZoomControls";

/**
 * The only module that renders React Flow. It turns a Scene, which is plain geometry with routed
 * lines, into nodes and edges; zoom here is purely geometric.
 */

const EDGE_TYPES = { routed: RoutedEdge };

interface StageProps {
  scene: Scene;
  /** Which way the Flow reads, for where a too-large drawing opens. */
  direction: Direction;
  selectedId: string | null;
  selectedGroupId: string | null;
  onSelect: (id: string | null) => void;
  /** A box's label selects its stage. */
  onBand: (groupId: string) => void;
  /** A box's ⊖ closes its stage into one card. */
  onCloseStage: (groupId: string) => void;
  onOpenStage: (stageId: string) => void;
  /** Rendered as a node beside the drawing; pass a stable element so the nodes are not rebuilt. */
  legend: ReactNode;
  controls?: ReactNode;
  /** False when the selection was made for the reader, so the graph is not faded for them. */
  dimUnrelated: boolean;
  /** When set, the view frames this Step with its neighbours; null opens on the whole Flow, or its start. */
  focusNodeId: string | null;
  /** Opens on the whole Flow at any size, as Fit does, instead of on its start when it is too large to read. */
  opensWhole?: boolean;
  /** The cards of the Steps a transition joins to the focus, or to "now", which its framing keeps in view. */
  targetNeighbours?: FramingNeighbours;
  /** False while the drawing is on its way, such as a run's history; the last settled one stays meanwhile. */
  isSettled?: boolean;
  /** The Step (or the stage holding it) the shown run is at now, ringed and labelled "now". */
  nowNodeId?: string | null;
  /** Pixel width of the right-hand panel overlay, or null when none is open. */
  insetRightPx: number | null;
  /** Changing this re-opens the view. Deliberately not the zoom, or opening would fight the user. */
  fitKey: string;
  /** After a stage opens or closes: what takes focus and what to bring into view. */
  cue?: { focusId: string | null; reveal: { x: number; y: number; w: number; h: number } | null; nonce: number } | null;
}

function Inner({
  scene: latestScene,
  direction,
  selectedId,
  selectedGroupId,
  onSelect,
  onBand,
  onCloseStage,
  onOpenStage,
  legend,
  controls,
  dimUnrelated,
  focusNodeId,
  opensWhole = false,
  nowNodeId: latestNowNodeId = null,
  insetRightPx,
  fitKey,
  cue = null,
  targetNeighbours,
  isSettled = true,
}: StageProps): JSX.Element {
  const rf = useReactFlow();
  const paneRef = useRef<HTMLDivElement | null>(null);
  const { scene, nowNodeId } = useSettledDrawing(isSettled, latestScene, latestNowNodeId);
  // The Fit button is marked while the whole Flow is in view; the reader's own pan or zoom leaves it.
  const [fitted, setFitted] = useState(true);
  const fitWhole = useCallback(() => {
    const el = paneRef.current?.querySelector<HTMLElement>(".react-flow");
    if (!el) return;
    void rf.fitView({ ...fitOptions(insetRightPx, el.clientWidth, true), ...cameraMotion() });
    setFitted(true);
  }, [insetRightPx, rf]);
  // Once the view is open, a resize with a selection (a drawer opening beside it) only pans that into view.
  const keepSelectionInView = useRef<() => boolean>(() => false);
  const { delays, isEntered, panToShow, setOwner } = useCamera({
    viewport: rf,
    paneRef,
    scene,
    direction,
    isSettled,
    subjectKey: fitKey,
    targetId: focusNodeId,
    targetNeighbours,
    insetRightPx,
    wholeMaxZoom: 1,
    openFloor: opensWhole ? FIT_FLOOR : undefined,
    fitWhole,
    keepSelectionInView: () => keepSelectionInView.current(),
    onFramed: (scope) => setFitted(scope === "whole"),
  });
  const [selectedLinkId, setSelectedLinkId] = useState<string | null>(null);
  const [observedSelection, setObservedSelection] = useState(selectedId);
  if (observedSelection !== selectedId) {
    setObservedSelection(selectedId);
    if (selectedId !== null) setSelectedLinkId(null);
  }

  /**
   * A jump chip selects where it leads and brings it into view at a readable size; a jump into a
   * collapsed stage opens it there.
   */
  const jump = useCallback(
    (id: string) => {
      const node = rf.getNode(id);
      if (node === undefined) return;
      const kind = (node.data as BoxData | undefined)?.box.kind;
      if (kind === "step") onSelect(id);
      if (kind === "stage") onOpenStage(id);
      setOwner("reader");
      void rf.setCenter(node.position.x + (node.width ?? 0) / 2, node.position.y + (node.height ?? 0) / 2, {
        zoom: Math.max(rf.getZoom(), OPEN_FLOOR),
        ...cameraMotion(),
      });
    },
    [onOpenStage, onSelect, rf, setOwner],
  );
  const travel = useRunTravel(scene, fitKey, nowNodeId, isEntered);

  // Sizes come from the Scene, so fitting never measures unrendered nodes; nothing draws before the camera frames it.
  const nodes = useMemo<Node[]>(() => {
    if (!isEntered) return [];
    const travelledTo = travel === null ? undefined : scene.links.find((l) => l.id === travel.linkId)?.to;
    const bands: Node[] = scene.bands.map((b) => ({
      id: `band:${b.id}`,
      type: "band",
      position: { x: b.x, y: b.y },
      width: b.w,
      height: b.h,
      data: { ...b, selected: b.groupId === selectedGroupId, onPick: onBand, onClose: onCloseStage },
      draggable: false,
      selectable: false,
      zIndex: 0,
      style: { zIndex: 0, ...entranceStyle(delays, `band:${b.id}`) },
    }));
    // A failure detour sits above the drawing and its legend, beside the card it opened from.
    const boxes: Node[] = scene.boxes.map((b) => ({
      id: b.id,
      type: "pbox",
      position: { x: b.x, y: b.y },
      width: b.w,
      height: b.h,
      handles: anchorHandles(b.w, b.h),
      data: {
        box: b,
        selected: b.id === selectedId,
        now: nowNodeId !== null && (b.id === nowNodeId || (b.holds ?? []).includes(nowNodeId)),
        onSelect,
        onOpen: onOpenStage,
        onJump: jump,
      } satisfies BoxData,
      draggable: false,
      zIndex: b.detourOf === undefined ? 2 : 4,
      style: {
        zIndex: b.detourOf === undefined ? 2 : 4,
        ...entranceStyle(delays, b.id, travelledTo === b.id),
      },
    }));
    const legendNode: Node[] = [];
    const anchor = legendAnchor(
      scene.boxes.filter((b) => b.detourOf === undefined),
      scene.bands,
    );
    if (anchor !== undefined) {
      legendNode.push({
        id: "__legend",
        type: "legend",
        position: anchor,
        width: LEGEND_W,
        height: LEGEND_BOX_H,
        data: { legend },
        draggable: false,
        selectable: false,
        zIndex: 3,
        style: { zIndex: 3, width: LEGEND_W, height: LEGEND_BOX_H, ...entranceStyle(delays, "__legend") },
      });
    }
    return [...bands, ...boxes, ...legendNode];
  }, [
    scene,
    selectedId,
    nowNodeId,
    onSelect,
    selectedGroupId,
    onBand,
    onCloseStage,
    onOpenStage,
    jump,
    legend,
    isEntered,
    delays,
    travel,
  ]);

  const edges = useMemo<Edge<RoutedEdgeData>[]>(
    () =>
      (isEntered ? scene.links : []).map((l) => {
        const lit = selectedId !== null && (l.from === selectedId || l.to === selectedId);
        const picked = l.id === selectedLinkId;
        const dim = dimUnrelated && selectedId !== null && !lit && !picked;
        return {
          id: l.id,
          source: l.from,
          target: l.to,
          type: "routed",
          data: {
            points: l.points,
            ...(l.label?.at ? { labelAt: l.label.at } : {}),
            ...(travel?.linkId === l.id ? { travel: travel.nonce } : {}),
          },
          style: entranceStyle(delays, l.id),
          // A bare marker id: React Flow wraps a string marker in `url(#…)` itself.
          markerEnd: EDGE_MARKER[l.family],
          selected: picked,
          zIndex: picked || lit ? 2 : 1,
          // Colour and width come from app/react-flow.css, so zoom compensation reaches them without re-rendering.
          domAttributes: edgeAttributes(l.family, { "data-lit": lit || picked, "data-dim": dim }),
          // A case value is always shown; a count of merged conditions only while an end is selected.
          label: l.label && (l.label.always || lit || picked) ? l.label.text : undefined,
          labelStyle: { fill: "var(--color-ink-dim)", fontSize: 10.5, fontWeight: 500 },
          labelBgStyle: { fill: "var(--color-panel)" },
        };
      }),
    [scene, selectedId, selectedLinkId, dimUnrelated, isEntered, delays, travel],
  );

  // Written to CSS custom properties on every wheel tick, so zooming re-renders nothing.
  const applyZoomVars = useCallback((zoom: number) => {
    const el = paneRef.current;
    if (el === null || !Number.isFinite(zoom)) return;
    el.style.setProperty("--zoom-comp", chromeCompensation(zoom).toFixed(3));
    el.style.setProperty("--edge-boost", edgeContrastBoost(zoom).toFixed(3));
  }, []);

  useEffect(() => {
    applyZoomVars(rf.getZoom());
  }, [applyZoomVars, rf, fitKey]);

  // After a stage opens or closes: focus what the reader acted on and pan just enough, once per cue.
  const appliedCue = useRef(0);
  useEffect(() => {
    if (cue === null || cue.nonce === appliedCue.current) return;
    const raf = requestAnimationFrame(() => {
      appliedCue.current = cue.nonce;
      if (cue.focusId !== null) {
        paneRef.current
          ?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(cue.focusId)}"] [role="button"]`)
          ?.focus({ preventScroll: true });
      }
      if (cue.reveal !== null) panToShow(cue.reveal);
    });
    return () => cancelAnimationFrame(raf);
  }, [cue, panToShow]);

  // The reader's selection, with the detour it opened, stays in view beside the Step panel: one pan per selection.
  const selection = useMemo(() => {
    if (!dimUnrelated || selectedId === null) return null;
    const parts = scene.boxes.filter((b) => b.id === selectedId || b.detourOf === selectedId);
    if (parts.length === 0) return null;
    const x = Math.min(...parts.map((b) => b.x));
    const y = Math.min(...parts.map((b) => b.y));
    const w = Math.max(...parts.map((b) => b.x + b.w)) - x;
    const h = Math.max(...parts.map((b) => b.y + b.h)) - y;
    const key = `${selectedId}@${Math.round(x)},${Math.round(y)},${Math.round(w)},${Math.round(h)}|${insetRightPx ?? 0}`;
    return { key, x, y, w, h };
  }, [dimUnrelated, insetRightPx, scene, selectedId]);
  const shownSelection = useRef("");
  useEffect(() => {
    keepSelectionInView.current = () => selection !== null && (panToShow(selection), true);
    if (selection === null || selection.key === shownSelection.current) return;
    const raf = requestAnimationFrame(() => {
      shownSelection.current = selection.key;
      panToShow(selection);
    });
    return () => cancelAnimationFrame(raf);
  }, [selection, panToShow]);

  // While cards glide to new places, the lines between them fade out and back.
  const placed = placementKey(scene);
  const [observedPlacement, setObservedPlacement] = useState(placed);
  const [moving, setMoving] = useState(false);
  if (observedPlacement !== placed) {
    setObservedPlacement(placed);
    // Before the entrance nothing is drawn, so nothing moves.
    if (isEntered) setMoving(true);
  }
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(() => setMoving(false), 320);
    return () => window.clearTimeout(timer);
  }, [moving]);

  const handleMove = useCallback(
    (_: unknown, viewport: { zoom: number }) => applyZoomVars(viewport.zoom),
    [applyZoomVars],
  );

  return (
    // The pane insets its top, where a host's heading may cover empty canvas, never a Step; zoom vars follow moves.
    <div
      className="size-full min-h-0 pt-[54px] pb-3 [--arrive-delay:0ms] [--edge-boost:1] [--enter-delay:0ms] [--len:0px] [--zoom-comp:1]"
      ref={paneRef}
    >
      <ReactFlow
        data-canvas-flow="true"
        data-relayout={moving ? "true" : undefined}
        data-pending={isEntered && !isSettled ? "true" : undefined}
        data-entering={delays !== null ? "true" : undefined}
        nodes={nodes}
        edges={edges}
        nodeTypes={NODE_TYPES}
        edgeTypes={EDGE_TYPES}
        minZoom={MIN_ZOOM}
        maxZoom={MAX_ZOOM}
        nodesDraggable={false}
        nodesConnectable={false}
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
        onMove={handleMove}
        onMoveEnd={(event) => {
          // Only the reader's own pan or zoom carries an event; it takes the view over from the camera.
          if (!event) return;
          setFitted(false);
          setOwner("reader");
        }}
        onPaneClick={() => {
          setSelectedLinkId(null);
          onSelect(null);
        }}
        onEdgeClick={(_, edge) => {
          setSelectedLinkId(edge.id);
          onSelect(null);
        }}
      >
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} />
        {isEntered ? null : (
          <p
            className="pointer-events-none absolute inset-x-0 top-1/2 z-5 m-0 animate-fade-in-late text-center text-[13px] text-ink-faint"
            role="status"
          >
            Drawing the flow…
          </p>
        )}
        <ZoomControls
          fitted={fitted}
          onFit={() => {
            setOwner("fit");
            fitWhole();
          }}
          onZoomed={() => {
            setFitted(false);
            setOwner("reader");
          }}
          insetRightPx={insetRightPx}
          controls={controls}
        />
      </ReactFlow>
    </div>
  );
}

export function Stage(props: StageProps): JSX.Element {
  return (
    // React Flow reads the bare --xy-* names; without the pattern colour the dots keep their light default.
    <div className="min-h-0 w-full flex-auto [--xy-background-color:var(--color-canvas)] [--xy-background-pattern-color:var(--color-line-soft)]">
      <ReactFlowProvider>
        <Inner {...props} />
      </ReactFlowProvider>
    </div>
  );
}
