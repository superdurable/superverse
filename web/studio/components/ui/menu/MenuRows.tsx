// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { POPOVER_ITEM } from "@/components/ui/Popover";

/** A heading that opens a group of rows. */
export type MenuHeading = { heading: string };
/** A rule between two groups of rows. */
export type MenuRule = "separator";

/** A row of a dropdown or a menu: the popover row, highlighted while the keys or the pointer are on it. */
export const MENU_ROW = cx(POPOVER_ITEM, "cursor-pointer data-[active=true]:bg-hover");
/** A row that cannot be chosen; its title says why. */
export const MENU_ROW_OFF = cx("cursor-not-allowed text-ink-faint hover:bg-transparent");

export const isMenuHeading = (item: unknown): item is MenuHeading =>
  typeof item === "object" && item !== null && "heading" in item;

/**
 * The note at the top of a card: why a choice reads as it does, in small dim text above a rule. The card
 * names it as its description, so it is hidden here and read once.
 */
export function MenuNote({ id, children }: { id: string; children: ReactNode }) {
  return (
    <p
      id={id}
      aria-hidden="true"
      className="mb-1 border-b border-line-faint px-2.25 pt-1.5 pb-2 text-[12px]/[1.4] whitespace-normal text-ink-dim"
    >
      {children}
    </p>
  );
}

/** A row's words: its label, in its tone, and a quieter second line. */
export function RowText({
  labelId,
  subId,
  label,
  sub,
  className,
}: {
  labelId: string;
  subId: string;
  label: string;
  sub?: string;
  className?: string;
}) {
  return (
    <span className="flex min-w-0 flex-1 flex-col items-start gap-px">
      <span id={labelId} className={cx("max-w-full wrap-anywhere", className)}>
        {label}
      </span>
      {sub && (
        <small id={subId} className="max-w-full text-[11.5px] wrap-anywhere text-ink-faint">
          {sub}
        </small>
      )}
    </span>
  );
}

/**
 * Rows split at headings and rules: each heading opens a group the heading names, and a rule draws a line.
 * A listbox may hold only options and groups, so its rules are presentational; a menu's are separators.
 */
export function MenuSections<R extends { id: string }>({
  items,
  baseId,
  rule,
  renderRow,
}: {
  items: readonly (R | MenuHeading | MenuRule)[];
  baseId: string;
  rule: "none" | "separator";
  renderRow: (row: R) => ReactNode;
}) {
  const sections: { heading: string | null; ruled: boolean; rows: R[] }[] = [{ heading: null, ruled: false, rows: [] }];
  for (const item of items) {
    const last = sections[sections.length - 1];
    if (item === "separator") sections.push({ heading: null, ruled: true, rows: [] });
    else if (isMenuHeading(item)) {
      if (last.rows.length === 0 && last.heading === null) last.heading = item.heading;
      else sections.push({ heading: item.heading, ruled: false, rows: [] });
    } else last.rows.push(item);
  }
  return sections
    .filter((section) => section.rows.length > 0)
    .map((section, index) => {
      const headingId = `${baseId}-g${index}`;
      return (
        <div
          key={headingId}
          role={section.heading ? "group" : "none"}
          aria-labelledby={section.heading ? headingId : undefined}
        >
          {section.ruled && index > 0 && <div role={rule} className="mx-0.5 my-1 border-t border-line-faint" />}
          {section.heading && (
            <div
              id={headingId}
              aria-hidden="true"
              className="px-2.25 pt-1.5 pb-0.5 text-[11px] tracking-[0.07em] text-ink-faint uppercase"
            >
              {section.heading}
            </div>
          )}
          {section.rows.map(renderRow)}
        </div>
      );
    });
}
