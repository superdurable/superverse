// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { cx } from "./cx";

/** Room kept between a card and the window's edges, and between the card and its anchor. */
const EDGE = 8;
const GAP = 4;
/** What marks a popover's card, so a press inside a card that renders outside its anchor counts as inside. */
const CARD_ATTRIBUTE = "data-popover-card";

/**
 * The open state of one popover around `anchorRef`, the element that holds its trigger and card: a
 * pointer press outside the anchor, or Escape, closes it. Escape is taken in the capture phase and
 * marked handled, so a page-wide Escape (such as Stop) does not also fire while a popover is open.
 */
export function usePopover(anchorRef: RefObject<HTMLElement | null>) {
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const toggle = useCallback(() => setOpen((value) => !value), []);

  useEffect(() => {
    if (!open) return;
    const pressOutside = (event: PointerEvent) => {
      const target = event.target as Element;
      if (!anchorRef.current?.contains(target) && !target.closest?.(`[${CARD_ATTRIBUTE}]`)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
    };
    document.addEventListener("pointerdown", pressOutside);
    window.addEventListener("keydown", escape, true);
    return () => {
      document.removeEventListener("pointerdown", pressOutside);
      window.removeEventListener("keydown", escape, true);
    };
  }, [anchorRef, open]);

  return { open, setOpen, close, toggle };
}

/** The card's surface, shared by every popover, menu and listbox. */
const CARD = cx(
  "fixed top-(--popover-y) left-(--popover-x) z-50 max-h-(--popover-max-h) overflow-y-auto rounded-[10px] bg-panel p-1.5 text-[13px] shadow-(--shadow-lift) focus:outline-none",
);

/**
 * The floating card of a popover. It renders at the top of the page, not inside its anchor, so no
 * scrolling or clipping box around the trigger can hide it; inside a modal dialog it renders in the
 * dialog, which stays the top layer. It opens below `anchorRef`, or above it when the room below is
 * short and there is more above, aligned to its start or end edge, moved back inside the window when it
 * would cross an edge, and capped to the window's height and `maxHeight`. It follows its anchor when the
 * window resizes or scrolls. `fitAnchor` makes it at least as wide as its anchor and as wide as its
 * content up to 360px, as a dropdown's list; `autoFocus` focuses it once placed, without scrolling.
 */
export function PopoverCard({
  anchorRef,
  cardRef,
  align = "start",
  role = "dialog",
  fitAnchor = false,
  autoFocus = false,
  maxHeight,
  className,
  children,
  label,
  ...rest
}: {
  anchorRef: RefObject<HTMLElement | null>;
  cardRef?: RefObject<HTMLDivElement | null>;
  align?: "start" | "end";
  role?: "dialog" | "listbox" | "menu";
  fitAnchor?: boolean;
  autoFocus?: boolean;
  maxHeight?: number;
  className?: string;
  children: ReactNode;
  label: string;
} & Pick<
  HTMLAttributes<HTMLDivElement>,
  "id" | "tabIndex" | "onKeyDown" | "onFocus" | "aria-activedescendant" | "aria-describedby"
>) {
  const own = useRef<HTMLDivElement>(null);
  const card = cardRef ?? own;
  const [place, setPlace] = useState<{
    x: number;
    y: number;
    maxHeight: number;
    anchorWidth: number;
    host: Element;
  } | null>(null);
  useLayoutEffect(() => {
    const placeCard = () => {
      const anchorElement = anchorRef.current;
      const anchor = anchorElement?.getBoundingClientRect();
      const element = card.current;
      if (!anchorElement || !anchor || !element) return;
      const width = element.offsetWidth;
      const wanted = align === "start" ? anchor.left : anchor.right - width;
      const x = Math.max(EDGE, Math.min(wanted, window.innerWidth - width - EDGE));
      const cap = maxHeight ?? Infinity;
      const below = window.innerHeight - anchor.bottom - GAP - EDGE;
      const above = anchor.top - GAP - EDGE;
      const natural = Math.min(element.scrollHeight, cap);
      const up = natural > below && above > below;
      const room = Math.max(120, Math.min(up ? above : below, cap));
      const y = up ? Math.max(EDGE, anchor.top - GAP - Math.min(natural, room)) : anchor.bottom + GAP;
      const host = anchorElement.closest("dialog[open]") ?? document.body;
      setPlace({ x, y, maxHeight: room, anchorWidth: anchor.width, host });
    };
    placeCard();
    window.addEventListener("resize", placeCard);
    window.addEventListener("scroll", placeCard, true);
    return () => {
      window.removeEventListener("resize", placeCard);
      window.removeEventListener("scroll", placeCard, true);
    };
  }, [align, anchorRef, card, maxHeight]);
  const placed = place !== null;
  useLayoutEffect(() => {
    if (placed && autoFocus) card.current?.focus({ preventScroll: true });
  }, [autoFocus, card, placed]);
  const position = place && {
    "--popover-x": `${Math.round(place.x)}px`,
    "--popover-y": `${Math.round(place.y)}px`,
    "--popover-max-h": `${Math.round(place.maxHeight)}px`,
    "--popover-anchor-w": `${Math.round(place.anchorWidth)}px`,
  };
  return createPortal(
    <div
      ref={card}
      role={role}
      aria-label={label}
      {...rest}
      {...{ [CARD_ATTRIBUTE]: "" }}
      style={(position ?? undefined) as CSSProperties | undefined}
      className={cx(
        CARD,
        fitAnchor ? "w-max max-w-90 min-w-[max(15rem,var(--popover-anchor-w))]" : "min-w-60",
        !place && "invisible",
        className,
      )}
    >
      {children}
    </div>,
    place?.host ?? document.body,
  );
}

/** One row of a popover's list: a link or button the caller renders with these classes. */
export const POPOVER_ITEM = cx(
  "flex w-full items-center gap-2 rounded-md px-2.25 py-1.75 text-left text-ink hover:bg-hover focus-visible:bg-hover focus-visible:outline-none aria-[current=page]:bg-accent-wash",
);
