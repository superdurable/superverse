// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { FlowModel } from "@/components/flow/canvas/model/flow-model";
import type { RunOverlay } from "@/components/flow/canvas/model/run-overlay";
import type { StepGroup } from "@/components/flow/canvas/model/step-groups";
import { anchorOf, rectOf, shiftScene, type AnchorRef } from "@/components/flow/canvas/layout/anchor";
import { isStageGroup } from "@/components/flow/canvas/layout/group-bands";
import { layoutFlow } from "@/components/flow/canvas/layout/layout-flow";
import { stageId } from "@/components/flow/canvas/layout/stage-card";
import type { Direction, Scene } from "@/components/flow/canvas/layout/scene";

/**
 * The canvas's Scene and the stages drawn open. Opening or closing one stage keeps the reader's place:
 * the new drawing is moved so the stage's leading edge stays where it was on screen, focus moves into
 * it (or back to its card), the view pans only as far as it takes to show it, and a screen reader
 * hears what changed. Closing every stage (Collapsed) or opening every stage (Expanded) starts the
 * view again, so Collapsed is one click back to the view the canvas opened on.
 */

/** Whether every stage is closed, every stage is open, or some of each. */
export type StageState = "collapsed" | "expanded" | "mixed";

type Point = { x: number; y: number };

/** What the reader just did to a stage, and where it and every other card were when they did it. */
interface Anchor {
  from: Point;
  to: AnchorRef;
  /** The group whose Steps take focus once it opens; null when a stage closes onto its card. */
  opened: StepGroup | null;
  message: string;
}

/** Applied once the new Scene is drawn: what to focus, what to bring into view, what to announce. */
export interface StageCue {
  focusId: string | null;
  reveal: { x: number; y: number; w: number; h: number } | null;
  message: string;
  nonce: number;
}

interface Laid {
  scene: Scene;
  /** The direction and the reset it was laid out for; a new one refits the view. */
  view: string;
  offset: Point;
  /** What it was laid out from; the drawing has settled while these are still the inputs. */
  inputs: readonly unknown[];
}

export function useStageScene({
  flow,
  groups,
  direction,
  run,
}: {
  flow: FlowModel | null;
  groups: StepGroup[];
  direction: Direction;
  run: RunOverlay | null;
}) {
  const [request, setRequest] = useState<{
    open: ReadonlySet<string>;
    anchor: Anchor | null;
    nonce: number;
    /** Bumped by Collapsed and Expanded, which start the view again. */
    reset: number;
  }>(() => ({ open: new Set(), anchor: null, nonce: 0, reset: 0 }));
  /** The groups that are stages: a run or a choice of two or more of the Flow's Steps. */
  const stageIds = useMemo(
    () => (flow ? groups.filter((g) => isStageGroup(flow, g)).map((g) => g.id) : []),
    [flow, groups],
  );
  const [laid, setLaid] = useState<Laid | null>(null);
  const [cue, setCue] = useState<StageCue | null>(null);
  // Mirrors for the asynchronous layout callback, written only after render.
  const last = useRef<Laid | null>(null);
  const applied = useRef(0);
  useEffect(() => {
    last.current = laid;
  }, [laid]);

  // ELK lays out asynchronously; the previous Scene stays on screen until the next one is ready.
  useEffect(() => {
    if (!flow) return undefined;
    let current = true;
    const view = `${direction}|${request.reset}`;
    void layoutFlow(flow, { direction, run, groups, openStages: request.open }).then((next) => {
      if (!current) return;
      const prior = last.current;
      let offset = prior !== null && prior.view === view ? prior.offset : { x: 0, y: 0 };
      const anchor = request.nonce !== applied.current ? request.anchor : null;
      applied.current = request.nonce;
      const at = anchor ? anchorOf(next, anchor.to, direction) : undefined;
      if (anchor && at) offset = { x: anchor.from.x - at.x, y: anchor.from.y - at.y };
      setLaid({ scene: next, view, offset, inputs: [direction, flow, groups, request, run] });
      if (anchor) {
        const shown = shiftScene(next, offset);
        const opened = anchor.opened;
        const first = opened
          ? shown.boxes
              .filter((box) => opened.stepIds.includes(box.id))
              .sort((a, b) => (direction === "lr" ? a.x - b.x || a.y - b.y : a.y - b.y || a.x - b.x))[0]
          : undefined;
        setCue({
          focusId: opened ? (first?.id ?? null) : "box" in anchor.to ? anchor.to.box : null,
          reveal: rectOf(shown, anchor.to) ?? null,
          message: anchor.message,
          nonce: request.nonce,
        });
      }
    });
    return () => {
      current = false;
    };
  }, [direction, flow, groups, request, run]);

  const scene = useMemo(() => (laid ? shiftScene(laid.scene, laid.offset) : null), [laid]);
  const inputs = [direction, flow, groups, request, run];
  /** False while a layout is still on its way, so the canvas keeps the last one on screen. */
  const isSettled = laid !== null && laid.inputs.every((input, i) => input === inputs[i]);

  /** Opens a stage in place from its card, growing from the card's leading edge. */
  const openStage = useCallback(
    (boxId: string) => {
      const group = groups.find((g) => stageId(g.id) === boxId);
      if (!group || scene === null) return;
      const from = anchorOf(scene, { box: boxId }, direction);
      const count = group.stepIds.length;
      setRequest((prior) => ({
        ...prior,
        open: new Set([...prior.open, group.id]),
        anchor: from
          ? { from, to: { group: group.id }, opened: group, message: `${group.label} opened, ${count} steps` }
          : null,
        nonce: prior.nonce + 1,
      }));
    },
    [direction, groups, scene],
  );

  /** Closes an opened stage back into its card, at the box's leading edge. */
  const collapseStage = useCallback(
    (groupId: string) => {
      const group = groups.find((g) => g.id === groupId);
      if (!group || scene === null) return;
      const from = anchorOf(scene, { group: groupId }, direction);
      setRequest((prior) => ({
        ...prior,
        open: new Set([...prior.open].filter((id) => id !== groupId)),
        anchor: from ? { from, to: { box: stageId(groupId) }, opened: null, message: `${group.label} closed` } : null,
        nonce: prior.nonce + 1,
      }));
    },
    [direction, groups, scene],
  );

  /** Opens a stage for the reader, such as the one a run is waiting in, without moving anything. */
  const revealStage = useCallback((groupId: string) => {
    setRequest((prior) =>
      prior.open.has(groupId)
        ? prior
        : { ...prior, open: new Set([...prior.open, groupId]), anchor: null, nonce: prior.nonce + 1 },
    );
  }, []);

  /** Closes every stage and starts the view again: the view the canvas opened on. */
  const collapseAll = useCallback(() => {
    setRequest((prior) => ({ open: new Set(), anchor: null, nonce: prior.nonce + 1, reset: prior.reset + 1 }));
  }, []);

  /** Opens every stage and starts the view again. */
  const expandAll = useCallback(() => {
    setRequest((prior) => ({ open: new Set(stageIds), anchor: null, nonce: prior.nonce + 1, reset: prior.reset + 1 }));
  }, [stageIds]);

  const openCount = stageIds.filter((id) => request.open.has(id)).length;
  const state: StageState = openCount === 0 ? "collapsed" : openCount === stageIds.length ? "expanded" : "mixed";

  return {
    scene,
    view: laid?.view ?? "",
    isSettled,
    openStages: request.open,
    hasStages: stageIds.length > 0,
    state,
    openStage,
    collapseStage,
    revealStage,
    collapseAll,
    expandAll,
    cue,
  };
}
