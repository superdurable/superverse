// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type RefObject } from "react";
import { usePopover } from "@/components/ui/Popover";

/** One row the keys move over: its element ID, its text for typeahead, and whether it can be chosen. */
export interface KeyedRow {
  id: string;
  text: string;
  disabled?: boolean;
}

/** How long typed letters keep adding to one typeahead search. */
const TYPEAHEAD_MS = 600;

/** Scrolls only the card, never the page, so `id` is in view. */
function reveal(card: HTMLElement | null, id: string | null) {
  const row = id ? card?.querySelector<HTMLElement>(`[id="${CSS.escape(id)}"]`) : null;
  if (!card || !row) return;
  const top = row.offsetTop;
  const bottom = top + row.offsetHeight;
  if (top < card.scrollTop) card.scrollTop = top - 6;
  else if (bottom > card.scrollTop + card.clientHeight) card.scrollTop = bottom - card.clientHeight + 6;
}

/**
 * The open state and keys of a dropdown's listbox or an action menu, whose card holds focus while open
 * and names its active row with aria-activedescendant. ArrowDown, ArrowUp, Enter and Space on the closed
 * trigger open it. In the card, the arrows move over the rows that can be chosen, Home and End go to
 * the first and last, Enter or Space chooses, letters jump by typeahead, Tab closes and moves on, and
 * Escape (taken by usePopover) closes. Focus returns to the trigger when the card closes with focus in it.
 */
export function useListboxKeys<R extends KeyedRow>({
  anchorRef,
  triggerRef,
  cardRef,
  rows,
  selectedId = null,
  onChoose,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  triggerRef: RefObject<HTMLElement | null>;
  cardRef: RefObject<HTMLElement | null>;
  rows: readonly R[];
  /** The row a listbox opens on; a menu opens on its first row. */
  selectedId?: string | null;
  onChoose: (row: R) => void;
}) {
  const popover = usePopover(anchorRef);
  const [activeId, setActiveId] = useState<string | null>(null);
  const typed = useRef({ text: "", at: -Infinity });
  const enabled = rows.filter((row) => !row.disabled);

  // A card that closed while it held focus, or left focus nowhere, hands focus back to its trigger.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (popover.open) {
      wasOpen.current = true;
      return;
    }
    if (!wasOpen.current) return;
    wasOpen.current = false;
    const focused = document.activeElement;
    if (!focused || focused === document.body) triggerRef.current?.focus({ preventScroll: true });
  }, [popover.open, triggerRef]);

  const selected = enabled.find((row) => row.id === selectedId);
  // The keys open on the chosen row or an end; a pointer opens with nothing highlighted until it moves.
  const openAt = (end: "first" | "last" | "pointer") => {
    setActiveId(end === "pointer" ? null : ((selected ?? (end === "last" ? enabled.at(-1) : enabled[0]))?.id ?? null));
    popover.setOpen(true);
  };
  const move = (row: R | undefined) => {
    if (!row) return;
    setActiveId(row.id);
    reveal(cardRef.current, row.id);
  };
  const choose = (row: R) => {
    if (row.disabled) return;
    popover.close();
    onChoose(row);
  };

  const typeahead = (key: string, timeStamp: number, index: number) => {
    const state = typed.current;
    state.text = timeStamp - state.at < TYPEAHEAD_MS ? state.text + key.toLowerCase() : key.toLowerCase();
    state.at = timeStamp;
    // Repeating one letter cycles through the rows that start with it.
    const repeated = [...state.text].every((letter) => letter === state.text[0]);
    const needle = repeated ? state.text[0] : state.text;
    const from = repeated ? index + 1 : Math.max(index, 0);
    const order = [...enabled.slice(from), ...enabled.slice(0, from)];
    move(order.find((row) => row.text.trim().toLowerCase().startsWith(needle)));
  };

  return {
    open: popover.open,
    close: popover.close,
    activeId,
    setActiveId,
    choose,
    /** The trigger's click, from a pointer or from Enter or Space (detail 0): opens, or closes. */
    toggle: (event: MouseEvent) => (popover.open ? popover.close() : openAt(event.detail === 0 ? "first" : "pointer")),
    onTriggerKeyDown: (event: KeyboardEvent) => {
      if (popover.open || (event.key !== "ArrowDown" && event.key !== "ArrowUp")) return;
      event.preventDefault();
      openAt(event.key === "ArrowUp" ? "last" : "first");
    },
    /** Shows the active row once the card has focus, which it takes after it is placed. */
    onCardFocus: () => reveal(cardRef.current, activeId ?? selectedId),
    onCardKeyDown: (event: KeyboardEvent) => {
      const index = enabled.findIndex((row) => row.id === activeId);
      switch (event.key) {
        case "ArrowDown":
          move(index < 0 ? (selected ?? enabled[0]) : enabled[Math.min(index + 1, enabled.length - 1)]);
          break;
        case "ArrowUp":
          move(index < 0 ? (selected ?? enabled.at(-1)) : enabled[Math.max(index - 1, 0)]);
          break;
        case "Home":
          move(enabled[0]);
          break;
        case "End":
          move(enabled.at(-1));
          break;
        case "Enter":
        case " ": {
          const row = enabled[index];
          if (row) choose(row);
          break;
        }
        case "Tab":
          // Focus goes back to the trigger first, so Tab moves on from there.
          popover.close();
          triggerRef.current?.focus({ preventScroll: true });
          return;
        default:
          if (event.key.length !== 1 || event.altKey || event.ctrlKey || event.metaKey) return;
          typeahead(event.key, event.timeStamp, index);
      }
      // Handled keys do nothing else, so Enter never also clicks the trigger that takes focus back.
      event.preventDefault();
    },
  };
}
