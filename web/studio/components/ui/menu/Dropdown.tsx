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

/**
 * A trigger's colour. ok is green and only for a connection that works; none is red, nothing chosen where
 * something must be; warn is amber, chosen but needing someone; same is quiet, a value that follows another;
 * off is disabled; link reads as text.
 */
export type DropdownTone = "neutral" | "ok" | "none" | "warn" | "same" | "off" | "link";

/** One value to choose, with a quieter second line, a colour, and a tooltip that can say why it is disabled. */
interface DropdownOption<V extends string> {
  value: V;
  label: string;
  sub?: string;
  tone?: "ok" | "warn" | "none";
  title?: string;
  disabled?: boolean;
}

/** A row that runs `action` instead of choosing a value, such as "New connection…". */
interface DropdownAction {
  action: () => void;
  label: string;
  glyph?: GlyphName;
  sub?: string;
  title?: string;
  disabled?: boolean;
}

export type DropdownItem<V extends string> = DropdownOption<V> | DropdownAction | MenuHeading | MenuRule;

const SHAPE = {
  sm: cx("h-7.5 gap-1.75 rounded-[7px] border pr-1.75 pl-2.5 text-[12.5px] aria-expanded:shadow-(--focus-ring)"),
  md: cx("h-8.5 gap-2 rounded-lg border pr-2 pl-3 text-[13px] aria-expanded:shadow-(--focus-ring)"),
  link: cx("h-5.5 gap-1 rounded-sm px-0.5 text-[12px] hover:underline"),
};

const TONE = {
  neutral: cx("border-line-soft bg-panel font-normal text-ink hover:border-line aria-expanded:border-accent-rule"),
  ok: cx("border-pass-rule bg-pass-subtle font-medium text-pass"),
  none: cx("border-fail-rule bg-fail-subtle font-medium text-fail"),
  warn: cx("border-busy-rule bg-busy-subtle font-medium text-busy"),
  same: cx("border-transparent bg-transparent font-normal text-ink-faint hover:bg-hover"),
  off: cx("border-line-soft bg-transparent font-normal text-ink-faint"),
  link: cx("font-normal text-ink-dim"),
} satisfies Record<DropdownTone, string>;

const OPTION_TONE = { ok: cx("text-pass"), warn: cx("text-busy"), none: cx("text-fail") };

/** The tones whose glyphs keep the text colour, and those that carry an alert glyph. */
const QUIET_CHEVRON: readonly DropdownTone[] = ["neutral", "same", "off", "link"];
const ALERTS: readonly DropdownTone[] = ["none", "warn"];

type Row<V extends string> = { id: string; text: string; disabled?: boolean; item: DropdownOption<V> | DropdownAction };

/**
 * Every single choice in Studio: a button that shows what is chosen and opens the popover card as a
 * listbox, with a check on the chosen option. Reasons go in the menu `note` or an option's `title`, never
 * as lines under the control; `pending` puts a blue dot in the trigger, with its words as the tooltip, and
 * `display` shows what a chosen option resolves to in place of its label.
 * Its accessible name is `label`; `required` keeps a form from submitting while nothing is chosen.
 */
