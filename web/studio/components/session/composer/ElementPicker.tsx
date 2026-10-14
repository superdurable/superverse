// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect } from "react";
import { useComposerDraft } from "@/lib/session/ComposerDraftProvider";

/** What a picked element is about: the scopes around it, outermost first, then its own words. */
function aboutOf(element: Element): string {
  const parts = [element.getAttribute("data-pick") ?? ""];
  for (
    let node = element.parentElement?.closest("[data-pick-scope]");
    node;
    node = node.parentElement?.closest("[data-pick-scope]")
  )
    parts.unshift(node.getAttribute("data-pick-scope") ?? "");
  return parts.filter(Boolean).join(" ▸ ");
}

/**
 * The composer's element picker at work: while it is on, a click on anything that names itself with
 * `data-pick` (a Step, a run, an Inbox item, a Process) hands it to the composer as the chip,
 * instead of doing what the click would do. Escape or Cancel stops picking.
 */
export function ElementPicker() {
  const draft = useComposerDraft();
  const picking = draft?.picking ?? false;
  const setPicking = draft?.setPicking;
  const propose = draft?.propose;
  useEffect(() => {
    if (!picking || !setPicking || !propose) return;
    const onClick = (event: MouseEvent) => {
      const target = (event.target as Element | null)?.closest("[data-pick]");
      if (!target || target.closest("[data-element-picker]")) return;
      event.preventDefault();
      event.stopPropagation();
      setPicking(false);
      propose(aboutOf(target));
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      setPicking(false);
    };
    document.addEventListener("click", onClick, true);
    window.addEventListener("keydown", onKey, true);
    document.body.dataset.picking = "true";
    return () => {
      document.removeEventListener("click", onClick, true);
      window.removeEventListener("keydown", onKey, true);
      delete document.body.dataset.picking;
    };
  }, [picking, propose, setPicking]);
  if (!picking || !setPicking) return null;
  return (
    <div
      data-element-picker
      role="status"
      className="fixed top-15 left-1/2 z-60 flex -translate-x-1/2 items-center gap-2.5 rounded-lg bg-accent px-3 py-1.25 text-[12.5px] text-panel shadow-(--shadow-lift)"
    >
      Click a step, run or item to ask about it
      <button type="button" className="underline" onClick={() => setPicking(false)}>
        Cancel
      </button>
    </div>
  );
}
