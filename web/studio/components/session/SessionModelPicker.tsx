// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useId, useRef } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { PopoverCard, usePopover } from "@/components/ui/Popover";
import { AGENT_MODEL_OPTIONS, agentModelLabel, type AgentModel } from "@/lib/session/agent-types";

type SessionModelPickerProps = Parameters<typeof SessionModelPicker>[0];

/**
 * The model of a Session: a selector for the model its next turn runs on, the model and effort its
 * current context runs on, and a note while the two differ. Changing the model is allowed at any
 * time; the Agent takes it up when its next turn starts. The mock model is listed only where the
 * server lets this account select it; the server still authorizes every change.
 *
 * The Session shows it in the ⋯ menu on its composer's row (`SessionModelMenu`), and the creation
 * page shows it for a Session that does not exist yet, which applies the chosen model once it starts.
 */
export function SessionModelPicker({
  model,
  contextModel = null,
  effort = "",
  mockSelectable,
  disabled,
  onSelect,
  className = "border-t border-line",
}: {
  /** The selected model, or null before the Agent has one or when none was chosen. */
  model: AgentModel | null;
  /** The model the current context segment runs on, or null before its first turn. */
  contextModel?: AgentModel | null;
  /** The effort of the current context, empty when unknown or not applicable. */
  effort?: string;
  /** Whether ListMockScenarios lets this account select the mock model here. */
  mockSelectable: boolean;
  disabled: boolean;
  onSelect: (model: AgentModel) => void;
  /** The frame around the picker, such as the creation page's border. */
  className?: string;
}) {
  const selectId = useId();
  const noteId = useId();
  const options = AGENT_MODEL_OPTIONS.filter(({ value }) => value !== "mock" || mockSelectable || model === "mock");
  const current = contextModel ?? model;
  const switchPending = model !== null && contextModel !== null && model !== contextModel;
  return (
    <div className={cx("shrink-0 bg-panel px-3 py-2 text-[13px] text-ink-faint", className)}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="flex min-w-0 items-center gap-2">
          <label className="font-medium text-ink-dim" htmlFor={selectId}>
            Model
          </label>
          <Dropdown
            id={selectId}
            label="Model"
            inline
            describedBy={switchPending ? noteId : undefined}
            disabled={disabled}
            value={model}
            placeholder="Not selected yet"
            onChange={onSelect}
            items={options.map((option) => ({ value: option.value, label: option.label, sub: option.description }))}
          />
        </div>
        {current === null ? null : (
          <p data-testid="session-model-line">
            {agentModelLabel(current)}
            {effort ? `, ${effort} effort` : ""}
          </p>
        )}
      </div>
      {switchPending ? (
        <p className="mt-1" id={noteId}>
          Takes effect at the start of the next turn
        </p>
      ) : null}
    </div>
  );
}

/**
 * The ⋯ menu on the Session composer's row, which holds the model picker so the conversation keeps its
 * room. Its tooltip names the model of the next turn, and a blue dot marks a change the next turn
 * takes up.
 */
export function SessionModelMenu(props: Omit<SessionModelPickerProps, "className">) {
  const anchor = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const popover = usePopover(anchor);
  const wasOpen = useRef(false);
  // Escape or a press outside closes the card; the focus it held returns to the trigger.
  useEffect(() => {
    if (wasOpen.current && !popover.open && document.activeElement === document.body) trigger.current?.focus();
    wasOpen.current = popover.open;
  }, [popover.open]);
  const { model, contextModel = null } = props;
  const switchPending = model !== null && contextModel !== null && model !== contextModel;
  const title = model === null ? "Chat options" : `Chat options · ${agentModelLabel(model)}`;
  return (
    <span ref={anchor} className="relative inline-flex shrink-0">
      <button
        ref={trigger}
        type="button"
        aria-label="Chat options"
        title={title}
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        onClick={popover.toggle}
        className="relative grid size-7 place-items-center rounded-[7px] text-ink-faint hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none aria-expanded:bg-hover aria-expanded:text-ink"
      >
        <Glyph name="more" className="rotate-90" />
        {switchPending && (
          <span aria-hidden="true" className="absolute top-1 right-1 size-1.5 rounded-full bg-accent" />
        )}
      </button>
      {popover.open && (
        // A change the next turn takes up adds a note; placing the card again keeps the trigger in view.
        <PopoverCard
          key={String(switchPending)}
          anchorRef={anchor}
          label="Chat options"
          align="end"
          autoFocus
          tabIndex={-1}
          className="w-80"
        >
          <SessionModelPicker {...props} className="rounded-md" />
        </PopoverCard>
      )}
    </span>
  );
}
