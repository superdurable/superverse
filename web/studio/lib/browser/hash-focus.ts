// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef } from "react";

const FOCUSABLE = "button, input, select, textarea, a[href], [tabindex]:not([tabindex='-1'])";
/** How long a jump waits for its target to render, as after a read the page waits for. */
const WAIT_MS = 5_000;
/**
 * The event a jump sends before each look for its target, so a folded container that holds the anchor,
 * such as a Setup row, unfolds and focuses it itself; a container that does so cancels the event.
 */
const ANCHOR_REQUEST = "studio:anchor-request";

const visible = (element: Element) => element.checkVisibility();

/** The first control inside `element` that shows, or `element` itself when it is one. */
function visibleControl(element: Element | null): HTMLElement | null {
  if (!element) return null;
  if (element.matches(FOCUSABLE) && visible(element)) return element as HTMLElement;
  return [...element.querySelectorAll<HTMLElement>(FOCUSABLE)].find(visible) ?? null;
}

/**
 * Scrolls the element `id` into view once it renders and focuses its first visible control, or its row's,
 * so a link to a missing item lands the keyboard on that item. Answers a cancel.
 */
export function focusAnchor(id: string): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const deadline = Date.now() + WAIT_MS;
  const attempt = () => {
    // A container that holds the anchor takes the jump over.
    if (!window.dispatchEvent(new CustomEvent(ANCHOR_REQUEST, { detail: id, cancelable: true }))) return;
    const target = document.getElementById(id);
    if (!target) {
      if (Date.now() < deadline) timer = setTimeout(attempt, 100);
      return;
    }
    target.scrollIntoView({ block: "center" });
    // A marker without a control of its own, as for a connection row's further names, focuses its row's.
    (visibleControl(target) ?? visibleControl(target.parentElement) ?? target).focus();
  };
  // A client navigation renders the target after this click.
  timer = setTimeout(attempt, 0);
  return () => {
    if (timer) clearTimeout(timer);
  };
}

/**
 * Focuses the anchor `id` inside `container`: the shown element of those named `id` or marked
 * `data-anchor`, as when one setting has a control in the row and another in its unfolded body, else
 * the container itself when it is the anchor.
 */
export function focusAnchorWithin(container: HTMLElement, id: string) {
  const escaped = CSS.escape(id);
  const inside = [...container.querySelectorAll(`#${escaped}, [data-anchor="${escaped}"]`)].filter(visible);
  const target = inside.find((element) => visibleControl(element)) ?? (container.id === id ? container : null);
  if (!target) return;
  target.scrollIntoView({ block: "center" });
  visibleControl(target)?.focus();
}

/**
 * Calls `open` with an anchor that `holds` names whenever a jump asks for it, and once for the URL's hash
 * when the container mounts, so a folded container unfolds and focuses the anchor itself.
 */
export function useAnchorRequests(holds: (id: string) => boolean, open: (id: string) => void) {
  const latest = useRef({ holds, open });
  useEffect(() => {
    latest.current = { holds, open };
  });
  useEffect(() => {
    const hash = decodeURIComponent(window.location.hash.slice(1));
    if (hash && latest.current.holds(hash)) latest.current.open(hash);
    const listener = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      if (!latest.current.holds(id)) return;
      event.preventDefault();
      latest.current.open(id);
    };
    window.addEventListener(ANCHOR_REQUEST, listener);
    return () => window.removeEventListener(ANCHOR_REQUEST, listener);
  }, []);
}

/** Jumps to the element the URL's hash names when the page opens with one. */
export function useHashFocus() {
  useEffect(() => {
    const id = decodeURIComponent(window.location.hash.slice(1));
    return id ? focusAnchor(id) : undefined;
  }, []);
}
