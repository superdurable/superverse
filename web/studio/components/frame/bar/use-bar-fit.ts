// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * How the bar fits its width, from roomiest: every label; only the open tab's label (and no Live
 * chip); icons alone; and last, tight: a shortened process picker, Publish as its icon, and the left
 * block at its content's width instead of the chat column's.
 */
const BAR_FITS = ["labels", "open", "icons", "tight"] as const;
export type BarFit = (typeof BAR_FITS)[number];

function horizontalPadding(element: HTMLElement): number {
  const style = getComputedStyle(element);
  return parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
}

function width(element: HTMLElement | null): number {
  return element?.getBoundingClientRect().width ?? 0;
}

/** The bar's parts whose widths change with the fit. */
type BarParts = Record<
  "header" | "left" | "nav" | "before" | "tabs" | "after" | "actions",
  RefObject<HTMLElement | null>
>;

/** Steps `setStep` one fit tighter when the parts overflow the bar, or one roomier when they would fit. */
function fitBar(parts: BarParts, needed: number[], setStep: (next: (current: number) => number) => void) {
  const header = parts.header.current;
  const nav = parts.nav.current;
  const tabs = parts.tabs.current;
  if (!header || !nav || !tabs) return;
  const navGap = parseFloat(getComputedStyle(nav).columnGap) || 0;
  const navWidth =
    horizontalPadding(nav) + width(parts.before.current) + tabs.scrollWidth + width(parts.after.current) + 2 * navGap;
  const content = width(parts.left.current) + navWidth + width(parts.actions.current);
  const room = header.clientWidth - horizontalPadding(header);
  setStep((current) => {
    if (content > room + 0.5 && current < BAR_FITS.length - 1) {
      needed[current] = content;
      return current + 1;
    }
    if (current > 0 && room >= (needed[current - 1] ?? Infinity)) return current - 1;
    return current;
  });
}

/**
 * The roomiest fit at which the whole bar fits. Every part that changes with the fit is measured
 * together: the left block, the picker, the tabs at their full width (they scroll only past the last
 * fit), the tool tabs with +, and the actions. When they are wider than the bar the next fit applies;
 * when the bar grows back to the width a roomier fit needed when it was left, the bar returns to it.
 * Measured whenever the bar or one of its parts changes size.
 */
export function useBarFit({ header, left, nav, before, tabs, after, actions }: BarParts): BarFit {
  const [step, setStep] = useState(0);
  /** The width the bar needed at each fit when it left it. */
  const needed = useRef<number[]>([]);
  // A new fit can change only what is inside the clipped tabs, which resizes no part: measure again.
  useLayoutEffect(
    () => fitBar({ header, left, nav, before, tabs, after, actions }, needed.current, setStep),
    [actions, after, before, header, left, nav, step, tabs],
  );
  // Any part's size changes when a label, a pin, a badge or the fit itself changes, and the bar's when
  // the window or the chat column does; each change measures again.
  useLayoutEffect(() => {
    const parts = { header, left, nav, before, tabs, after, actions };
    const observer = new ResizeObserver(() => fitBar(parts, needed.current, setStep));
    for (const part of Object.values(parts)) if (part.current) observer.observe(part.current);
    return () => observer.disconnect();
  }, [actions, after, before, header, left, nav, tabs]);
  return BAR_FITS[step];
}
