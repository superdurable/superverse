// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState, type RefObject } from "react";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";

/** How far from an end the reader may be and still count as at it, in pixels. */
const NEAR = 80;

function JumpButton({
  glyph,
  label,
  disabled = false,
  onClick,
}: {
  glyph: GlyphName;
  label: string;
  disabled?: boolean;
  onClick: () => unknown;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={() => void onClick()}
      className="pointer-events-auto grid size-8 place-items-center rounded-full border border-line-soft bg-panel text-ink-dim shadow-(--shadow-lift) hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none disabled:cursor-default disabled:text-ink-faint"
    >
      <Glyph name={glyph} />
    </button>
  );
}

/**
 * The chat history's jumps, floating at the bottom of its scroll once the reader scrolls away from the latest
 * message, as chat apps place them: to the beginning, which loads every earlier message first, and back to the
 * latest. Scrolling up loads earlier messages on its own, so there is no Load earlier.
 */
export function HistoryJumps({
  scroller,
  earlier,
  loading,
  onBeginning,
  onLatest,
}: {
  /** The history's scroll container, which the jumps float over. */
  scroller: RefObject<HTMLDivElement | null>;
  /** Earlier messages are not loaded yet. */
  earlier: boolean;
  loading: boolean;
  onBeginning: () => unknown;
  onLatest: () => unknown;
}) {
  const [place, setPlace] = useState({ away: false, atBeginning: true });
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const read = () => {
      const away = element.scrollHeight - element.scrollTop - element.clientHeight >= NEAR;
      const atBeginning = !earlier && element.scrollTop < NEAR;
      setPlace((current) =>
        current.away === away && current.atBeginning === atBeginning ? current : { away, atBeginning },
      );
    };
    read();
    element.addEventListener("scroll", read, { passive: true });
    return () => element.removeEventListener("scroll", read);
  }, [earlier, scroller]);
  if (!place.away) return null;
  return (
    <div
      className="pointer-events-none absolute right-3 bottom-3 z-10 flex flex-col gap-1.5"
      role="group"
      aria-label="Jump in the chat"
    >
      {!place.atBeginning && (
        <JumpButton glyph="arrowUp" label="Jump to beginning" disabled={loading} onClick={onBeginning} />
      )}
      <JumpButton glyph="arrowDown" label="Jump to latest" onClick={onLatest} />
    </div>
  );
}
