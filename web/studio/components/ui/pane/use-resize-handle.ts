// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useRef, type KeyboardEvent, type PointerEvent } from "react";

/** How one separator answers its gestures, in the separator's own unit, bounds and storage. */
interface ResizeGestures<Drag extends object> {
  /** Pixels one arrow key moves the separator, and with Shift held. */
  keyStep: { normal: number; shift: number };
  /** Moves the separator by `pixels`, negative toward the start, and keeps the result. */
  moveBy(pixels: number): void;
  /** Home and End move to the bounds; a separator without this ignores those keys. */
  moveToBound?(bound: "minimum" | "maximum"): void;
  /** The state a drag starts from, or null when the separator cannot be dragged now. */
  startDrag(event: PointerEvent<HTMLElement>): Drag | null;
  drag(event: PointerEvent<HTMLElement>, start: Drag): void;
  /** `released` is false when the gesture was cancelled or lost its pointer capture. */
  endDrag(event: PointerEvent<HTMLElement>, start: Drag, released: boolean): void;
}

/**
 * The gestures every resize separator shares: a drag with the primary button that holds its pointer
 * until the pointer is released, the gesture is cancelled or the capture is lost, and arrow keys
 * that move by a small step, or a large one with Shift. Returns the separator's event handlers.
 */
export function useResizeHandle<Drag extends object>(gestures: ResizeGestures<Drag>) {
  const active = useRef<{ pointerId: number; start: Drag } | null>(null);

  const finish = (event: PointerEvent<HTMLElement>, released: boolean) => {
    const drag = active.current;
    if (drag?.pointerId !== event.pointerId) return;
    active.current = null;
    try {
      event.currentTarget.releasePointerCapture(event.pointerId);
    } catch {
      // Capture was already released with the pointer.
    }
    gestures.endDrag(event, drag.start, released);
  };

  return {
    onPointerDown(event: PointerEvent<HTMLElement>) {
      if (event.button !== 0) return;
      const start = gestures.startDrag(event);
      if (start === null) return;
      active.current = { pointerId: event.pointerId, start };
      try {
        event.currentTarget.setPointerCapture(event.pointerId);
      } catch {
        // The pointer was already released; moves still arrive while captured elsewhere.
      }
    },
    onPointerMove(event: PointerEvent<HTMLElement>) {
      const drag = active.current;
      if (drag?.pointerId === event.pointerId) gestures.drag(event, drag.start);
    },
    onPointerUp: (event: PointerEvent<HTMLElement>) => finish(event, true),
    // Both, because a cancelled gesture and a lost capture are different events and either one
    // would otherwise leave the separator stuck mid-drag.
    onPointerCancel: (event: PointerEvent<HTMLElement>) => finish(event, false),
    onLostPointerCapture: (event: PointerEvent<HTMLElement>) => finish(event, false),
    onKeyDown(event: KeyboardEvent<HTMLElement>) {
      if ((event.key === "Home" || event.key === "End") && gestures.moveToBound) {
        event.preventDefault();
        gestures.moveToBound(event.key === "Home" ? "minimum" : "maximum");
        return;
      }
      const direction = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
      if (direction === 0) return;
      event.preventDefault();
      gestures.moveBy(direction * (event.shiftKey ? gestures.keyStep.shift : gestures.keyStep.normal));
    },
  };
}
