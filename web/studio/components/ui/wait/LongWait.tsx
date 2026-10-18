// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState, type ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { Spinner } from "@/components/ui/Icons";

/** How long a wait runs before its running mark shows, so a quick load never flashes it. */
const LONG_WAIT_MS = 3_000;
/** A mark of a chain that has just gone hands the animation on: the chain's next mark within this long shows at once. */
const HANDOFF_MS = 1_500;

/**
 * The dex-ui `logo/pipe-d` running icon in `public/brand`: the chevron handing on, on the forest tile, and its
 * first frame for reduced motion. The tile is opaque and the same in both themes, so it reads as a deliberate
 * tile on every ground (panel, canvas, page or well) instead of a near-match square.
 */
const BRAND = {
  running: "/brand/dex-running-icon-128.gif",
  still: "/brand/dex-running-icon-still-128.png",
};

/**
 * The mark's box by where it sits: beside a line of text (no taller than the line), at the head of a card, or
 * alone on a page or canvas. The corner follows the tile's own radius at every size.
 */
const SIZES = {
  line: { className: cx("size-5"), px: 20 },
  panel: { className: cx("size-12"), px: 48 },
  page: { className: cx("size-16"), px: 64 },
} satisfies Record<string, { className: string; px: number }>;

export type RunningMarkSize = keyof typeof SIZES;

/**
 * One wait that moves between surfaces, whose marks hand the animation on. Only marks of the same chain hand
 * off: a mark of any other wait always waits out its own delay, so it never flashes because something else runs.
 */
export type RunningMarkChain = "startup";

// Per chain, the marks showing now and when the last one went.
const chains = new Map<RunningMarkChain, { showing: number; lastGone: number }>();

/** Not yet; shown at once (no delay, or taking over within its chain); or shown late, fading in. */
type Reveal = "waiting" | "shown" | "late";

/**
 * The Dex mark running, for a wait that takes long: nothing for its first seconds, then the running icon GIF,
 * and its still frame under reduced motion. It is decorative: the words beside it carry the wait. The image is
 * fetched as the wait starts and fades in once it has loaded, so the box never shows an empty tile. `still`
 * shows the still frame at once, for a wait that has stopped on the person but keeps its place.
 */
export function RunningMark({
  size,
  delay = LONG_WAIT_MS,
  still = false,
  chain,
  pending = size === "line" ? <Spinner className="h-3.5 w-3.5" /> : undefined,
  className,
}: {
  size: RunningMarkSize;
  /** How long before it shows, in milliseconds; 0 shows it at once, for a wait already known to be long. */
  delay?: number;
  /** Shows the still mark at once instead of running. */
  still?: boolean;
  /** The wait this mark belongs to when that wait moves between surfaces; see `RunningMarkChain`. */
  chain?: RunningMarkChain;
  /** What the box holds until the mark shows; beside a line, the spinner the mark takes over from. */
  pending?: ReactNode;
  className?: string;
}) {
  const [reveal, setReveal] = useState<Reveal>(() => (still || delay <= 0 ? "shown" : "waiting"));
  const [loaded, setLoaded] = useState(false);
  const visible = reveal !== "waiting";
  // Fetch the image while the wait runs, so it is ready when the mark shows.
  useEffect(() => {
    const image = new Image();
    image.src = window.matchMedia("(prefers-reduced-motion: reduce)").matches ? BRAND.still : BRAND.running;
  }, []);
  useEffect(() => {
    if (visible) return;
    const link = chain && chains.get(chain);
    // A mark of the same chain on screen, or one that has just gone, means this wait goes on here.
    const handoff = Boolean(link && (link.showing > 0 || Date.now() - link.lastGone < HANDOFF_MS));
    const timer = window.setTimeout(() => setReveal(handoff ? "shown" : "late"), handoff ? 0 : delay);
    return () => window.clearTimeout(timer);
  }, [chain, delay, visible]);
  useEffect(() => {
    if (!visible || still || !chain) return;
    const link = chains.get(chain) ?? { showing: 0, lastGone: 0 };
    chains.set(chain, link);
    link.showing += 1;
    return () => {
      link.showing -= 1;
      link.lastGone = Date.now();
    };
  }, [chain, still, visible]);
  const { className: box, px } = SIZES[size];
  const shown = still || visible;
  const image = (src: string) => (
    <img
      alt=""
      className="block size-full"
      decoding="async"
      height={px}
      onLoad={() => setLoaded(true)}
      ref={(element) => {
        if (element?.complete && element.naturalWidth > 0) setLoaded(true);
      }}
      src={src}
      width={px}
    />
  );
  return (
    <span
      aria-hidden="true"
      className={cx("relative grid shrink-0 place-items-center overflow-hidden rounded-[22%]", box, className)}
      data-running-mark={still ? "still" : reveal}
    >
      {shown ? (
        <span
          className={cx(
            "absolute inset-0",
            !loaded ? "opacity-0" : reveal === "late" && !still && "animate-fade-in-late motion-reduce:animate-none",
          )}
        >
          {still ? (
            image(BRAND.still)
          ) : (
            <picture>
              <source media="(prefers-reduced-motion: reduce)" srcSet={BRAND.still} />
              {image(BRAND.running)}
            </picture>
          )}
        </span>
      ) : (
        pending
      )}
    </span>
  );
}

/**
 * A wait told in words with the running mark: a polite status that holds the words at once and the mark once
 * the wait has run long, beside the words (`row`) or above them (`stack`).
 */
export function LongWait({
  size,
  layout = "row",
  delay,
  pending,
  className,
  children,
}: {
  size: RunningMarkSize;
  layout?: "row" | "stack";
  delay?: number;
  pending?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      aria-live="polite"
      className={cx(
        layout === "row" ? "flex items-center gap-3" : "flex flex-col items-center gap-3 text-center",
        className,
      )}
      role="status"
    >
      <RunningMark delay={delay} pending={pending} size={size} />
      <div className="min-w-0">{children}</div>
    </div>
  );
}