export function Dropdown<V extends string>({
  label,
  value,
  items,
  onChange,
  placeholder = "Choose…",
  tone = "neutral",
  note,
  pending,
  display,
  title,
  disabled = false,
  inline = false,
  size = "sm",
  align = "start",
  id,
  describedBy,
  required = false,
  ...data
}: {
  label: string;
  /** The chosen value; a value no option has, or null, shows `placeholder`. */
  value: V | null;
  items: readonly DropdownItem<V>[];
  onChange: (value: V) => void;
  placeholder?: string;
  tone?: DropdownTone;
  note?: string;
  pending?: string;
  /** What the trigger shows instead of the chosen option's label, such as the value a "same as" choice resolves to. */
  display?: string;
  /** The trigger's tooltip; the note when absent. */
  title?: string;
  disabled?: boolean;
  /** As wide as its text instead of its container. */
  inline?: boolean;
  size?: "sm" | "md";
  align?: "start" | "end";
  id?: string;
  describedBy?: string;
  required?: boolean;
} & { [attribute: `data-${string}`]: string | undefined }) {
  const anchor = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const baseId = useId();
  const listId = `${baseId}-list`;
  const entries = items.map((item, index): Row<V> | MenuHeading | MenuRule =>
    item === "separator" || isMenuHeading(item)
      ? item
      : { id: `${baseId}-${index}`, text: item.label, disabled: item.disabled, item },
  );
  const rows = entries.filter((entry): entry is Row<V> => typeof entry === "object" && "item" in entry);
  const chosen = rows.find((row) => "value" in row.item && row.item.value === value);
  const keys = useListboxKeys({
    anchorRef: anchor,
    triggerRef: trigger,
    cardRef: card,
    rows,
    selectedId: chosen?.id ?? null,
    onChoose: ({ item }) => {
      if ("action" in item) item.action();
      else if (item.value !== value) onChange(item.value);
    },
  });
  const shownTone: DropdownTone = disabled ? (tone === "link" ? "link" : "off") : tone;
  const description = [`${baseId}-value`, pending && `${baseId}-pending`, describedBy].filter(Boolean).join(" ");
  return (
    <span ref={anchor} className={cx("relative min-w-0", inline ? "inline-block max-w-full" : "block w-full")}>
      <button
        {...data}
        ref={trigger}
        id={id}
        type="button"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={keys.open}
        aria-controls={keys.open ? listId : undefined}
        aria-describedby={description}
        title={title ?? note}
        disabled={disabled}
        onClick={keys.toggle}
        onKeyDown={keys.onTriggerKeyDown}
        className={cx(
          "flex max-w-full min-w-0 items-center text-left whitespace-nowrap focus-visible:shadow-(--focus-ring) focus-visible:outline-none disabled:cursor-not-allowed",
          tone === "link" ? SHAPE.link : SHAPE[size],
          !inline && tone !== "link" && "w-full",
          TONE[shownTone],
        )}
      >
        {ALERTS.includes(shownTone) && <Glyph name="alert" className="-ml-0.5 size-3.5" />}
        {pending && (
          <span
            id={`${baseId}-pending`}
            role="img"
            aria-label={pending}
            title={pending}
            className="size-1.75 shrink-0 rounded-full bg-accent"
          />
        )}
        <span id={`${baseId}-value`} className="min-w-0 flex-1 truncate">
          {display ?? chosen?.item.label ?? placeholder}
        </span>
        {!disabled && (
          <Glyph name="chevronDown" className={cx("size-3.5", QUIET_CHEVRON.includes(shownTone) && "text-ink-faint")} />
        )}
      </button>
      {required && (
        <input
          aria-hidden="true"
          tabIndex={-1}
          required
          value={chosen ? (value ?? "") : ""}
          onChange={() => undefined}
          onFocus={() => trigger.current?.focus()}
          className="pointer-events-none absolute inset-0 size-full opacity-0"
        />
      )}
      {keys.open && (
        <PopoverCard
          anchorRef={anchor}
          cardRef={card}
          role="listbox"
          label={label}
          align={align}
          fitAnchor
          autoFocus
          maxHeight={340}
          id={listId}
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
            rule="none"
            renderRow={(row) => {
              const { item } = row;
              const selected = row === chosen;
              return (
                <div
                  key={row.id}
                  id={row.id}
                  role="option"
                  aria-selected={selected}
                  aria-disabled={item.disabled || undefined}
                  aria-labelledby={`${row.id}-label`}
                  aria-describedby={item.sub ? `${row.id}-sub` : undefined}
                  title={item.title}
                  data-active={row.id === keys.activeId}
                  onMouseEnter={() => !item.disabled && keys.setActiveId(row.id)}
                  onClick={() => keys.choose(row)}
                  className={cx(MENU_ROW, item.disabled && MENU_ROW_OFF)}
                >
                  {"action" in item && item.glyph && <Glyph name={item.glyph} />}
                  <RowText
                    labelId={`${row.id}-label`}
                    subId={`${row.id}-sub`}
                    label={item.label}
                    sub={item.sub}
                    className={"tone" in item && item.tone && !item.disabled ? OPTION_TONE[item.tone] : undefined}
                  />
                  <span className="ml-auto flex w-3.5 shrink-0 justify-end">
                    {selected && <Glyph name="check" className="size-3.5 text-accent" />}
                  </span>
                </div>
              );
            }}
          />
        </PopoverCard>
      )}
    </span>
  );
}
