// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";

/**
 * One row of the sidebar: an icon or tile, then a label that hides while the sidebar is a rail. Rows
 * are 32px with 4px between them, as on every strip of the Builder (the Tools list, the folded chat
 * and lists), so their icons line up across the strips.
 */
export const SIDEBAR_ROW = cx(
  "relative mx-2 flex h-8 w-[calc(100%-1rem)] min-w-0 items-center gap-2.5 rounded-[7px] px-2.5 text-left text-sm whitespace-nowrap text-ink-dim hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none",
);

/** The row of the page that is open. */
export const SIDEBAR_ROW_ON = cx("bg-panel text-ink shadow-[0_0_0_1px_var(--color-line-soft)] hover:bg-panel");

/**
 * A label inside the rail: hidden until the rail opens over the page, which it does after a short
 * hover delay or when focus enters it.
 */
export const RAIL_LABEL = cx(
  "opacity-0 transition-opacity duration-100 group-focus-within/sidebar:opacity-100 group-hover/sidebar:opacity-100 group-hover/sidebar:delay-300 motion-reduce:transition-none",
);

/** A section heading of the sidebar (Projects, Team): small uppercase faint text. */
export const SIDEBAR_HEADING = cx(
  "px-5 pt-3 pb-1 text-[11px] font-normal tracking-[0.07em] whitespace-nowrap text-ink-faint uppercase",
);

/** In the rail a section heading becomes this rule, until the rail opens over the page. */
export const SIDEBAR_HEADING_RULE = cx(
  "absolute inset-x-3.5 top-[60%] border-t border-line-soft transition-opacity group-focus-within/sidebar:opacity-0 group-hover/sidebar:opacity-0 group-hover/sidebar:delay-300",
);
