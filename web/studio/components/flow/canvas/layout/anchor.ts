// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Direction, Scene } from "./scene";

/**
 * Keeping the reader's place when a stage opens or closes: the new Scene is moved so the thing they
 * acted on stays where it was on screen, instead of the view refitting around a different drawing.
 */

/** A card by its box id, or an open stage by its group's band. */
export type AnchorRef = { box: string } | { group: string };

type Point = { x: number; y: number };

type Rect = { x: number; y: number; w: number; h: number };

/**
 * The point that stays put: the middle of the leading edge, top-centre when the Flow reads top-down
 * and left-middle when it reads left-right. A stage then opens evenly to both sides of where its card
 * was, and the cards that line up with it above keep their place, where pinning a corner would push
 * them sideways.
 */
function edgeMiddle(r: Rect, direction: Direction): Point {
  return direction === "lr" ? { x: r.x, y: r.y + r.h / 2 } : { x: r.x + r.w / 2, y: r.y };
}

/** A card's or an open stage's box, in Scene coordinates. */
export function rectOf(scene: Scene, ref: AnchorRef): Rect | undefined {
  if ("box" in ref) {
    const box = scene.boxes.find((b) => b.id === ref.box);
    return box ? { x: box.x, y: box.y, w: box.w, h: box.h } : undefined;
  }
  return groupRect(scene, ref.group);
}

/** Where a card or an open stage is pinned while the drawing changes around it. */
export function anchorOf(scene: Scene, ref: AnchorRef, direction: Direction): Point | undefined {
  const rect = rectOf(scene, ref);
  return rect ? edgeMiddle(rect, direction) : undefined;
}

/** The rectangle a group's bands cover, to bring an opened stage into view. */
function groupRect(scene: Scene, groupId: string): Rect | undefined {
  const bands = scene.bands.filter((b) => b.groupId === groupId);
  if (bands.length === 0) return undefined;
  const x = Math.min(...bands.map((b) => b.x));
  const y = Math.min(...bands.map((b) => b.y));
  return {
    x,
    y,
    w: Math.max(...bands.map((b) => b.x + b.w)) - x,
    h: Math.max(...bands.map((b) => b.y + b.h)) - y,
  };
}

/** Every coordinate of a Scene moved by an offset; its size grows to keep the moved drawing inside. */
export function shiftScene(scene: Scene, offset: Point): Scene {
  if (offset.x === 0 && offset.y === 0) return scene;
  const move = <T extends Point>(p: T): T => ({ ...p, x: p.x + offset.x, y: p.y + offset.y });
  return {
    ...scene,
    boxes: scene.boxes.map(move),
    bands: scene.bands.map(move),
    links: scene.links.map((link) => ({
      ...link,
      points: link.points.map(move),
      ...(link.label?.at ? { label: { ...link.label, at: move(link.label.at) } } : {}),
    })),
    width: scene.width + Math.max(0, offset.x),
    height: scene.height + Math.max(0, offset.y),
  };
}

/**
 * A cheap fingerprint of where every card sits, so a re-read that moved nothing animates nothing. A
 * failure detour opens beside its Step without moving anything, so its cards are not counted.
 */
export function placementKey(scene: Scene): string {
  return scene.boxes
    .filter((b) => b.detourOf === undefined)
    .map((b) => `${b.id}@${Math.round(b.x)},${Math.round(b.y)}`)
    .join("|");
}
