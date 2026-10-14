// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useId, useRef } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import { PopoverCard } from "@/components/ui/Popover";
import {
  isMenuHeading,
  MENU_ROW,
  MENU_ROW_OFF,
  MenuNote,
  MenuSections,
  RowText,
  type MenuHeading,
  type MenuRule,
} from "./MenuRows";
import { useListboxKeys } from "./use-listbox-keys";

/** One action: a glyph, a label with a quieter second line, a word at the end, and a tooltip for why it is off. */
interface ActionMenuItem {
  label: string;
  onSelect: () => void;
  glyph?: GlyphName;
  sub?: string;
  /** A short word at the row's end, such as "open". */
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  title?: string;
}

export type ActionMenuEntry = ActionMenuItem | MenuHeading | MenuRule;

type Row = { id: string; text: string; disabled?: boolean; item: ActionMenuItem };

const ICON_TRIGGER = cx(
  "grid size-6.5 shrink-0 place-items-center rounded-md text-ink-faint hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none aria-expanded:bg-hover aria-expanded:text-ink",
);

/**
 * A row's actions behind one icon button, ⋮ unless `glyph` says otherwise: the dropdown's card and rows as
 * a menu, without a check. A danger action reads red; `triggerClassName` replaces the icon button's look
 * where a bar draws its own.
 */
export function ActionMenu({
  label,
  items,
  glyph = "more",
  glyphClassName,
  triggerClassName,
  note,
  align = "end",
}: {
  /** The button's accessible name and tooltip, and the menu's name. */
  label: string;
  items: readonly ActionMenuEntry[];
  glyph?: GlyphName;
  glyphClassName?: string;
  triggerClassName?: string;
  note?: string;
  align?: "start" | "end";
}) {
  const anchor = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const baseId = useId();
  const entries = items.map((item, index): Row | MenuHeading | MenuRule =>
    item === "separator" || isMenuHeading(item)
      ? item
      : { id: `${baseId}-${index}`, text: item.label, disabled: item.disabled, item },
  );
  const rows = entries.filter((entry): entry is Row => typeof entry === "object" && "item" in entry);
  const keys = useListboxKeys({
    anchorRef: anchor,
    triggerRef: trigger,
    cardRef: card,
    rows,
    onChoose: ({ item }) => item.onSelect(),
  });
  return (
    <span ref={anchor} className="relative inline-flex shrink-0 items-stretch">
      <button
        ref={trigger}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={keys.open}
        aria-controls={keys.open ? `${baseId}-menu` : undefined}
        onClick={keys.toggle}
        onKeyDown={keys.onTriggerKeyDown}
        className={triggerClassName ?? ICON_TRIGGER}
      >
        <Glyph name={glyph} className={glyphClassName} />
      </button>
      {keys.open && (
        <PopoverCard
          anchorRef={anchor}
          cardRef={card}
          role="menu"
          label={label}
          align={align}
          autoFocus
          id={`${baseId}-menu`}
          tabIndex={-1}
          aria-activedescendant={keys.activeId ?? undefined}
          aria-describedby={note ? `${baseId}-note` : undefined}
          onKeyDown={keys.onCardKeyDown}
          onFocus={keys.onCardFocus}
        >
          {note && <MenuNote id={`${baseId}-note`}>{note}</MenuNote>}
          <MenuSections
            items={entries}
            baseId={baseId}
            rule="separator"
            renderRow={(row) => {
              const { item } = row;
              return (
                <div
                  key={row.id}
                  id={row.id}
                  role="menuitem"
                  aria-disabled={item.disabled || undefined}
                  aria-labelledby={`${row.id}-label`}
                  aria-describedby={item.sub ? `${row.id}-sub` : undefined}
                  title={item.title}
                  data-active={row.id === keys.activeId}
                  onMouseEnter={() => !item.disabled && keys.setActiveId(row.id)}
                  onClick={() => keys.choose(row)}
                  className={cx(MENU_ROW, item.danger && !item.disabled && "text-fail", item.disabled && MENU_ROW_OFF)}
                >
                  {item.glyph && <Glyph name={item.glyph} />}
                  <RowText labelId={`${row.id}-label`} subId={`${row.id}-sub`} label={item.label} sub={item.sub} />
                  {item.hint && <span className="ml-auto shrink-0 text-xs text-ink-faint">{item.hint}</span>}
                </div>
              );
            }}
          />
        </PopoverCard>
      )}
    </span>
  );
}
