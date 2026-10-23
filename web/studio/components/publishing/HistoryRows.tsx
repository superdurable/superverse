// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { formatMonthDayTime, formatTimeAgo } from "@/lib/shared/format";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

const LINE = "flex items-baseline gap-2.5 px-3 py-2.25 text-[12.5px]";

/** When, as an age, with the date and time on hover. */
function Age({ at, now }: { at: string | null; now: number }) {
  return (
    <time
      className="w-19 shrink-0 text-ink-faint"
      dateTime={at ?? undefined}
      title={at ? formatMonthDayTime(at) : undefined}
    >
      {formatTimeAgo(at, now)}
    </time>
  );
}

/**
 * One line of a Publishing history list: when, then what happened, which a narrow line cuts short. With `trailing`,
 * such as a status badge, what happened wraps instead, whole, and the trailing part, which is never cut, moves under it
 * when both do not fit. With `detail`, the line opens the rest underneath it; the rest stays in the page while folded.
 * With `open`, the caller holds whether it is open, such as for a row a link opens, and `onToggle` hears each opening
 * and folding; `anchor` names the line, which a link to the row lands on; `wide` gives a rest with a layout of its own
 * the row's full width.
 */
export function HistoryRow({
  at,
  now,
  detail,
  open,
  onToggle,
  anchor,
  wide = false,
  trailing,
  children,
}: {
  at: string | null;
  now: number;
  detail?: ReactNode;
  open?: boolean;
  onToggle?: (open: boolean) => void;
  anchor?: string;
  wide?: boolean;
  trailing?: ReactNode;
  children: ReactNode;
}) {
  const what = trailing ? (
    <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-1">
      <span className="max-w-full min-w-0 wrap-anywhere">{children}</span>
      <span className="shrink-0">{trailing}</span>
    </span>
  ) : (
    <span className="min-w-0 flex-1 truncate">{children}</span>
  );
  if (!detail)
    return (
      <div id={anchor} className={LINE}>
        <Age at={at} now={now} />
        {what}
        <span className="w-3.5 shrink-0" />
      </div>
    );
  return (
    <details className="group" open={open} onToggle={(event) => onToggle?.(event.currentTarget.open)}>
      <summary
        id={anchor}
        className={cx(LINE, "cursor-pointer list-none hover:bg-hover [&::-webkit-details-marker]:hidden")}
      >
        <Age at={at} now={now} />
        {what}
        <Glyph
          name="chevronRight"
          className="size-3.5 shrink-0 self-center text-ink-faint transition-transform group-open:rotate-90"
        />
      </summary>
      <div
        className={cx(
          "grid gap-0.5 pr-3 pb-2.5",
          wide ? "pt-1 pl-3 text-caption text-ink-dim" : "pl-24.5 text-[11.5px] text-ink-faint",
        )}
      >
        {detail}
      </div>
    </details>
  );
}

/** A Publishing history list: its heading, with an action beside it, over a box of one-line rows, newest first. */
export function HistoryList({
  label,
  heading,
  action,
  testId,
  children,
  footer,
}: {
  label: string;
  heading: string;
  action?: ReactNode;
  testId?: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <section aria-label={label} data-testid={testId}>
      <div className="mb-2 flex min-h-6 items-center gap-2">
        <h3 className="text-[11px] font-normal tracking-[0.07em] text-ink-faint uppercase">{heading}</h3>
        {action && <span className="ml-auto">{action}</span>}
      </div>
      <div className="overflow-hidden rounded-[10px] border border-line-soft bg-panel">{children}</div>
      {footer}
    </section>
  );
}
