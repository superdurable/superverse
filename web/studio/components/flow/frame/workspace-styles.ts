// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";

/**
 * The looks Runs, the Inbox and Connectors share: the list column beside a case panel, the case's
 * blocks and facts, and the Flow views' form controls.
 */

/**
 * The list column and the case or canvas beside it; the list's width is a CSS variable its handle drags.
 * The columns never stack: the panes flow into columns, so even without a column template (such as a
 * stylesheet older than the page's classes) they stay side by side.
 */
export const WORKSPACE_BODY = cx("relative grid h-full min-h-0 auto-cols-fr grid-flow-col");
export const LIST_BESIDE_CASE = cx("grid-cols-[var(--v2-list-w,250px)_minmax(0,1fr)]");

/** The case column beside the list when there is no canvas: it scrolls, and the case caps its width. */
export const CASE_COLUMN = cx("flex min-h-0 min-w-0 flex-col overflow-y-auto bg-panel dark:bg-transparent");

/** One selected thing's facts and forms; its host caps the width. */
export const CASE = cx("w-full shrink-0 px-4 pt-3 pb-4");

export const CASE_HEAD = cx("flex flex-wrap items-baseline gap-2 border-b border-line-faint pb-2.5");
export const CASE_TITLE = cx("m-0 min-w-0 truncate text-sm font-semibold");
export const CASE_STATUS = cx("text-xs text-ink-faint");

/** A titled section of a case. */
export const BLOCK = cx("mt-4");
export const BLOCK_HEAD = cx("mb-1.5 text-[11px] font-semibold tracking-wider text-ink-faint uppercase");

/** The small uppercase name of a zone, a search field or a fact. */
export const LABEL = cx("text-[10px] tracking-wider text-ink-faint uppercase");

/** Secondary text under a field or a heading. */
export const NOTE = cx("block text-[11px]/[1.45] text-ink-faint");
export const STATE = cx(
  "text-xs text-ink-faint data-[liveness=stranded]:text-fail data-[liveness=unreachable]:text-fail",
);
export const ERROR = cx("mb-2 text-xs text-fail");
export const EMPTY = cx("px-5 py-4.5 text-[13px]/normal text-ink-faint");

/** Label and value columns, so quantities line up for comparison; each FACT is one dt/dd row. */
export const FACTS = cx("grid grid-cols-[max-content_1fr] gap-x-3.5 gap-y-1");
export const FACT = cx("col-span-full grid grid-cols-subgrid");
export const FACT_TERM = cx("self-center text-[10px] tracking-wider text-ink-faint uppercase");
export const FACT_VALUE = cx("min-w-0 text-xs/[1.45] wrap-anywhere text-ink-dim");

/** A Step type or other identifier named in a case. */
export const STEP_NAME = cx("m-0 font-mono text-xs text-ink");
export const PURPOSE = cx("mt-0.5 text-xs/normal text-ink-dim");

/** A numbered or checked mark in a sequence of steps; `data-state` says where the sequence is. */
export const STEP_MARK = cx(
  "inline-flex size-[15px] flex-none items-center justify-center rounded-full border border-line-soft text-[9px] text-mark data-[state=current]:border-busy data-[state=current]:bg-busy data-[state=current]:text-bg data-[state=done]:border-pass data-[state=done]:text-pass",
);

/** The quiet ✕ that closes a case or a drawer. */
export const CLOSE = cx("flex-none px-1 text-[13px] leading-none text-mark hover:text-ink");
/** A quiet link out of the case, such as into Diagnostics. */
export const LINK_OUT = cx("ml-auto text-xs whitespace-nowrap text-ink-faint hover:text-ink-dim");
export const LINK = cx("wrap-anywhere text-accent hover:underline");

/** The list column's rows: lines, not boxes, so a row reads as an entry rather than a card. */
export const LIST_SCROLL = cx("min-h-0 flex-auto overflow-auto border-t border-line-soft pt-2");
export const LIST_ITEM = cx("group border-line-faint not-first:border-t");
export const LIST_ROW = cx(
  "grid w-full grid-cols-[minmax(0,1fr)_auto] gap-x-2 gap-y-px border-l-2 border-transparent px-2 py-1.75 text-left group-data-[selected=true]:border-accent group-data-[selected=true]:bg-raised hover:bg-hover focus-visible:shadow-(--focus-ring) focus-visible:outline-none",
);
/** The row's name; the caller colours it. */
export const LIST_ROW_TITLE = cx("min-w-0 truncate text-xs");
export const LIST_ROW_STATE = cx("col-span-full text-[11px] text-ink-faint");

const FIELD_LOOK = cx(
  "w-full min-w-0 rounded-md border border-line-soft bg-panel text-[13px] text-ink focus-visible:border-accent focus-visible:shadow-(--focus-ring) focus-visible:outline-none disabled:text-mark",
);
/** An input, select or text area in a Flow view's list or case. */
export const FIELD = cx(FIELD_LOOK, "px-2 py-1");
/** The same control with room to type in, for a dialog or a setup form. */
export const ROOMY_FIELD = cx(FIELD_LOOK, "px-2.25 py-1.75");
/** A form label names its input, so it reads as primary text, not muted metadata. */
export const FIELD_LABEL = cx("text-xs font-semibold text-ink-dim");

/** A radio or checkbox card: the chosen one reads like a selected run row, raised and ruled darker. */
export const CHOICE = cx(
  "grid cursor-pointer grid-cols-[auto_minmax(0,1fr)] gap-2 rounded-md border border-line-faint bg-panel px-2.5 py-2 text-xs text-ink-dim focus-within:shadow-(--focus-ring) hover:border-line-soft has-checked:border-ink-faint has-checked:bg-raised",
);
export const CHOICE_INPUT = cx("mt-0.5 accent-cta");
/** An informational note in a setup form; something to act on is a busy Banner instead. */
export const NOTICE = cx("rounded-md border border-line-faint px-2.5 py-2 text-xs/[1.45] text-ink-dim");
