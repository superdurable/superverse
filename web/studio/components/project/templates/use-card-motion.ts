// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useLayoutEffect, useRef } from "react";

const MOVE = { duration: 240, easing: "cubic-bezier(0.2, 0, 0, 1)" } as const;
const ENTER = { duration: 180, easing: "ease-out" } as const;

/**
 * Moves each card of a grid that reorders from where it was to where it now is, and fades in a card that
 * joins, so the cards move rather than jump; nothing moves under prefers-reduced-motion. Positions are
 * read relative to the grid, so a scroll between two orders moves nothing. `order` names the shown cards
 * in order; the returned function gives the ref of the card with an ID.
 */
export function useCardMotion(order: string): (id: string) => (card: HTMLElement | null) => void {
  const cards = useRef(new Map<string, HTMLElement>());
  const positions = useRef(new Map<string, { left: number; top: number }>());
  useLayoutEffect(() => {
    const moves = !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const shownBefore = positions.current.size > 0;
    const next = new Map<string, { left: number; top: number }>();
    for (const [id, card] of cards.current) {
      const position = { left: card.offsetLeft, top: card.offsetTop };
      next.set(id, position);
      const before = positions.current.get(id);
      if (!moves) continue;
      if (before && (before.left !== position.left || before.top !== position.top))
        card.animate(
          [
            { transform: `translate(${before.left - position.left}px, ${before.top - position.top}px)` },
            { transform: "none" },
          ],
          MOVE,
        );
      else if (!before && shownBefore)
        card.animate(
          [
            { opacity: 0, transform: "scale(0.98)" },
            { opacity: 1, transform: "none" },
          ],
          ENTER,
        );
    }
    positions.current = next;
  }, [order]);
  return useCallback(
    (id: string) => (card: HTMLElement | null) => {
      if (card) cards.current.set(id, card);
      else cards.current.delete(id);
    },
    [],
  );
}
