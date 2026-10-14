// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { cx } from "@/components/ui/cx";

/** The blocks event details are built from: record stacks, records, subsections, wells and alerts. */

/** plain lists a value, decision marks what a Step decided, entry nests a Map instance in its map. */
export type RecordTone = "plain" | "decision" | "entry";

const RECORD_TONE = {
  plain: cx("border-line-soft bg-panel"),
  decision: cx("border-pass-rule/50 bg-pass-subtle"),
  entry: cx("border-line-faint bg-raised"),
} satisfies Record<RecordTone, string>;

const RECORD = cx("min-w-0 rounded-md border p-2 text-ink-dim");

/** A pre or block code value: bounded by its caller's max height, scrolled within. */
export const WELL = cx(
  "m-0 block overflow-auto rounded-md border border-line-faint bg-raised p-2 font-mono text-[10px]/[1.5] wrap-anywhere whitespace-pre-wrap text-ink-dim",
);

/** A failure inside a detail section. */
export const ALERT = cx(
  "flex flex-col gap-1.5 rounded-md border border-fail-rule/50 bg-fail-subtle p-2 text-[10px] text-fail",
);

/** A column of records. */
export function Records({ children }: { children: ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-1.5">{children}</div>;
}

/** One record: its title first, then its fields and values. */
export function RecordCard({ tone = "plain", children }: { tone?: RecordTone; children: ReactNode }) {
  return <div className={cx(RECORD, "flex flex-col gap-1.5", RECORD_TONE[tone])}>{children}</div>;
}

/** A record that shows only its title until it is opened. */
export function RecordDisclosure({
  title,
  tone = "plain",
  children,
}: {
  title: ReactNode;
  tone?: RecordTone;
  children: ReactNode;
}) {
  return (
    // The arrow follows this record's own details: a group-open variant would also match an open
    // record this one is nested in.
    <details className={cx(RECORD, RECORD_TONE[tone])}>
      <summary className="flex cursor-pointer list-none items-center gap-1.25 text-[10px] before:text-ink-faint before:transition-transform before:content-['▸'] [&::-webkit-details-marker]:hidden [[open]>&]:before:rotate-90">
        {title}
      </summary>
      <div className="mt-2 flex flex-col gap-1.5">{children}</div>
    </details>
  );
}

/** A record's title; a Channel record carries the Channel glyph in the waiting hue. */
export function RecordTitle({ channel = false, children }: { channel?: boolean; children: ReactNode }) {
  return (
    <strong className={cx("flex min-w-0 items-center gap-1 text-[10px] wrap-anywhere", channel && "text-busy")}>
      {channel && <ConditionIcon kind="channel" className="size-3.25 shrink-0" />}
      {children}
    </strong>
  );
}

/** A titled group inside a detail section. */
export function Subsection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <h5 className="text-[10px] text-ink-faint">{title}</h5>
      {children}
    </div>
  );
}

/** The glyph for a WaitFor condition kind; the caller sizes it. */
export function ConditionIcon({ kind, className }: { kind: "timer" | "channel" | "subflow"; className: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth={1.4}
      viewBox="0 0 16 16"
    >
      {kind === "timer" && (
        <>
          <circle cx="8" cy="8" r="5.5" />
          <path d="M8 4.8v3.5l2.4 1.4" />
        </>
      )}
      {kind === "channel" && (
        <>
          <circle cx="4" cy="4" r="1.7" />
          <circle cx="12" cy="8" r="1.7" />
          <circle cx="4" cy="12" r="1.7" />
          <path d="M5.7 4.7 10.3 7M5.7 11.3 10.3 9" />
        </>
      )}
      {kind === "subflow" && (
        <>
          <rect x="2" y="3" width="5" height="4" rx="1" />
          <rect x="9" y="9" width="5" height="4" rx="1" />
          <path d="M7 5h2.5a2 2 0 0 1 2 2v2" />
        </>
      )}
    </svg>
  );
}
